package v1beta1

import "k8s.io/apimachinery/pkg/runtime/schema"

// DataPlaneGVR returns current package DataPlane GVR.
func DataPlaneGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    SchemeGroupVersion.Group,
		Version:  SchemeGroupVersion.Version,
		Resource: "dataplanes",
	}
}

// ControlPlaneGVR returns current package ControlPlane GVR.
func ControlPlaneGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    SchemeGroupVersion.Group,
		Version:  SchemeGroupVersion.Version,
		Resource: "controlplanes",
	}
}
