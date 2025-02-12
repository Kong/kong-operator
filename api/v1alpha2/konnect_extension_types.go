/*
Copyright 2025 Kong Inc.
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

package v1alpha2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"

	operatorv1alpha1 "github.com/kong/gateway-operator/api/v1alpha1"
)

func init() {
	SchemeBuilder.Register(&KonnectExtension{}, &KonnectExtensionList{})
}

const (
	// KonnectExtensionKind holds the kind for the KonnectExtension.
	KonnectExtensionKind = "KonnectExtension"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:resource:categories=kong;all
// +kubebuilder:subresource:status
// +kubebuilder:storageversion

// KonnectExtension is the Schema for the KonnectExtension API,
// and is intended to be referenced as extension by the DataPlane API.
// If a DataPlane successfully refers a KonnectExtension, the DataPlane
// deployment spec gets customized to include the konnect-related configuration.
// +kubebuilder:validation:XValidation:rule="oldSelf.spec.controlPlaneRef == self.spec.controlPlaneRef", message="spec.controlPlaneRef is immutable."
// +kubebuilder:validation:XValidation:rule="self.spec.controlPlaneRef.type == 'konnectID' ? has(self.spec.konnect) : true",message="konnect must be set when ControlPlaneRef is set to KonnectID."
// +kubebuilder:validation:XValidation:rule="self.spec.controlPlaneRef.type == 'konnectNamespacedRef' ? !has(self.spec.konnect) : true",message="konnect must be unset when ControlPlaneRef is set to konnectNamespacedRef."
// +apireference:kgo:include
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
	// +kubebuilder:validation:XValidation:rule="self.type != 'kic'", message="kic type not supported as controlPlaneRef."
	ControlPlaneRef configurationv1alpha1.ControlPlaneRef `json:"controlPlaneRef"`

	// DataPlaneClientAuth is the configuration for the client certificate authentication for the DataPlane.
	// It is required to set up the connection with the Konnect Platform.
	// +kubebuilder:validation:Optional
	// +kubebuilder:default={certificateSecret:{provisioning: Automatic}}
	DataPlaneClientAuth *DataPlaneClientAuth `json:"dataPlaneClientAuth,omitempty"`

	// +kubebuilder:validation:Optional
	KonnectConfiguration *konnectv1alpha1.KonnectConfiguration `json:"konnect,omitempty"`

	// ClusterDataPlaneLabels is a set of labels that will be applied to the Konnect DataPlane.
	// +optional
	ClusterDataPlaneLabels map[string]string `json:"clusterDataPlaneLabels,omitempty"`

	// Deprecated fields below - to be removed in a future release.

	// ControlPlaneRegion is the region of the Konnect Control Plane.
	//
	// deprecated: controlPlaneRegion is deprecated and will be removed in a future release.
	//
	// +kubebuilder:example:=us
	// +kubebuilder:validation:Optional
	ControlPlaneRegion *string `json:"controlPlaneRegion,omitempty"`

	//
	// ServerHostname is the fully qualified domain name of the Konnect server.
	// For typical operation a default value doesn't need to be adjusted.
	// It matches the RFC 1123 definition of a hostname with 1 notable exception
	// that numeric IP addresses are not allowed.
	//
	// Note that as per RFC1035 and RFC1123, a *label* must consist of lower case
	// alphanumeric characters or '-', and must start and end with an alphanumeric
	// character. No other punctuation is allowed.
	//
	// deprecated: serverHostname is deprecated and will be removed in a future release.
	//
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	// +kubebuilder:validation:Optional
	ServerHostname *string `json:"serverHostname,omitempty"`

	// AuthConfiguration must be used to configure the Konnect API authentication.
	//
	// deprecated: konnectControlPlaneAPIAuthConfiguration is deprecated and will be removed in a future release.
	//
	// +kubebuilder:validation:Optional
	AuthConfiguration KonnectControlPlaneAPIAuthConfiguration `json:"konnectControlPlaneAPIAuthConfiguration"`
}

// DataPlaneClientAuth contains the configuration for the client certificate authentication for the DataPlane.
// At the moment authentication is only supported through client certificate, but it could be improved in the future,
// with e.g., token-based authentication.
type DataPlaneClientAuth struct {
	// certificateSecret is the reference to the Secret containing the client certificate.
	//
	// +kubebuilder:validation:XValidation:rule="self.provisioning == 'Manual' ? has(self.secretRef) : true",message="secretRef must be set when provisioning is set to Manual."
	// +kubebuilder:validation:XValidation:rule="self.provisioning == 'Automatic' ? !has(self.secretRef) : true",message="secretRef must not be set when provisioning is set to Automatic."
	// +kubebuilder:validation:Required
	CertificateSecret *CertificateSecret `json:"certificateSecret,omitempty"`
}

type CertificateSecret struct {
	// Provisioning is the method used to provision the certificate. It can be either Manual or Automatic.
	// In case manual provisioning is used, the certificate must be provided by the user.
	// In case automatic provisioning is used, the certificate will be automatically generated by the system.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Enum=Manual;Automatic
	// +kubebuilder:default=Automatic
	Provisioning *string `json:"provisioning,omitempty"`

	// SecretRef is the reference to the Secret containing the client certificate.
	// +kubebuilder:validation:Optional
	SecretRef *CertificateSecretRef `json:"secretRef,omitempty"`
}

// KonnectControlPlaneAPIAuthConfiguration contains the configuration to authenticate with Konnect API ControlPlane.
// +apireference:kgo:include
type KonnectControlPlaneAPIAuthConfiguration struct {
	// ClusterCertificateSecretRef is the reference to the Secret containing the Konnect Control Plane's cluster certificate.
	// +kubebuilder:validation:Required
	ClusterCertificateSecretRef CertificateSecretRef `json:"clusterCertificateSecretRef"`
}

// CertificateSecretRef contains the reference to the Secret containing the Konnect Control Plane's cluster certificate.
// +apireference:kgo:include
type CertificateSecretRef struct {
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
	DataPlaneRefs []operatorv1alpha1.NamespacedRef `json:"dataPlaneRefs,omitempty"`
}
