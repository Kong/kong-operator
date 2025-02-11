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

// KongPluginInstallationGVR returns current package KongPluginInstallation GVR.
func KongPluginInstallationGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    SchemeGroupVersion.Group,
		Version:  SchemeGroupVersion.Version,
		Resource: "kongplugininstallations",
	}
}

// KonnectExtensionGVR returns current package KonnectExtension GVR.
func KonnectExtensionGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    SchemeGroupVersion.Group,
		Version:  SchemeGroupVersion.Version,
		Resource: "konnectextensions",
	}
}
