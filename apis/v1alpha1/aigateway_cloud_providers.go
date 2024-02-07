package v1alpha1

// -----------------------------------------------------------------------------
// AIGateway API - Cloud Providers - Supported Providers
// -----------------------------------------------------------------------------

// AICloudProviderName indicates the unique name of a supported AI cloud
// provider.
type AICloudProviderName string

const (
	// AICloudProviderOpenAI is the OpenAI cloud provider.
	//
	// They are known for models such as ChatGPT 3.5, 4, Dall-e, e.t.c.
	AICloudProviderOpenAI AICloudProviderName = "OpenAI"

	// AICloudProviderAzure is the Azure cloud provider.
	//
	// They are known for models such as PHI-2.
	AICloudProviderAzure AICloudProviderName = "Azure"

	// AICloudProviderCohere is the Cohere cloud provider.
	//
	// They are known for models such as Cohere-Embed, and Cohere-Rerank.
	AICloudProviderCohere AICloudProviderName = "Cohere"
)

// -----------------------------------------------------------------------------
// AIGateway API - Cloud Providers - Types
// -----------------------------------------------------------------------------

// AICloudProvider is the organization that provides API access to Large Language
// Models (LLMs).
type AICloudProvider struct {
	// Name is the unique name of an LLM provider.
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=OpenAI;Azure;Cohere
	Name AICloudProviderName `json:"name"`

	// APITokenRef indicates the reference to the API token to communicate with
	// and enable access to the API provided by the cloud provider for access
	// to their AI models.
	//
	// +kubebuilder:validation:Required
	APITokenRef AICloudProviderAPITokenRef `json:"apiTokenRef"`
}

// AICloudProviderAPITokenRef is an reference to another object which contains
// the API token for an AI cloud provider.
type AICloudProviderAPITokenRef struct {
	// Name is the name of the reference object.
	//
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Namespace is the namespace of the reference object.
	//
	// If not specified, it will be assumed to be the same namespace as the
	// object which references it.
	//
	// +kubebuilder:validation:Optional
	Namespace *string `json:"namespace,omitempty"`

	// Kind is the API object kind
	//
	// If not specified, it will be assumed to be "Secret". If a Secret is used
	// as the Kind, the secret must contain a single key-value pair where the
	// value is the secret API token. The key can be named anything, as long as
	// there's only one entry, but by convention it should be "apiToken".
	//
	// +kubebuilder:validation:Optional
	Kind *string `json:"kind,omitempty"`
}
