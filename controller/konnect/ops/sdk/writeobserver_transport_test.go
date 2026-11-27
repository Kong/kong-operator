package sdk

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingObserver struct {
	mu    sync.Mutex
	marks []string
}

func (o *recordingObserver) BeforeWrite(_ context.Context, controlPlaneID string) {
	o.record("before:" + controlPlaneID)
}

func (o *recordingObserver) AfterWrite(_ context.Context, controlPlaneID string) {
	o.record("after:" + controlPlaneID)
}

func (o *recordingObserver) record(mark string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.marks = append(o.marks, mark)
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestWriteObservingTransport(t *testing.T) {
	testCases := []struct {
		name          string
		method        string
		url           string
		expectedMarks []string
	}{
		{
			name:          "entity upsert",
			method:        http.MethodPut,
			url:           "https://us.api.konghq.com/v2/control-planes/cp-1/core-entities/services/svc-1",
			expectedMarks: []string{"before:cp-1", "after:cp-1"},
		},
		{
			name:          "entity create",
			method:        http.MethodPost,
			url:           "https://us.api.konghq.com/v2/control-planes/cp-1/core-entities/services",
			expectedMarks: []string{"before:cp-1", "after:cp-1"},
		},
		{
			name:          "entity delete",
			method:        http.MethodDelete,
			url:           "https://us.api.konghq.com/v2/control-planes/cp-1/core-entities/routes/r-1",
			expectedMarks: []string{"before:cp-1", "after:cp-1"},
		},
		{
			name:          "control plane update",
			method:        http.MethodPatch,
			url:           "https://us.api.konghq.com/v2/control-planes/cp-1",
			expectedMarks: []string{"before:cp-1", "after:cp-1"},
		},
		{
			name:          "server URL with path prefix",
			method:        http.MethodPut,
			url:           "https://us.svc.konghq.tech/api/v2/control-planes/cp-2/core-entities/plugins/p-1",
			expectedMarks: []string{"before:cp-2", "after:cp-2"},
		},
		{
			name:   "entity get",
			method: http.MethodGet,
			url:    "https://us.api.konghq.com/v2/control-planes/cp-1/core-entities/services/svc-1",
		},
		{
			name:   "control plane create",
			method: http.MethodPost,
			url:    "https://us.api.konghq.com/v2/control-planes",
		},
		{
			name:          "AI Gateway entity update",
			method:        http.MethodPut,
			url:           "https://us.api.konghq.com/v1/ai-gateways/aigw-1/consumers/c-1",
			expectedMarks: []string{"before:ai-gateways/aigw-1", "after:ai-gateways/aigw-1"},
		},
		{
			name:          "AI Gateway consumer group membership update",
			method:        http.MethodPut,
			url:           "https://us.api.konghq.com/v1/ai-gateways/aigw-1/consumers/c-1/consumer-groups",
			expectedMarks: []string{"before:ai-gateways/aigw-1", "after:ai-gateways/aigw-1"},
		},
		{
			name:          "AI Gateway update",
			method:        http.MethodPut,
			url:           "https://us.api.konghq.com/v1/ai-gateways/aigw-1",
			expectedMarks: []string{"before:ai-gateways/aigw-1", "after:ai-gateways/aigw-1"},
		},
		{
			name:   "AI Gateway entity get",
			method: http.MethodGet,
			url:    "https://us.api.konghq.com/v1/ai-gateways/aigw-1/consumers/c-1",
		},
		{
			name:   "AI Gateway create",
			method: http.MethodPost,
			url:    "https://us.api.konghq.com/v1/ai-gateways",
		},
		{
			name:   "write outside control planes and AI Gateways",
			method: http.MethodPut,
			url:    "https://us.api.konghq.com/v1/event-gateways/eg-1/listeners/l-1",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			observer := &recordingObserver{}
			var marksDuringRequest int
			base := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				marksDuringRequest = len(observer.marks)
				return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: req}, nil
			})
			tr := NewWriteObservingTransport(base, observer)

			req, err := http.NewRequestWithContext(t.Context(), tc.method, tc.url, strings.NewReader("{}"))
			require.NoError(t, err)
			resp, err := tr.RoundTrip(req)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())

			assert.Equal(t, tc.expectedMarks, observer.marks)
			if len(tc.expectedMarks) > 0 {
				assert.Equal(t, 1, marksDuringRequest, "write must be marked before it is sent")
			}
		})
	}
}

func TestWriteObservingTransportNilObserver(t *testing.T) {
	var called bool
	base := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		called = true
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: req}, nil
	})

	tr := NewWriteObservingTransport(base, nil)
	req, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodPut,
		"https://us.api.konghq.com/v2/control-planes/cp-1/core-entities/services/svc-1",
		strings.NewReader("{}"),
	)
	require.NoError(t, err)
	resp, err := tr.RoundTrip(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.True(t, called)
}

func TestAIGatewayWriteScope(t *testing.T) {
	scope := AIGatewayWriteScope("aigw-1")
	id, ok := AIGatewayIDFromWriteScope(scope)
	require.True(t, ok)
	assert.Equal(t, "aigw-1", id)

	for _, scope := range []string{"cp-1", "ai-gateways/", ""} {
		_, ok := AIGatewayIDFromWriteScope(scope)
		assert.False(t, ok, "scope %q is not an AI Gateway scope", scope)
	}
}
