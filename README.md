# validating 用于验证pod是否符合标准
## 1.resource资源（保证资源合理利用）
## 2.探针设置（保证发布可用）
## 3.调度策略（禁止白嫖）
## 4.名称规范（必须以项目标识开头）
## 5.Pod SecurityContext 部分字段检测
# mutating webhook 用于patch SecurityContext 部分字段并添加annotation标明已被webhook修改过

# 流程

- 其实编写一个Admission Webhook也就是编写一个web服务器，用于拦截接收ApiServer发送的webhook请求，进行处理判断之后再返回响应给ApiServer，如何告知ApiServer有这么一个准入控制插件呢？
- 那就是通过声明一个首先声明一个`ValidatingWebhookConfiguration/MutatingWebhookConfiguration`


- 简单梳理下编写一个Admission Webhook需要做的事情：
    1. 首先需要确认集群apiserver是否启用了MutatingAdmissionWebhook和ValidatingAdmissionWebhook这两个控制器

        ```go
          - command:
            - kube-apiserver
              ......
            - --enable-admission-plugins=NodeRestriction,MutatingAdmissionWebhook,ValidatingAdmissionWebhook
        
        # 运行该命令检查集群中是否启用了准入注册 API
        $ kubectl api-versions |grep admission
        admissionregistration.k8s.io/v1beta1
        ```

    2. 为了保证api-server和admission webhook服务之间的数据完整性和安全性，所以在通信时需要用到TLS，众所周知CA是负责签发和管理证书的实体，如果使用openssl命令生成一套证书需要将其加入到apiserver的信任列表；不如直接使用cert-manager来的方便；
    3. 所以这里选择使用Cert-Manager，创建cert-manager的CRD资源 Issuer，其代表Namespace级别的证书颁发机构

        ```yaml
        apiVersion: cert-manager.io/v1
        kind: Issuer
        metadata:
          name: validating-application-standards-selfsigned-issuer
          namespace: ops-admission-webhook
        spec:
          selfSigned: {}
        ```

    4. 创建Certificates用来描述请求的证书信息，记录了要请求哪个 Issuer 来使用何种方式签发需要的证书

        ```yaml
        apiVersion: cert-manager.io/v1
        kind: Certificate
        metadata:
          name: validating-application-standards-tls-secret
          namespace: ops-admission-webhook
        spec:
          duration: 8760h
          renewBefore: 8000h
          subject:
            organizations:
              - XXXXX.com
          commonName: validating-application-standards.ops-admission-webhook
          isCA: false
          privateKey:
            algorithm: RSA
            encoding: PKCS1
            size: 2048
          usages:
            - digital signature
            - key encipherment
            - server auth
          dnsNames:
            - validating-application-standards
            - validating-application-standards.ops-admission-webhook
            - validating-application-standards.ops-admission-webhook.svc
          #ipAddresses:
          #  - 10.43.125.113 # change it to your IP addresses
          issuerRef:
            kind: Issuer
            name: validating-application-standards-selfsigned-issuer
          secretName: validating-application-standards-tls-secret
        ```

        - **`CA Injector 是 cert-manager 的一个组件，它可以将CA信息写入到ValidatingWebhookConfiguration或MutatingWebhookConfiguration的CA Bundle字段。`**
        - **`要使用CA Injector，需要在webhook的yaml中添加 annotation: cert-manager.io/inject-ca-from。告诉CA Injector，要将哪一个证书写到CA Bundle中。`**
    5. 然后来编写我们的admission webhook插件的代码，首先创建go代码工作目录，编写admission webhook的处理逻辑

        ```yaml
        webhook段代码的处理逻辑其实特别简单，按照先后顺序：
        1、因为apiserver的请求和响应都是Content-Type: application/json类型的，所以首先需要判断请求的类型
        2、然后声明两个变量来实例化AdmissionReview对象（*AdmissionReview对象的数据格式是结构体，包含了apiserver发送过来的所有参数*），分别用来接收apiserver发送过来的参数、以及发送返回给apiserver的响应
        3、然后将apiserver发送来的参数解码并反序列化到刚才声明的变量中
        4、然后写一个判断逻辑用以判断Deployment是否设置了SecurityContext的*allowPrivilegeEscalation、privileged两个字段*
        5、随后根据判断结果设置返回给apiserver的响应，是允许还是拒绝等等
        6、最后将响应序列化并写入请求体中即可
        除了第4步的判断逻辑需要根据自己的需求来编写，其余部分基本都是高度一致的代码。
        ```

    6. 接下来是main函数

        ```yaml
        main函数的主要作用就是使用tls证书启动一个http监听，将apiserver的请求转发给函数webhook.go
        其中flag.StringVar()指定了程序启动要使用的tls证书，路径可自定义，回头创建admission webhook的容器时指定的启动命令与其保持一致即可。
        ```

    7. 代码写完之后就该考虑部署了，首先准备一个构建的Dockerfile

        ```docker
        # Dockerfile
        FROM  golang:1.18-bullseye as builder
        ARG jarvan
        WORKDIR /workspace
        ENV GOPROXY=https://goproxy.cn,direct
        
        # COPY go.mod go.mod
        # COPY go.sum go.sum
        # COPY main.go main.go
        # COPY pkg/ pkg/
        COPY ./ /workspace/
        
        #RUN --mount=type=cache,target=/go/pkg/mod \
          #--mount=type=cache,target=/root/.cache/go-build go mod download
        
        RUN CGO_ENABLED=0 GOOS=linux  go build -o security-admission-webhook main.go
        RUN ls -lah /workspace/
        
        # 使用私人镜像，根据需要可以使用公共的alpine镜像
        FROM harbor-sh.xxxxx.com/poker-public/alpine:3.14
        RUN mkdir /webhook
        WORKDIR /webhook
        COPY --from=builder /workspace/security-admission-webhook /webhook/security-admission-webhook
        RUN chmod -R 777 /webhook/ && ls -lah /webhook/
        # 可以设置ENTRYPOINT,也可以不设置，在Deployment中设置command和args
        ```

    8. 构建镜像

        ```docker
        variables:
          PROJECT_NAME: resource-validating
        
        stages:
          - build
          # - deploy
        
        编译beta镜像:
          stage: build
          image:
            name: harbor-sh.XXXXX.com/poker-public/golang:1.20.1-centos7.9-graphviz
          script:
            - docker login --username=$HARBOR_PUSH_USER $HARBOR_SH_ADDR -p $HARBOR_SH_USER_PASSWD
            - docker build -t harbor-sh.pocketcity.com/ops/${PROJECT_NAME}:merge-security-0.0.1 .
            - docker push harbor-sh.pocketcity.com/ops/${PROJECT_NAME}:merge-security-0.0.1
            - docker rmi harbor-sh.pocketcity.com/ops/${PROJECT_NAME}:merge-security-0.0.1   
        ```

    9. 准备Webhook的YAML资源清单

        ```yaml
        apiVersion: v1
        kind: Namespace
        metadata:
          name: ops-admission-webhook
        ---
        apiVersion: apps/v1
        kind: Deployment
        metadata:
          labels:
            app: validating-application-standards
          name: validating-application-standards
          namespace: ops-admission-webhook
        spec:
          replicas: 1
          selector:
            matchLabels:
              app: validating-application-standards
          template:
            metadata:
              labels:
                app: validating-application-standards
            spec:
              imagePullSecrets:
                - name: harbor-sh
              containers:
                - image: harbor-sh.XXXXX.com/ops/resource-validating:master-5d448c9a
                  env:
                    - name: CERT_DIR
                      value: "/etc/validating-application-standards-webhook/certs"
                    - name: TZ
                      value: Asia/Shanghai
                  name: validating-application-standards
                  ports:
                    - containerPort: 8000
                      protocol: TCP
                  volumeMounts:
                    - mountPath: /etc/validating-application-standards-webhook/certs/
                      name: tls-cert
              ## cert-manager在该名称空间生成的ca/tls证书的secret
              volumes:
                - name: tls-cert
                  secret:
                    secretName: validating-application-standards-tls-secret
        ---
        apiVersion: v1
        kind: Service
        metadata:
          labels:
            app: validating-application-standards
          name: validating-application-standards
          namespace: ops-admission-webhook
        spec:
          ports:
            - name: https
              port: 443
              protocol: TCP
              targetPort: 8000
          selector:
            app: validating-application-standards
          type: ClusterIP
        ```

  10. 待程序的deployment和service都部署完成之后，就可以声明ValidatingWebhookConfiguration即时配置准入 Webhook来动态配置哪些资源要被哪些准入 Webhook 处理，具体字段配置含义可参考[k8s动态准入Webhook配置](https://kubernetes.io/zh-cn/docs/reference/access-authn-authz/extensible-admission-controllers/#webhook-configuration)

    ```yaml
    apiVersion: admissionregistration.k8s.io/v1
    kind: ValidatingWebhookConfiguration
    metadata:
      name: validating-application-standards
      annotations:
        # 此处和cert-manager证书创建的Secret名称对应
        cert-manager.io/inject-ca-from: ops-admission-webhook/validating-application-standards-tls-secret
    webhooks:
      - admissionReviewVersions:
          - v1
        clientConfig:
          caBundle: ""                                ## caBundle是一个PEM编码的 CA 包,将用于验证 webhook 的服务器证书，这里将由cert-manager自动插入
          service:                                    
            name: validating-application-standards    ## 名称可自定义
            namespace: ops-admission-webhook          ## webhook支持的admissionReview版本
            port: 443
            path: /validate
        failurePolicy: Fail
        matchPolicy: Exact
        name: validating-application-standards.XXXXX.com
        rules:
          - apiGroups:
              - ""
            apiVersions:
              - v1
            operations:
              - CREATE
            resources:
              - pods
            scope: '*'
          - operations: ["CREATE","UPDATE"]
            apiGroups: ["*"]
            apiVersions: ["*"]
            resources: ["deployments"]
            scope: '*'
        objectSelector:
          matchExpressions:
            - key: app
              operator: NotIn
              values:
                - validating-application-standards
            - key: component
              operator: In
              values:
                - server-acl-init
            - key: component
              operator: In
              values:
                - server-acl-init-cleanup
        namespaceSelector:
          matchExpressions:
          - key: kubernetes.io/metadata.name
            operator: NotIn
            values:
              - kube-system
              - kube-public
              - kube-node-lease
              - cattle-system
              - cattle-monitoring-system
              - cattle-impersonation
              - cattle-fleet-system
              # - default
              - fleet-system
              - local
        sideEffects: None
        timeoutSeconds: 3
    ---
    apiVersion: admissionregistration.k8s.io/v1
    kind: MutatingWebhookConfiguration
    metadata:
      name: security-mutating-webhook-cfg
      labels:
        app: security-mutating-webhook
      annotations:
        cert-manager.io/inject-ca-from: ops-admission-webhook/validating-application-standards-tls-secret
    webhooks:
      - name: security-mutating-webhook.XXXXX.com
        admissionReviewVersions:
          - v1
        clientConfig:
          caBundle: ""
          service:
            name: validating-application-standards
            namespace: ops-admission-webhook
            port: 443
            path: /mutate
        rules:
          - operations: ["CREATE","UPDATE"]
            apiGroups: ["*"]
            apiVersions: ["*"]
            resources: ["deployments"]
            scope: '*'
        namespaceSelector:
          matchExpressions:
          - key: kubernetes.io/metadata.name
            operator: NotIn
            values:
              - kube-system
              - kube-public
              - kube-node-lease
              - cattle-system
              - cattle-monitoring-system
              - cattle-impersonation
              - cattle-fleet-system
              - fleet-system
              - local
              - ops-admission-webhook
        sideEffects: None
        timeoutSeconds: 3
        failurePolicy: Fail
        matchPolicy: Equivalent
    ```