package gatewayapi

import (
	"errors"
	"fmt"

	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

const (
	// DefaultKongServiceRetries is Kong Gateway's default number of retries for a
	// Service when no retry configuration is provided.
	DefaultKongServiceRetries int64 = 5

	// MaxKongServiceRetries is the largest retries value Kong Gateway accepts for
	// a Service.
	MaxKongServiceRetries int64 = 32767
)

// HTTPRouteRetryUnsupportedError describes an HTTPRoute retry field that Kong
// Gateway cannot honor.
type HTTPRouteRetryUnsupportedError struct {
	// Field is the HTTPRoute field path relative to the rule (for example
	// "retry.codes").
	Field string
	// Detail explains why the field is unsupported.
	Detail string
}

func (e *HTTPRouteRetryUnsupportedError) Error() string {
	return fmt.Sprintf("%s is unsupported: %s", e.Field, e.Detail)
}

// IsHTTPRouteRetryUnsupported reports whether err describes retry
// configuration that Kong Gateway cannot honor. Callers that surface such
// configuration through Route status can use this to avoid also rejecting the
// resource at admission time.
func IsHTTPRouteRetryUnsupported(err error) bool {
	_, ok := errors.AsType[*HTTPRouteRetryUnsupportedError](err)
	return ok
}

// KongRetriesForHTTPRouteRule translates an HTTPRoute rule's retry stanza into
// the Kong Service retries value.
//
// A nil retry stanza returns nil so callers leave Kong's default untouched.
// Kong Gateway retries transport-level failures only, so retry.codes and
// retry.backoff cannot be represented and are rejected instead of being
// silently ignored.
func KongRetriesForHTTPRouteRule(rule gatewayv1.HTTPRouteRule) (*int64, error) {
	retry := rule.Retry
	if retry == nil {
		return nil, nil
	}

	if len(retry.Codes) > 0 {
		return nil, &HTTPRouteRetryUnsupportedError{
			Field:  "retry.codes",
			Detail: "Kong Gateway retries transport-level failures only and cannot retry on HTTP response status codes",
		}
	}
	if retry.Backoff != nil {
		return nil, &HTTPRouteRetryUnsupportedError{
			Field:  "retry.backoff",
			Detail: "Kong Gateway retries immediately and has no configurable backoff",
		}
	}

	// The CRD defaults attempts to 1 when the retry stanza is present. Applying
	// the same default here keeps API objects that have not gone through CRD
	// defaulting (for example in unit tests) consistent with the API contract.
	attempts := int64(1)
	if retry.Attempts != nil {
		attempts = int64(*retry.Attempts)
	}
	if attempts < 0 {
		return nil, &HTTPRouteRetryUnsupportedError{
			Field:  "retry.attempts",
			Detail: "must be greater than or equal to 0",
		}
	}
	if attempts > MaxKongServiceRetries {
		return nil, &HTTPRouteRetryUnsupportedError{
			Field:  "retry.attempts",
			Detail: fmt.Sprintf("must not exceed %d, the maximum supported by Kong Gateway", MaxKongServiceRetries),
		}
	}

	return &attempts, nil
}

// ValidateHTTPRouteRetries returns an error if any rule contains retry
// configuration Kong Gateway cannot honor.
func ValidateHTTPRouteRetries(rules []gatewayv1.HTTPRouteRule) error {
	for i := range rules {
		if _, err := KongRetriesForHTTPRouteRule(rules[i]); err != nil {
			return fmt.Errorf("rules[%d].%w", i, err)
		}
	}
	return nil
}
