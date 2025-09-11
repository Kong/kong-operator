package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func init() {
	SchemeBuilder.Register(&KonnectAPIAuthConfiguration{}, &KonnectAPIAuthConfigurationList{})
}

// KonnectAPIAuthConfiguration is the Schema for the Konnect configuration type.
//
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:resource:categories=kong;konnect
// +kubebuilder:object:root=true
// +kubebuilder:object:generate=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Valid",description="The API authentication information is valid",type=string,JSONPath=`.status.conditions[?(@.type=='APIAuthValid')].status`
// +kubebuilder:printcolumn:name="OrgID",description="Konnect Organization ID this API authentication configuration belongs to.",type=string,JSONPath=`.status.organizationID`
// +kubebuilder:printcolumn:name="ServerURL",description="Configured server URL.",type=string,JSONPath=`.status.serverURL`
// +kubebuilder:validation:XValidation:rule="self.spec.type != 'token' || (self.spec.token.startsWith('spat_') || self.spec.token.startsWith('kpat_'))", message="Konnect tokens have to start with spat_ or kpat_"
// +kubebuilder:validation:XValidation:rule="self.spec.type != 'token' || (!has(oldSelf.spec.token) || has(self.spec.token))", message="Token is required once set"
// +apireference:kgo:include
// +kong:channels=gateway-operator
type KonnectAPIAuthConfiguration struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec is the specification of the KonnectAPIAuthConfiguration resource.
	Spec KonnectAPIAuthConfigurationSpec `json:"spec,omitempty"`

	// Status is the status of the KonnectAPIAuthConfiguration resource.
	//
	// +optional
	Status KonnectAPIAuthConfigurationStatus `json:"status,omitempty"`
}

// KonnectAPIAuthType is the type of authentication used to authenticate with the Konnect API.
// +apireference:kgo:include
type KonnectAPIAuthType string

const (
	// KonnectAPIAuthTypeToken is the token authentication type.
	KonnectAPIAuthTypeToken KonnectAPIAuthType = "token"

	// KonnectAPIAuthTypeSecretRef is the secret reference authentication type.
	KonnectAPIAuthTypeSecretRef KonnectAPIAuthType = "secretRef"
)

// KonnectAPIAuthConfigurationSpec is the specification of the KonnectAPIAuthConfiguration resource.
//
// +kubebuilder:validation:XValidation:rule="self.type == 'token' ? size(self.token) > 0 : true", message="spec.token is required if auth type is set to token"
// +kubebuilder:validation:XValidation:rule="self.type == 'secretRef' ? has(self.secretRef) : true", message="spec.secretRef is required if auth type is set to secretRef"
// +kubebuilder:validation:XValidation:rule="!(has(self.token) && has(self.secretRef))", message="spec.token and spec.secretRef cannot be set at the same time"
// +apireference:kgo:include
type KonnectAPIAuthConfigurationSpec struct {
	// +required
	// +kubebuilder:validation:Enum=token;secretRef
	Type KonnectAPIAuthType `json:"type"`

	// Token is the Konnect token used to authenticate with the Konnect API.
	//
	// +optional
	Token string `json:"token,omitempty"`

	// SecretRef is a reference to a Kubernetes Secret containing the Konnect token.
	// This secret is required to have the konghq.com/credential label set to "konnect".
	//
	// +optional
	SecretRef *corev1.SecretReference `json:"secretRef,omitempty"`

	// ServerURL is the URL of the Konnect server.
	// It can be either a full URL with an HTTPs scheme or just a hostname.
	// Please refer to https://docs.konghq.com/konnect/network/ for the list of supported hostnames.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:XValidation:rule="size(self) > 0", message="Server URL is required"
	// +kubebuilder:validation:XValidation:rule="self == oldSelf", message="Server URL is immutable"
	// +kubebuilder:validation:XValidation:rule="isURL(self) ? url(self).getScheme() == 'https' : true", message="Server URL must use HTTPs if specifying scheme"
	// +kubebuilder:validation:XValidation:rule="size(self) > 0 && !isURL(self) ? self.matches('^(([a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9]).)*([A-Za-z0-9]|[A-Za-z0-9][A-Za-z0-9-]*[A-Za-z0-9])$') : true", message="Server URL must satisfy hostname (RFC 1123) regex if not a valid absolute URL"
	ServerURL string `json:"serverURL"`
}

// KonnectAPIAuthConfigurationStatus is the status of the KonnectAPIAuthConfiguration resource.
// +apireference:kgo:include
type KonnectAPIAuthConfigurationStatus struct {
	// Conditions describe the status of the Konnect configuration.
	//
	// +optional
	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=8
	// +kubebuilder:default={{type: "Valid", status: "Unknown", reason:"Pending", message:"Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`

	// OrganizationID is the unique identifier of the organization in Konnect.
	//
	// +optional
	OrganizationID string `json:"organizationID,omitempty"`

	// ServerURL is configured server URL.
	//
	// +optional
	ServerURL string `json:"serverURL,omitempty"`
}

// KonnectAPIAuthConfigurationList contains a list of KonnectAPIAuthConfiguration resources.
// +kubebuilder:object:root=true
// +apireference:kgo:include
type KonnectAPIAuthConfigurationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []KonnectAPIAuthConfiguration `json:"items"`
}
