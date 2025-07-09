package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha1"
)

// KongCACertificate is the schema for CACertificate API which defines a Kong CA Certificate.
//
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:resource:categories=kong
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Programmed",description="The Resource is Programmed on Konnect",type=string,JSONPath=`.status.conditions[?(@.type=='Programmed')].status`
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.spec.controlPlaneRef) || has(self.spec.controlPlaneRef)", message="controlPlaneRef is required once set"
// +kubebuilder:validation:XValidation:rule="(!has(self.spec.controlPlaneRef) || !has(self.spec.controlPlaneRef.konnectNamespacedRef)) ? true : !has(self.spec.controlPlaneRef.konnectNamespacedRef.__namespace__)", message="spec.controlPlaneRef cannot specify namespace for namespaced resource"
// +kubebuilder:validation:XValidation:rule="(!self.status.conditions.exists(c, c.type == 'Programmed' && c.status == 'True')) ? true : oldSelf.spec.controlPlaneRef == self.spec.controlPlaneRef", message="spec.controlPlaneRef is immutable when an entity is already Programmed"
// +apireference:kgo:include
// +kong:channels=gateway-operator
type KongCACertificate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec KongCACertificateSpec `json:"spec"`

	// +kubebuilder:default={conditions: {{type: "Programmed", status: "Unknown", reason:"Pending", message:"Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}}
	Status KongCACertificateStatus `json:"status,omitempty"`
}

// KongCACertificateSpec contains the specification for the KongCACertificate.
// +kubebuilder:validation:XValidation:rule="!has(self.controlPlaneRef) ? true : self.controlPlaneRef.type != 'kic'", message="KIC is not supported as control plane"
// +apireference:kgo:include
type KongCACertificateSpec struct {
	// ControlPlaneRef references the Konnect Control Plane that this KongCACertificate should be created in.
	// +kubebuilder:validation:XValidation:message="'konnectID' type is not supported", rule="self.type != 'konnectID'"
	// +required
	ControlPlaneRef *commonv1alpha1.ControlPlaneRef `json:"controlPlaneRef"`

	KongCACertificateAPISpec `json:",inline"`
}

// KongCACertificateAPISpec contains the API specification for the KongCACertificate.
// +apireference:kgo:include
type KongCACertificateAPISpec struct {
	// Cert is the PEM-encoded CA certificate.
	// +required
	Cert string `json:"cert,omitempty"`

	// Tags is an optional set of tags applied to the certificate.
	Tags commonv1alpha1.Tags `json:"tags,omitempty"`
}

// KongCACertificateStatus defines the observed state of KongCACertificate.
// +apireference:kgo:include
type KongCACertificateStatus struct {
	// Konnect contains the Konnect entity status.
	// +optional
	Konnect *konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef `json:"konnect,omitempty"`

	// Conditions describe the status of the Konnect entity.
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=8
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// KongCACertificateList contains a list of KongCACertificates.
// +kubebuilder:object:root=true
// +apireference:kgo:include
type KongCACertificateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KongCACertificate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KongCACertificate{}, &KongCACertificateList{})
}
