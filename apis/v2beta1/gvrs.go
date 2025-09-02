package v2beta1

import "k8s.io/apimachinery/pkg/runtime/schema"

// ControlPlaneGVR returns current package ControlPlane GVR.
func ControlPlaneGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    SchemeGroupVersion.Group,
		Version:  SchemeGroupVersion.Version,
		Resource: "controlplanes",
	}
}

// GatewayConfigurationGVR returns current package GatewayConfiguration GVR.
func GatewayConfigurationGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    SchemeGroupVersion.Group,
		Version:  SchemeGroupVersion.Version,
		Resource: "gatewayconfigurations",
	}
}
