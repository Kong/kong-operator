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

package onprem

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/managedfields"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminapi"
	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminsync"
	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminsync/gwadminsynctest"
	"github.com/kong/kong-operator/v2/internal/utils/gwadminref"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

// fakeGWAdminClient is a gw-admin pod: it records the calls it gets, and
// answers each with respond, when it returns a reply or an error. Otherwise
// it behaves as gw-admin does: it holds the gateways registered, with their
// entities, and answers 404 for a gateway it doesn't know.
type fakeGWAdminClient struct {
	gwadminapi.ClientInterface

	mu       sync.Mutex
	calls    []string
	respond  func(call string) (*reply, error)
	gateways map[uuid.UUID]gwadminapi.GatewayEntities
}

// reply is the response of a gw-admin pod to a call.
type reply struct {
	status int
	body   string
}

// Call names, as recorded.
const (
	callGetEntities   = "get-entities"
	callRegister      = "register"
	callExpected      = "expected"
	callEntities      = "entities"
	callDeleteGateway = "delete-gateway"
)

// call records the call, and returns the response of respond, if any.
// respond returns nil when the pod answers as gw-admin does: then serve, run
// with the lock held, answers.
func (f *fakeGWAdminClient) call(name string, serve func() reply) (*http.Response, error) {
	f.mu.Lock()
	f.calls = append(f.calls, name)
	respond := f.respond
	f.mu.Unlock()
	if respond != nil {
		got, err := respond(name)
		if err != nil {
			return nil, err
		}
		if got != nil {
			return response(*got), nil
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.gateways == nil {
		f.gateways = map[uuid.UUID]gwadminapi.GatewayEntities{}
	}
	return response(serve()), nil
}

func response(r reply) *http.Response {
	return &http.Response{
		StatusCode: r.status,
		Status:     fmt.Sprintf("%d %s", r.status, http.StatusText(r.status)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(r.body)),
	}
}

var notFound = reply{status: http.StatusNotFound, body: `{"status":404,"title":"Not Found"}`}

// badRequest returns the reply of gw-admin to a request with an invalid
// field.
func badRequest(field, reason string) reply {
	return reply{status: http.StatusBadRequest, body: fmt.Sprintf(
		`{"status":400,"title":"Bad Request","invalid_parameters":[{"field":%q,"reason":%q}]}`, field, reason)}
}

func (f *fakeGWAdminClient) recorded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// reset forgets the calls recorded.
func (f *fakeGWAdminClient) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = nil
}

// restart makes the pod lose everything, as gw-admin does on a restart.
func (f *fakeGWAdminClient) restart() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gateways = nil
}

func (f *fakeGWAdminClient) GetEventGatewayEntities(_ context.Context, id uuid.UUID, _ ...gwadminapi.RequestEditorFn) (*http.Response, error) {
	return f.call(callGetEntities, func() reply {
		entities, ok := f.gateways[id]
		if !ok {
			return notFound
		}
		body, err := json.Marshal(entities)
		if err != nil {
			panic(err)
		}
		return reply{status: http.StatusOK, body: string(body)}
	})
}

func (f *fakeGWAdminClient) UpdateEventGateway(_ context.Context, id uuid.UUID, _ gwadminapi.UpdateEventGatewayJSONRequestBody, _ ...gwadminapi.RequestEditorFn) (*http.Response, error) {
	return f.call(callRegister, func() reply {
		if _, ok := f.gateways[id]; !ok {
			f.gateways[id] = gwadminapi.GatewayEntities{}
		}
		return reply{status: http.StatusOK, body: "{}"}
	})
}

func (f *fakeGWAdminClient) SetExpectedEventGateways(_ context.Context, body gwadminapi.SetExpectedEventGatewaysJSONRequestBody, _ ...gwadminapi.RequestEditorFn) (*http.Response, error) {
	ids := make([]string, 0, len(body.EventGatewayIds))
	for _, id := range body.EventGatewayIds {
		ids = append(ids, id.String())
	}
	return f.call(callExpected+" "+strings.Join(ids, ","), func() reply {
		if len(slices.Compact(slices.Sorted(slices.Values(ids)))) != len(ids) {
			return badRequest("event_gateway_ids", "duplicate items found")
		}
		return reply{status: http.StatusNoContent}
	})
}

func (f *fakeGWAdminClient) UpdateEventGatewayEntities(_ context.Context, id uuid.UUID, body gwadminapi.UpdateEventGatewayEntitiesJSONRequestBody, _ ...gwadminapi.RequestEditorFn) (*http.Response, error) {
	return f.call(callEntities, func() reply {
		if _, ok := f.gateways[id]; !ok {
			return notFound
		}
		// gw-admin requires every list.
		for name, list := range map[string]bool{
			"backend_clusters":  body.BackendClusters == nil,
			"listeners":         body.Listeners == nil,
			"schema_registries": body.SchemaRegistries == nil,
			"static_keys":       body.StaticKeys == nil,
			"tls_trust_bundles": body.TlsTrustBundles == nil,
			"virtual_clusters":  body.VirtualClusters == nil,
		} {
			if list {
				return badRequest(name, "required and not nullable")
			}
		}
		f.gateways[id] = body
		return reply{status: http.StatusOK, body: "{}"}
	})
}

func (f *fakeGWAdminClient) DeleteEventGateway(_ context.Context, id uuid.UUID, _ ...gwadminapi.RequestEditorFn) (*http.Response, error) {
	return f.call(callDeleteGateway, func() reply {
		if _, ok := f.gateways[id]; !ok {
			return notFound
		}
		delete(f.gateways, id)
		return reply{status: http.StatusNoContent}
	})
}

// failing returns a respond function failing the calls starting with prefix
// with the status.
func failing(prefix string, status int) func(string) (*reply, error) {
	return func(call string) (*reply, error) {
		if strings.HasPrefix(call, prefix) {
			return &reply{status: status, body: `{"title": "failed ` + prefix + `"}`}, nil
		}
		return nil, nil
	}
}

// fakePods are the gw-admin pods of a test, by address.
type fakePods struct {
	mu   sync.Mutex
	pods map[string]*fakeGWAdminClient
}

func newFakePods(addresses ...string) *fakePods {
	p := &fakePods{pods: map[string]*fakeGWAdminClient{}}
	for _, a := range addresses {
		p.pods[a] = &fakeGWAdminClient{}
	}
	return p
}

// add adds the pod at the address.
func (p *fakePods) add(address string, pod *fakeGWAdminClient) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pods[address] = pod
}

func (p *fakePods) get(address string) *fakeGWAdminClient {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pods[address]
}

// newClient is a gwadminsync.Syncer.NewClient returning the pod of the
// address.
func (p *fakePods) newClient(server string, _ *http.Client) (gwadminapi.ClientInterface, error) {
	address := strings.TrimSuffix(strings.TrimPrefix(server, "https://"), "/v1")
	if pod := p.get(address); pod != nil {
		return pod, nil
	}
	return nil, fmt.Errorf("no gw-admin pod at %s", address)
}

// The names of the GWAdmin of the tests.
const testGWAdminName = gwadminsynctest.GWAdminName

// testServerURL is the cp endpoint of the GWAdmin of the tests.
var testServerURL = gwadminsynctest.ServerURL(ns)

// newPushReconciler returns a reconciler pushing to gw-admin pods, without a
// cluster, e.g. for push and removeGateway.
func newPushReconciler() *Reconciler {
	return &Reconciler{GWAdminSync: &gwadminsync.Syncer{}}
}

// newGWAdminFixture returns a GWAdmin of the namespace ns whose API Service
// publishes the endpoints.
func newGWAdminFixture(t *testing.T, endpoints ...discoveryv1.Endpoint) *gwadminsynctest.GWAdmin {
	t.Helper()
	return gwadminsynctest.NewGWAdmin(t, ns, endpoints...)
}

// endpoint returns the endpoint of a gw-admin pod.
func endpoint(name, ip string, terminating bool) discoveryv1.Endpoint {
	return gwadminsynctest.Endpoint(name, ip, terminating)
}

// newTestGateway returns an OnPremEventGateway of the GWAdmin of the test,
// with a version 4 UID.
func newTestGateway(name string) *eventgatewayv1alpha1.OnPremEventGateway {
	gateway := &eventgatewayv1alpha1.OnPremEventGateway{Name: name, Namespace: ns}
	gateway.Spec.GWAdminRef.Name = testGWAdminName
	gateway.SetUID(types.UID(uuid.NewString()))
	gateway.SetGeneration(1)
	return gateway
}

// testTypeConverter is the type converter of the Server-Side Apply of the
// status, in the fake client and the reconcilers of the tests.
var testTypeConverter = managedfields.NewDeducedTypeConverter()

// newFakeClient returns a fake client with the objects, the index of the
// gateways by GWAdmin, and the type converter of the status.
func newFakeClient(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()
	return newFakeClientWithInterceptor(t, interceptor.Funcs{}, objects...)
}

// newFakeClientWithInterceptor is newFakeClient, with the calls going through
// funcs.
func newFakeClientWithInterceptor(t *testing.T, funcs interceptor.Funcs, objects ...client.Object) client.WithWatch {
	t.Helper()
	b := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(objects...).
		WithStatusSubresource(&eventgatewayv1alpha1.OnPremEventGateway{}).
		WithTypeConverters(testTypeConverter).
		WithInterceptorFuncs(funcs)
	for _, opt := range gwadminref.IndexOptions() {
		b = b.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	return b.Build()
}

// read reads the object again.
func read[T client.Object](t *testing.T, cl client.Client, obj T) T {
	t.Helper()
	fresh, ok := obj.DeepCopyObject().(T)
	require.True(t, ok)
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(obj), fresh))
	return fresh
}

// recordedEvents returns the events recorded so far.
func recordedEvents(recorder *events.FakeRecorder) []string {
	var recorded []string
	for {
		select {
		case e := <-recorder.Events:
			recorded = append(recorded, e)
		default:
			return recorded
		}
	}
}

func failingList(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
	return errors.New("unavailable")
}

func requestsFor(objs ...client.Object) []reconcile.Request {
	requests := make([]reconcile.Request, 0, len(objs))
	for _, obj := range objs {
		requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(obj)})
	}
	return requests
}

// connectionRefused returns the error of a connection to a gw-admin pod that
// doesn't listen, as the HTTP client returns it.
func connectionRefused() error {
	return &url.Error{Op: "Get", URL: "https://10.0.0.1:8083/v1", Err: &net.OpError{
		Op: "dial", Net: "tcp", Err: &os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED},
	}}
}
