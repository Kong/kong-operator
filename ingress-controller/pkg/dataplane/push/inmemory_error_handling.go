package push

import (
	"encoding/json"

	"github.com/go-logr/logr"
)

// configError is an error response from Kong's DB-less /config endpoint.
type configError struct {
	Code      int               `json:"code,omitempty" yaml:"code,omitempty"`
	Flattened []FlatEntityError `json:"flattened_errors,omitempty" yaml:"flattened_errors,omitempty"`
	Message   string            `json:"message,omitempty" yaml:"message,omitempty"`
	Name      string            `json:"name,omitempty" yaml:"name,omitempty"`
}

// FlatEntityError represents a single Kong entity with one or more invalid fields.
type FlatEntityError struct {
	// Name is the name of the Kong entity.
	Name string `json:"entity_name,omitempty" yaml:"entity_name,omitempty"`

	// ID is the ID of the Kong entity.
	ID string `json:"entity_id,omitempty" yaml:"entity_id,omitempty"`

	// Tags are the tags of the Kong entity.
	Tags []string `json:"entity_tags,omitempty" yaml:"entity_tags,omitempty"`

	// Type is the type of the Kong entity.
	Type string `json:"entity_type,omitempty" yaml:"entity_type,omitempty"`

	// Errors are the errors associated with the Kong entity.
	Errors []FlatError `json:"errors,omitempty" yaml:"errors,omitempty"`
}

// FlatErrorType tells whether a FlatError is associated with a single field or a whole entity.
type FlatErrorType string

const (
	// FlatErrorTypeField is an error associated with a single field.
	FlatErrorTypeField FlatErrorType = "field"

	// FlatErrorTypeEntity is an error associated with a whole entity.
	FlatErrorTypeEntity FlatErrorType = "entity"
)

// FlatError represents an error for a single field within a Kong entity or a whole entity.
type FlatError struct {
	// Field is the name of the entity's field that has an error.
	// Optional: Field can be empty if the error is associated with the whole entity.
	Field string `json:"field,omitempty" yaml:"field,omitempty"`

	// Message is the error associated with Field (for single-value fields) or with a whole entity when Type is "entity".
	Message string `json:"message,omitempty" yaml:"message,omitempty"`

	// Messages are the errors associated with Field for multi-value fields. The array index in Messages matches the
	// array index in the input.
	Messages []string `json:"messages,omitempty" yaml:"messages,omitempty"`

	// Type tells whether the error is associated with a single field or a whole entity.
	Type FlatErrorType `json:"type,omitempty" yaml:"type,omitempty"`
}

// ParseFlatEntityErrors takes a Kong /config error response body and returns its flattened entity errors.
// Entities without Kubernetes metadata tags are returned too: callers decide whether to attribute them.
func ParseFlatEntityErrors(body []byte, logger logr.Logger) ([]FlatEntityError, error) {
	// Directly return here to avoid the misleading "could not unmarshal config" message appear in logs.
	if len(body) == 0 {
		return nil, nil
	}

	var parsed configError
	if err := json.Unmarshal(body, &parsed); err != nil {
		// We may be able to parse the "message" field even if the full set of flattened errors cannot be decoded.
		fallback := map[string]any{}
		if fallbackErr := json.Unmarshal(body, &fallback); fallbackErr == nil {
			if message, ok := fallback["message"]; ok {
				logger.Error(nil, "Could not fully parse config error", "message", message)
			}
		}
		return nil, NewResponseParsingError(body)
	}
	if len(parsed.Flattened) == 0 {
		if len(parsed.Message) > 0 {
			logger.Error(nil, "Config error missing per-resource errors", "message", parsed.Message)
		} else {
			logger.Error(nil, "Config error missing per-resource and message", "message", parsed.Message)
		}
	}

	return parsed.Flattened, nil
}
