package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Portal is the Schema for the portals API.
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
type Portal struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +optional
	Spec PortalSpec `json:"spec,omitzero"`

	// +optional
	Status PortalStatus `json:"status,omitzero"`
}

// PortalList contains a list of Portal.
//
// +kubebuilder:object:root=true
type PortalList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []Portal `json:"items"`
}

// PortalSpec defines the desired state of Portal.
type PortalSpec struct {
	// APISpec defines the desired state of the resource's API spec fields.
	//
	// +optional
	APISpec PortalAPISpec `json:"apiSpec,omitzero"`
}

// PortalAPISpec defines the API spec fields for Portal.
type PortalAPISpec struct {
	// Whether the portal supports developer authentication.
	// If disabled, developers cannot register for accounts or create applications.
	//
	// +optional
	// +kubebuilder:default=true
	AuthenticationEnabled bool `json:"authentication_enabled,omitempty"`

	// Whether requests from applications to register for APIs will be
	// automatically approved, or if they will be set to pending until approved by
	// an admin.
	//
	// +optional
	// +kubebuilder:default=false
	AutoApproveApplications bool `json:"auto_approve_applications,omitempty"`

	// Whether developer account registrations will be automatically approved, or
	// if they will be set to pending until approved by an admin.
	//
	// +optional
	// +kubebuilder:default=false
	AutoApproveDevelopers bool `json:"auto_approve_developers,omitempty"`

	// The default visibility of APIs in the portal.
	// If set to `public`, newly published APIs are visible to unauthenticated
	// developers.
	// If set to `private`, newly published APIs are hidden from unauthenticated
	// developers.
	//
	// +optional
	// +kubebuilder:validation:MaxLength=256
	// +kubebuilder:validation:Enum=public;private
	DefaultAPIVisibility string `json:"default_api_visibility,omitempty"`

	// The default authentication strategy for APIs published to the portal.
	// Newly published APIs will use this authentication strategy unless overridden
	// during publication.
	// If set to `null`, API publications will not use an authentication strategy
	// unless set during publication.
	//
	// +optional
	// +kubebuilder:validation:MaxLength=256
	DefaultApplicationAuthStrategyIDRef *ObjectRef `json:"default_application_auth_strategy_id_ref,omitempty"`

	// The default visibility of pages in the portal.
	// If set to `public`, newly created pages are visible to unauthenticated
	// developers.
	// If set to `private`, newly created pages are hidden from unauthenticated
	// developers.
	//
	// +optional
	// +kubebuilder:validation:MaxLength=256
	// +kubebuilder:validation:Enum=public;private
	DefaultPageVisibility string `json:"default_page_visibility,omitempty"`

	// A description of the portal.
	//
	// +optional
	// +kubebuilder:validation:MaxLength=512
	Description *string `json:"description,omitempty"`

	// The display name of the portal.
	// This value will be the portal's `name` in Portal API.
	//
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=255
	DisplayName string `json:"display_name,omitempty"`

	// Labels store metadata of an entity that can be used for filtering an entity
	// list or for searching across entity types.
	//
	// Labels are intended to store **INTERNAL** metadata.
	//
	// Keys must be of length 1-63 characters, and cannot start with "kong",
	// "konnect", "mesh", "kic", or "_".
	//
	//
	// +optional
	Labels LabelsUpdate `json:"labels,omitempty"`

	// The name of the portal, used to distinguish it from other portals.
	// Name must be unique.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=255
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="name is immutable"
	Name string `json:"name,omitempty"`

	// Whether the portal resources are protected by Role Based Access Control
	// (RBAC).
	// If enabled, developers view or register for APIs until unless assigned to
	// teams with access to view and consume specific APIs.
	// Authentication must be enabled to use RBAC.
	//
	// +optional
	// +kubebuilder:default=false
	RBACEnabled bool `json:"rbac_enabled,omitempty"`
}

// PortalStatus defines the observed state of Portal.
type PortalStatus struct {
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

	// ObservedGeneration is the most recent generation observed
	//
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

func init() {
	SchemeBuilder.Register(&Portal{}, &PortalList{})
}
