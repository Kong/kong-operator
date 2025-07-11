package specialized

import (
	operatorv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1alpha1"
)

// -----------------------------------------------------------------------------
// AIGateway API - Plugins - Configuration Blocks
// -----------------------------------------------------------------------------

// AICloudPromptDecoratorConfig is a Golang-conversion of the 'AI Prompt Decorator'
// plugin configuration, from the AI family of Kong plugins.
type AICloudPromptDecoratorConfig struct {
	Prompts *AICloudPromptDecoratorPrompts `json:"prompts,omitempty"`
}

// AICloudPromptDecoratorPrompts is a Golang-conversion of the 'Prompts' configuration
// for the AI family of Kong plugins.
type AICloudPromptDecoratorPrompts struct {
	Prepend []operatorv1alpha1.LLMPrompt `json:"prepend,omitempty"`
	Append  []operatorv1alpha1.LLMPrompt `json:"append,omitempty"`
}

// AICloudProviderLLMConfig is a Golang-conversion of the 'LLM' configuration
// for the AI family of Kong plugins.
type AICloudProviderLLMConfig struct {
	RouteType *string                       `json:"route_type,omitempty"`
	Auth      *AICloudProviderAuthConfig    `json:"auth,omitempty"`
	Logging   *AICloudProviderLoggingConfig `json:"logging,omitempty"`
	Model     *AICloudProviderModelConfig   `json:"model,omitempty"`
}

// AICloudProviderAuthConfig is a Golang-conversion of the 'Auth' configuration
// for the AI family of Kong plugins.
type AICloudProviderAuthConfig struct {
	HeaderName  *string `json:"header_name,omitempty"`
	HeaderValue *string `json:"header_value,omitempty"`
}

// AICloudProviderLoggingConfig is a Golang-conversion of the 'Logging' configuration
// for the AI family of Kong plugins.
type AICloudProviderLoggingConfig struct {
	LogStatistics bool `json:"log_statistics"`
	LogPayloads   bool `json:"log_payloads"`
}

// AICloudProviderModelConfig is a Golang-conversion of the 'Model' configuration
// for the AI family of Kong plugins.
type AICloudProviderModelConfig struct {
	Provider *string                       `json:"provider,omitempty"`
	Name     *string                       `json:"name,omitempty"`
	Options  *AICloudProviderOptionsConfig `json:"options,omitempty"`
}

// AICloudProviderOptionsConfig is a Golang-conversion of the 'Options' configuration
// for the AI family of Kong plugins.
type AICloudProviderOptionsConfig struct {
	MaxTokens   *int    `json:"max_tokens,omitempty"`
	Temperature *string `json:"temperature,omitempty"`
}
