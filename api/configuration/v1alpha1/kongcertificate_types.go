package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
)

// KongCertificateSourceType is the type of source for the certificate data.
type KongCertificateSourceType string

const (
	// KongCertificateSourceTypeInline indicates that the certificate data is provided inline in the spec.
	KongCertificateSourceTypeInline KongCertificateSourceType = "inline"
	// KongCertificateSourceTypeSecretRef indicates that the certificate data is sourced from a Kubernetes Secret.
	KongCertificateSourceTypeSecretRef KongCertificateSourceType = "secretRef"
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
// +kong:channels=kong-operator
type KongCertificate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec KongCertificateSpec `json:"spec"`

	// +kubebuilder:default={conditions: {{type: "Programmed", status: "Unknown", reason:"Pending", message:"Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}}
	Status KongCertificateStatus `json:"status,omitempty"`
}

// KongCertificateSpec contains the specification for the KongCertificate.
// +kubebuilder:validation:XValidation:rule="!has(self.controlPlaneRef) ? true : self.controlPlaneRef.type != 'kic'", message="KIC is not supported as control plane"
// +kubebuilder:validation:XValidation:rule="!has(self.adopt) ? true : (has(self.controlPlaneRef) && self.controlPlaneRef.type == 'konnectNamespacedRef')", message="spec.adopt is allowed only when controlPlaneRef is konnectNamespacedRef"
// +kubebuilder:validation:XValidation:rule="(has(oldSelf.adopt) && has(self.adopt)) || (!has(oldSelf.adopt) && !has(self.adopt))", message="Cannot set or unset spec.adopt in updates"
// +kubebuilder:validation:XValidation:rule="self.type != 'inline' || (has(self.cert) && self.cert.size() != 0)", message="spec.cert is required when type is 'inline'"
// +kubebuilder:validation:XValidation:rule="self.type != 'inline' || (has(self.key) && self.key.size() != 0)", message="spec.key is required when type is 'inline'"
// +kubebuilder:validation:XValidation:rule="self.type != 'secretRef' ||  has(self.secretRef)", message="spec.secretRef is required when type is 'secretRef'"// +kubebuilder:validation:XValidation:rule="!((has(self.cert) || has(self.key)) && (has(self.secretRef) || has(self.secretRefAlt)))", message="cert/key and secretRef/secretRefAlt cannot be set at the same time"
// +kubebuilder:validation:XValidation:rule="!((has(self.cert_alt) || has(self.key_alt)) && (has(self.secretRef) || has(self.secretRefAlt)))", message="cert_alt/key_alt and secretRef/secretRefAlt cannot be set at the same time"
// +apireference:kgo:include
type KongCertificateSpec struct {
	// Type indicates the source of the certificate data.
	// Can be 'inline' or 'secretRef'.
	// +kubebuilder:validation:Enum=inline;secretRef
	// +kubebuilder:default=inline
	// +optional
	Type *KongCertificateSourceType `json:"type,omitempty"`

	// ControlPlaneRef references the Konnect Control Plane that this KongCertificate should be created in.
	// +kubebuilder:validation:XValidation:message="'konnectID' type is not supported", rule="self.type != 'konnectID'"
	// +required
	ControlPlaneRef *commonv1alpha1.ControlPlaneRef `json:"controlPlaneRef"`

	// Adopt is the options for adopting a certificate from an existing certificate in Konnect.
	// +optional
	Adopt *commonv1alpha1.AdoptOptions `json:"adopt,omitempty"`

	// SecretRef is a reference to a Kubernetes Secret containing the certificate and key.
	// This field is used when type is 'secretRef'.
	// The Secret must contain keys named 'tls.crt' and 'tls.key'.
	// +optional
	SecretRef *corev1.SecretReference `json:"secretRef,omitempty"`

	// SecretRefAlt is a reference to a Kubernetes Secret containing the alternative certificate and key.
	// This should only be set if you have both RSA and ECDSA types of certificate available
	// and would like Kong to prefer serving using ECDSA certs when client advertises support for it.
	// This field is used when type is 'secretRef'.
	// The Secret must contain keys named 'tls.crt' and 'tls.key'.
	// +optional
	SecretRefAlt *corev1.SecretReference `json:"secretRefAlt,omitempty"`

	KongCertificateAPISpec `json:",inline"`
}

// KongCertificateAPISpec contains the API specification for the KongCertificate.
// +apireference:kgo:include
type KongCertificateAPISpec struct {
	// Cert is the PEM-encoded certificate.
	// This field is used when type is 'inline'.
	// +optional
	Cert string `json:"cert,omitempty"`
	// CertAlt is the PEM-encoded certificate.
	// This should only be set if you have both RSA and ECDSA types of
	// certificate available and would like Kong to prefer serving using ECDSA certs
	// when client advertises support for it.
	// This field is used when type is 'inline'.
	// +optional
	CertAlt string `json:"cert_alt,omitempty"`
	// Key is the PEM-encoded private key.
	// This field is used when type is 'inline'.
	// +optional
	Key string `json:"key,omitempty"`
	// KeyAlt is the PEM-encoded private key.
	// This should only be set if you have both RSA and ECDSA types of
	// certificate available and would like Kong to prefer serving using ECDSA certs
	// when client advertises support for it.
	// This field is used when type is 'inline'.
	// +optional
	KeyAlt string `json:"key_alt,omitempty"`

	// Tags is an optional set of tags applied to the certificate.
	// Tags will be applied when type is 'inline' or 'secretRef'.
	// This field allows you to attach metadata to the certificate for identification or organization purposes.
	Tags commonv1alpha1.Tags `json:"tags,omitempty"`
}

// KongCertificateStatus defines the observed state of KongCertificate.
// +apireference:kgo:include
type KongCertificateStatus struct {
	// Konnect contains the Konnect entity status.
	// +optional
	Konnect *konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef `json:"konnect,omitempty"`

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
