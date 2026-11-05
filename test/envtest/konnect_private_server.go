package envtest

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kong/kong-operator/v2/test/helpers/certificate"
)

// Values for the private Konnect path: the two hostnames Kong serves behind
// PrivateLink, and the identifiers the fake answers with. The chainsaw suite
// under test/e2e/chainsaw/fixtures/konnect-private mirrors them.
const (
	// KonnectPrivateServerURL is the canonical private serverURL.
	KonnectPrivateServerURL = "https://us.svc.konghq.tech/api"
	// KonnectPrivateRegionalHost is the geo host serving the API and telemetry.
	KonnectPrivateRegionalHost = "us.svc.konghq.tech"
	// KonnectPrivateGlobalHost is the host the global-only APIs go to.
	KonnectPrivateGlobalHost = "global.svc.konghq.tech"

	KonnectPrivateOrgID                   = "5f9fd312-a987-4628-b4c5-bb4f4fddd5f7"
	KonnectPrivateControlPlaneID          = "aee0667a-90c6-45a6-a2d8-575e1e487b86"
	KonnectPrivateNetworkID               = "0d6a9a44-6d3c-4e0e-9a3b-2a3f9c1b7e10"
	KonnectPrivateControlPlaneEndpoint    = "https://" + KonnectPrivateRegionalHost + "/cp/abc123"
	KonnectPrivateTelemetryEndpoint       = "https://" + KonnectPrivateRegionalHost + "/tp/abc123"
	konnectPrivateControlPlaneSubresource = "/api/v2/control-planes/"
	konnectPrivateNetworksPath            = "/api/v2/cloud-gateways/networks"
)

// KonnectRequest is one request the fake received. ServerName is the SNI the
// client presented, Host the authority it addressed.
type KonnectRequest struct {
	Method        string
	Host          string
	ServerName    string
	Path          string
	Authorization string
}

// KonnectPrivateServer is a fake Konnect API answering on the private
// hostnames over TLS, so the operator's own client reaches it with the real
// hostname, path and SNI.
type KonnectPrivateServer struct {
	*httptest.Server

	t        *testing.T
	mu       sync.Mutex
	requests []KonnectRequest
	caPEM    []byte
	listener net.Addr
}

// StartKonnectPrivateServer starts the fake. Its certificate is signed by a CA
// generated for the test and covers the two private hostnames only: a client
// that trusts this CA cannot reach any other host.
func StartKonnectPrivateServer(t *testing.T) *KonnectPrivateServer {
	t.Helper()

	ca := certificate.MustGenerateCert(
		certificate.WithCommonName("konnect-private-test-ca"),
		certificate.WithCATrue(),
	)
	caPEM, _ := certificate.CertToPEMFormat(ca)
	leaf := certificate.MustGenerateCert(
		certificate.WithCommonName(KonnectPrivateRegionalHost),
		certificate.WithParent(ca),
		certificate.WithDNSNames(KonnectPrivateRegionalHost, KonnectPrivateGlobalHost),
	)

	s := &KonnectPrivateServer{t: t, caPEM: caPEM}
	s.Server = httptest.NewUnstartedServer(s.handler())
	s.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{leaf},
	}
	s.StartTLS()
	s.listener = s.Listener.Addr()
	t.Cleanup(s.Close)

	return s
}

// HTTPClient returns a client that dials every address at the fake while
// keeping the hostname, the path and the certificate verification of the
// original request: it trusts the fake's CA and nothing else.
func (s *KonnectPrivateServer) HTTPClient() *http.Client {
	s.t.Helper()

	pool := x509.NewCertPool()
	require.True(s.t, pool.AppendCertsFromPEM(s.caPEM), "fake CA must be parseable")

	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: pool},
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, network, s.listener.String())
			},
		},
	}
}

// Requests returns the requests received so far.
func (s *KonnectPrivateServer) Requests() []KonnectRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]KonnectRequest(nil), s.requests...)
}

func (s *KonnectPrivateServer) handler() http.Handler {
	mux := http.NewServeMux()

	controlPlane := map[string]any{
		"id":   KonnectPrivateControlPlaneID,
		"name": "private",
		"config": map[string]any{
			"cluster_type":           "CLUSTER_TYPE_CONTROL_PLANE",
			"control_plane_endpoint": KonnectPrivateControlPlaneEndpoint,
			"telemetry_endpoint":     KonnectPrivateTelemetryEndpoint,
		},
	}
	page := map[string]any{"page": map[string]any{"number": 1, "size": 10, "total": 1}}
	network := map[string]any{"id": KonnectPrivateNetworkID, "state": "initializing"}

	regional := func(method, path string, status int, body any) {
		mux.HandleFunc(method+" "+KonnectPrivateRegionalHost+path, jsonHandler(status, body))
	}
	global := func(method, path string, status int, body any) {
		mux.HandleFunc(method+" "+KonnectPrivateGlobalHost+path, jsonHandler(status, body))
	}

	regional("GET", "/api/v3/organizations/me", http.StatusOK, map[string]any{"id": KonnectPrivateOrgID})
	regional("POST", "/api/v2/control-planes", http.StatusCreated, controlPlane)
	regional("GET", "/api/v2/control-planes", http.StatusOK, map[string]any{"meta": page, "data": []any{controlPlane}})
	regional("PATCH", konnectPrivateControlPlaneSubresource+"{id}", http.StatusOK, controlPlane)
	regional("DELETE", konnectPrivateControlPlaneSubresource+"{id}", http.StatusNoContent, nil)

	// The data plane certificate calls are not served here: nothing in this
	// test attaches a data plane, and the cluster suite covers that flow with
	// its own fixture (the Kong stub). A call to them lands in the catch-all
	// below, which is the intent.
	global("POST", konnectPrivateNetworksPath, http.StatusCreated, network)
	global("GET", konnectPrivateNetworksPath+"/{id}", http.StatusOK, network)
	global("DELETE", konnectPrivateNetworksPath+"/{id}", http.StatusNoContent, nil)

	// Anything else is a call the test does not know about: record it and fail,
	// so an operator change that adds a call surfaces here instead of silently.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		s.t.Errorf("unexpected Konnect request %s %s%s", r.Method, r.Host, r.URL.Path)
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusNotFound)
	})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := KonnectRequest{Method: r.Method, Host: r.Host, Path: r.URL.Path, Authorization: r.Header.Get("Authorization")}
		if r.TLS != nil {
			req.ServerName = r.TLS.ServerName
		}
		s.mu.Lock()
		s.requests = append(s.requests, req)
		s.mu.Unlock()
		mux.ServeHTTP(w, r)
	})
}

func jsonHandler(status int, body any) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if body == nil {
			return
		}
		if err := json.NewEncoder(w).Encode(body); err != nil {
			panic(fmt.Sprintf("encoding the fake response: %v", err))
		}
	}
}
