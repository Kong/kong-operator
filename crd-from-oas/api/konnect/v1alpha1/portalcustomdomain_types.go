package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PortalCustomDomain is the Schema for the portalcustomdomains API.
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
type PortalCustomDomain struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +optional
	Spec PortalCustomDomainSpec `json:"spec,omitzero"`

	// +optional
	Status PortalCustomDomainStatus `json:"status,omitzero"`
}

// PortalCustomDomainList contains a list of PortalCustomDomain.
//
// +kubebuilder:object:root=true
type PortalCustomDomainList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []PortalCustomDomain `json:"items"`
}

// PortalCustomDomainSpec defines the desired state of PortalCustomDomain.
type PortalCustomDomainSpec struct {
	// PortalRef is the reference to the parent Portal object.
	//
	// +required
	PortalRef ObjectRef `json:"portal_ref,omitzero"`

	PortalCustomDomainAPISpec `json:",inline"`
}

// PortalCustomDomainAPISpec defines the API spec fields for PortalCustomDomain.
type PortalCustomDomainAPISpec struct {
	//
	//
	// +required
	Enabled *bool `json:"enabled,omitempty"`

	//
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	Hostname string `json:"hostname,omitempty"`

	//
	//
	// +required
	SSL CreatePortalCustomDomainSSL `json:"ssl,omitempty"`
}

// PortalCustomDomainStatus defines the observed state of PortalCustomDomain.
type PortalCustomDomainStatus struct {
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
	SchemeBuilder.Register(&PortalCustomDomain{}, &PortalCustomDomainList{})
}

// SSL represents a union type for ssl.
// Only one of the fields should be set based on the Type.
//
type SSL struct {
	// Type designates the type of configuration.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Enum=WithCustomCertificate;Standard
	Type SSLType `json:"type,omitempty"`

	// WithCustomCertificate configuration.
	//
	// +optional
	WithCustomCertificate *CreatePortalCustomDomainSSLWithCustomCertificate `json:"withcustomcertificate,omitempty"`
	// Standard configuration.
	//
	// +optional
	Standard *CreatePortalCustomDomainSSLStandard `json:"standard,omitempty"`
}

// SSLType represents the type of ssl.
type SSLType string

// SSLType values.
const (
	SSLTypeWithCustomCertificate SSLType = "WithCustomCertificate"
	SSLTypeStandard SSLType = "Standard"
)
