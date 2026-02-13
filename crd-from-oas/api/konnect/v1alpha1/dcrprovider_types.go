package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DcrProvider is the Schema for the dcrproviders API.
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
type DcrProvider struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +optional
	Spec DcrProviderSpec `json:"spec,omitzero"`

	// +optional
	Status DcrProviderStatus `json:"status,omitzero"`
}

// DcrProviderList contains a list of DcrProvider.
//
// +kubebuilder:object:root=true
type DcrProviderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []DcrProvider `json:"items"`
}

// DcrProviderSpec defines the desired state of DcrProvider.
type DcrProviderSpec struct {
	// APISpec defines the desired state of the resource's API spec fields.
	//
	// +optional
	APISpec DcrProviderAPISpec `json:"apiSpec,omitzero"`
}

// DcrProviderAPISpec defines the API spec fields for DcrProvider.
type DcrProviderAPISpec struct {
	// DcrProviderConfig embeds the union type configuration.
	//
	// +optional
	*DcrProviderConfig `json:",inline"`
}

// DcrProviderStatus defines the observed state of DcrProvider.
type DcrProviderStatus struct {
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
	SchemeBuilder.Register(&DcrProvider{}, &DcrProviderList{})
}

// DcrProviderConfig represents a union type for DcrProviderConfig.
// Only one of the fields should be set based on the Type.
type DcrProviderConfig struct {
	// Type designates the type of configuration.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Enum=Auth0;AzureAd;Curity;Okta;Http
	Type DcrProviderConfigType `json:"type,omitempty"`

	// Auth0 configuration.
	//
	// +optional
	Auth0 *CreateDcrProviderRequestAuth0 `json:"auth0,omitempty"`
	// AzureAd configuration.
	//
	// +optional
	AzureAd *CreateDcrProviderRequestAzureAd `json:"azuread,omitempty"`
	// Curity configuration.
	//
	// +optional
	Curity *CreateDcrProviderRequestCurity `json:"curity,omitempty"`
	// Okta configuration.
	//
	// +optional
	Okta *CreateDcrProviderRequestOkta `json:"okta,omitempty"`
	// Http configuration.
	//
	// +optional
	Http *CreateDcrProviderRequestHttp `json:"http,omitempty"`
}

// DcrProviderConfigType represents the type of DcrProviderConfig.
type DcrProviderConfigType string

// DcrProviderConfigType values.
const (
	DcrProviderConfigTypeAuth0   DcrProviderConfigType = "Auth0"
	DcrProviderConfigTypeAzureAd DcrProviderConfigType = "AzureAd"
	DcrProviderConfigTypeCurity  DcrProviderConfigType = "Curity"
	DcrProviderConfigTypeOkta    DcrProviderConfigType = "Okta"
	DcrProviderConfigTypeHttp    DcrProviderConfigType = "Http"
)
