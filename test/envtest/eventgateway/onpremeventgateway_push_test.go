package eventgateway

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/managedfields"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	egonprem "github.com/kong/kong-operator/v2/controller/eventgateway/onprem"
	controllerpkgssa "github.com/kong/kong-operator/v2/controller/pkg/ssa"
	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminapi"
	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminsync"
	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminsync/gwadminsynctest"
	"github.com/kong/kong-operator/v2/internal/utils/gwadminref"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/envtest"
)

// fakeGWAdmin is a gw-admin pod: a gateway must be registered before its
// entities, a PUT is an idempotent upsert, the entities are checked against
// the schema only and replaced as a whole, and a new pod holds nothing, as
// gw-admin keeps nothing across a restart.
type fakeGWAdmin struct {
	*httptest.Server

	mu       sync.Mutex
	gateways map[uuid.UUID]*fakeGateway
	expected []uuid.UUID
	// writes counts the requests changing what the pod holds.
	writes int
	// reject, when it returns a non-empty field, rejects the entities with a 400
	// on that field.
	reject func(gwadminapi.GatewayEntitySet) string
	// rejectCertificate, when it returns true, rejects the data plane
	// certificate of the gateway with a 400.
	rejectCertificate func(gateway uuid.UUID, body gwadminapi.UpsertEventGatewayDataPlaneCertificateRequestOnPrem) bool
}

// fakeGateway is a gateway a gw-admin pod holds.
type fakeGateway struct {
	name         string
	entities     gwadminapi.GatewayEntitySet
	version      gwadminapi.GatewayConfigVersion
	certificates map[uuid.UUID]gwadminapi.EventGatewayDataPlaneCertificateOnPrem
}

var createdAtPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$`)

// newFakeGWAdmin starts a gw-admin pod serving its API with mutual TLS.
func newFakeGWAdmin(t *testing.T, tlsConfig *tls.Config) *fakeGWAdmin {
	t.Helper()
	f := &fakeGWAdmin{gateways: map[uuid.UUID]*fakeGateway{}}
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /v1/event-gateways/{id}", f.withGateway(false, func(w http.ResponseWriter, r *http.Request, id uuid.UUID, _ *fakeGateway) {
		var body gwadminapi.UpsertGatewayRequestOnPrem
		if !decode(w, r, &body) {
			return
		}
		if g, ok := f.gateways[id]; ok {
			g.name = body.Name
		} else {
			f.gateways[id] = &fakeGateway{name: body.Name, certificates: map[uuid.UUID]gwadminapi.EventGatewayDataPlaneCertificateOnPrem{}}
		}
		w.WriteHeader(http.StatusOK)
	}))
	mux.HandleFunc("DELETE /v1/event-gateways/{id}", f.withGateway(true, func(w http.ResponseWriter, _ *http.Request, id uuid.UUID, _ *fakeGateway) {
		// With its entities and its data plane certificates.
		delete(f.gateways, id)
		w.WriteHeader(http.StatusNoContent)
	}))
	mux.HandleFunc("PUT /v1/event-gateways/{id}/entities", f.withGateway(true, func(w http.ResponseWriter, r *http.Request, _ uuid.UUID, g *fakeGateway) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			badRequest(w, "", err.Error())
			return
		}
		var body struct {
			ConfigVersion json.RawMessage            `json:"config_version"`
			Entities      map[string]json.RawMessage `json:"entities"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			badRequest(w, "", err.Error())
			return
		}
		if len(body.ConfigVersion) == 0 || string(body.ConfigVersion) == "null" {
			badRequest(w, "config_version", "required and not nullable")
			return
		}
		for _, key := range []string{"backend_clusters", "listeners", "schema_registries", "static_keys", "tls_trust_bundles", "virtual_clusters"} {
			if v, ok := body.Entities[key]; !ok || string(v) == "null" {
				badRequest(w, "entities."+key, "required and not nullable")
				return
			}
		}
		var sent gwadminapi.GatewayEntities
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&sent); err != nil {
			badRequest(w, "", err.Error())
			return
		}
		doc := sent.Entities
		for i, bc := range doc.BackendClusters {
			if bc.Id.Version() != 4 {
				badRequest(w, fmt.Sprintf("entities.backend_clusters.%d.id", i), "not a version 4 UUID")
				return
			}
			if !createdAtPattern.MatchString(bc.CreatedAt.Format("2006-01-02T15:04:05.999999999Z07:00")) {
				badRequest(w, fmt.Sprintf("entities.backend_clusters.%d.created_at", i), "not whole seconds in UTC")
				return
			}
		}
		if f.reject != nil {
			if field := f.reject(doc); field != "" {
				badRequest(w, "entities."+field, "rejected by the test")
				return
			}
		}
		g.entities, g.version = withResolvedDestinations(doc), sent.ConfigVersion
		w.WriteHeader(http.StatusOK)
	}))
	mux.HandleFunc("GET /v1/event-gateways/{id}/entities", f.withGateway(true, func(w http.ResponseWriter, _ *http.Request, _ uuid.UUID, g *fakeGateway) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(gwadminapi.GatewayEntities{ConfigVersion: g.version, Entities: g.entities}); err != nil {
			panic(err)
		}
	}))
	mux.HandleFunc("GET /v1/event-gateways/{id}/data-plane-certificates", f.withGateway(true, func(w http.ResponseWriter, _ *http.Request, _ uuid.UUID, g *fakeGateway) {
		list := gwadminapi.ListEventGatewayDataPlaneCertificatesOnPrem{Data: []gwadminapi.EventGatewayDataPlaneCertificateOnPrem{}}
		for _, id := range slices.SortedFunc(maps.Keys(g.certificates), func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) }) {
			list.Data = append(list.Data, g.certificates[id])
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(list); err != nil {
			panic(err)
		}
	}))
	mux.HandleFunc("PUT /v1/event-gateways/{id}/data-plane-certificates/{cert}", f.withGateway(true, func(w http.ResponseWriter, r *http.Request, id uuid.UUID, g *fakeGateway) {
		cert, err := uuid.Parse(r.PathValue("cert"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		// gw-admin accepts unknown properties in a certificate, and any string
		// as one, but checks created_at.
		var body gwadminapi.UpsertEventGatewayDataPlaneCertificateRequestOnPrem
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			badRequest(w, "", err.Error())
			return
		}
		if !createdAtPattern.MatchString(body.CreatedAt.Format("2006-01-02T15:04:05.999999999Z07:00")) {
			badRequest(w, "created_at", "not whole seconds in UTC")
			return
		}
		if f.rejectCertificate != nil && f.rejectCertificate(id, body) {
			badRequest(w, "certificate", "rejected by the test")
			return
		}
		g.certificates[cert] = gwadminapi.EventGatewayDataPlaneCertificateOnPrem{
			Id: cert, Certificate: body.Certificate, CreatedAt: body.CreatedAt, Name: body.Name, Description: body.Description,
		}
		w.WriteHeader(http.StatusOK)
	}))
	mux.HandleFunc("DELETE /v1/event-gateways/{id}/data-plane-certificates/{cert}", f.withGateway(true, func(w http.ResponseWriter, r *http.Request, _ uuid.UUID, g *fakeGateway) {
		cert, err := uuid.Parse(r.PathValue("cert"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if _, ok := g.certificates[cert]; !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		delete(g.certificates, cert)
		w.WriteHeader(http.StatusNoContent)
	}))
	mux.HandleFunc("PUT /v1/expected-event-gateways", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		var body gwadminapi.ExpectedEventGateways
		if !decode(w, r, &body) {
			return
		}
		ids := slices.Clone(body.EventGatewayIds)
		slices.SortFunc(ids, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
		if len(slices.Compact(slices.Clone(ids))) != len(ids) {
			badRequest(w, "event_gateway_ids", "duplicate items found")
			return
		}
		f.expected = body.EventGatewayIds
		w.WriteHeader(http.StatusNoContent)
	})
	f.Server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			f.mu.Lock()
			f.writes++
			f.mu.Unlock()
		}
		mux.ServeHTTP(w, r)
	}))
	f.TLS = tlsConfig
	f.StartTLS()
	t.Cleanup(f.Close)
	return f
}

// withResolvedDestinations returns the entities as gw-admin keeps them: each
// destination with both the id and the name of its target, resolved from the
// one it was sent with, the backend cluster of a virtual cluster, or the
// virtual cluster a listener policy forwards to.
func withResolvedDestinations(e gwadminapi.GatewayEntitySet) gwadminapi.GatewayEntitySet {
	data, err := json.Marshal(e)
	if err != nil {
		panic(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		panic(err)
	}
	names := func(list string) (byID, byName map[string]string) {
		byID, byName = map[string]string{}, map[string]string{}
		items, _ := doc[list].([]any)
		for _, item := range items {
			fields, _ := item.(map[string]any)
			id, _ := fields["id"].(string)
			name, _ := fields["name"].(string)
			byID[id], byName[name] = name, id
		}
		return byID, byName
	}
	resolve := func(destination any, byID, byName map[string]string) {
		fields, ok := destination.(map[string]any)
		if !ok {
			return
		}
		if id, ok := fields["id"].(string); ok {
			fields["name"] = byID[id]
		} else if name, ok := fields["name"].(string); ok {
			fields["id"] = byName[name]
		}
	}
	bcByID, bcByName := names("backend_clusters")
	vcByID, vcByName := names("virtual_clusters")
	vcs, _ := doc["virtual_clusters"].([]any)
	for _, vc := range vcs {
		if fields, ok := vc.(map[string]any); ok {
			resolve(fields["destination"], bcByID, bcByName)
		}
	}
	listeners, _ := doc["listeners"].([]any)
	for _, l := range listeners {
		fields, _ := l.(map[string]any)
		policies, _ := fields["policies"].([]any)
		for _, p := range policies {
			policy, _ := p.(map[string]any)
			if config, ok := policy["config"].(map[string]any); ok {
				resolve(config["destination"], vcByID, vcByName)
			}
		}
	}
	if data, err = json.Marshal(doc); err != nil {
		panic(err)
	}
	var resolved gwadminapi.GatewayEntitySet
	if err := json.Unmarshal(data, &resolved); err != nil {
		panic(err)
	}
	return resolved
}

// withGateway serves a request on a gateway, with the lock held. When
// mustExist, an unknown gateway is a 404.
func (f *fakeGWAdmin) withGateway(mustExist bool, h func(http.ResponseWriter, *http.Request, uuid.UUID, *fakeGateway)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		id, err := uuid.Parse(r.PathValue("id"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		g, ok := f.gateways[id]
		if mustExist && !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"status":404,"title":"Not Found","detail":"The requested gateway was not found"}`))
			return
		}
		h(w, r, id, g)
	}
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		badRequest(w, "", err.Error())
		return false
	}
	return true
}

func badRequest(w http.ResponseWriter, field, reason string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(http.StatusBadRequest)
	type invalidParameter struct {
		Field  string `json:"field"`
		Rule   string `json:"rule"`
		Reason string `json:"reason"`
	}
	body := struct {
		Status            int                `json:"status"`
		Title             string             `json:"title"`
		Detail            string             `json:"detail"`
		InvalidParameters []invalidParameter `json:"invalid_parameters,omitempty"`
	}{Status: http.StatusBadRequest, Title: "Bad Request", Detail: "Bad Request: " + reason}
	if field != "" {
		body.InvalidParameters = []invalidParameter{{Field: field, Rule: "test", Reason: reason}}
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		panic(err)
	}
}

// state returns a copy of what gw-admin holds for the gateway, and the
// expected gateways.
func (f *fakeGWAdmin) state(id uuid.UUID) (fakeGateway, bool, []uuid.UUID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	g, ok := f.gateways[id]
	if !ok {
		return fakeGateway{}, false, slices.Clone(f.expected)
	}
	copied := *g
	copied.certificates = maps.Clone(g.certificates)
	return copied, true, slices.Clone(f.expected)
}

func (f *fakeGWAdmin) setReject(reject func(gwadminapi.GatewayEntitySet) string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reject = reject
}

// setRejectCertificate sets the data plane certificates the pod rejects (see
// fakeGWAdmin.rejectCertificate).
func (f *fakeGWAdmin) setRejectCertificate(reject func(gateway uuid.UUID, body gwadminapi.UpsertEventGatewayDataPlaneCertificateRequestOnPrem) bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rejectCertificate = reject
}

// writeCount returns how many requests changed what the pod holds.
func (f *fakeGWAdmin) writeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.writes
}

// testGWAdmin is a GWAdmin, as its controller provisions it, with fake
// gw-admin pods: the GWAdmin, with its API published in its status, the API CA
// bundle, the operator client certificate and the EndpointSlice of the API
// Service. The pods are reached on the addresses of the EndpointSlice through
// dial.
type testGWAdmin struct {
	t         *testing.T
	cl        client.Client
	namespace string
	apiCA     gwadminsynctest.CA

	gwAdmin       *operatorv1alpha1.GWAdmin
	endpointSlice *discoveryv1.EndpointSlice

	mu sync.Mutex
	// pods are the gw-admin pods by address.
	pods map[string]*fakeGWAdmin
	// unreachable are the addresses of the pods that can't be connected to,
	// although they hold what they were told, e.g. on a node cut off.
	unreachable map[string]bool
}

// testGWAdminName is the name of the GWAdmin of the tests, the one of
// gwadminsynctest.
const testGWAdminName = gwadminsynctest.GWAdminName

func newTestGWAdmin(t *testing.T, cl client.Client, namespace string) *testGWAdmin {
	t.Helper()
	return &testGWAdmin{
		t: t, cl: cl, namespace: namespace,
		apiCA: gwadminsynctest.NewCA(t, "api-ca"),
		pods:  map[string]*fakeGWAdmin{},
	}
}

// reconciler returns an OnPremEventGateway reconciler of the manager pushing
// to the pods of the GWAdmin.
func (g *testGWAdmin) reconciler(ctx context.Context, mgr ctrl.Manager) *egonprem.Reconciler {
	return &egonprem.Reconciler{
		Client: mgr.GetClient(),
		GWAdminSync: &gwadminsync.Syncer{
			Client:            mgr.GetClient(),
			OperatorNamespace: g.namespace,
			DialContext:       g.dial,
		},
		TypeConverter: newOnPremTypeConverter(ctx, g.t, mgr),
	}
}

// newOnPremTypeConverter returns the type converter the OnPremEventGateway
// controller applies the status of the gateways with, built from the CRDs
// envtest installed, as modules/manager does.
func newOnPremTypeConverter(ctx context.Context, t *testing.T, mgr ctrl.Manager) managedfields.TypeConverter {
	t.Helper()
	ssaProvider, err := controllerpkgssa.NewTypeConverterProvider(ctx, mgr.GetLogger(), mgr, map[string]struct{}{eventgatewayv1alpha1.GroupVersion.Group: {}})
	require.NoError(t, err)
	return ssaProvider
}

func (g *testGWAdmin) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	g.mu.Lock()
	pod, ok := g.pods[addr]
	unreachable := g.unreachable[addr]
	g.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("no gw-admin pod at %s", addr)
	}
	if unreachable {
		return nil, &net.OpError{Op: "dial", Net: network, Err: errors.New("unreachable")}
	}
	var d net.Dialer
	return d.DialContext(ctx, network, pod.Listener.Addr().String())
}

// createGWAdmin creates the GWAdmin, without its status.
func (g *testGWAdmin) createGWAdmin(ctx context.Context) {
	g.t.Helper()
	g.gwAdmin = &operatorv1alpha1.GWAdmin{Name: testGWAdminName, Namespace: g.namespace}
	require.NoError(g.t, g.cl.Create(ctx, g.gwAdmin))
}

// publishAPI publishes the API of the GWAdmin in its status, with its API CA
// bundle and the EndpointSlice of its API Service, with no pod.
func (g *testGWAdmin) publishAPI(ctx context.Context) {
	g.t.Helper()
	require.NoError(g.t, g.cl.Create(ctx, &corev1.Secret{
		Name: gwadminsynctest.APIBundleSecret, Namespace: g.namespace,
		Labels: gwadminsynctest.SecretLabels(g.gwAdmin, false),
		Data:   map[string][]byte{"ca.crt": g.apiCA.PEM},
	}))
	// The EndpointSlice controller labels an EndpointSlice like its Service.
	labels := gwadminsynctest.EndpointSliceLabels(gwadminsynctest.APIServiceName)
	g.endpointSlice = &discoveryv1.EndpointSlice{
		Name: gwadminsynctest.APIServiceName + "-abcde", Namespace: g.namespace,
		Labels:      labels,
		AddressType: discoveryv1.AddressTypeIPv4,
		Ports:       []discoveryv1.EndpointPort{{Name: new("api"), Port: new(int32(gwadminsynctest.APIPort))}},
	}
	require.NoError(g.t, g.cl.Create(ctx, g.endpointSlice))
	g.updateStatus(ctx, func(status *operatorv1alpha1.GWAdminStatus) {
		status.API = &operatorv1alpha1.GWAdminAPIStatus{ServiceName: gwadminsynctest.APIServiceName, PortName: "api", Port: gwadminsynctest.APIPort}
		status.CP = &operatorv1alpha1.GWAdminCPStatus{ServerURL: gwadminsynctest.ServerURL(g.namespace)}
		status.TLS = &operatorv1alpha1.GWAdminTLSStatus{APICABundleSecretName: gwadminsynctest.APIBundleSecret}
	})
}

// updateStatus updates the status of the GWAdmin, as its controller does.
func (g *testGWAdmin) updateStatus(ctx context.Context, update func(*operatorv1alpha1.GWAdminStatus)) {
	g.t.Helper()
	// The client reads from the cache, which may not hold the last update yet,
	// nor the GWAdmin just created.
	require.NoError(g.t, retry.OnError(retry.DefaultBackoff, func(err error) bool {
		return apierrors.IsConflict(err) || apierrors.IsNotFound(err)
	}, func() error {
		if err := g.cl.Get(ctx, client.ObjectKeyFromObject(g.gwAdmin), g.gwAdmin); err != nil {
			return err
		}
		update(&g.gwAdmin.Status)
		return g.cl.Status().Update(ctx, g.gwAdmin)
	}))
}

// issueOperatorClientCertificate creates the operator client certificate
// Secret of the GWAdmin, in the operator namespace.
func (g *testGWAdmin) issueOperatorClientCertificate(ctx context.Context) {
	g.t.Helper()
	certPEM, keyPEM := g.apiCA.Issue("kong-operator")
	require.NoError(g.t, g.cl.Create(ctx, &corev1.Secret{
		Name: gwadminref.OperatorClientTLSSecretName(g.gwAdmin), Namespace: g.namespace,
		Labels: gwadminsynctest.SecretLabels(g.gwAdmin, true),
		Type:   corev1.SecretTypeTLS,
		Data:   map[string][]byte{"tls.crt": certPEM, "tls.key": keyPEM},
	}))
}

// serverTLS returns the TLS configuration of a gw-admin pod whose server
// certificate is issued by ca: the API listener requires a client certificate
// issued by the API CA.
func (g *testGWAdmin) serverTLS(ca gwadminsynctest.CA) *tls.Config {
	certPEM, keyPEM := ca.Issue(gwadminsynctest.APIServiceName, gwadminsynctest.APIServiceName+"."+g.namespace+".svc")
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	require.NoError(g.t, err)
	clientCAs := x509.NewCertPool()
	clientCAs.AddCert(g.apiCA.Cert)
	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    clientCAs,
	}
}

// startPod starts a gw-admin pod on the address and adds it to the
// EndpointSlice, not Ready, as gw-admin is until provisioned.
func (g *testGWAdmin) startPod(ctx context.Context, name, ip string) *fakeGWAdmin {
	return g.startPodWith(ctx, name, ip, g.apiCA)
}

// startPodWith starts a gw-admin pod whose server certificate is issued by
// ca.
func (g *testGWAdmin) startPodWith(ctx context.Context, name, ip string, ca gwadminsynctest.CA) *fakeGWAdmin {
	g.t.Helper()
	pod := newFakeGWAdmin(g.t, g.serverTLS(ca))
	g.mu.Lock()
	g.pods[fmt.Sprintf("%s:%d", ip, gwadminsynctest.APIPort)] = pod
	g.mu.Unlock()
	g.updateEndpoints(ctx, func(endpoints []discoveryv1.Endpoint) []discoveryv1.Endpoint {
		return append(endpoints, discoveryv1.Endpoint{
			Addresses: []string{ip},
			// Ready, as the API Service publishes the pods not Ready, but not
			// serving until provisioned, as the EndpointSlice controller
			// publishes it.
			Conditions: discoveryv1.EndpointConditions{Ready: new(true), Serving: new(false), Terminating: new(false)},
			TargetRef:  &corev1.ObjectReference{Kind: "Pod", Name: name, Namespace: g.namespace, UID: types.UID(name + "-uid")},
		})
	})
	return pod
}

// disconnect makes the pod on the address unreachable, and not serving, as
// its endpoint tells, while it keeps what it holds; reconnect makes it
// reachable and serving again.
func (g *testGWAdmin) disconnect(ctx context.Context, ip string) {
	g.t.Helper()
	addr := fmt.Sprintf("%s:%d", ip, gwadminsynctest.APIPort)
	g.mu.Lock()
	if g.unreachable == nil {
		g.unreachable = map[string]bool{}
	}
	g.unreachable[addr] = true
	pod := g.pods[addr]
	g.mu.Unlock()
	pod.CloseClientConnections()
	g.setServing(ctx, ip, false)
}

func (g *testGWAdmin) reconnect(ctx context.Context, ip string) {
	g.t.Helper()
	g.mu.Lock()
	delete(g.unreachable, fmt.Sprintf("%s:%d", ip, gwadminsynctest.APIPort))
	g.mu.Unlock()
	g.setServing(ctx, ip, true)
}

func (g *testGWAdmin) setServing(ctx context.Context, ip string, serving bool) {
	g.updateEndpoints(ctx, func(endpoints []discoveryv1.Endpoint) []discoveryv1.Endpoint {
		for i := range endpoints {
			if endpoints[i].Addresses[0] == ip {
				endpoints[i].Conditions.Serving = new(serving)
			}
		}
		return endpoints
	})
}

// stopPod removes the pod from the EndpointSlice, and closes its
// connections, as a pod stopped does: a pod started on its address, e.g. the
// pod restarted, isn't reached over them.
func (g *testGWAdmin) stopPod(ctx context.Context, ip string) {
	g.t.Helper()
	g.updateEndpoints(ctx, func(endpoints []discoveryv1.Endpoint) []discoveryv1.Endpoint {
		return slices.DeleteFunc(endpoints, func(ep discoveryv1.Endpoint) bool { return ep.Addresses[0] == ip })
	})
	addr := fmt.Sprintf("%s:%d", ip, gwadminsynctest.APIPort)
	g.mu.Lock()
	pod := g.pods[addr]
	delete(g.pods, addr)
	g.mu.Unlock()
	if pod != nil {
		pod.CloseClientConnections()
	}
}

func (g *testGWAdmin) updateEndpoints(ctx context.Context, update func([]discoveryv1.Endpoint) []discoveryv1.Endpoint) {
	g.t.Helper()
	// The client reads from the cache, which may not hold the last update yet,
	// nor the GWAdmin just created.
	require.NoError(g.t, retry.OnError(retry.DefaultBackoff, func(err error) bool {
		return apierrors.IsConflict(err) || apierrors.IsNotFound(err)
	}, func() error {
		if err := g.cl.Get(ctx, client.ObjectKeyFromObject(g.endpointSlice), g.endpointSlice); err != nil {
			return err
		}
		g.endpointSlice.Endpoints = update(g.endpointSlice.Endpoints)
		return g.cl.Update(ctx, g.endpointSlice)
	}))
}

func gatewayID(t require.TestingT, gateway *eventgatewayv1alpha1.OnPremEventGateway) uuid.UUID {
	id, err := uuid.Parse(string(gateway.UID))
	require.NoError(t, err)
	return id
}

// TestOnPremEventGatewayPush verifies that the configuration of an
// OnPremEventGateway is pushed to every gw-admin pod of its GWAdmin, over
// mutual TLS, pushed again to a new pod, reported on the gateway, and removed
// from gw-admin when the gateway is deleted.
func TestOnPremEventGatewayPush(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get(), envtest.WithInstallGatewayCRDs(true))
	// A resync would push again to a new gw-admin pod: only the watches must.
	mgr, logs := newManagerWithoutResync(t, ctx, cfg)
	cl := mgr.GetClient()
	gwAdmin := newTestGWAdmin(t, cl, ns.Name)
	envtest.StartReconcilersWithConfig(ctx, t, mgr, logs, onPremControllerConfig, gwAdmin.reconciler(ctx, mgr))

	newGateway := func(name string) *eventgatewayv1alpha1.OnPremEventGateway {
		return objectFromYAML[eventgatewayv1alpha1.OnPremEventGateway](t, ns.Name, `
metadata: {name: `+name+`}
spec:
  gwAdminRef: {name: `+testGWAdminName+`}
`)
	}
	getGateway := func(ct *assert.CollectT, gateway *eventgatewayv1alpha1.OnPremEventGateway) *eventgatewayv1alpha1.OnPremEventGateway {
		g := &eventgatewayv1alpha1.OnPremEventGateway{}
		if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(gateway), g)) {
			return nil
		}
		return g
	}
	conditionOf := func(ct *assert.CollectT, gateway *eventgatewayv1alpha1.OnPremEventGateway, conditionType string) *metav1.Condition {
		g := getGateway(ct, gateway)
		if g == nil {
			return nil
		}
		cond := apimeta.FindStatusCondition(g.Status.Conditions, conditionType)
		assert.NotNil(ct, cond, "condition %s", conditionType)
		if cond != nil {
			assert.Equal(ct, g.Generation, cond.ObservedGeneration)
		}
		return cond
	}
	expectNotPushed := func(t *testing.T, gateway *eventgatewayv1alpha1.OnPremEventGateway, reason, message string) {
		t.Helper()
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			for _, conditionType := range []string{"Programmed", "ConfigurationPushed"} {
				if cond := conditionOf(ct, gateway, conditionType); cond != nil {
					assert.Equal(ct, metav1.ConditionFalse, cond.Status)
					assert.Equal(ct, reason, cond.Reason)
					assert.Contains(ct, cond.Message, message)
				}
			}
		}, waitTime, tickTime)
	}
	// expectUnresolved waits for the gateway to report its GWAdmin can't serve
	// it, with the reason and the message of the GWAdminResolved condition.
	expectUnresolved := func(t *testing.T, gateway *eventgatewayv1alpha1.OnPremEventGateway, reason, message string) {
		t.Helper()
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			if resolved := conditionOf(ct, gateway, "GWAdminResolved"); resolved != nil {
				assert.Equal(ct, metav1.ConditionFalse, resolved.Status)
				assert.Equal(ct, reason, resolved.Reason)
				assert.Contains(ct, resolved.Message, message)
			}
			if pushed := conditionOf(ct, gateway, "ConfigurationPushed"); pushed != nil {
				assert.Equal(ct, metav1.ConditionFalse, pushed.Status)
				assert.Equal(ct, "DependenciesNotReady", pushed.Reason)
				assert.Contains(ct, pushed.Message, message)
			}
			if programmed := conditionOf(ct, gateway, "Programmed"); programmed != nil {
				assert.Equal(ct, metav1.ConditionFalse, programmed.Status)
				assert.Equal(ct, "DependenciesNotReady", programmed.Reason)
				assert.Equal(ct, eventgatewayv1alpha1.OnPremEventGatewayGWAdminNotResolvedMessage, programmed.Message)
			}
		}, waitTime, tickTime)
	}
	// expectPushed waits for the gateway to be Programmed, and for every pod
	// to hold what check expects.
	expectPushed := func(t *testing.T, gateway *eventgatewayv1alpha1.OnPremEventGateway, pods []*fakeGWAdmin, check func(ct *assert.CollectT, g fakeGateway, expected []uuid.UUID)) {
		t.Helper()
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			if programmed := conditionOf(ct, gateway, "Programmed"); programmed != nil {
				assert.Equal(ct, metav1.ConditionTrue, programmed.Status, programmed.Message)
				assert.Equal(ct, "Programmed", programmed.Reason)
			}
			if pushed := conditionOf(ct, gateway, "ConfigurationPushed"); pushed != nil {
				assert.Equal(ct, metav1.ConditionTrue, pushed.Status, pushed.Message)
			}
			// The configuration of a gateway without entities, its name and
			// creation time, never changes: its first revision stays, whatever
			// the status writes of the reconciles.
			if g := getGateway(ct, gateway); g != nil {
				assert.Equal(ct, int64(1), g.Status.ConfigRevision)
				assert.True(ct, strings.HasPrefix(g.Status.ConfigHash, "sha256:"), g.Status.ConfigHash)
			}
			for i, pod := range pods {
				g, ok, expected := pod.state(gatewayID(ct, gateway))
				if assert.True(ct, ok, "gw-admin pod %d doesn't know the gateway", i) {
					assert.Equal(ct, gateway.Name, g.name)
					check(ct, g, expected)
				}
			}
		}, waitTime, tickTime)
	}

	gatewayA := newGateway("gateway-a")
	require.NoError(t, cl.Create(ctx, gatewayA))

	t.Run("a gateway without its GWAdmin reports it", func(t *testing.T) {
		expectUnresolved(t, gatewayA, "NotFound", "The GWAdmin "+ns.Name+"/"+testGWAdminName+" does not exist")
	})

	// What a gateway reports while its GWAdmin misses something is tested by
	// TestOnPremEventGatewayGWAdmin.
	t.Run("a GWAdmin provisioned without pods", func(t *testing.T) {
		gwAdmin.createGWAdmin(ctx)
		gwAdmin.publishAPI(ctx)
		gwAdmin.issueOperatorClientCertificate(ctx)
		expectUnresolved(t, gatewayA, "NotReady", "has no gw-admin pod")
	})

	pod0 := gwAdmin.startPod(ctx, "gw-admin-0", "10.0.0.1")
	pod1 := gwAdmin.startPod(ctx, "gw-admin-1", "10.0.0.2")

	t.Run("a gateway is pushed to every gw-admin pod over mutual TLS", func(t *testing.T) {
		expectPushed(t, gatewayA, []*fakeGWAdmin{pod0, pod1}, func(ct *assert.CollectT, g fakeGateway, expected []uuid.UUID) {
			assert.Equal(ct, []uuid.UUID{gatewayID(ct, gatewayA)}, expected)
			assert.Empty(ct, g.entities.BackendClusters)
		})
		require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(gatewayA), gatewayA))
		require.Contains(t, gatewayA.Finalizers, egonprem.FinalizerGWAdminCleanup)
		require.Equal(t, gwadminsynctest.ServerURL(ns.Name), gatewayA.Status.ServerURL)
	})

	t.Run("a status changed by someone else is set back, without pushing again", func(t *testing.T) {
		writes := pod0.writeCount() + pod1.writeCount()
		// The API server is read, not the cache: the cache may not hold the
		// change yet, and would show the status as set back already.
		reader := mgr.GetAPIReader()
		require.NoError(t, retry.RetryOnConflict(retry.DefaultRetry, func() error {
			g := &eventgatewayv1alpha1.OnPremEventGateway{}
			if err := reader.Get(ctx, client.ObjectKeyFromObject(gatewayA), g); err != nil {
				return err
			}
			apimeta.SetStatusCondition(&g.Status.Conditions, metav1.Condition{
				Type: "Programmed", Status: metav1.ConditionFalse, Reason: "Tampered", Message: "changed by someone else", ObservedGeneration: g.Generation,
			})
			return cl.Status().Update(ctx, g)
		}))
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			g := &eventgatewayv1alpha1.OnPremEventGateway{}
			if !assert.NoError(ct, reader.Get(ctx, client.ObjectKeyFromObject(gatewayA), g)) {
				return
			}
			if programmed := apimeta.FindStatusCondition(g.Status.Conditions, "Programmed"); assert.NotNil(ct, programmed) {
				assert.Equal(ct, metav1.ConditionTrue, programmed.Status)
				assert.Equal(ct, "Programmed", programmed.Reason)
			}
		}, waitTime, tickTime)
		require.Equal(t, writes, pod0.writeCount()+pod1.writeCount(), "nothing changed for gw-admin: the pods are only read")
	})

	t.Run("a new gw-admin pod gets the configuration", func(t *testing.T) {
		// A pod replaced by a new one, e.g. in a rollout: the new pod has a
		// new address and holds nothing.
		gwAdmin.stopPod(ctx, "10.0.0.2")
		pod2 := gwAdmin.startPod(ctx, "gw-admin-2", "10.0.0.3")
		expectPushed(t, gatewayA, []*fakeGWAdmin{pod0, pod2}, func(ct *assert.CollectT, g fakeGateway, expected []uuid.UUID) {
			assert.Equal(ct, []uuid.UUID{gatewayID(ct, gatewayA)}, expected)
			assert.NotNil(ct, g.entities.BackendClusters, "the entities are pushed, none yet")
		})
		pod1 = pod2
	})

	t.Run("a gw-admin pod whose server certificate can't be verified is not pushed to", func(t *testing.T) {
		rogue := gwAdmin.startPodWith(ctx, "gw-admin-rogue", "10.0.0.9", gwadminsynctest.NewCA(t, "rogue-ca"))
		expectNotPushed(t, gatewayA, "ConfigurationPushFailed", "gw-admin pod gw-admin-rogue: failed to read the entities of the gateway")
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			if cond := conditionOf(ct, gatewayA, "ConfigurationPushed"); cond != nil {
				assert.Contains(ct, cond.Message, "certificate signed by unknown authority")
				assert.NotContains(ct, cond.Message, "gw-admin-0")
			}
		}, waitTime, tickTime)
		_, ok, _ := rogue.state(gatewayID(t, gatewayA))
		require.False(t, ok, "the rogue pod must get nothing")
		gwAdmin.stopPod(ctx, "10.0.0.9")
		expectPushed(t, gatewayA, []*fakeGWAdmin{pod0, pod1}, func(*assert.CollectT, fakeGateway, []uuid.UUID) {})
	})

	gatewayB := newGateway("gateway-b")

	t.Run("gw-admin serves every gateway of its GWAdmin", func(t *testing.T) {
		require.NoError(t, cl.Create(ctx, gatewayB))
		ids := []uuid.UUID{}
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			if !assert.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(gatewayB), gatewayB)) || !assert.NotEmpty(ct, gatewayB.UID) {
				return
			}
			ids = []uuid.UUID{gatewayID(ct, gatewayA), gatewayID(ct, gatewayB)}
			slices.SortFunc(ids, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
		}, waitTime, tickTime)
		expectPushed(t, gatewayB, []*fakeGWAdmin{pod0, pod1}, func(ct *assert.CollectT, _ fakeGateway, expected []uuid.UUID) {
			assert.Equal(ct, expected, ids)
		})
	})

	t.Run("a deleted gateway is removed from every gw-admin pod", func(t *testing.T) {
		require.NoError(t, cl.Delete(ctx, gatewayB))
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			assert.True(ct, apierrors.IsNotFound(cl.Get(ctx, client.ObjectKeyFromObject(gatewayB), &eventgatewayv1alpha1.OnPremEventGateway{})))
			for i, pod := range []*fakeGWAdmin{pod0, pod1} {
				_, ok, expected := pod.state(gatewayID(ct, gatewayB))
				assert.False(ct, ok, "gw-admin pod %d still serves the deleted gateway", i)
				assert.Equal(ct, []uuid.UUID{gatewayID(ct, gatewayA)}, expected)
			}
		}, waitTime, tickTime)
		_, ok, _ := pod0.state(gatewayID(t, gatewayA))
		require.True(t, ok)
	})

	t.Run("a gateway whose GWAdmin is gone is deleted without waiting for its pods", func(t *testing.T) {
		gatewayC := newGateway("gateway-c")
		require.NoError(t, cl.Create(ctx, gatewayC))
		expectPushed(t, gatewayC, []*fakeGWAdmin{pod0, pod1}, func(*assert.CollectT, fakeGateway, []uuid.UUID) {})

		require.NoError(t, cl.Delete(ctx, gwAdmin.gwAdmin))
		// The pods of the deleted GWAdmin are collected after it, but can't
		// be told anything anymore: their operator client certificate is
		// named after the UID of the GWAdmin, which is gone, and the GWAdmin
		// controller deletes it before letting the GWAdmin go.
		expectUnresolved(t, gatewayA, "NotFound", "but its gw-admin pods are still running")
		require.NoError(t, cl.Delete(ctx, gatewayC))
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			assert.True(ct, apierrors.IsNotFound(cl.Get(ctx, client.ObjectKeyFromObject(gatewayC), &eventgatewayv1alpha1.OnPremEventGateway{})))
		}, waitTime, tickTime, "deleted within waitTime, well before the cleanup grace period ends")

		gwAdmin.updateEndpoints(ctx, func([]discoveryv1.Endpoint) []discoveryv1.Endpoint { return nil })
		expectUnresolved(t, gatewayA, "NotFound", "The GWAdmin "+ns.Name+"/"+testGWAdminName+" does not exist")
		// The cp endpoint the controller published goes with the GWAdmin.
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			if g := getGateway(ct, gatewayA); g != nil {
				assert.Empty(ct, g.Status.ServerURL)
			}
		}, waitTime, tickTime)
		require.NoError(t, cl.Delete(ctx, gatewayA))
		require.EventuallyWithT(t, func(ct *assert.CollectT) {
			assert.True(ct, apierrors.IsNotFound(cl.Get(ctx, client.ObjectKeyFromObject(gatewayA), &eventgatewayv1alpha1.OnPremEventGateway{})))
		}, waitTime, tickTime)
	})
}
