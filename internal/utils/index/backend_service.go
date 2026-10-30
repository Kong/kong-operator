package index

import (
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/v2/controller/hybridgateway/metadata"
)

// ClientKongCertificateOnBackendServiceIndex maps certificate keys and target
// namespaces (namespace/*) to backend Services, including before translation succeeds.
const ClientKongCertificateOnBackendServiceIndex = "ClientKongCertificateOnBackendService"

// OptionsForBackendService indexes hybrid Gateway API client certificate annotations.
func OptionsForBackendService() []Option {
	return []Option{{
		Object: &corev1.Service{}, Field: ClientKongCertificateOnBackendServiceIndex,
		ExtractValueFn: ClientKongCertificateOnBackendService,
	}}
}

// ClientKongCertificateOnBackendService resolves the annotation relative to the backend namespace.
func ClientKongCertificateOnBackendService(obj client.Object) []string {
	service, ok := obj.(*corev1.Service)
	if !ok {
		return nil
	}
	ref, err := metadata.ExtractClientCertificateRef(service.Annotations, service.Namespace)
	if err != nil || ref == nil {
		return nil
	}
	return []string{*ref.Namespace + "/" + ref.Name, *ref.Namespace + "/*"}
}
