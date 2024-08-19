package v1alpha1

import (
	sdkkonnectgocomp "github.com/Kong/sdk-konnect-go/models/components"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func init() {
	SchemeBuilder.Register(&KonnectControlPlane{}, &KonnectControlPlaneList{})
}

// KonnectControlPlane is the Schema for the KonnectControlplanes API.
//
// +genclient
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:object:root=true
// +kubebuilder:object:generate=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Programmed",description="The Resource is Programmed on Konnect",type=string,JSONPath=`.status.conditions[?(@.type=='Programmed')].status`
// +kubebuilder:printcolumn:name="ID",description="Konnect ID",type=string,JSONPath=`.status.id`
// +kubebuilder:printcolumn:name="OrgID",description="Konnect Organization ID this resource belongs to.",type=string,JSONPath=`.status.organizationID`
// +kubebuilder:validation:XValidation:rule="!self.status.conditions.exists(c, c.type == 'Programmed' && c.status == 'True') ? true : self.spec.konnect.authRef == oldSelf.spec.konnect.authRef", message="spec.konnect.authRef is immutable when entity is already Programmed."
// +kubebuilder:validation:XValidation:rule="!self.status.conditions.exists(c, c.type == 'APIAuthValid' && c.status == 'True') ? true : self.spec.konnect.authRef == oldSelf.spec.konnect.authRef", message="spec.konnect.authRef is immutable when entity refers to a Valid API Auth Configuration."
type KonnectControlPlane struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the desired state of KonnectControlPlane.
	Spec KonnectControlPlaneSpec `json:"spec,omitempty"`

	// Status defines the observed state of KonnectControlPlane.
	Status KonnectControlPlaneStatus `json:"status,omitempty"`
}

// KonnectControlPlaneSpec defines the desired state of KonnectControlPlane.
type KonnectControlPlaneSpec struct {
	sdkkonnectgocomp.CreateControlPlaneRequest `json:",inline"`

	KonnectConfiguration KonnectConfiguration `json:"konnect,omitempty"`
}

// KonnectControlPlaneStatus defines the observed state of KonnectControlPlane.
type KonnectControlPlaneStatus struct {
	KonnectEntityStatus `json:",inline"`

	// Conditions describe the current conditions of the KonnectControlPlane.
	//
	// Known condition types are:
	//
	// * "Programmed"
	//
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MaxItems=8
	// +kubebuilder:default={{type: "Programmed", status: "Unknown", reason:"Pending", message:"Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// GetKonnectStatus returns the Konnect Status of the KonnectControlPlane.
func (c *KonnectControlPlane) GetKonnectStatus() *KonnectEntityStatus {
	return &c.Status.KonnectEntityStatus
}

// GetTypeName returns the KonnectControlPlane type name.
func (c KonnectControlPlane) GetTypeName() string {
	return "KonnectControlPlane"
}

// GetKonnectLabels gets the Konnect Labels from object's spec.
func (c *KonnectControlPlane) GetKonnectLabels() map[string]string {
	return c.Spec.Labels
}

// SetKonnectLabels sets the Konnect Labels in object's spec.
func (c *KonnectControlPlane) SetKonnectLabels(labels map[string]string) {
	c.Spec.Labels = labels
}

func (c *KonnectControlPlane) SetKonnectID(id string) {
	c.Status.ID = id
}

// GetKonnectAPIAuthConfigurationRef returns the Konnect API Auth Configuration Ref.
func (c *KonnectControlPlane) GetKonnectAPIAuthConfigurationRef() KonnectAPIAuthConfigurationRef {
	return c.Spec.KonnectConfiguration.APIAuthConfigurationRef
}

// GetConditions returns the Status Conditions
func (c *KonnectControlPlane) GetConditions() []metav1.Condition {
	return c.Status.Conditions
}

// SetConditions sets the Status Conditions
func (c *KonnectControlPlane) SetConditions(conditions []metav1.Condition) {
	c.Status.Conditions = conditions
}

// +kubebuilder:object:root=true
type KonnectControlPlaneList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []KonnectControlPlane `json:"items"`
}
