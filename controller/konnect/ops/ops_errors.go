package ops

import (
	"encoding/json"
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
)

// ErrNilResponse is an error indicating that a Konnect operation returned an empty response.
// This can sometimes happen regardless of the err being nil.
var ErrNilResponse = errors.New("nil response received")

// EntityWithMatchingUIDNotFoundError is an error indicating that an entity with a matching UID was not found on Konnect.
type EntityWithMatchingUIDNotFoundError struct {
	Entity interface {
		GetTypeName() string
		GetName() string
		GetUID() types.UID
	}
}

// Error implements the error interface.
func (e EntityWithMatchingUIDNotFoundError) Error() string {
	return fmt.Sprintf(
		"%s %s (matching UID %s) not found on Konnect",
		e.Entity.GetTypeName(), e.Entity.GetName(), e.Entity.GetUID(),
	)
}

type sdkErrorBody struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Details []struct {
		TypeAt   string   `json:"@type"`
		Type     string   `json:"type"`
		Field    string   `json:"field"`
		Messages []string `json:"messages"`
	} `json:"details"`
}

// ParseSDKErrorBody parses the body of an SDK error response.
// Exemplary body:
//
//	{
//		"code": 3,
//		"message": "data constraint error",
//		"details": [
//			{
//				"@type": "type.googleapis.com/kong.admin.model.v1.ErrorDetail",
//				"type": "ERROR_TYPE_REFERENCE",
//				"field": "name",
//				"messages": [
//					"name (type: unique) constraint failed"
//				]
//			}
//		]
//	}
func ParseSDKErrorBody(body string) (sdkErrorBody, error) {
	var sdkErr sdkErrorBody
	if err := json.Unmarshal([]byte(body), &sdkErr); err != nil {
		return sdkErr, err
	}

	return sdkErr, nil
}
