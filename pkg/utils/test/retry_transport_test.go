package test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testGatewayListURL = "https://127.0.0.1:42115/apis/gateway.networking.k8s.io/v1/gateways"

func TestWithTransientConnectionRetries(t *testing.T) {
	protocolErr := errors.New("stream error: stream ID 11601; PROTOCOL_ERROR; received from peer")

	newRequest := func(t *testing.T, method, body string) *http.Request {
		t.Helper()
		var reader io.Reader
		if body != "" {
			reader = strings.NewReader(body)
		}
		req, err := http.NewRequestWithContext(t.Context(), method, testGatewayListURL, reader)
		require.NoError(t, err)
		return req
	}

	tests := []struct {
		name      string
		request   func(t *testing.T) *http.Request
		failures  int
		failure   error
		wantCalls int
		wantErr   bool
	}{
		{
			name:      "GET is retried when the connection breaks",
			request:   func(t *testing.T) *http.Request { return newRequest(t, http.MethodGet, "") },
			failures:  2,
			failure:   protocolErr,
			wantCalls: 3,
		},
		{
			name:     "GET is retried when the error is wrapped by url.Error",
			request:  func(t *testing.T) *http.Request { return newRequest(t, http.MethodGet, "") },
			failures: 1,
			failure: &url.Error{
				Op:  http.MethodGet,
				URL: testGatewayListURL,
				Err: protocolErr,
			},
			wantCalls: 2,
		},
		{
			name:      "GET gives up after the retry budget",
			request:   func(t *testing.T) *http.Request { return newRequest(t, http.MethodGet, "") },
			failures:  3,
			failure:   protocolErr,
			wantCalls: 3,
			wantErr:   true,
		},
		{
			name:      "POST is not retried",
			request:   func(t *testing.T) *http.Request { return newRequest(t, http.MethodPost, "{}") },
			failures:  2,
			failure:   protocolErr,
			wantCalls: 1,
			wantErr:   true,
		},
		{
			name:      "GET with a body is not retried",
			request:   func(t *testing.T) *http.Request { return newRequest(t, http.MethodGet, "{}") },
			failures:  2,
			failure:   protocolErr,
			wantCalls: 1,
			wantErr:   true,
		},
		{
			name:      "GET is not retried on an error that is not a broken connection",
			request:   func(t *testing.T) *http.Request { return newRequest(t, http.MethodGet, "") },
			failures:  2,
			failure:   errors.New("the server could not find the requested resource"),
			wantCalls: 1,
			wantErr:   true,
		},
		{
			name: "GET is not retried when the context is done",
			request: func(t *testing.T) *http.Request {
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, testGatewayListURL, nil)
				require.NoError(t, err)
				return req
			},
			failures:  2,
			failure:   protocolErr,
			wantCalls: 1,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := &failingRoundTripper{failures: tt.failures, err: tt.failure}

			resp, err := WithTransientConnectionRetries(base).RoundTrip(tt.request(t))
			if resp != nil {
				require.NoError(t, resp.Body.Close())
			}

			assert.Equal(t, tt.wantCalls, base.calls)
			if tt.wantErr {
				assert.ErrorIs(t, err, tt.failure)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, resp)
			assert.Equal(t, http.StatusOK, resp.StatusCode)
		})
	}
}

// failingRoundTripper fails the first failures calls with err.
type failingRoundTripper struct {
	failures int
	err      error
	calls    int
}

func (f *failingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	f.calls++
	if f.calls <= f.failures {
		return nil, f.err
	}
	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
}
