package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// IdentityProviderRequest is the Schema for the identityproviderrequests API.
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
type IdentityProviderRequest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +optional
	Spec IdentityProviderRequestSpec `json:"spec,omitzero"`

	// +optional
	Status IdentityProviderRequestStatus `json:"status,omitzero"`
}

// IdentityProviderRequestList contains a list of IdentityProviderRequest.
//
// +kubebuilder:object:root=true
type IdentityProviderRequestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []IdentityProviderRequest `json:"items"`
}

// IdentityProviderRequestSpec defines the desired state of IdentityProviderRequest.
type IdentityProviderRequestSpec struct {
	// APISpec defines the desired state of the resource's API spec fields.
	//
	// +optional
	APISpec IdentityProviderRequestAPISpec `json:"apiSpec,omitzero"`
}

// IdentityProviderRequestAPISpec defines the API spec fields for IdentityProviderRequest.
type IdentityProviderRequestAPISpec struct {
	//
	//
	// +optional
	Config *Config `json:"config,omitempty"`

	// Indicates whether the identity provider is enabled.
	// Only one identity provider can be active at a time, such as SAML or OIDC.
	//
	//
	// +optional
	// +kubebuilder:default=false
	Enabled IdentityProviderEnabled `json:"enabled,omitempty"`

	// The path used for initiating login requests with the identity provider.
	//
	// +optional
	// +kubebuilder:validation:MaxLength=256
	LoginPath IdentityProviderLoginPath `json:"login_path,omitempty"`

	// Specifies the type of identity provider.
	//
	// +optional
	// +kubebuilder:validation:MaxLength=256
	// +kubebuilder:validation:Enum=oidc;saml
	Type IdentityProviderType `json:"type,omitempty"`
}

// IdentityProviderRequestStatus defines the observed state of IdentityProviderRequest.
type IdentityProviderRequestStatus struct {
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
	SchemeBuilder.Register(&IdentityProviderRequest{}, &IdentityProviderRequestList{})
}

// Config represents a union type for config.
// Only one of the fields should be set based on the Type.
type Config struct {
	// Type designates the type of configuration.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Enum=OIDC;SAML
	Type ConfigType `json:"type,omitempty"`

	// OIDC configuration.
	//
	// +optional
	OIDC *ConfigureOIDCIdentityProviderConfig `json:"oidc,omitempty"`
	// SAML configuration.
	//
	// +optional
	SAML *SAMLIdentityProviderConfig `json:"saml,omitempty"`
}

// ConfigType represents the type of config.
type ConfigType string

// ConfigType values.
const (
	ConfigTypeOIDC ConfigType = "OIDC"
	ConfigTypeSAML ConfigType = "SAML"
)
