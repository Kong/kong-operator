package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DeveloperToTeam is the Schema for the developertoteams API.
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
type DeveloperToTeam struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +optional
	Spec DeveloperToTeamSpec `json:"spec,omitzero"`

	// +optional
	Status DeveloperToTeamStatus `json:"status,omitzero"`
}

// DeveloperToTeamList contains a list of DeveloperToTeam.
//
// +kubebuilder:object:root=true
type DeveloperToTeamList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []DeveloperToTeam `json:"items"`
}

// DeveloperToTeamSpec defines the desired state of DeveloperToTeam.
type DeveloperToTeamSpec struct {
	// PortalRef is the reference to the parent Portal object.
	//
	// +required
	PortalRef ObjectRef `json:"portal_ref,omitzero"`

	// TeamRef is the reference to the parent Team object.
	//
	// +required
	TeamRef ObjectRef `json:"team_ref,omitzero"`

	// APISpec defines the desired state of the resource's API spec fields.
	//
	// +optional
	APISpec DeveloperToTeamAPISpec `json:"apiSpec,omitzero"`
}

// DeveloperToTeamAPISpec defines the API spec fields for DeveloperToTeam.
type DeveloperToTeamAPISpec struct {
}

// DeveloperToTeamStatus defines the observed state of DeveloperToTeam.
type DeveloperToTeamStatus struct {
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

	// TeamID is the Konnect ID of the parent Team.
	//
	// +optional
	TeamID *KonnectEntityRef `json:"teamID,omitempty"`

	// ObservedGeneration is the most recent generation observed
	//
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

func init() {
	SchemeBuilder.Register(&DeveloperToTeam{}, &DeveloperToTeamList{})
}
