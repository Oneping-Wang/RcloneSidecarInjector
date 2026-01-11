package mutation

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	// 引入 K8s 官方库：这里就是我们在“直接操作” K8s 源码定义的数据结构

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

// 定义编解码器，用于把 HTTP 请求体(JSON)转成 K8s 的 Go 结构体
var (
	runtimeScheme = runtime.NewScheme()
	codecs        = serializer.NewCodecFactory(runtimeScheme)
	deserializer  = codecs.UniversalDeserializer()
)

// 核心处理函数
func HandleMutate(w http.ResponseWriter, r *http.Request) {
	// TODO 1: 声明一个变量 body 类型是 []byte
	body := make([]byte, 0)

	// TODO 2: 如果 r.Body 不是 nil，则使用 io.ReadAll 读取它
	// 提示: data, err := io.ReadAll(r.Body)
	// 记得处理 err，如果出错，打印错误并返回 http.Error
	if r.Body != nil {
		data, err := io.ReadAll(r.Body)

		if err != nil {
			fmt.Printf("读取请求体错误：%v", err)
			http.Error(w, "Could not read request body", http.StatusBadRequest)
			return
		}

		body = data
	}

	// TODO 3: 如果 body 长度为 0，返回 http.Error 说 "empty body"
	if len(body) == 0 {
		http.Error(w, "Empty request", http.StatusBadRequest)
		return
	}

	fmt.Printf("成功读取到 Body，长度为: %d\n", len(body)) // 临时打印验证

	// 定义一个变量 ar，类型是 admissionv1.AdmissionReview。
	// 使用 deserializer.Decode 函数，把 body 转换进 ar 里面。
	// 如果转换出错，像刚才一样返回 400 错误。
	var ar admissionv1.AdmissionReview
	if _, _, err := deserializer.Decode(body, nil, &ar); err != nil {
		fmt.Printf("无法解析请求体：%v", err)
		http.Error(w, "Could not decode body", http.StatusBadRequest)
		return
	}

	if ar.Request != nil {
		fmt.Printf("成功解析！请求 UID：%s\n", ar.Request.UID)
	} else {
		fmt.Printf("解析成功但 Request 为空。\n")
	}

	req := ar.Request
	if req == nil {
		http.Error(w, "Http request is nil", http.StatusBadRequest)
		return
	}

	pod := &corev1.Pod{}
	err := json.Unmarshal(req.Object.Raw, pod)

	if err != nil {
		fmt.Printf("二次解析失败：%v\n", err)
		http.Error(w, "Could not re-decoding", http.StatusBadRequest)
		return
	}
	fmt.Printf("二次解析成功，Pod 名字是：%s\n", pod.Name)

	annotationKey := "rclone.io/inject"
	value, ok := pod.Annotations[annotationKey]
	if !ok || value != "true" {
		fmt.Printf("忽略此Pod\n")
		writeResponse(w, ar, &admissionv1.AdmissionResponse{
			Allowed: true,
		})
		return
	}
	fmt.Printf("发现目标，准备注入\n")

	res := &admissionv1.AdmissionResponse{
		Allowed: true,
	}

	addSidecar(pod, res)

	writeResponse(w, ar, res)

}

func writeResponse(w http.ResponseWriter, ar admissionv1.AdmissionReview, response *admissionv1.AdmissionResponse) {
	if ar.Request != nil {
		response.UID = ar.Request.UID
	}

	respReview := admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			Kind:       "AdmissionReview",
			APIVersion: "admission.k8s.io/v1",
		},
		Response: response,
	}

	respBytes, err := json.Marshal(respReview)
	if err != nil {
		fmt.Printf("无法序列化响应: %v\n", err)
		http.Error(w, fmt.Sprintf("could not encode response: %v", err), http.StatusInternalServerError)
		return
	}

	// 4. 写回 HTTP 响应
	fmt.Printf("准备发送响应... Allowed=%v\n", response.Allowed)
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(respBytes); err != nil {
		fmt.Printf("发送响应失败: %v\n", err)
	}
}

func addSidecar(pod *corev1.Pod, response *admissionv1.AdmissionResponse) {
	privileged := true
	bidirectional := corev1.MountPropagationBidirectional
	sidecar := corev1.Container{
		Name:            "rclone-sidecar",
		Image:           "rclone/rclone:latest",
		ImagePullPolicy: corev1.PullIfNotPresent, // 防止每次都拉镜像
		SecurityContext: &corev1.SecurityContext{
			Privileged: &privileged,
		},
		Args: []string{
			"mount",
			"minio:test-data",
			"/data/mount",
			"--config=/etc/rclone/rclone.conf",
			"--allow-other",
			"--vfs-cache-mode=full",
			"--no-check-certificate",
			"--allow-non-empty",
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:             "shared-data",
				MountPath:        "/data/mount",
				MountPropagation: &bidirectional,
			},
			{
				Name:      "rclone-config",
				MountPath: "/etc/rclone",
			},
		},
		Lifecycle: &corev1.Lifecycle{
			PreStop: &corev1.LifecycleHandler{
				Exec: &corev1.ExecAction{
					Command: []string{"/bin/sh", "-c", "fusermount -u /data/mount"},
				},
			},
		},
	}

	vol1 := corev1.Volume{
		Name:         "shared-data",
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	}

	// 定义 Volume 2
	vol2 := corev1.Volume{
		Name:         "rclone-config",
		VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "rclone-config"}},
	}

	patch := []map[string]interface{}{
		// 1. 加 Sidecar 容器
		{
			"op":    "add",
			"path":  "/spec/containers/-",
			"value": sidecar,
		},
		// 2. 加 Shared Data 卷
		{
			"op":    "add",
			"path":  "/spec/volumes/-", // 追加到 volumes 列表末尾
			"value": vol1,
		},
		// 3. 加 Config 卷
		{
			"op":    "add",
			"path":  "/spec/volumes/-", // 继续追加
			"value": vol2,
		},
	}

	patchBytes, err := json.Marshal(patch)
	if err != nil {
		fmt.Printf("序列化 Patch 失败：%v\n", err)
		return
	}

	response.Patch = patchBytes
	pt := admissionv1.PatchTypeJSONPatch
	response.PatchType = &pt

	response.Allowed = true
}
