package webhook

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	admissionv1 "k8s.io/api/admission/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// 如果单纯只检测Pod的相关配置，只会在创建Pod资源的时候会显示拒绝的理由。
// 创建Deployment等资源时不会直接显示拒绝的理由，只会在rs创建pod时在rs的event处显示拒绝理由，所以这里得额外加一步处理
func Validate(w http.ResponseWriter, r *http.Request) {
	// fmt.Printf("Receive Validating request for %s\n", r.URL.Path)

	// 将 APIServer请求body反序列化解析为AdmissionReview结构体
	// AdmissionReview对象可以通过导入"k8s.io/api/admission/v1"直接使用，也可以通过自己创建一个AdmissionReview结构体的方式使用
	// AdmissionReview对象中包含的数据可以在k8s动态准入控制文档查看：https://kubernetes.io/zh-cn/docs/reference/access-authn-authz/extensible-admission-controllers/
	// 也可以在Go文档中查看：https://pkg.go.dev/k8s.io/api/admission/v1#AdmissionReview
	// 为了省事儿，这里选择直接导入"k8s.io/api/admission/v1"对象使用
	var (
		reviewReq, reviewResp admissionv1.AdmissionReview
		// pod                   corev1.Pod
		// deployment            appsv1.Deployment
	)

	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&reviewReq); err != nil {
		fmt.Println("Validate decode wrong")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// 初始化AdmissionResponseObject
	// TypeMeta信息内容即{TypeMeta:{Kind:Deployment APIVersion:apps/v1}，需要同时返回给apiserver
	reviewResp.TypeMeta = reviewReq.TypeMeta
	reviewResp.Response = &admissionv1.AdmissionResponse{
		UID:     reviewReq.Request.UID,
		Allowed: true,
		Result:  nil,
	}

	if reviewReq.Request.Kind.Kind == "Deployment" {
		deploymentStandardsCheck(w, r, &reviewReq, &reviewResp)
	}
	if reviewReq.Request.Kind.Kind == "Pod" {
		podStandardsCheck(w, r, &reviewReq, &reviewResp)
	}

	// 输出最后的reviewResp值
	// finalResp, err := json.MarshalIndent(reviewResp, "", "    ")
	// if err != nil {
	// 	log.Fatal(err)
	// }
	// fmt.Println("check 最后输出Resp值: " + string(finalResp))

	js, err := json.Marshal(reviewResp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(js)
}

func deploymentStandardsCheck(w http.ResponseWriter, r *http.Request, reviewReq *admissionv1.AdmissionReview, reviewResp *admissionv1.AdmissionReview) {
	var deployment *appsv1.Deployment
CheckDeploymentPodStandards:
	for {

		if err := json.Unmarshal(reviewReq.Request.Object.Raw, &deployment); err != nil {
			fmt.Println("deployment jsonUnmarshal error")
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// fmt.Printf("reviewReq.Request.Kind.Kind: %s\n", reviewReq.Request.Kind.Kind)
		// deploymentJson, err := json.MarshalIndent(deployment, "", "    ")
		// if err != nil {
		// 	fmt.Println("deploymentJson MarshalIndent error")
		// 	fmt.Println(err.Error())
		// }
		// fmt.Println("validate判断为deployment后输出deployment值: " + string(deploymentJson))
		// fmt.Println("validating deployment: ", deployment.Name)

		if deployment.Spec.Template.Spec.Affinity == nil || deployment.Spec.Template.Spec.NodeSelector == nil {
			reviewResp.Response.Allowed = false
			reviewResp.Response.Result = &metav1.Status{
				Status:  "Failure",
				Reason:  metav1.StatusReason("BK Container Rules"),
				Message: fmt.Sprintf("deployment %s is no Setting scheduling policy", deployment.Name),
			}
			// fmt.Println("走到了deployment调度检测break")
			break CheckDeploymentPodStandards
		}

		for _, container := range deployment.Spec.Template.Spec.Containers {
			if container.Name == "istio-proxy" {
				continue
			}

			// fmt.Printf("deployment循环输出container name：%s\n", container.Name)
			// fmt.Println("container.SecurityContext: ", container.SecurityContext)
			if container.SecurityContext == nil || container.SecurityContext.AllowPrivilegeEscalation == nil || container.SecurityContext.Privileged == nil || *container.SecurityContext.AllowPrivilegeEscalation || *container.SecurityContext.Privileged {
				reviewResp.Response.Allowed = false
				reviewResp.Response.Result = &metav1.Status{
					Status:  "Failure",
					Reason:  metav1.StatusReason("BK Container Rules"),
					Message: fmt.Sprintf("container %s is not Setting SecurityContext.AllowPrivilegeEscalation or SecurityContext.Privileged", container.Name),
				}
				// fmt.Println("走到了deployment安全上下文 continue")
				break CheckDeploymentPodStandards
				// continue
			}

			// fmt.Println("container.Resources.Limits: ", container.Resources.Limits)
			// fmt.Println("container.Resources.Requests: ", container.Resources.Requests)
			if container.Resources.Limits == nil || container.Resources.Requests == nil {
				reviewResp.Response.Allowed = false
				reviewResp.Response.Result = &metav1.Status{
					Status:  "Failure",
					Reason:  metav1.StatusReason("BK Container Rules"),
					Message: fmt.Sprintf("container %s is not Setting Resource Limit", container.Name),
				}
				// fmt.Println("走到了deployment资源设置 continue")
				break CheckDeploymentPodStandards
				// continue
			}

			// fmt.Println("container.LivenessProbe: ", container.LivenessProbe)
			// fmt.Println("container.ReadinessProbe: ", container.ReadinessProbe)
			if container.LivenessProbe == nil || container.ReadinessProbe == nil {
				reviewResp.Response.Allowed = false
				reviewResp.Response.Result = &metav1.Status{
					Status:  "Failure",
					Reason:  metav1.StatusReason("BK Container Rules"),
					Message: fmt.Sprintf("container %s is not Setting LivenessProbe or ReadinessProbe", container.Name),
				}
				// fmt.Println("走到了deployment探针设置 continue")
				break CheckDeploymentPodStandards
				// continue
			}
		}
		// fmt.Println("走到了默认 deployment break")
		break
	}
}

func podStandardsCheck(w http.ResponseWriter, r *http.Request, reviewReq *admissionv1.AdmissionReview, reviewResp *admissionv1.AdmissionReview) {
	var pod *corev1.Pod
CheckPodStandards:
	for {
		// Get pod object from request
		if err := json.Unmarshal(reviewReq.Request.Object.Raw, &pod); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// println("reviewReq.Request.Kind.Kind: %s", reviewReq.Request.Kind.Kind)
		// formattedJson, err := json.MarshalIndent(pod, "", "    ")
		// if err != nil {
		// 	fmt.Println(err.Error())
		// }
		// fmt.Println("validate判断pod后输出pod值: " + string(formattedJson))
		// fmt.Println("validating pod: ", pod.Name)

		// 获取命名空间中的项目标识
		projectIdentifier := getProjectIdentifier(reviewReq.Request.Namespace)
		// 验证 pod 的名称是否以项目标识开头
		if !strings.HasPrefix(pod.Name, projectIdentifier+"-") {
			reviewResp.Response.Allowed = false
			reviewResp.Response.Result = &metav1.Status{
				Status:  "Failure",
				Reason:  metav1.StatusReason("BK Container Rules"),
				Message: fmt.Sprintf("Pod name must start with '%s-'", projectIdentifier),
				Code:    402,
			}
			// fmt.Println("走到了pod项目标识break")
			break CheckPodStandards
		}

		// fmt.Println("pod.Spec.NodeSelector: ", pod.Spec.NodeSelector)
		// fmt.Println("pod.Spec.Affinity: ", pod.Spec.Affinity)
		//判断pod是否设置调度策略
		if pod.Spec.NodeSelector == nil && pod.Spec.Affinity == nil {
			reviewResp.Response.Allowed = false
			reviewResp.Response.Result = &metav1.Status{
				Status:  "Failure",
				Reason:  metav1.StatusReason("BK Container Rules"),
				Message: fmt.Sprintf("pod %s is not Setting scheduling policy", pod.Name),
				Code:    402,
			}
			// fmt.Println("走到了pod调度检测break")
			break CheckPodStandards
		}

		for _, ctr := range pod.Spec.Containers {
			//判断pod是否有注入envoy的sidecar，有则忽略envoy的校验
			if ctr.Name == "istio-proxy" {
				continue
			}
			// fmt.Printf("pod循环输出container name：%s\n", ctr.Name)
			// fmt.Println("ctr.Resources.Limits: ", ctr.Resources.Limits)
			// fmt.Println("ctr.Resources.Requests: ", ctr.Resources.Requests)
			//判断pod中的容器是否设置资源设置
			if ctr.Resources.Limits == nil || ctr.Resources.Requests == nil {
				reviewResp.Response.Allowed = false
				reviewResp.Response.Result = &metav1.Status{
					Status:  "Failure",
					Reason:  metav1.StatusReason("BK Container Rules"),
					Message: fmt.Sprintf("container %s is not Setting Resource", ctr.Name),
					Code:    402,
				}
				// fmt.Println("走到了pod资源设置 continue")
				break CheckPodStandards
				// continue
			}

			// fmt.Println("ctr.ReadinessProbe: ", ctr.ReadinessProbe)
			// fmt.Println("ctr.LivenessProbe: ", ctr.LivenessProbe)
			//判断pod中的容器是否设置探针检测
			if ctr.ReadinessProbe == nil || ctr.LivenessProbe == nil {
				reviewResp.Response.Allowed = false
				reviewResp.Response.Result = &metav1.Status{
					Status:  "Failure",
					Reason:  metav1.StatusReason("BK Container Rules"),
					Message: fmt.Sprintf("container %s is not Setting ReadinessProbe or livenessProbe", ctr.Name),
					Code:    402,
				}
				// fmt.Println("走到了pod探针检测 continue")
				break CheckPodStandards
				// continue
			}
		}
		// fmt.Println("走到了默认 pod break")
		break
	}
}

func getProjectIdentifier(namespace string) string {
	parts := strings.Split(namespace, "-")
	if len(parts) > 1 {
		return parts[0]
	}
	return ""
}
