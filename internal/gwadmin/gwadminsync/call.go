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
	"cmp"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminapi"
)

const (
	// requestTimeout bounds every request to a gw-admin pod.
	requestTimeout = 30 * time.Second

	// podTimeout bounds the requests to a gw-admin pod of one ForEachPod,
	// however many there are.
	podTimeout = time.Minute

	// notServingTimeout bounds how long a gw-admin pod not serving gets to
	// accept a connection in one ForEachPod: one starting refuses it at once,
	// one on a node down never answers. Once connected, it gets podTimeout.
	notServingTimeout = 5 * time.Second

	// dialTimeout bounds the connection to a gw-admin pod.
	dialTimeout = 10 * time.Second

	// idleConnTimeout is how long a connection to a gw-admin pod is kept open
	// between calls.
	idleConnTimeout = 90 * time.Second
)

// PodsError is the error of the gw-admin pods that failed, each named with
// its error.
type PodsError struct {
	// Failures are the errors of the pods, each naming its pod.
	Failures []string
}

func (e PodsError) Error() string { return strings.Join(e.Failures, "; ") }

// ForEachPod calls fn with each of the pods given, of the target, and a
// client of it, all at once. It returns the pods still pending: those not
// serving that couldn't be connected to, and never were, e.g. starting while
// their image is pulled, or on a node that is down, which a later call may
// reach. A pod not
// serving gets notServingTimeout to accept a connection, so that one that
// never answers doesn't hold the caller longer. It returns the errors of the
// other pods, each naming its pod, as a PodsError. Each request to a pod is
// bounded by requestTimeout, and all the requests to a pod by podTimeout. The
// connections to the pods of a GWAdmin are reused until its TLS material
// changes.
func (s *Syncer) ForEachPod(
	ctx context.Context, target *Target, pods []Pod, fn func(context.Context, Pod, gwadminapi.ClientInterface) error,
) (pending []Pod, err error) {
	newClient := s.NewClient
	if newClient == nil {
		newClient = newAPIClient
	}
	newTransport := func() *http.Transport {
		dial := s.DialContext
		if dial == nil {
			dial = (&net.Dialer{Timeout: dialTimeout}).DialContext
		}
		return &http.Transport{
			TLSClientConfig:   target.tlsConfig,
			DialContext:       dial,
			ForceAttemptHTTP2: true,
			// The pods of a rollout come and go: the connections to those gone
			// are closed once idle that long.
			IdleConnTimeout: idleConnTimeout,
		}
	}
	var transport *http.Transport
	if !s.epochs.keep(target.Key(), target.epoch, func() {
		transport = s.transports.get(target.Key(), target.TLSFingerprint, newTransport)
	}) {
		// The GWAdmin was forgotten since it was resolved, e.g. deleted: its
		// connections aren't kept.
		transport = newTransport()
		defer transport.CloseIdleConnections()
	}
	httpClient := &http.Client{Transport: transport, Timeout: requestTimeout}

	failed := make([]string, len(pods))
	isPending := make([]bool, len(pods))
	var wg sync.WaitGroup
	for i, pod := range pods {
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(ctx, podTimeout)
			defer cancel()
			ctx, connected := traceConnection(ctx)
			if !pod.Serving {
				var stop func()
				ctx, stop = cancelUnlessConnected(ctx, connected, cmp.Or(s.notServingTimeout, notServingTimeout))
				defer stop()
			}
			gwAdmin, err := newClient("https://"+pod.Address+"/v1", httpClient)
			if err == nil {
				err = fn(ctx, pod, gwAdmin)
			}
			switch {
			case err == nil:
			case !pod.Serving && !connected.Load() && notConnected(err):
				isPending[i] = true
			default:
				failed[i] = fmt.Sprintf("gw-admin pod %s: %v", pod.Name, err)
			}
		})
	}
	wg.Wait()
	for i, pod := range pods {
		if isPending[i] {
			pending = append(pending, pod)
		}
	}
	failed = slices.DeleteFunc(failed, func(f string) bool { return f == "" })
	if len(failed) > 0 {
		return pending, PodsError{Failures: failed}
	}
	return pending, nil
}

// traceConnection returns a context whose requests report, through the
// returned flag, whether a connection to the pod was made: one whose TLS
// handshake completed, or one reused from an earlier call, which completed
// it. A TCP connection alone doesn't tell: the kernel accepts it on a
// listening socket even when the process never reads it, e.g. one stuck.
func traceConnection(ctx context.Context) (context.Context, *atomic.Bool) {
	connected := &atomic.Bool{}
	return httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		TLSHandshakeDone: func(_ tls.ConnectionState, err error) {
			if err == nil {
				connected.Store(true)
			}
		},
		GotConn: func(info httptrace.GotConnInfo) {
			if info.Reused {
				connected.Store(true)
			}
		},
	}), connected
}

// cancelUnlessConnected returns a context cancelled after timeout unless a
// connection to the pod was made by then, and the function releasing it.
func cancelUnlessConnected(ctx context.Context, connected *atomic.Bool, timeout time.Duration) (context.Context, func()) {
	ctx, cancel := context.WithCancel(ctx)
	timer := time.AfterFunc(timeout, func() {
		if !connected.Load() {
			cancel()
		}
	})
	return ctx, func() {
		timer.Stop()
		cancel()
	}
}

// notConnected reports whether the error is of a connection to the pod that
// couldn't be made: it refused it, as nothing listens on its address yet or
// anymore, or it didn't answer in time. Waiting for a connection, the HTTP
// transport returns the error of the context, not the one of the dial.
func notConnected(err error) bool {
	if opErr, ok := errors.AsType[*net.OpError](err); ok && opErr.Op == "dial" {
		return true
	}
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}

// newAPIClient returns a client of the gw-admin pod at server, over
// httpClient.
func newAPIClient(server string, httpClient *http.Client) (gwadminapi.ClientInterface, error) {
	return gwadminapi.NewClient(server, gwadminapi.WithHTTPClient(httpClient))
}

// ResponseError is the error of a call gw-admin answered with a status other
// than 2xx.
type ResponseError struct {
	// Action names the call, e.g. "register the gateway".
	Action string
	// StatusCode is the status of the response.
	StatusCode int
	// Status is the status of the response, as text.
	Status string
	// Message is the problem gw-admin reports, or the body of the response.
	Message string
}

func (e ResponseError) Error() string {
	return fmt.Sprintf("failed to %s: gw-admin returned %s: %s", e.Action, e.Status, e.Message)
}

// IsNotFound reports whether gw-admin answered the call with a 404, e.g. for
// something it doesn't know.
func IsNotFound(err error) bool {
	respErr, ok := errors.AsType[ResponseError](err)
	return ok && respErr.StatusCode == http.StatusNotFound
}

// problem is the error body of the gw-admin API.
type problem struct {
	Status            int    `json:"status"`
	Title             string `json:"title"`
	Detail            string `json:"detail"`
	InvalidParameters []struct {
		Field  string `json:"field"`
		Reason string `json:"reason"`
	} `json:"invalid_parameters"`
}

// Call makes a call to gw-admin, and returns its error, as CheckResponse
// does.
func Call(action string, call func() (*http.Response, error)) error {
	return CallDescribingFields(action, nil, call)
}

// CallDescribingFields makes a call to gw-admin, as Call does, describing
// each field gw-admin reports invalid with describeField, e.g. to name the
// object it comes from. A nil describeField leaves the fields as they are.
func CallDescribingFields(action string, describeField func(string) string, call func() (*http.Response, error)) error {
	resp, err := call()
	return checkResponse(action, resp, err, describeField)
}

// CheckResponse returns an error when the call failed: the error of the
// transport, or a ResponseError with the problem gw-admin reports. It closes
// the body of the response.
func CheckResponse(action string, resp *http.Response, err error) error {
	return checkResponse(action, resp, err, nil)
}

func checkResponse(action string, resp *http.Response, err error, describeField func(string) string) error {
	if err != nil {
		return fmt.Errorf("failed to %s: %w", action, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	respErr := ResponseError{Action: action, StatusCode: resp.StatusCode, Status: resp.Status}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var p problem
	if err := json.Unmarshal(body, &p); err != nil || (p.Detail == "" && p.Title == "") {
		respErr.Message = strings.TrimSpace(string(body))
		return respErr
	}
	respErr.Message = cmp.Or(p.Detail, p.Title)
	if len(p.InvalidParameters) > 0 {
		reasons := make([]string, 0, len(p.InvalidParameters))
		for _, param := range p.InvalidParameters {
			field := param.Field
			if describeField != nil {
				field = describeField(field)
			}
			reasons = append(reasons, fmt.Sprintf("%s: %s", field, param.Reason))
		}
		respErr.Message = strings.Join(reasons, "; ")
	}
	return respErr
}
