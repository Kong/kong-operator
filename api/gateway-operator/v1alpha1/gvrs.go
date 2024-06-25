package v1alpha1

import "k8s.io/apimachinery/pkg/runtime/schema"

// AIGatewayGVR returns current package AIGateway GVR.
func AIGatewayGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    SchemeGroupVersion.Group,
		Version:  SchemeGroupVersion.Version,
		Resource: "aigateways",
	}
}
