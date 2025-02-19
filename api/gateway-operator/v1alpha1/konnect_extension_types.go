package v1alpha1

/*
Copyright 2024 Kong Inc.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

func init() {
	SchemeBuilder.Register(&KonnectExtension{}, &KonnectExtensionList{})
}

const (
	// KonnectExtensionKind holds the kind for the KonnectExtension.
	KonnectExtensionKind = "KonnectExtension"
)

// KonnectExtension is the Schema for the KonnectExtension API,
// and is intended to be referenced as extension by the DataPlane API.
// If a DataPlane successfully refers a KonnectExtension, the DataPlane
// deployment spec gets customized to include the konnect-related configuration.
//
// +genclient
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:object:root=true
// +kubebuilder:object:generate=true
// +kubebuilder:subresource:status
// +kubebuilder:deprecatedversion:warning="The v1alpha1 version of KonnectExtension in the gateway-operator.konghq.com API group has been deprecated and will be removed in a future release of the API. Please use the version from the konnect.konghq.com API group."
// +kubebuilder:validation:XValidation:rule="oldSelf.spec.controlPlaneRef == self.spec.controlPlaneRef", message="spec.controlPlaneRef is immutable."
// +apireference:kgo:include
// +kong:channels=gateway-operator
type KonnectExtension struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec is the specification of the KonnectExtension resource.
	Spec KonnectExtensionSpec `json:"spec,omitempty"`
	// Status is the status of the KonnectExtension resource.
	Status KonnectExtensionStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// KonnectExtensionList contains a list of KonnectExtension.
// +apireference:kgo:include
type KonnectExtensionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KonnectExtension `json:"items"`
}

// KonnectExtensionSpec defines the desired state of KonnectExtension.
// +apireference:kgo:include
type KonnectExtensionSpec struct {
	// ControlPlaneRef is a reference to a ControlPlane this KonnectExtension is associated with.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:XValidation:rule="self.type == 'konnectID'", message="Only konnectID type currently supported as controlPlaneRef."
	ControlPlaneRef configurationv1alpha1.ControlPlaneRef `json:"controlPlaneRef"`

	// ControlPlaneRegion is the region of the Konnect Control Plane.
	//
	// +kubebuilder:example:=us
	// +kubebuilder:validation:Required
	ControlPlaneRegion string `json:"controlPlaneRegion"`

	// ServerHostname is the fully qualified domain name of the Konnect server.
	// For typical operation a default value doesn't need to be adjusted.
	// It matches the RFC 1123 definition of a hostname with 1 notable exception
	// that numeric IP addresses are not allowed.
	//
	// Note that as per RFC1035 and RFC1123, a *label* must consist of lower case
	// alphanumeric characters or '-', and must start and end with an alphanumeric
	// character. No other punctuation is allowed.
	//
	// +kubebuilder:default:=konghq.com
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	ServerHostname string `json:"serverHostname"`

	// AuthConfiguration must be used to configure the Konnect API authentication.
	//
	// +kubebuilder:validation:Required
	AuthConfiguration KonnectControlPlaneAPIAuthConfiguration `json:"konnectControlPlaneAPIAuthConfiguration"`

	// ClusterDataPlaneLabels is a set of labels that will be applied to the Konnect DataPlane.
	// +optional
	ClusterDataPlaneLabels map[string]string `json:"clusterDataPlaneLabels,omitempty"`
}

// KonnectControlPlaneAPIAuthConfiguration contains the configuration to authenticate with Konnect API ControlPlane.
// +apireference:kgo:include
type KonnectControlPlaneAPIAuthConfiguration struct {
	// ClusterCertificateSecretRef is the reference to the Secret containing the Konnect Control Plane's cluster certificate.
	// +kubebuilder:validation:Required
	ClusterCertificateSecretRef ClusterCertificateSecretRef `json:"clusterCertificateSecretRef"`
}

// ClusterCertificateSecretRef contains the reference to the Secret containing the Konnect Control Plane's cluster certificate.
// +apireference:kgo:include
type ClusterCertificateSecretRef struct {
	// Name is the name of the Secret containing the Konnect Control Plane's cluster certificate.
	// +kubebuilder:validation:Required
	Name string `json:"name"`
}

// KonnectExtensionStatus defines the observed state of KonnectExtension.
// +apireference:kgo:include
type KonnectExtensionStatus struct {
	// DataPlaneRefs is the array  of DataPlane references this is associated with.
	// A new reference is set by the operator when this extension is associated with
	// a DataPlane through its extensions spec.
	//
	// +kube:validation:Optional
	DataPlaneRefs []commonv1alpha1.NamespacedRef `json:"dataPlaneRefs,omitempty"`
}
