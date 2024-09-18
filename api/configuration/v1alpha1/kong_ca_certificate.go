package v1alpha1

import (
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// KongCACertificate is the schema for CACertificate API which defines a Kong CA Certificate.
//
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Programmed",description="The Resource is Programmed on Konnect",type=string,JSONPath=`.status.conditions[?(@.type=='Programmed')].status`
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.spec.controlPlaneRef) || has(self.spec.controlPlaneRef)", message="controlPlaneRef is required once set"
// +kubebuilder:validation:XValidation:rule="(!self.status.conditions.exists(c, c.type == 'Programmed' && c.status == 'True')) ? true : oldSelf.spec.controlPlaneRef == self.spec.controlPlaneRef", message="spec.controlPlaneRef is immutable when an entity is already Programmed"
type KongCACertificate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec KongCACertificateSpec `json:"spec"`

	// +kubebuilder:default={conditions: {{type: "Programmed", status: "Unknown", reason:"Pending", message:"Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}}
	Status KongCACertificateStatus `json:"status,omitempty"`
}

// GetKonnectStatus returns the Konnect status contained in the KongRoute status.
func (r *KongCACertificate) GetKonnectStatus() *konnectv1alpha1.KonnectEntityStatus {
	if r.Status.Konnect == nil {
		return nil
	}
	return &r.Status.Konnect.KonnectEntityStatus
}

// GetControlPlaneID returns the Konnect Control Plane ID of the KongCACertificate.
func (r *KongCACertificate) GetControlPlaneID() string {
	if r.Status.Konnect == nil {
		return ""
	}
	return r.Status.Konnect.ControlPlaneID
}

// SetControlPlaneID sets the Konnect Control Plane ID in the KongCACertificate status.
func (r *KongCACertificate) SetControlPlaneID(id string) {
	if r.Status.Konnect == nil {
		r.initKonnectStatus()
	}
	r.Status.Konnect.ControlPlaneID = id
}

// GetKonnectID returns the Konnect ID in the KongCACertificate status.
func (r *KongCACertificate) GetKonnectID() string {
	if r.Status.Konnect == nil {
		return ""
	}
	return r.Status.Konnect.ID
}

// SetKonnectID sets the Konnect ID in the KongCACertificate status.
func (r *KongCACertificate) SetKonnectID(id string) {
	if r.Status.Konnect == nil {
		r.initKonnectStatus()
	}
	r.Status.Konnect.ID = id
}

// GetTypeName returns the KongCACertificate Kind name.
func (r KongCACertificate) GetTypeName() string {
	return "KongCACertificate"
}

// GetConditions returns the Status Conditions.
func (r *KongCACertificate) GetConditions() []metav1.Condition {
	return r.Status.Conditions
}

// SetConditions sets the Status Conditions.
func (r *KongCACertificate) SetConditions(conditions []metav1.Condition) {
	r.Status.Conditions = conditions
}

func (r *KongCACertificate) initKonnectStatus() {
	r.Status.Konnect = &konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef{}
}

// KongCACertificateSpec contains the specification for the KongCACertificate.
type KongCACertificateSpec struct {
	// ControlPlaneRef references the Konnect Control Plane that this KongCACertificate should be created in.
	ControlPlaneRef          *ControlPlaneRef `json:"controlPlaneRef,omitempty"`
	KongCACertificateAPISpec `json:",inline"`
}

// KongCACertificateAPISpec contains the API specification for the KongCACertificate.
type KongCACertificateAPISpec struct {
	// Cert is the PEM-encoded CA certificate.
	// +kubebuilder:validation:Required
	Cert string `json:"cert,omitempty"`
	// Tags is an optional set of tags applied to the certificate.
	Tags []string `json:"tags,omitempty"`
}

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
type KongCACertificateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KongCACertificate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KongCACertificate{}, &KongCACertificateList{})
}
