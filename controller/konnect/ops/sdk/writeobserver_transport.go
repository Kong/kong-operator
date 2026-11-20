package sdk

import (
	"context"
	"net/http"
	"regexp"
)

// WriteObserver is notified of the operator's writes to Konnect control planes.
type WriteObserver interface {
	// BeforeWrite is called before sending a write request to the control plane.
	BeforeWrite(ctx context.Context, controlPlaneID string)
	// AfterWrite is called after receiving the response to a write request to the control plane.
	AfterWrite(ctx context.Context, controlPlaneID string)
}

var controlPlanePathRegex = regexp.MustCompile(`/v2/control-planes/([^/]+)(?:/|$)`)

type writeObservingTransport struct {
	base     http.RoundTripper
	observer WriteObserver
}

// NewWriteObservingTransport returns an [http.RoundTripper] that sends requests through base and reports
// every write request to a Konnect control plane, or any of its entities, to observer.
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
	m := controlPlanePathRegex.FindStringSubmatch(req.URL.Path)
	if m == nil {
		return t.base.RoundTrip(req)
	}

	ctx, controlPlaneID := req.Context(), m[1]
	t.observer.BeforeWrite(ctx, controlPlaneID)
	defer t.observer.AfterWrite(ctx, controlPlaneID)
	return t.base.RoundTrip(req)
}
