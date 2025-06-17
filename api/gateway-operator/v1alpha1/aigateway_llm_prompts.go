package v1alpha1

// -----------------------------------------------------------------------------
// AIGateway API - Prompts
// -----------------------------------------------------------------------------

// LLMPrompt is a text prompt that includes parameters, a role and content.
//
// This is intended for situations like when you need to provide roles in a
// prompt to an LLM in order to influence its behavior and responses.
//
// For example, you might want to provide a "system" role and tell the LLM
// something like "you are a helpful assistant who responds in the style of
// Sherlock Holmes".
// +apireference:kgo:include
type LLMPrompt struct {
	// Content is the prompt text sent for inference.
	//
	// +required
	Content string `json:"content"`

	// Role indicates the role of the prompt. This is used to identify the
	// prompt's purpose, such as "system" or "user" and can influence the
	// behavior of the LLM.
	//
	// If not specified, "user" will be used as the default.
	//
	// +optional
	// +kubebuilder:validation:Enum=user;system
	// +kubebuilder:default=user
	Role *LLMPromptRole `json:"role"`
}

// -----------------------------------------------------------------------------
// AIGateway API - Prompts - Parameters
// -----------------------------------------------------------------------------

// LLMPromptParams contains parameters that can be used to control the behavior
// of a large language model (LLM) when generating text based on a prompt.
// +apireference:kgo:include
type LLMPromptParams struct {
	// Temperature controls the randomness of predictions by scaling the logits
	// before applying softmax. A lower temperature (e.g., 0.0 to 0.7) makes
	// the model more confident in its predictions, leading to more repetitive
	// and deterministic outputs. A higher temperature (e.g., 0.8 to 1.0)
	// increases randomness, generating more diverse and creative outputs. At
	// very high temperatures, the outputs may become nonsensical or highly
	// unpredictable.
	//
	// +optional
	Temperature *string `json:"temperature"`

	// Max Tokens specifies the maximum length of the model's output in terms
	// of the number of tokens (words or pieces of words). This parameter
	// limits the output's size, ensuring the model generates content within a
	// manageable scope. A token can be a word or part of a word, depending on
	// the model's tokenizer.
	//
	// +optional
	MaxTokens *int `json:"maxTokens"`

	// TopK sampling is a technique where the model's prediction is limited to
	// the K most likely next tokens at each step of the generation process.
	// The probability distribution is truncated to these top K tokens, and the
	// next token is randomly sampled from this subset. This method helps in
	// reducing the chance of selecting highly improbable tokens, making the
	// text more coherent. A smaller K leads to more predictable text, while a
	// larger K allows for more diversity but with an increased risk of
	// incoherence.
	//
	// +optional
	TopK *int `json:"topK"`

	// TopP (also known as nucleus sampling) is an alternative to top K
	// sampling. Instead of selecting the top K tokens, top P sampling chooses
	// from the smallest set of tokens whose cumulative probability exceeds the
	// threshold P. This method dynamically adjusts the number of tokens
	// considered at each step, depending on their probability distribution. It
	// helps in maintaining diversity while also avoiding very unlikely tokens.
	// A higher P value increases diversity but can lead to less coherence,
	// whereas a lower P value makes the model's outputs more focused and
	// coherent.
	//
	// +optional
	TopP *string `json:"topP"`
}

// -----------------------------------------------------------------------------
// AIGateway API - Prompts - Types
// -----------------------------------------------------------------------------

// LLMPromptRole indicates the role of a prompt for a large language model (LLM).
// +apireference:kgo:include
type LLMPromptRole string

const (
	// LLMPromptRoleUser indicates that the prompt is for the user.
	LLMPromptRoleUser LLMPromptRole = "user"

	// LLMPromptRoleSystem indicates that the prompt is for the system.
	LLMPromptRoleSystem LLMPromptRole = "system"

	// LLMPromptRoleAssistant indicates that the prompt is for the 'virtual assistant'.
	// It represents something that the chat bot "did", or "theoretically could have," said.
	LLMPromptRoleAssistant LLMPromptRole = "assistance"
)

// LLMPromptType indicates the type of prompt to be used for a large
// language model (LLM).
// +apireference:kgo:include
type LLMPromptType string

const (
	// LLMPromptTypeChat indicates that the prompt is for a chat.
	LLMPromptTypeChat LLMPromptType = "chat"

	// LLMPromptTypeCompletion indicates that the prompt is for a completion.
	LLMPromptTypeCompletion LLMPromptType = "completions"
)
