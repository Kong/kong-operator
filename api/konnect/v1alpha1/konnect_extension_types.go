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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/api/common/v1alpha1"
)

func init() {
	SchemeBuilder.Register(&KonnectExtension{}, &KonnectExtensionList{})
}

const (
	// KonnectExtensionKind holds the kind for the KonnectExtension.
	KonnectExtensionKind = "KonnectExtension"
)

// KonnectExtension is the Schema for the KonnectExtension API, and is intended to be referenced as
// extension by the DataPlane, ControlPlane or GatewayConfiguration APIs.
// If one of the above mentioned resources successfully refers a KonnectExtension, the underlying
// deployment(s) spec gets customized to include the konnect-related configuration.
//
// +genclient
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:object:root=true
// +kubebuilder:object:generate=true
// +kubebuilder:subresource:status
// +kubebuilder:validation:XValidation:rule="oldSelf.spec.konnectControlPlane.controlPlaneRef == self.spec.konnectControlPlane.controlPlaneRef", message="spec.konnectControlPlane.controlPlaneRef is immutable."
// +kubebuilder:validation:XValidation:rule="self.spec.konnectControlPlane.controlPlaneRef.type == 'konnectID' ? has(self.spec.konnect) : true",message="konnect must be set when ControlPlaneRef is set to KonnectID."
// +kubebuilder:validation:XValidation:rule="self.spec.konnectControlPlane.controlPlaneRef.type == 'konnectNamespacedRef' ? !has(self.spec.konnect) : true",message="konnect must be unset when ControlPlaneRef is set to konnectNamespacedRef."
// +apireference:kgo:include
// +kong:channels=gateway-operator
type KonnectExtension struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec is the specification of the KonnectExtension resource.
	Spec KonnectExtensionSpec `json:"spec,omitempty"`

	// Status is the status of the KonnectExtension resource.
	//
	// +kubebuilder:default={conditions: {{type: "Ready", status: "Unknown", reason:"Pending", message:"Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}}
	Status KonnectExtensionStatus `json:"status,omitempty"`
}

// KonnectExtensionList contains a list of KonnectExtension.
//
// +kubebuilder:object:root=true
// +apireference:kgo:include
type KonnectExtensionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KonnectExtension `json:"items"`
}

// KonnectExtensionSpec defines the desired state of KonnectExtension.
type KonnectExtensionSpec struct {
	// KonnectControlPlane is the configuration for the Konnect Control Plane.
	//
	// +kubebuilder:validation:Required
	KonnectControlPlane KonnectExtensionControlPlane `json:"konnectControlPlane"`

	// DataPlaneClientAuth is the configuration for the client certificate authentication for the DataPlane.
	// In case the ControlPlaneRef is of type KonnectID, it is required to set up the connection with the
	// Konnect Platform.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:default={certificateSecret:{provisioning: Automatic}}
	DataPlaneClientAuth *DataPlaneClientAuth `json:"dataPlaneClientAuth,omitempty"`

	// KonnectConfiguration holds the information needed to setup the Konnect Configuration.
	//
	// +kubebuilder:validation:Optional
	KonnectConfiguration *KonnectConfiguration `json:"konnect,omitempty"`

	// DataPlaneLabels is a set of labels that will be applied to the Konnect DataPlane.
	//
	// +optional
	// +kubebuilder:validation:MaxItems=5
	// +kubebuilder:validation:XValidation:rule="self.all(key, key.matches('^[a-zA-Z0-9]([a-zA-Z0-9._-]*[a-zA-Z0-9])?$'))",message="keys must match the pattern '^[a-zA-Z0-9]([a-zA-Z0-9._-]*[a-zA-Z0-9])?$'."
	// +kubebuilder:validation:XValidation:rule="self.all(key, !(key.startsWith('kong') || key.startsWith('konnect') || key.startsWith('insomnia') || key.startsWith('mesh') || key.startsWith('kic') || key.startsWith('_')))",message="keys must not start with 'kong', 'konnect', 'insomnia', 'mesh', 'kic', or '_'."
	// +kubebuilder:validation:XValidation:rule="self.all(key, size(key) > 0 && size(key) < 64)",message="Too long: may not be more than 63 bytes"
	DataPlaneLabels map[string]DataPlaneLabelValue `json:"dataPlaneLabels,omitempty"`
}

// KonnectExtensionControlPlane is the configuration for the Konnect Control Plane.
type KonnectExtensionControlPlane struct {
	// ControlPlaneRef is a reference to a Konnect ControlPlane this KonnectExtension is associated with.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:XValidation:rule="self.type != 'kic'", message="kic type not supported as controlPlaneRef."
	ControlPlaneRef commonv1alpha1.ControlPlaneRef `json:"controlPlaneRef"`
}

// DataPlaneLabelValue is the type that defines the value of a label that will be applied to the Konnect DataPlane.
//
// +kubebuilder:validation:MinLength=1
// +kubebuilder:validation:MaxLength=63
// +kubebuilder:validation:Pattern="^[a-zA-Z0-9]([a-zA-Z0-9._-]*[a-zA-Z0-9])?$"
type DataPlaneLabelValue string

// DataPlaneClientAuth contains the configuration for the client authentication for the DataPlane.
// At the moment authentication is only supported through client certificate, but it might be extended in the future,
// with e.g., token-based authentication.
//
// +kubebuilder:validation:XValidation:rule="self.certificateSecret.provisioning == 'Manual' ? has(self.certificateSecret.secretRef) : true",message="secretRef must be set when provisioning is set to Manual."
// +kubebuilder:validation:XValidation:rule="self.certificateSecret.provisioning == 'Automatic' ? !has(self.certificateSecret.secretRef) : true",message="secretRef must not be set when provisioning is set to Automatic."
type DataPlaneClientAuth struct {
	// CertificateSecret contains the information to access the client certificate.
	//
	// +kubebuilder:validation:Required
	CertificateSecret CertificateSecret `json:"certificateSecret"`
}

// ProvisioningMethod is the type of the provisioning methods available to provision the certificate.
type ProvisioningMethod string

const (
	// ManualSecretProvisioning is the method used to provision the certificate manually.
	ManualSecretProvisioning ProvisioningMethod = "Manual"
	// AutomaticSecretProvisioning is the method used to provision the certificate automatically.
	AutomaticSecretProvisioning ProvisioningMethod = "Automatic"
)

// CertificateSecret contains the information to access the client certificate.
type CertificateSecret struct {
	// Provisioning is the method used to provision the certificate. It can be either Manual or Automatic.
	// In case manual provisioning is used, the certificate must be provided by the user.
	// In case automatic provisioning is used, the certificate will be automatically generated by the system.
	//
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Enum=Manual;Automatic
	// +kubebuilder:default=Automatic
	Provisioning *ProvisioningMethod `json:"provisioning,omitempty"`

	// CertificateSecretRef is the reference to the Secret containing the client certificate.
	//
	// +kubebuilder:validation:Optional
	CertificateSecretRef *SecretRef `json:"secretRef,omitempty"`
}

// SecretRef contains the reference to the Secret containing the Konnect Control Plane's cluster certificate.
type SecretRef struct {
	// Name is the name of the Secret containing the Konnect Control Plane's cluster certificate.
	//
	// +kubebuilder:validation:Required
	Name string `json:"name"`
}

// KonnectExtensionStatus defines the observed state of KonnectExtension.
type KonnectExtensionStatus struct {
	// DataPlaneRefs is the array  of DataPlane references this is associated with.
	// A new reference is set by the operator when this extension is associated with
	// a DataPlane through its extensions spec.
	//
	// +kubebuilder:validation:MaxItems=16
	DataPlaneRefs []commonv1alpha1.NamespacedRef `json:"dataPlaneRefs,omitempty"`

	// ControlPlaneRefs is the array  of ControlPlane references this is associated with.
	// A new reference is set by the operator when this extension is associated with
	// a ControlPlane through its extensions spec.
	//
	// +kubebuilder:validation:MaxItems=16
	ControlPlaneRefs []commonv1alpha1.NamespacedRef `json:"controlPlaneRefs,omitempty"`

	// DataPlaneClientAuth contains the configuration for the client certificate authentication for the DataPlane.
	//
	// +kubebuilder:validation:Optional
	DataPlaneClientAuth *DataPlaneClientAuthStatus `json:"dataPlaneClientAuth,omitempty"`

	// Konnect contains the status information related to the Konnect Control Plane.
	//
	// +kubebuilder:validation:Optional
	Konnect *KonnectExtensionControlPlaneStatus `json:"konnect,omitempty"`

	// Conditions describe the current conditions of the KonnectExtensionStatus.
	// Known condition types are:
	//
	// +optional
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MaxItems=8
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// KonnectExtensionClusterType is the type of the Konnect Control Plane.
type KonnectExtensionClusterType string

const (
	// ClusterTypeControlPlane is the type of the Konnect Control Plane.
	ClusterTypeControlPlane KonnectExtensionClusterType = "ControlPlane"
	// ClusterTypeK8sIngressController is the type of the Kubernetes Control Plane.
	ClusterTypeK8sIngressController KonnectExtensionClusterType = "K8SIngressController"
)

// KonnectExtensionControlPlaneStatus contains the Konnect Control Plane status information.
type KonnectExtensionControlPlaneStatus struct {
	// ControlPlaneID is the Konnect ID of the ControlPlane this KonnectExtension is associated with.
	//
	// +kubebuilder:validation:Required
	ControlPlaneID string `json:"controlPlaneID"`

	// ClusterType is the type of the Konnect Control Plane.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=ControlPlane;K8SIngressController
	ClusterType KonnectExtensionClusterType `json:"clusterType"`

	// Endpoints defines the Konnect endpoints for the control plane.
	//
	// +kubebuilder:validation:Required
	Endpoints KonnectEndpoints `json:"endpoints"`
}

// DataPlaneClientAuthStatus contains the status information related to the ClientAuth configuration.
type DataPlaneClientAuthStatus struct {
	// CertificateSecretRef is the reference to the Secret containing the client certificate.
	//
	// +kubebuilder:validation:Optional
	CertificateSecretRef *SecretRef `json:"certificateSecretRef,omitempty"`
}
