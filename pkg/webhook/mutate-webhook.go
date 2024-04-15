package webhook

import (
	"encoding/json"
	"fmt"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	admissionWebhookAnnotationStatusKey = "security-admission-webhook.XXXXX.com/status"
)

type patchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

// 在 Kubernetes 中，spec.securityContext 是一个结构体字段，而不是一个数组或对象字段，
// 因此适合使用 Op: replace 来替换其值，而不是使用 Op: add。如果使用 Op: add，则需要提供完整的路径到要添加的字段，会更复杂
func updateSecurityContext(deployment *appsv1.Deployment) (patch []patchOperation) {
	// 增加for循环处理Deployment.Spec.Template.Spec.Containers.SecurityContext为空的情况
	for i := range deployment.Spec.Template.Spec.Containers {
		container := &deployment.Spec.Template.Spec.Containers[i]
		if container.SecurityContext == nil {
			container.SecurityContext = &corev1.SecurityContext{}
			patch = append(patch, patchOperation{
				Op:    "add",
				Path:  fmt.Sprintf("/spec/template/spec/containers/%d/securityContext", i),
				Value: container.SecurityContext,
			})
		}
		if container.SecurityContext.Privileged == nil {
			container.SecurityContext.Privileged = new(bool)
		}
		if container.SecurityContext.AllowPrivilegeEscalation == nil {
			container.SecurityContext.AllowPrivilegeEscalation = new(bool)
		}

		patch = append(patch, patchOperation{
			Op:   "replace",
			Path: fmt.Sprintf("/spec/template/spec/containers/%d/securityContext/privileged", i),
			// Path:  "/spec/securityContext/privileged",
			Value: false,
		})
		patch = append(patch, patchOperation{
			Op:   "replace",
			Path: fmt.Sprintf("/spec/template/spec/containers/%d/securityContext/allowPrivilegeEscalation", i),
			// Path:  "/spec/securityContext/allowPrivilegeEscalation",
			Value: false,
		})
	}
	return patch
}

// Patch Pod 的 annotation
func updateAnnotation(target map[string]string, added map[string]string) (patch []patchOperation) {
	for key, value := range added {
		if target == nil || target[key] == "" {
			target = map[string]string{}
			patch = append(patch, patchOperation{
				Op:   "add",
				Path: "/metadata/annotations",
				Value: map[string]string{
					key: value,
				},
			})
		} else {
			patch = append(patch, patchOperation{
				Op:    "replace",
				Path:  "/metadata/annotations/" + key,
				Value: value,
			})
		}
	}
	return patch
}

func createPatch(deployment *appsv1.Deployment, availableAnnotations map[string]string, annotations map[string]string) ([]byte, error) {
	var patch []patchOperation

	// ...可变参数追加到patch中相当于下面的
	// updateAnnotationPatch := updateAnonation(availableAnnotations, annotations)
	// for i := range updateAnnotationPatch {
	// 	patch = append(patch, updateAnnotationPatch[i])
	// }
	patch = append(patch, updateAnnotation(availableAnnotations, annotations)...)
	patch = append(patch, updateSecurityContext(deployment)...)
	return json.Marshal(patch)
}

func Mutate(w http.ResponseWriter, r *http.Request) {
	// fmt.Printf("Receive Mutating pod for %s", r.URL.Path)

	// 检测 ApiServer请求类型是否为application/json
	contentType := r.Header.Get("Content-Type")
	if contentType != "application/json" {
		http.Error(w, fmt.Sprintf("Content-Type=%s, expect application/json", contentType), http.StatusUnsupportedMediaType)
		return
	}

	var (
		AdmissionReviewObject, AdmissionResponseObject admissionv1.AdmissionReview
		deployment                                     appsv1.Deployment
		availableAnnotations                           map[string]string
	)

	if err := json.NewDecoder(r.Body).Decode(&AdmissionReviewObject); err != nil {
		http.Error(w, fmt.Sprintf("Failed to decode AdmissionReview request: %v", err), http.StatusBadRequest)
		return
	}

	AdmissionResponseObject.TypeMeta = AdmissionReviewObject.TypeMeta
	AdmissionResponseObject.Response = &admissionv1.AdmissionResponse{
		// 指定响应的UID和AdmissionReview请求一致
		UID:     AdmissionReviewObject.Request.UID,
		Allowed: true,
		Result:  nil,
	}

	if err := json.Unmarshal(AdmissionReviewObject.Request.Object.Raw, &deployment); err != nil {
		http.Error(w, fmt.Sprintf("Failed to unmarshal Deployment object: %v", err), http.StatusBadRequest)
		return
	}

	if AdmissionReviewObject.Request.Kind.Kind == "Deployment" {
		// fmt.Println("mutating deployment: ", deployment.Name)
		// deploymentJson, err := json.MarshalIndent(deployment, "", "    ")
		// if err != nil {
		// 	fmt.Println(err.Error())
		// }
		// fmt.Println("mutate判断为deployment后输出deployment值: " + string(deploymentJson))

		for _, container := range deployment.Spec.Template.Spec.Containers {
			if container.SecurityContext != nil && !*container.SecurityContext.AllowPrivilegeEscalation && !*container.SecurityContext.Privileged {
				AdmissionResponseObject.Response.Allowed = true
				AdmissionResponseObject.Response.Result = nil
			} else {
				annotations := map[string]string{admissionWebhookAnnotationStatusKey: "mutated"}
				patchBytes, err := createPatch(&deployment, availableAnnotations, annotations)
				if err != nil {
					AdmissionResponseObject.Response.Allowed = false
					AdmissionResponseObject.Response.Result = &metav1.Status{
						TypeMeta: AdmissionReviewObject.TypeMeta,
						Status:   "Failure",
						Message:  error.Error(err),
					}
				} else {
					AdmissionResponseObject.Response.Allowed = true
					AdmissionResponseObject.Response.Result = &metav1.Status{
						TypeMeta: AdmissionReviewObject.TypeMeta,
						Code:     200,
						Status:   "Success",
					}
					AdmissionResponseObject.Response.Patch = patchBytes
					AdmissionResponseObject.Response.PatchType = func() *admissionv1.PatchType {
						pt := admissionv1.PatchTypeJSONPatch
						return &pt
					}()
				}
			}
		}
	}

	responJson, err := json.Marshal(AdmissionResponseObject)
	if err != nil {
		fmt.Printf("AdmissionResponseObject: %+v\n", AdmissionResponseObject)
		http.Error(w, "Marshal AdmissionResponseObject error", http.StatusInternalServerError)
	}
	// fmt.Printf("AdmissionResponseObject JSON: %s\n", responJson)
	w.Header().Set("Content-Type", "application/json")
	w.Write(responJson)
}
