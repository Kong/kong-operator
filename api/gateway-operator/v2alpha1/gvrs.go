package v2alpha1

import "k8s.io/apimachinery/pkg/runtime/schema"

// ControlPlaneGVR returns current package ControlPlane GVR.
func ControlPlaneGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    SchemeGroupVersion.Group,
		Version:  SchemeGroupVersion.Version,
		Resource: "controlplanes",
	}
}
