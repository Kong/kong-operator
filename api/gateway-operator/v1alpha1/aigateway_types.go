package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// -----------------------------------------------------------------------------
// AIGateway API - Resources
// -----------------------------------------------------------------------------

// AIGateway is a network Gateway enabling access and management for AI &
// Machine Learning models such as Large Language Models (LLM).
//
// The underlying technology for the AIGateway is the Kong Gateway configured
// with a variety of plugins which provide the the AI featureset.
//
// This is a list of the plugins, which are available in Kong Gateway v3.6.x+:
//
//   - ai-proxy (https://github.com/kong/kong/tree/master/kong/plugins/ai-proxy)
//   - ai-request-transformer (https://github.com/kong/kong/tree/master/kong/plugins/ai-request-transformer)
//   - ai-response-transformers (https://github.com/kong/kong/tree/master/kong/plugins/ai-response-transformer)
//   - ai-prompt-template (https://github.com/kong/kong/tree/master/kong/plugins/ai-prompt-template)
//   - ai-prompt-guard-plugin (https://github.com/kong/kong/tree/master/kong/plugins/ai-prompt-guard)
//   - ai-prompt-decorator-plugin (https://github.com/kong/kong/tree/master/kong/plugins/ai-prompt-decorator)
//
// So effectively the AIGateway resource provides a bespoke Gateway resource
// (which it owns and manages) with the gateway, consumers and plugin
// configurations automated and configurable via Kubernetes APIs.
//
// The current iteration only supports the proxy itself, but the API is being
// built with room for future growth in several dimensions. For instance:
//
//   - Supporting auxiliary functions (e.g. decorator, guard, templater, token-rate-limit)
//   - Supporting request/response transformers
//   - Supporting more than just LLMs (e.g. CCNs, GANs, e.t.c.)
//   - Supporting more hosting options for LLMs (e.g. self hosted)
//   - Supporting more AI cloud providers
//   - Supporting more AI cloud provider features
//
// The validation rules throughout are set up to ensure at least one
// cloud-provider-based LLM is specified, but in the future when we have more
// model types and more hosting options for those types so we may want to look
// into using CEL validation to ensure that at least one model configuration is
// provided. We may also want to use CEL to validate things like identifier
// unique-ness, e.t.c.
//
// See: https://kubernetes.io/docs/reference/using-api/cel/
//
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Endpoint",type="string",JSONPath=".status.endpoint",description="The URL endpoint for the AIGateway"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`,description="Age"
// +apireference:kgo:include
// +kong:channels=gateway-operator
type AIGateway struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec is the desired state of the AIGateway.
	//
	// +kubebuilder:validation:XValidation:message="At least one type of LLM has been specified",rule="(self.largeLanguageModels != null)"
	Spec AIGatewaySpec `json:"spec,omitempty"`

	// Status is the observed state of the AIGateway.
	Status AIGatewayStatus `json:"status,omitempty"`
}

// AIGatewayList contains a list of AIGateways.
//
// +kubebuilder:object:root=true
// +apireference:kgo:include
type AIGatewayList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is the list of AIGateways.
	Items []AIGateway `json:"items"`
}

// -----------------------------------------------------------------------------
// AIGateway API - Specification
// -----------------------------------------------------------------------------

// AIGatewaySpec defines the desired state of an AIGateway.
// +apireference:kgo:include
type AIGatewaySpec struct {
	// GatewayClassName is the name of the GatewayClass which is responsible for
	// the AIGateway.
	//
	// +required
	GatewayClassName string `json:"gatewayClassName"`

	// LargeLanguageModels is a list of Large Language Models (LLMs) to be
	// managed by the AI Gateway.
	//
	// This is a required field because we only support LLMs at the moment. In
	// future iterations we may support other model types.
	//
	// +required
	// +kubebuilder:validation:XValidation:message="At least one class of LLMs has been configured",rule="(self.cloudHosted.size() != 0)"
	LargeLanguageModels *LargeLanguageModels `json:"largeLanguageModels,omitempty"`

	// CloudProviderCredentials is a reference to an object (e.g. a Kubernetes
	// Secret) which contains the credentials needed to access the APIs of
	// cloud providers.
	//
	// This is the global configuration that will be used by DEFAULT for all
	// model configurations. A secret configured this way MAY include any number
	// of key-value pairs equal to the number of providers you have, but used
	// this way the keys MUST be named according to their providers (e.g.
	// "openai", "azure", "cohere", e.t.c.). For example:
	//
	//   apiVersion: v1
	//   kind: Secret
	//   metadata:
	//     name: devteam-ai-cloud-providers
	//   type: Opaque
	//   data:
	//     openai: *****************
	//     azure: *****************
	//     cohere: *****************
	//
	// See AICloudProviderName for a list of known and valid cloud providers.
	//
	// Note that the keys are NOT case-sensitive (e.g. "OpenAI", "openai", and
	// "openAI" are all valid and considered the same keys) but if there are
	// duplicates endpoints failures conditions will be emitted and endpoints
	// will not be configured until the duplicates are resolved.
	//
	// This is currently considered required, but in future iterations will be
	// optional as we do things like enable configuring credentials at the model
	// level.
	//
	// +required
	CloudProviderCredentials *AICloudProviderAPITokenRef `json:"cloudProviderCredentials,omitempty"`
}

// -----------------------------------------------------------------------------
// AIGateway API - Specification - Large Language Models (LLM)
// -----------------------------------------------------------------------------

// LargeLanguageModels is a list of Large Language Models (LLM) hosted in
// various ways (cloud hosted, self hosted, e.t.c.) which the AIGateway should
// serve and manage traffic for.
// +apireference:kgo:include
type LargeLanguageModels struct {
	// CloudHosted configures LLMs hosted and served by cloud providers.
	//
	// This is currently a required field, requiring at least one cloud-hosted
	// LLM be specified, however in future iterations we may add other hosting
	// options such as self-hosted LLMs as separate fields.
	//
	// +required
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=64
	CloudHosted []CloudHostedLargeLanguageModel `json:"cloudHosted"`
}

// CloudHostedLargeLanguageModel is the configuration for Large Language Models
// (LLM) hosted by a known and supported AI cloud provider (e.g. OpenAI, Cohere,
// Azure, e.t.c.).
// +apireference:kgo:include
type CloudHostedLargeLanguageModel struct {
	// Identifier is the unique name which identifies the LLM. This will be used
	// as part of the requests made to an AIGateway endpoint. For instance: if
	// you provided the identifier "devteam-gpt-access", then you would access
	// this model via "https://${endpoint}/devteam-gpt-access" and supply it
	// with your consumer credentials to authenticate requests.
	//
	// +required
	Identifier string `json:"identifier"`

	// Model is the model name of the LLM (e.g. gpt-3.5-turbo, phi-2, e.t.c.).
	//
	// If not specified, whatever the cloud provider specifies as the default
	// model will be used.
	//
	// +optional
	Model *string `json:"model"`

	// PromptType is the type of prompt to be used for inference requests to
	// the LLM (e.g. "chat", "completions").
	//
	// If "chat" is specified, prompts sent by the user will be interactive,
	// contextual and stateful. The LLM will dynamically answer questions and
	// simulate a dialogue, while also keeping track of the conversation to
	// provide contextually relevant responses.
	//
	// If "completions" is specified, prompts sent by the user will be
	// stateless and "one-shot". The LLM will provide a single response to the
	// prompt, without any context from previous prompts.
	//
	// If not specified, "completions" will be used as the default.
	//
	// +optional
	// +kubebuilder:validation:Enum=chat;completions
	// +kubebuilder:default=completions
	PromptType *LLMPromptType `json:"promptType"`

	// DefaultPrompts is a list of prompts that should be provided to the LLM
	// by default. This is generally used to influence inference behavior, for
	// instance by providing a "system" role prompt that instructs the LLM to
	// take on a certain persona.
	//
	// +optional
	// +kubebuilder:validation:MaxItems=64
	DefaultPrompts []LLMPrompt `json:"defaultPrompts"`

	// DefaultPromptParams configures the parameters which will be sent with
	// any and every inference request.
	//
	// If this is set, there is currently no way to override these parameters
	// at the individual prompt level. This is an expected feature from later
	// releases of our AI plugins.
	//
	// +optional
	DefaultPromptParams *LLMPromptParams `json:"defaultPromptParams"`

	// AICloudProvider defines the cloud provider that will fulfill the LLM
	// requests for this CloudHostedLargeLanguageModel
	//
	// +required
	AICloudProvider AICloudProvider `json:"aiCloudProvider"`
}

// -----------------------------------------------------------------------------
// AIGateway API - Status
// -----------------------------------------------------------------------------

// AIGatewayStatus defines the observed state of AIGateway.
// +apireference:kgo:include
type AIGatewayStatus struct {
	// Endpoints are collections of the URL, credentials and metadata needed in
	// order to access models served by the AIGateway for inference.
	//
	// +optional
	// +kubebuilder:validation:MaxItems=64
	Endpoints []AIGatewayEndpoint `json:"endpoints,omitempty"`

	// Conditions describe the current conditions of the AIGateway.
	//
	// Known condition types are:
	//
	//   - "Accepted"
	//   - "Provisioning"
	//   - "EndpointsReady"
	//
	// +listType=map
	// +listMapKey=type
	// +optional
	// +kubebuilder:validation:MaxItems=8
	// +kubebuilder:default={{type: "Accepted", status: "Unknown", reason:"Pending", message:"Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// -----------------------------------------------------------------------------
// AIGateway API - Setup
// -----------------------------------------------------------------------------

func init() {
	SchemeBuilder.Register(&AIGateway{}, &AIGatewayList{})
}
