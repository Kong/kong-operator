package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha1"
)

// KongCertificate is the schema for Certificate API which defines a Kong Certificate.
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
type KongCertificate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec KongCertificateSpec `json:"spec"`

	// +kubebuilder:default={conditions: {{type: "Programmed", status: "Unknown", reason:"Pending", message:"Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}}
	Status KongCertificateStatus `json:"status,omitempty"`
}

// KongCertificateSpec contains the specification for the KongCertificate.
// +kubebuilder:validation:XValidation:rule="!has(self.controlPlaneRef) ? true : self.controlPlaneRef.type != 'kic'", message="KIC is not supported as control plane"
// +apireference:kgo:include
type KongCertificateSpec struct {
	// ControlPlaneRef references the Konnect Control Plane that this KongCertificate should be created in.
	// +kubebuilder:validation:XValidation:message="'konnectID' type is not supported", rule="self.type != 'konnectID'"
	// +required
	ControlPlaneRef *commonv1alpha1.ControlPlaneRef `json:"controlPlaneRef"`

	KongCertificateAPISpec `json:",inline"`
}

// KongCertificateAPISpec contains the API specification for the KongCertificate.
// +apireference:kgo:include
type KongCertificateAPISpec struct {
	// Cert is the PEM-encoded certificate.
	// +required
	Cert string `json:"cert,omitempty"`
	// CertAlt is the PEM-encoded certificate.
	// This should only be set if you have both RSA and ECDSA types of
	// certificate available and would like Kong to prefer serving using ECDSA certs
	// when client advertises support for it.
	CertAlt string `json:"cert_alt,omitempty"`
	// Key is the PEM-encoded private key.
	// +required
	Key string `json:"key,omitempty"`
	// KeyAlt is the PEM-encoded private key.
	// This should only be set if you have both RSA and ECDSA types of
	// certificate available and would like Kong to prefer serving using ECDSA certs
	// when client advertises support for it.
	KeyAlt string `json:"key_alt,omitempty"`

	// Tags is an optional set of tags applied to the certificate.
	Tags commonv1alpha1.Tags `json:"tags,omitempty"`
}

// KongCertificateStatus defines the observed state of KongCertificate.
// +apireference:kgo:include
type KongCertificateStatus struct {
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

// KongCertificateList contains a list of KongCertificates.
// +kubebuilder:object:root=true
// +apireference:kgo:include
type KongCertificateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KongCertificate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KongCertificate{}, &KongCertificateList{})
}
