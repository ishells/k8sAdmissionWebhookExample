TAG = zengxu/validating-application-standards-validating-admission-webhook:v1

build-load:
	docker buildx build --load -t $(TAG) .
	kind load docker-image $(TAG)

ensure-image: 
ifeq ([], $(shell docker inspect --type=image $(TAG)))
	make build-load
endif

cert:
	./hack/gencert.sh
	./hack/create-csr-cert.sh --service validating-application-standards --namespace default --secret validating-application-standards-tls-secret

deploy: ensure-image
	make cert
	kubectl apply -f ./manifests/k.yaml
	./hack/set-kube-ca.sh

clear:
	kubectl delete secret validating-application-standards-tls-secret
	kubectl delete -f ./manifests/k.yaml
	kubectl delete CertificateSigningRequest validating-application-standards.default

deploy-cm: SHELL:=/bin/bash
deploy-cm: ensure-image
	# ./manifests/cert-manager-1.5.3.yaml was ported from https://github.com/jetstack/cert-manager/releases/download/v1.5.3/cert-manager.yaml
	kubectl apply -f ./manifests/cert-manager-1.5.3.yaml
	# loop until cert-manager pod ready
	for i in {1..30}; do kubectl apply -f ./manifests/k-cert-manager.yaml; if [ $$? -eq 0 ]; then break; else sleep 6; fi; done;
	kubectl apply -f ./manifests/k.yaml

clear-cm:
	kubectl delete -f ./manifests/k.yaml &
	kubectl delete -f ./manifests/cert-manager-1.5.3.yaml &
	kubectl delete -f ./manifests/k-cert-manager.yaml

save-cert:
	kubectl get secret validating-application-standards-tls-secret -o jsonpath={.data.'tls\.crt'} | base64 -d > tls.crt
	kubectl get secret validating-application-standards-tls-secret -o jsonpath={.data.'tls\.key'} | base64 -d > tls.key

install-outcluster:
	./hack/gencert.sh
	@CA=$$(cat ./tls.crt | base64) && \
	sed -e "s/{{.LOCALIP}}/$$LOCALIP/g" -e "s/{{.CA}}/$$CA/g" ./manifests/outcluster-webhook-configuration.yaml | kubectl apply -f -

clear-outcluster:
	kubectl delete -f ./manifests/outcluster-webhook-configuration.yaml

setup-kube-for-outcluster-cm: SHELL:=/bin/bash
setup-kube-for-outcluster-cm:
	# ./manifests/cert-manager-1.5.3.yaml was ported from https://github.com/jetstack/cert-manager/releases/download/v1.5.3/cert-manager.yaml
	kubectl apply -f ./manifests/cert-manager-1.5.3.yaml
	# loop until cert-manager pod ready
	for i in {1..30}; do kubectl apply -f ./manifests/k-cert-manager.yaml; if [ $$? -eq 0 ]; then break; else sleep 6; fi; done;
	kubectl apply -f ./manifests/outcluster-webhook-configuration.yaml
	make save-cert

clear-kube-for-outcluster-cm:
	kubectl delete -f ./manifests/outcluster-webhook-configuration.yaml
	kubectl delete -f ./manifests/cert-manager-1.5.3.yaml
	kubectl delete -f ./manifests/k-cert-manager.yaml
