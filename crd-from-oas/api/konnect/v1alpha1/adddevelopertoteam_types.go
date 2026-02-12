package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AddDeveloperToTeam is the Schema for the adddevelopertoteams API.
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
type AddDeveloperToTeam struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +optional
	Spec AddDeveloperToTeamSpec `json:"spec,omitzero"`

	// +optional
	Status AddDeveloperToTeamStatus `json:"status,omitzero"`
}

// AddDeveloperToTeamList contains a list of AddDeveloperToTeam.
//
// +kubebuilder:object:root=true
type AddDeveloperToTeamList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []AddDeveloperToTeam `json:"items"`
}

// AddDeveloperToTeamSpec defines the desired state of AddDeveloperToTeam.
type AddDeveloperToTeamSpec struct {
	// PortalRef is the reference to the parent Portal object.
	//
	// +required
	PortalRef ObjectRef `json:"portal_ref,omitzero"`

	// TeamRef is the reference to the parent Team object.
	//
	// +required
	TeamRef ObjectRef `json:"team_ref,omitzero"`

	AddDeveloperToTeamAPISpec `json:",inline"`
}

// AddDeveloperToTeamAPISpec defines the API spec fields for AddDeveloperToTeam.
type AddDeveloperToTeamAPISpec struct {
}

// AddDeveloperToTeamStatus defines the observed state of AddDeveloperToTeam.
type AddDeveloperToTeamStatus struct {
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
	SchemeBuilder.Register(&AddDeveloperToTeam{}, &AddDeveloperToTeamList{})
}
