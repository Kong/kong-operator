package v1alpha1

// AIGatewayPolicyConfigSourceType is the type of the source of the configuration for an AIGatewayPolicy.
type AIGatewayPolicyConfigSourceType string

const (
	// AIGatewayPolicyConfigSourceTypeSecret indicates that the configuration for the AIGatewayPolicy is sourced from a Kubernetes Secret.
	AIGatewayPolicyConfigSourceTypeSecret = "Secret"
)

// AIGatewayPolicyConfigFrom specifies the source of the configuration for an AIGatewayPolicy.
// Now it only supports sourcing from a Kubernetes Secret.
type AIGatewayPolicyConfigFrom struct {
	// SourceType specifies the type of the source of the configuration. Currently, only "Secret" is supported.
	// +kubebuilder:validation:Enum=Secret
	SourceType AIGatewayPolicyConfigSourceType `json:"type"`
	// SecretRef is a reference to a Kubernetes Secret that contains the configuration for the AIGatewayPolicy.
	// The specified key in `data` of the secret is used .
	// +optional
	SecretRef *SensitiveDataSecretRef `json:"secretRef,omitempty"`
}
