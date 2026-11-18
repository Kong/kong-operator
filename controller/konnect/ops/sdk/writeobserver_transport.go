package sdk

import (
	"net/http"
	"regexp"
)

// WriteObserver is notified of the operator's writes to Konnect control planes.
type WriteObserver interface {
	// MarkWrite is called before sending a write request to the control plane and after receiving its response.
	MarkWrite(controlPlaneID string)
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

	controlPlaneID := m[1]
	t.observer.MarkWrite(controlPlaneID)
	defer t.observer.MarkWrite(controlPlaneID)
	return t.base.RoundTrip(req)
}
