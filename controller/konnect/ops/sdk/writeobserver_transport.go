package sdk

import (
	"context"
	"net/http"
	"regexp"
	"strings"
)

// WriteObserver is notified of the operator's writes to Konnect config scopes.
//
// A config scope is a Konnect entity with its own expected config version: a
// Kong Gateway control plane, identified by its ID, or an AI Gateway,
// identified by AIGatewayWriteScope.
type WriteObserver interface {
	// BeforeWrite is called before sending a write request to the config scope.
	BeforeWrite(ctx context.Context, scope string)
	// AfterWrite is called after receiving the response to a write request to the config scope.
	AfterWrite(ctx context.Context, scope string)
}

const aiGatewayWriteScopePrefix = "ai-gateways/"

// AIGatewayWriteScope returns the config scope of the AI Gateway with the given ID.
func AIGatewayWriteScope(gatewayID string) string {
	return aiGatewayWriteScopePrefix + gatewayID
}

// AIGatewayIDFromWriteScope returns the AI Gateway ID of a config scope
// returned by AIGatewayWriteScope.
func AIGatewayIDFromWriteScope(scope string) (string, bool) {
	id, ok := strings.CutPrefix(scope, aiGatewayWriteScopePrefix)
	return id, ok && id != ""
}

var (
	controlPlanePathRegex = regexp.MustCompile(`/v2/control-planes/([^/]+)(?:/|$)`)
	aiGatewayPathRegex    = regexp.MustCompile(`/v1/ai-gateways/([^/]+)(?:/|$)`)
)

// writeScope returns the config scope a request path belongs to.
func writeScope(path string) (string, bool) {
	if m := controlPlanePathRegex.FindStringSubmatch(path); m != nil {
		return m[1], true
	}
	if m := aiGatewayPathRegex.FindStringSubmatch(path); m != nil {
		return AIGatewayWriteScope(m[1]), true
	}
	return "", false
}

type writeObservingTransport struct {
	base     http.RoundTripper
	observer WriteObserver
}

// NewWriteObservingTransport returns an [http.RoundTripper] that sends requests through base and reports
// every write request to a Konnect control plane or AI Gateway, or any of its entities, to observer.
func NewWriteObservingTransport(base http.RoundTripper, observer WriteObserver) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	if observer == nil {
		// Nothing to report: keep the base transport.
		return base
	}
	return &writeObservingTransport{base: base, observer: observer}
}

// RoundTrip implements [http.RoundTripper].
func (t *writeObservingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	switch req.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return t.base.RoundTrip(req)
	}
	scope, ok := writeScope(req.URL.Path)
	if !ok {
		return t.base.RoundTrip(req)
	}

	ctx := req.Context()
	t.observer.BeforeWrite(ctx, scope)
	defer t.observer.AfterWrite(ctx, scope)
	return t.base.RoundTrip(req)
}
