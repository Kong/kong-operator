package test

import (
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"
)

const (
	// transientConnectionRetries is the number of extra attempts made for an
	// idempotent request that failed before a response arrived.
	transientConnectionRetries = 2

	// transientConnectionRetryBackoff is multiplied by the attempt number to
	// give the API server time to recover between attempts.
	transientConnectionRetryBackoff = 250 * time.Millisecond
)

// WithTransientConnectionRetries wraps a transport so that GET and HEAD requests
// are retried when the connection breaks before a response arrives.
//
// client-go already retries those itself, but its predicate
// (k8s.io/apimachinery/pkg/util/net.IsProbableEOF) does not cover HTTP/2 stream
// errors such as "stream error: stream ID 11601; PROTOCOL_ERROR; received from
// peer". An API server published on localhost, which is how kind exposes it,
// emits those under load. One of them is enough to fail a gateway-api
// conformance test: kubernetes.NamespacesMustBeReady reports a failed list call
// with t.Error and keeps polling, so the test is reported as failed even when
// every later poll succeeds. Retrying the request keeps that transient error
// away from the test.
func WithTransientConnectionRetries(base http.RoundTripper) http.RoundTripper {
	return &transientConnectionRetryTransport{base: base}
}

type transientConnectionRetryTransport struct {
	base http.RoundTripper
}

// RoundTrip implements [http.RoundTripper].
func (t *transientConnectionRetryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		resp, err := t.base.RoundTrip(req)
		if err == nil || attempt == transientConnectionRetries || !isRetryableRequest(req, err) {
			return resp, err
		}
		select {
		case <-req.Context().Done():
			return nil, err
		case <-time.After(transientConnectionRetryBackoff * time.Duration(attempt+1)):
		}
	}
}

// isRetryableRequest reports whether req can be sent again after it failed with
// err. Only idempotent, body-less requests can be replayed.
func isRetryableRequest(req *http.Request, err error) bool {
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		return false
	}
	if req.Body != nil && req.Body != http.NoBody {
		return false
	}
	return isTransientConnectionError(err)
}

// isTransientConnectionError reports whether err means the connection broke
// before a response arrived, as opposed to the API server answering with an
// error or the caller giving up. It mirrors
// k8s.io/apimachinery/pkg/util/net.IsProbableEOF, plus the HTTP/2 stream errors
// that predicate misses.
func isTransientConnectionError(err error) bool {
	if err == nil {
		return false
	}
	if urlErr, ok := errors.AsType[*url.Error](err); ok {
		err = urlErr.Err
	}
	switch {
	case errors.Is(err, io.EOF),
		errors.Is(err, io.ErrUnexpectedEOF),
		errors.Is(err, syscall.ECONNRESET),
		errors.Is(err, syscall.EPIPE),
		errors.Is(err, net.ErrClosed):
		return true
	}
	msg := err.Error()
	for _, s := range []string{
		"stream error: stream ID",
		"http2: server sent GOAWAY",
		"http2: client connection lost",
		"connection reset by peer",
		"transport connection broken",
	} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}
