package sdk

import (
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

func (o *recordingObserver) MarkWrite(controlPlaneID string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.marks = append(o.marks, controlPlaneID)
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
			expectedMarks: []string{"cp-1", "cp-1"},
		},
		{
			name:          "entity create",
			method:        http.MethodPost,
			url:           "https://us.api.konghq.com/v2/control-planes/cp-1/core-entities/services",
			expectedMarks: []string{"cp-1", "cp-1"},
		},
		{
			name:          "entity delete",
			method:        http.MethodDelete,
			url:           "https://us.api.konghq.com/v2/control-planes/cp-1/core-entities/routes/r-1",
			expectedMarks: []string{"cp-1", "cp-1"},
		},
		{
			name:          "control plane update",
			method:        http.MethodPatch,
			url:           "https://us.api.konghq.com/v2/control-planes/cp-1",
			expectedMarks: []string{"cp-1", "cp-1"},
		},
		{
			name:          "server URL with path prefix",
			method:        http.MethodPut,
			url:           "https://us.svc.konghq.tech/api/v2/control-planes/cp-2/core-entities/plugins/p-1",
			expectedMarks: []string{"cp-2", "cp-2"},
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
			name:   "write outside control planes",
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
