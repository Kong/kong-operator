package v1alpha2

import "k8s.io/apimachinery/pkg/runtime/schema"

// KonnectGatewayControlPlaneGVR returns the current KonnectGatewayControlPlane GVR.
func KonnectGatewayControlPlaneGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    GroupVersion.Group,
		Version:  GroupVersion.Version,
		Resource: "konnectgatewaycontrolplanes",
	}
}
