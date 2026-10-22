/*
Copyright 2026 Kong, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package gwadminsync

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminapi"
	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminsync/gwadminsynctest"
)

func TestForEachPod(t *testing.T) {
	s := &Syncer{NewClient: func(server string, _ *http.Client) (gwadminapi.ClientInterface, error) {
		address := strings.TrimSuffix(strings.TrimPrefix(server, "https://"), "/v1")
		if address == "10.0.0.3:8083" {
			return nil, fmt.Errorf("no gw-admin pod at %s", address)
		}
		return fakeAPIClient{address: address}, nil
	}}
	target := &Target{GWAdmin: gwadminsynctest.NewGWAdmin(t, ns).GWAdmin}
	starting, err := s.ForEachPod(t.Context(), target, []Pod{
		{Name: "gw-admin-0", Address: "10.0.0.1:8083", Serving: true},
		{Name: "gw-admin-1", Address: "10.0.0.2:8083", Serving: true},
		{Name: "gw-admin-2", Address: "10.0.0.3:8083", Serving: true},
	}, func(_ context.Context, pod Pod, gwAdmin gwadminapi.ClientInterface) error {
		if gwAdmin.(fakeAPIClient).address != pod.Address {
			return errors.New("the client of another pod")
		}
		if pod.Name == "gw-admin-1" {
			return errors.New("rejected")
		}
		return nil
	})
	assert.Empty(t, starting)
	podsErr, ok := errors.AsType[PodsError](err)
	require.True(t, ok, "the error of the pods is a PodsError: %v", err)
	assert.Equal(t, []string{"gw-admin pod gw-admin-1: rejected", "gw-admin pod gw-admin-2: no gw-admin pod at 10.0.0.3:8083"}, podsErr.Failures)
	assert.Equal(t, "gw-admin pod gw-admin-1: rejected; gw-admin pod gw-admin-2: no gw-admin pod at 10.0.0.3:8083", err.Error())

	_, err = s.ForEachPod(t.Context(), target, nil, nil)
	require.NoError(t, err, "no pod, no error")

	t.Run("the default client", func(t *testing.T) {
		s := &Syncer{}
		var called bool
		_, err := s.ForEachPod(t.Context(), target, []Pod{{Name: "gw-admin-0", Address: "10.0.0.1:8083"}},
			func(context.Context, Pod, gwadminapi.ClientInterface) error {
				called = true
				return nil
			})
		require.NoError(t, err)
		assert.True(t, called)
	})

	t.Run("a pod not serving that can't be connected to is pending", func(t *testing.T) {
		dialErr := func(err error) error {
			return fmt.Errorf("failed to read: %w", &url.Error{Op: "Get", URL: "https://10.0.0.1:8083/v1", Err: &net.OpError{Op: "dial", Net: "tcp", Err: err}})
		}
		refused := dialErr(&os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED})
		timedOut := dialErr(context.DeadlineExceeded)
		pods := []Pod{
			{Name: "starting", Address: "10.0.0.1:8083"},
			{Name: "down", Address: "10.0.0.2:8083"},
			{Name: "serving", Address: "10.0.0.5:8083", Serving: true},
			{Name: "failing", Address: "10.0.0.4:8083"},
		}
		pending, err := s.ForEachPod(t.Context(), target, pods, func(_ context.Context, pod Pod, _ gwadminapi.ClientInterface) error {
			switch pod.Name {
			case "failing":
				return errors.New("rejected")
			case "down":
				return timedOut
			default:
				return refused
			}
		})
		assert.Equal(t, []Pod{pods[0], pods[1]}, pending, "refused, e.g. starting, or timed out, e.g. on a node down")
		podsErr, ok := errors.AsType[PodsError](err)
		require.True(t, ok, "%v", err)
		assert.Equal(t, []string{
			`gw-admin pod serving: failed to read: Get "https://10.0.0.1:8083/v1": dial tcp: connect: connection refused`,
			"gw-admin pod failing: rejected",
		}, podsErr.Failures, "a pod serving that can't be connected to, or a pod that fails otherwise, fails")
	})

	t.Run("the requests to a pod are bounded", func(t *testing.T) {
		deadlines := map[string]time.Time{}
		var mu sync.Mutex
		_, err := s.ForEachPod(t.Context(), target, []Pod{
			{Name: "serving", Address: "10.0.0.1:8083", Serving: true},
			{Name: "not-serving", Address: "10.0.0.2:8083"},
		}, func(ctx context.Context, pod Pod, _ gwadminapi.ClientInterface) error {
			mu.Lock()
			defer mu.Unlock()
			deadlines[pod.Name], _ = ctx.Deadline()
			return nil
		})
		require.NoError(t, err)
		assert.WithinDuration(t, time.Now().Add(podTimeout), deadlines["serving"], 5*time.Second)
		assert.WithinDuration(t, time.Now().Add(podTimeout), deadlines["not-serving"], 5*time.Second,
			"a pod not serving has the deadline of one serving: it is cancelled only when it doesn't connect in time")
	})

	t.Run("over HTTP", func(t *testing.T) {
		const window = 300 * time.Millisecond
		slowID, fastID := uuid.New(), uuid.New()
		server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.Path, slowID.String()) {
				// Longer than a pod not serving gets to connect.
				time.Sleep(2 * window)
			}
			w.WriteHeader(http.StatusNoContent)
		}))
		server.EnableHTTP2 = true
		server.StartTLS()
		t.Cleanup(server.Close)
		roots := x509.NewCertPool()
		roots.AddCert(server.Certificate())
		target := &Target{
			GWAdmin:        target.GWAdmin,
			TLSFingerprint: t.Name(),
			tlsConfig:      &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, ServerName: "example.com"},
		}
		s := &Syncer{
			notServingTimeout: window,
			// The pod at 10.0.0.9 is on a node down: the dial hangs until
			// the context is done. Every other pod is the server.
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				if strings.HasPrefix(addr, "10.0.0.9:") {
					<-ctx.Done()
					return nil, &net.OpError{Op: "dial", Net: network, Err: ctx.Err()}
				}
				return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
			},
		}
		get := func(id uuid.UUID) func(context.Context, Pod, gwadminapi.ClientInterface) error {
			return func(ctx context.Context, _ Pod, gwAdmin gwadminapi.ClientInterface) error {
				return Call("read", func() (*http.Response, error) { return gwAdmin.GetEventGatewayEntities(ctx, id) })
			}
		}

		start := time.Now()
		pending, err := s.ForEachPod(t.Context(), target, []Pod{{Name: "down", Address: "10.0.0.9:8083"}}, get(fastID))
		require.NoError(t, err)
		assert.Equal(t, []Pod{{Name: "down", Address: "10.0.0.9:8083"}}, pending,
			"a pod not serving whose connection hangs is pending, not failed")
		assert.Less(t, time.Since(start), podTimeout/2, "it gets the short window to connect")

		pending, err = s.ForEachPod(t.Context(), target, []Pod{{Name: "slow", Address: "10.0.0.8:8083"}}, get(slowID))
		require.NoError(t, err, "a pod not serving that connected gets more than the window to answer")
		assert.Empty(t, pending)
	})

	t.Run("the connections of a GWAdmin forgotten since it was resolved aren't kept", func(t *testing.T) {
		s := &Syncer{NewClient: s.NewClient}
		stale := &Target{GWAdmin: target.GWAdmin}
		s.forgetGWAdmin(stale.Key())
		_, err := s.ForEachPod(t.Context(), stale, nil, nil)
		require.NoError(t, err)
		s.transports.mu.Lock()
		defer s.transports.mu.Unlock()
		assert.Empty(t, s.transports.byGWAdmin)
	})
}

func TestCheckResponse(t *testing.T) {
	type reply struct {
		status int
		body   string
	}
	tests := []struct {
		name         string
		reply        *reply
		err          error
		wantErr      string
		wantNotFound bool
	}{
		{name: "a transport error", err: errors.New("connection reset"), wantErr: "failed to act: connection reset"},
		{name: "a success", reply: &reply{status: http.StatusNoContent}},
		{name: "a body that is not JSON", reply: &reply{status: http.StatusBadGateway, body: " bad gateway \n"},
			wantErr: "failed to act: gw-admin returned 502 Bad Gateway: bad gateway"},
		{name: "a problem without title nor detail", reply: &reply{status: http.StatusBadRequest, body: `{"status": 400}`},
			wantErr: `failed to act: gw-admin returned 400 Bad Request: {"status": 400}`},
		{name: "a problem with a title", reply: &reply{status: http.StatusConflict, body: `{"title": "conflict"}`},
			wantErr: "failed to act: gw-admin returned 409 Conflict: conflict"},
		{name: "the detail wins over the title", reply: &reply{status: http.StatusConflict, body: `{"title": "conflict", "detail": "the gateway is unknown"}`},
			wantErr: "failed to act: gw-admin returned 409 Conflict: the gateway is unknown"},
		{
			name: "the invalid parameters are reported",
			reply: &reply{status: http.StatusBadRequest, body: `{"title": "invalid", "invalid_parameters": [
				{"field": "name", "reason": "is required"},
				{"field": "listeners", "reason": "is missing"}]}`},
			wantErr: "failed to act: gw-admin returned 400 Bad Request: name: is required; listeners: is missing",
		},
		{name: "a 404", reply: &reply{status: http.StatusNotFound, body: `{"title": "Not Found"}`},
			wantErr: "failed to act: gw-admin returned 404 Not Found: Not Found", wantNotFound: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var resp *http.Response
			if tc.reply != nil {
				resp = &http.Response{
					StatusCode: tc.reply.status,
					Status:     fmt.Sprintf("%d %s", tc.reply.status, http.StatusText(tc.reply.status)),
					Body:       io.NopCloser(strings.NewReader(tc.reply.body)),
				}
			}
			err := CheckResponse("act", resp, tc.err)
			assert.Equal(t, tc.wantNotFound, IsNotFound(err))
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, tc.wantErr)
			if tc.reply != nil {
				respErr, ok := errors.AsType[ResponseError](err)
				require.True(t, ok)
				assert.Equal(t, tc.reply.status, respErr.StatusCode)
			}
		})
	}
	assert.False(t, IsNotFound(fmt.Errorf("wrapped: %w", errors.New("other"))))
	assert.True(t, IsNotFound(fmt.Errorf("wrapped: %w", ResponseError{StatusCode: http.StatusNotFound})), "a wrapped 404")
}

func TestCall(t *testing.T) {
	ok := func() (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(strings.NewReader("{}"))}, nil
	}
	require.NoError(t, Call("act", ok))

	notFound := func() (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNotFound, Status: "404 Not Found", Body: io.NopCloser(strings.NewReader(`{"title":"Not Found"}`))}, nil
	}
	err := Call("act", notFound)
	require.EqualError(t, err, "failed to act: gw-admin returned 404 Not Found: Not Found")
	assert.True(t, IsNotFound(err))

	require.EqualError(t, Call("act", func() (*http.Response, error) { return nil, errors.New("connection refused") }),
		"failed to act: connection refused")
}
