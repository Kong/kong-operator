package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PortalTeam is the Schema for the portalteams API.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="ID",description="Konnect ID",type="string",JSONPath=".status.id"
// +kubebuilder:printcolumn:name="Programmed",description="The Resource is Programmed on Konnect",type=string,JSONPath=`.status.conditions[?(@.type=='Programmed')].status`
// +kubebuilder:printcolumn:name="OrgID",description="Konnect Organization ID this resource belongs to.",type=string,JSONPath=`.status.organizationID`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:storageversion
// +apireference:kgo:include
// +kong:channels=kong-operator
type PortalTeam struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +optional
	Spec PortalTeamSpec `json:"spec,omitzero"`

	// +optional
	Status PortalTeamStatus `json:"status,omitzero"`
}

// PortalTeamList contains a list of PortalTeam.
//
// +kubebuilder:object:root=true
type PortalTeamList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []PortalTeam `json:"items"`
}

// PortalTeamSpec defines the desired state of PortalTeam.
type PortalTeamSpec struct {
	// PortalRef is the reference to the parent Portal object.
	//
	// +required
	PortalRef ObjectRef `json:"portal_ref,omitzero"`

	// APISpec defines the desired state of the resource's API spec fields.
	//
	// +optional
	APISpec PortalTeamAPISpec `json:"apiSpec,omitzero"`
}

// PortalTeamAPISpec defines the API spec fields for PortalTeam.
type PortalTeamAPISpec struct {
	//
	//
	// +optional
	// +kubebuilder:validation:MaxLength=250
	Description string `json:"description,omitempty"`

	//
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	// +kubebuilder:validation:Pattern=`^[\w \W]+$`
	Name string `json:"name,omitempty"`
}

// PortalTeamStatus defines the observed state of PortalTeam.
type PortalTeamStatus struct {
	// Conditions represent the current state of the resource.
	//
	// +optional
	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	// +kubebuilder:validation:MaxItems=8
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`

	// Konnect contains the Konnect entity status.
	//
	// +optional
	KonnectEntityStatus `json:",inline"`

	// PortalID is the Konnect ID of the parent Portal.
	//
	// +optional
	PortalID *KonnectEntityRef `json:"portalID,omitempty"`

	// ObservedGeneration is the most recent generation observed
	//
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

func init() {
	SchemeBuilder.Register(&PortalTeam{}, &PortalTeamList{})
}
