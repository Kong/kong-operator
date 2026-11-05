package eventgateway

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminapi"
	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminsync/gwadminsynctest"
)

// The behaviors of gw-admin the OnPremEventGateway controller relies on, each
// as a named test. They run against the fake gw-admin of the envtests, and
// against a real one when GW_ADMIN_CONTRACT_URL is set, e.g.
// https://127.0.0.1:8083/v1 through a port-forward, with
// GW_ADMIN_CONTRACT_SERVER_NAME (the name of the API Service),
// GW_ADMIN_CONTRACT_CA (the API CA bundle), GW_ADMIN_CONTRACT_CERT and
// GW_ADMIN_CONTRACT_KEY (the operator client certificate). The real gw-admin
// must serve nothing else: the tests replace the gateways it serves.
//
// A failing test against a real gw-admin points at a behavior that changed,
// and that the fake must follow.

// contractGWAdmin returns a client of the gw-admin under test.
func contractGWAdmin(t *testing.T) gwadminapi.ClientInterface {
	t.Helper()
	url := os.Getenv("GW_ADMIN_CONTRACT_URL")
	if url == "" {
		apiCA := gwadminsynctest.NewCA(t, "api-ca")
		const serverName = "gw-admin-api.contract.svc"
		certPEM, keyPEM := apiCA.Issue("gw-admin-api", serverName)
		serverCert, err := tls.X509KeyPair(certPEM, keyPEM)
		require.NoError(t, err)
		clientCAs := x509.NewCertPool()
		clientCAs.AddCert(apiCA.Cert)
		pod := newFakeGWAdmin(t, &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{serverCert},
			ClientAuth:   tls.RequireAndVerifyClientCert,
			ClientCAs:    clientCAs,
		})
		clientCertPEM, clientKeyPEM := apiCA.Issue("kong-operator")
		return newContractClient(t, pod.URL+"/v1", serverName, apiCA.PEM, clientCertPEM, clientKeyPEM)
	}
	read := func(env string) []byte {
		data, err := os.ReadFile(os.Getenv(env))
		require.NoError(t, err, env)
		return data
	}
	return newContractClient(t, url, os.Getenv("GW_ADMIN_CONTRACT_SERVER_NAME"),
		read("GW_ADMIN_CONTRACT_CA"), read("GW_ADMIN_CONTRACT_CERT"), read("GW_ADMIN_CONTRACT_KEY"))
}

func newContractClient(t *testing.T, url, serverName string, caPEM, certPEM, keyPEM []byte) gwadminapi.ClientInterface {
	t.Helper()
	roots := x509.NewCertPool()
	require.True(t, roots.AppendCertsFromPEM(caPEM))
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	require.NoError(t, err)
	httpClient := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12, RootCAs: roots, Certificates: []tls.Certificate{cert}, ServerName: serverName,
		}},
	}
	c, err := gwadminapi.NewClient(url, gwadminapi.WithHTTPClient(httpClient))
	require.NoError(t, err)
	return c
}

// code returns a function returning the status code of a response, which it
// closes.
func code(t *testing.T) func(*http.Response, error) int {
	return func(resp *http.Response, err error) int {
		t.Helper()
		require.NoError(t, err)
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		return resp.StatusCode
	}
}

// contractEntities returns the entities of a gateway: the backend cluster.
// contractVersion is the version of the configuration the entities of the
// tests are sent with.
var contractVersion = gwadminapi.GatewayConfigVersion{DataVersion: 1, OperatorVersion: "1.0.0"}

func contractEntities(bc gwadminapi.EventGatewayBackendClusterOnPrem) gwadminapi.GatewayEntities {
	return gwadminapi.GatewayEntities{ConfigVersion: contractVersion, Entities: gwadminapi.GatewayEntitySet{
		BackendClusters:  []gwadminapi.EventGatewayBackendClusterOnPrem{bc},
		Listeners:        []gwadminapi.EventGatewayListenerOnPrem{},
		SchemaRegistries: []gwadminapi.EventGatewaySchemaRegistryOnPrem{},
		StaticKeys:       []gwadminapi.EventGatewayStaticKeyOnPrem{},
		TlsTrustBundles:  []gwadminapi.EventGatewayTLSTrustBundleOnPrem{},
		VirtualClusters:  []gwadminapi.EventGatewayVirtualClusterOnPrem{},
	}}
}

func contractBackendCluster(t *testing.T, name string) gwadminapi.EventGatewayBackendClusterOnPrem {
	t.Helper()
	var bc gwadminapi.EventGatewayBackendClusterOnPrem
	require.NoError(t, json.Unmarshal([]byte(`{
		"id": "`+uuid.NewString()+`",
		"created_at": "2026-10-19T10:00:00Z",
		"name": "`+name+`",
		"bootstrap_servers": ["kafka:9092"],
		"authentication": {"type": "anonymous"},
		"tls": {"enabled": false}
	}`), &bc))
	return bc
}

// registerContractGateway registers a gateway, serves it alone, and deletes
// it at the end of the test.
//
//nolint:bodyclose // code closes every response it is given.
func registerContractGateway(t *testing.T, gwAdmin gwadminapi.ClientInterface) uuid.UUID {
	t.Helper()
	id := uuid.New()
	ctx := t.Context()
	require.Less(t, code(t)(gwAdmin.UpdateEventGateway(ctx, id, gwadminapi.UpsertGatewayRequestOnPrem{Name: "contract", CreatedAt: time.Now().UTC().Truncate(time.Second)})), 300)
	require.Equal(t, http.StatusNoContent, code(t)(gwAdmin.SetExpectedEventGateways(ctx, gwadminapi.ExpectedEventGateways{EventGatewayIds: []uuid.UUID{id}})))
	t.Cleanup(func() {
		// The context of the test is canceled when its cleanups run.
		ctx := context.Background()
		_ = code(t)(gwAdmin.SetExpectedEventGateways(ctx, gwadminapi.ExpectedEventGateways{EventGatewayIds: []uuid.UUID{}}))
		_ = code(t)(gwAdmin.DeleteEventGateway(ctx, id))
	})
	return id
}

//nolint:bodyclose // code closes every response it is given.
func TestGWAdminContract(t *testing.T) {
	t.Parallel()

	t.Run("a gateway must be registered before its entities and certificates", func(t *testing.T) {
		gwAdmin := contractGWAdmin(t)
		ctx := t.Context()
		unknown := uuid.New()
		assert.Equal(t, http.StatusNotFound, code(t)(gwAdmin.UpdateEventGatewayEntities(ctx, unknown, contractEntities(contractBackendCluster(t, "bc")))))
		assert.Equal(t, http.StatusNotFound, code(t)(gwAdmin.UpdateEventGatewayDataPlaneCertificate(ctx, unknown, uuid.New(),
			gwadminapi.UpsertEventGatewayDataPlaneCertificateRequestOnPrem{Certificate: selfSignedCertificatePEM(t), CreatedAt: time.Now().UTC().Truncate(time.Second)})))
	})

	t.Run("every call of a push can be repeated", func(t *testing.T) {
		gwAdmin := contractGWAdmin(t)
		ctx := t.Context()
		id := registerContractGateway(t, gwAdmin)
		doc := contractEntities(contractBackendCluster(t, "bc"))
		cert := gwadminapi.UpsertEventGatewayDataPlaneCertificateRequestOnPrem{Certificate: selfSignedCertificatePEM(t), CreatedAt: time.Now().UTC().Truncate(time.Second)}
		certID := uuid.New()
		for range 2 {
			assert.Less(t, code(t)(gwAdmin.UpdateEventGateway(ctx, id, gwadminapi.UpsertGatewayRequestOnPrem{Name: "contract", CreatedAt: time.Now().UTC().Truncate(time.Second)})), 300)
			assert.Equal(t, http.StatusNoContent, code(t)(gwAdmin.SetExpectedEventGateways(ctx, gwadminapi.ExpectedEventGateways{EventGatewayIds: []uuid.UUID{id}})))
			assert.Less(t, code(t)(gwAdmin.UpdateEventGatewayEntities(ctx, id, doc)), 300)
			assert.Less(t, code(t)(gwAdmin.UpdateEventGatewayDataPlaneCertificate(ctx, id, certID, cert)), 300)
		}
	})

	t.Run("the entities replace the previous ones as a whole", func(t *testing.T) {
		gwAdmin := contractGWAdmin(t)
		ctx := t.Context()
		id := registerContractGateway(t, gwAdmin)
		require.Less(t, code(t)(gwAdmin.UpdateEventGatewayEntities(ctx, id, contractEntities(contractBackendCluster(t, "first")))), 300)
		second := contractBackendCluster(t, "second")
		require.Less(t, code(t)(gwAdmin.UpdateEventGatewayEntities(ctx, id, contractEntities(second))), 300)
		assert.Equal(t, []string{"second"}, contractBackendClusterNames(t, gwAdmin, id))
	})

	t.Run("rejected entities name the field at fault and keep the previous ones", func(t *testing.T) {
		gwAdmin := contractGWAdmin(t)
		ctx := t.Context()
		id := registerContractGateway(t, gwAdmin)
		require.Less(t, code(t)(gwAdmin.UpdateEventGatewayEntities(ctx, id, contractEntities(contractBackendCluster(t, "kept")))), 300)
		invalid := contractBackendCluster(t, "invalid")
		invalid.Id = uuid.MustParse("11111111-1111-1111-1111-111111111111") // not a version 4 UUID
		fields := contractInvalidFields(t)(gwAdmin.UpdateEventGatewayEntities(ctx, id, contractEntities(invalid)))
		if assert.NotEmpty(t, fields) {
			assert.Contains(t, fields[0], "backend_clusters.0")
		}
		assert.Equal(t, []string{"kept"}, contractBackendClusterNames(t, gwAdmin, id))
	})

	t.Run("a certificate is deleted once", func(t *testing.T) {
		gwAdmin := contractGWAdmin(t)
		ctx := t.Context()
		id := registerContractGateway(t, gwAdmin)
		certID := uuid.New()
		require.Less(t, code(t)(gwAdmin.UpdateEventGatewayDataPlaneCertificate(ctx, id, certID,
			gwadminapi.UpsertEventGatewayDataPlaneCertificateRequestOnPrem{Certificate: selfSignedCertificatePEM(t), CreatedAt: time.Now().UTC().Truncate(time.Second)})), 300)
		assert.Equal(t, http.StatusNoContent, code(t)(gwAdmin.DeleteEventGatewayDataPlaneCertificate(ctx, id, certID)))
		assert.Equal(t, http.StatusNotFound, code(t)(gwAdmin.DeleteEventGatewayDataPlaneCertificate(ctx, id, certID)))
	})

	t.Run("the served gateways can't hold duplicates", func(t *testing.T) {
		gwAdmin := contractGWAdmin(t)
		id := registerContractGateway(t, gwAdmin)
		assert.Equal(t, http.StatusBadRequest, code(t)(gwAdmin.SetExpectedEventGateways(t.Context(), gwadminapi.ExpectedEventGateways{EventGatewayIds: []uuid.UUID{id, id}})))
	})

	t.Run("a deleted gateway is gone", func(t *testing.T) {
		gwAdmin := contractGWAdmin(t)
		ctx := t.Context()
		id := registerContractGateway(t, gwAdmin)
		assert.Equal(t, http.StatusNoContent, code(t)(gwAdmin.DeleteEventGateway(ctx, id)))
		assert.Equal(t, http.StatusNotFound, code(t)(gwAdmin.DeleteEventGateway(ctx, id)))
	})

	t.Run("a rejected data plane certificate is answered with a 400, and isn't registered", func(t *testing.T) {
		// The operator removes a certificate gw-admin rejects from the pod,
		// and goes on with the others (see rejectedByGWAdmin). gw-admin
		// checks created_at: it accepts any string as a certificate.
		gwAdmin := contractGWAdmin(t)
		ctx := t.Context()
		id := registerContractGateway(t, gwAdmin)
		certID := uuid.New()
		assert.Equal(t, http.StatusBadRequest, code(t)(gwAdmin.UpdateEventGatewayDataPlaneCertificate(ctx, id, certID,
			gwadminapi.UpsertEventGatewayDataPlaneCertificateRequestOnPrem{Certificate: selfSignedCertificatePEM(t), CreatedAt: time.Date(2026, 10, 19, 10, 0, 0, 500_000_000, time.UTC)})))
		assert.Equal(t, http.StatusNotFound, code(t)(gwAdmin.DeleteEventGatewayDataPlaneCertificate(ctx, id, certID)))
		assert.Less(t, code(t)(gwAdmin.UpdateEventGatewayDataPlaneCertificate(ctx, id, uuid.New(),
			gwadminapi.UpsertEventGatewayDataPlaneCertificateRequestOnPrem{Certificate: "not a certificate", CreatedAt: time.Now().UTC().Truncate(time.Second)})), 300,
			"any string is a certificate")
	})

	t.Run("a deleted gateway takes its certificates and its entities with it: registered again under the same id, it holds none", func(t *testing.T) {
		// The operator deletes a gateway that may not use its GWAdmin anymore,
		// and registers it again under the same id once it may: what it held
		// before mustn't be trusted again, e.g. a certificate deleted
		// meanwhile (see removeGateway).
		gwAdmin := contractGWAdmin(t)
		ctx := t.Context()
		id := registerContractGateway(t, gwAdmin)
		require.Less(t, code(t)(gwAdmin.UpdateEventGatewayEntities(ctx, id, contractEntities(contractBackendCluster(t, "kept")))), 300)
		require.Equal(t, []string{"kept"}, contractBackendClusterNames(t, gwAdmin, id))
		certID := uuid.New()
		require.Less(t, code(t)(gwAdmin.UpdateEventGatewayDataPlaneCertificate(ctx, id, certID,
			gwadminapi.UpsertEventGatewayDataPlaneCertificateRequestOnPrem{Certificate: selfSignedCertificatePEM(t), CreatedAt: time.Now().UTC().Truncate(time.Second)})), 300)
		require.Equal(t, http.StatusNoContent, code(t)(gwAdmin.DeleteEventGateway(ctx, id)))
		assert.Equal(t, http.StatusNotFound, code(t)(gwAdmin.ListEventGatewayDataPlaneCertificates(ctx, id)), "the gateway is gone")
		require.Less(t, code(t)(gwAdmin.UpdateEventGateway(ctx, id, gwadminapi.UpsertGatewayRequestOnPrem{Name: "contract", CreatedAt: time.Now().UTC().Truncate(time.Second)})), 300)
		assert.Empty(t, contractCertificates(t, gwAdmin, id))
		assert.Empty(t, contractBackendClusterNames(t, gwAdmin, id))
	})

	t.Run("the certificates of a gateway are listed as they were registered", func(t *testing.T) {
		// The operator compares what a pod holds with the configuration, and
		// writes the difference.
		gwAdmin := contractGWAdmin(t)
		ctx := t.Context()
		id := registerContractGateway(t, gwAdmin)
		certID, created := uuid.New(), time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)
		pem := selfSignedCertificatePEM(t)
		require.Less(t, code(t)(gwAdmin.UpdateEventGatewayDataPlaneCertificate(ctx, id, certID,
			gwadminapi.UpsertEventGatewayDataPlaneCertificateRequestOnPrem{Certificate: pem, CreatedAt: created, Name: new("cert")})), 300)
		got := contractCertificates(t, gwAdmin, id)
		if assert.Len(t, got, 1) {
			assert.Equal(t, certID, got[0].Id)
			assert.Equal(t, pem, got[0].Certificate)
			assert.True(t, created.Equal(got[0].CreatedAt), "%s", got[0].CreatedAt)
			assert.Equal(t, "cert", lo.FromPtr(got[0].Name))
		}
		assert.Equal(t, http.StatusNotFound, code(t)(gwAdmin.ListEventGatewayDataPlaneCertificates(ctx, uuid.New())), "an unknown gateway")
	})

	t.Run("a deleted data plane certificate stays deleted once its gateway is deleted and registered again", func(t *testing.T) {
		// A certificate the operator removed while its gateway was deleted
		// isn't trusted again once the gateway is registered again under the
		// same id.
		gwAdmin := contractGWAdmin(t)
		ctx := t.Context()
		id := registerContractGateway(t, gwAdmin)
		certID := uuid.New()
		require.Less(t, code(t)(gwAdmin.UpdateEventGatewayDataPlaneCertificate(ctx, id, certID,
			gwadminapi.UpsertEventGatewayDataPlaneCertificateRequestOnPrem{Certificate: selfSignedCertificatePEM(t), CreatedAt: time.Now().UTC().Truncate(time.Second)})), 300)
		require.Equal(t, http.StatusNoContent, code(t)(gwAdmin.DeleteEventGatewayDataPlaneCertificate(ctx, id, certID)))
		require.Equal(t, http.StatusNoContent, code(t)(gwAdmin.DeleteEventGateway(ctx, id)))
		require.Less(t, code(t)(gwAdmin.UpdateEventGateway(ctx, id, gwadminapi.UpsertGatewayRequestOnPrem{Name: "contract", CreatedAt: time.Now().UTC().Truncate(time.Second)})), 300)
		assert.Equal(t, http.StatusNotFound, code(t)(gwAdmin.DeleteEventGatewayDataPlaneCertificate(ctx, id, certID)))
	})

	t.Run("a destination by name is kept by name, along with the id of the virtual cluster", func(t *testing.T) {
		// gw-admin keeps a listener policy forwarding to a virtual cluster by
		// name, but gives the data planes an empty id for it, and keg then never
		// opens the listener (design, traffic spike of 2026-10-19). Kong Operator
		// works around it by sending the destination by id (destinationByID in
		// controller/eventgateway/onprem/configuration_routing.go).
		//
		// What gw-admin gives the data planes is served on the data plane
		// endpoint (GET .../config, with a data plane certificate), which is not
		// part of the API this test uses, and the fake doesn't serve it. So this
		// test pins what the API returns when the entities are read back: since
		// gw-admin 1.11.0, the name and the id it resolves it to, as for a
		// destination sent by id, which comes back with its name too (see
		// withoutDestinationNames). Whether the workaround can be dropped is
		// told by the data plane endpoint.
		gwAdmin := contractGWAdmin(t)
		ctx := t.Context()
		id := registerContractGateway(t, gwAdmin)
		vcID := uuid.NewString()
		doc := fmt.Sprintf(`{"config_version": {"data_version": 1, "operator_version": "1.0.0", "feature_bitmap": 0}, "entities": {
			"backend_clusters": [{
				"id": %[1]q, "created_at": "2026-10-19T10:00:00Z", "name": "bc",
				"bootstrap_servers": ["kafka:9092"], "authentication": {"type": "anonymous"}, "tls": {"enabled": false}
			}],
			"virtual_clusters": [{
				"id": %[2]q, "created_at": "2026-10-19T10:00:00Z", "name": "vc", "dns_label": "vc",
				"destination": {"id": %[1]q}, "acl_mode": "passthrough", "authentication": [{"type": "anonymous"}],
				"cluster_policies": [], "consume_policies": [], "produce_policies": []
			}],
			"listeners": [{
				"id": %[3]q, "created_at": "2026-10-19T10:00:00Z", "name": "listener",
				"addresses": ["0.0.0.0"], "ports": ["19092-19095"],
				"policies": [{
					"id": %[4]q, "created_at": "2026-10-19T10:00:00Z", "type": "forward_to_virtual_cluster",
					"config": {"type": "port_mapping", "advertised_host": "localhost", "bootstrap_port": "at_start", "destination": {"name": "vc"}}
				}]
			}],
			"schema_registries": [], "static_keys": [], "tls_trust_bundles": []
		}}`, uuid.NewString(), vcID, uuid.NewString(), uuid.NewString())
		require.Less(t, code(t)(gwAdmin.UpdateEventGatewayEntitiesWithBody(ctx, id, "application/json", strings.NewReader(doc))), 300)

		resp, err := gwAdmin.GetEventGatewayEntities(ctx, id)
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var got struct {
			Entities struct {
				Listeners []struct {
					Policies []struct {
						Config struct {
							Destination map[string]any `json:"destination"`
						} `json:"config"`
					} `json:"policies"`
				} `json:"listeners"`
			} `json:"entities"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
		require.Len(t, got.Entities.Listeners, 1)
		require.Len(t, got.Entities.Listeners[0].Policies, 1)
		destination := got.Entities.Listeners[0].Policies[0].Config.Destination
		assert.Equal(t, "vc", destination["name"], "the destination is kept by name")
		assert.Equal(t, vcID, destination["id"], "the name is resolved to the id of the virtual cluster")
	})

	t.Run("created_at must be whole seconds in UTC", func(t *testing.T) {
		for name, createdAt := range map[string]time.Time{
			"fractional seconds": time.Date(2026, 10, 19, 10, 0, 0, 500_000_000, time.UTC),
			"not in UTC":         time.Date(2026, 10, 19, 12, 0, 0, 0, time.FixedZone("CEST", 2*60*60)),
		} {
			t.Run(name, func(t *testing.T) {
				gwAdmin := contractGWAdmin(t)
				id := registerContractGateway(t, gwAdmin)
				bc := contractBackendCluster(t, "bc")
				bc.CreatedAt = createdAt
				fields := contractInvalidFields(t)(gwAdmin.UpdateEventGatewayEntities(t.Context(), id, contractEntities(bc)))
				if assert.NotEmpty(t, fields) {
					assert.Contains(t, fields[0], "backend_clusters.0")
				}
			})
		}
	})

	t.Run("an id must be a version 4 UUID", func(t *testing.T) {
		for name, newID := range map[string]func() (uuid.UUID, error){
			"version 1": uuid.NewUUID,
			"version 7": uuid.NewV7,
			"nil":       func() (uuid.UUID, error) { return uuid.Nil, nil },
		} {
			t.Run(name, func(t *testing.T) {
				gwAdmin := contractGWAdmin(t)
				id := registerContractGateway(t, gwAdmin)
				bc := contractBackendCluster(t, "bc")
				var err error
				bc.Id, err = newID()
				require.NoError(t, err)
				fields := contractInvalidFields(t)(gwAdmin.UpdateEventGatewayEntities(t.Context(), id, contractEntities(bc)))
				if assert.NotEmpty(t, fields) {
					assert.Contains(t, fields[0], "backend_clusters.0")
				}
			})
		}
	})

	t.Run("an unknown property is rejected", func(t *testing.T) {
		// The spike confirmed it for the entities; the other bodies are
		// expected to follow the same schema rule (additionalProperties: false).
		const contentType = "application/json"
		testCases := []struct {
			name string
			call func(t *testing.T, gwAdmin gwadminapi.ClientInterface, id uuid.UUID) (*http.Response, error)
		}{
			{
				name: "in the entities document",
				call: func(t *testing.T, gwAdmin gwadminapi.ClientInterface, id uuid.UUID) (*http.Response, error) {
					body := withUnknownProperty(t, contractEntities(contractBackendCluster(t, "bc")))
					return gwAdmin.UpdateEventGatewayEntitiesWithBody(t.Context(), id, contentType, jsonBody(t, body))
				},
			},
			{
				name: "in the entities",
				call: func(t *testing.T, gwAdmin gwadminapi.ClientInterface, id uuid.UUID) (*http.Response, error) {
					body := withUnknownProperty(t, contractEntities(contractBackendCluster(t, "bc")))
					delete(body, "unknown_property")
					body["entities"] = withUnknownProperty(t, contractEntities(contractBackendCluster(t, "bc")).Entities)
					return gwAdmin.UpdateEventGatewayEntitiesWithBody(t.Context(), id, contentType, jsonBody(t, body))
				},
			},
			{
				name: "in an entity",
				call: func(t *testing.T, gwAdmin gwadminapi.ClientInterface, id uuid.UUID) (*http.Response, error) {
					body := withUnknownProperty(t, contractEntities(contractBackendCluster(t, "bc")))
					delete(body, "unknown_property")
					entities := withUnknownProperty(t, contractEntities(contractBackendCluster(t, "bc")).Entities)
					delete(entities, "unknown_property")
					entities["backend_clusters"] = []any{withUnknownProperty(t, contractBackendCluster(t, "bc"))}
					body["entities"] = entities
					return gwAdmin.UpdateEventGatewayEntitiesWithBody(t.Context(), id, contentType, jsonBody(t, body))
				},
			},
			{
				name: "in a gateway",
				call: func(t *testing.T, gwAdmin gwadminapi.ClientInterface, id uuid.UUID) (*http.Response, error) {
					body := withUnknownProperty(t, gwadminapi.UpsertGatewayRequestOnPrem{Name: "contract", CreatedAt: time.Now().UTC().Truncate(time.Second)})
					return gwAdmin.UpdateEventGatewayWithBody(t.Context(), id, contentType, jsonBody(t, body))
				},
			},
			{
				name: "in the served gateways",
				call: func(t *testing.T, gwAdmin gwadminapi.ClientInterface, id uuid.UUID) (*http.Response, error) {
					body := withUnknownProperty(t, gwadminapi.ExpectedEventGateways{EventGatewayIds: []uuid.UUID{id}})
					return gwAdmin.SetExpectedEventGatewaysWithBody(t.Context(), contentType, jsonBody(t, body))
				},
			},
		}
		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				gwAdmin := contractGWAdmin(t)
				id := registerContractGateway(t, gwAdmin)
				assert.Equal(t, http.StatusBadRequest, code(t)(tc.call(t, gwAdmin, id)))
			})
		}
	})

	t.Run("an unknown property in a data plane certificate is accepted", func(t *testing.T) {
		// Unlike the other bodies: the operator only sends known ones.
		gwAdmin := contractGWAdmin(t)
		id := registerContractGateway(t, gwAdmin)
		body := withUnknownProperty(t, gwadminapi.UpsertEventGatewayDataPlaneCertificateRequestOnPrem{
			Certificate: selfSignedCertificatePEM(t), CreatedAt: time.Now().UTC().Truncate(time.Second),
		})
		assert.Less(t, code(t)(gwAdmin.UpdateEventGatewayDataPlaneCertificateWithBody(t.Context(), id, uuid.New(), "application/json", jsonBody(t, body))), 300)
	})

	t.Run("a data plane certificate is upserted, deleted, and an unknown one is not found", func(t *testing.T) {
		gwAdmin := contractGWAdmin(t)
		ctx := t.Context()
		id := registerContractGateway(t, gwAdmin)
		certID := uuid.New()
		for range 2 {
			// A new certificate under the same id replaces the previous one.
			assert.Less(t, code(t)(gwAdmin.UpdateEventGatewayDataPlaneCertificate(ctx, id, certID,
				gwadminapi.UpsertEventGatewayDataPlaneCertificateRequestOnPrem{Certificate: selfSignedCertificatePEM(t), CreatedAt: time.Now().UTC().Truncate(time.Second)})), 300)
		}
		assert.Equal(t, http.StatusNoContent, code(t)(gwAdmin.DeleteEventGatewayDataPlaneCertificate(ctx, id, certID)))
		assert.Equal(t, http.StatusNotFound, code(t)(gwAdmin.DeleteEventGatewayDataPlaneCertificate(ctx, id, uuid.New())), "a certificate never registered")
		assert.Equal(t, http.StatusNotFound, code(t)(gwAdmin.DeleteEventGatewayDataPlaneCertificate(ctx, uuid.New(), uuid.New())), "a certificate of an unknown gateway")
	})

	t.Run("the served gateways are replaced as a whole, and a gateway left out is kept", func(t *testing.T) {
		// "Served" shows only in the readiness of the pod: a pod is ready once
		// every gateway of the set has entities and a data plane certificate,
		// and the API can't read the set back. What the API shows is that a
		// gateway left out keeps its entities: only DELETE removes it.
		gwAdmin := contractGWAdmin(t)
		ctx := t.Context()
		left := uuid.New()
		require.Less(t, code(t)(gwAdmin.UpdateEventGateway(ctx, left, gwadminapi.UpsertGatewayRequestOnPrem{Name: "left-out", CreatedAt: time.Now().UTC().Truncate(time.Second)})), 300)
		t.Cleanup(func() { _ = code(t)(gwAdmin.DeleteEventGateway(context.Background(), left)) })
		served := registerContractGateway(t, gwAdmin)
		require.Equal(t, http.StatusNoContent, code(t)(gwAdmin.SetExpectedEventGateways(ctx, gwadminapi.ExpectedEventGateways{EventGatewayIds: []uuid.UUID{left, served}})))
		require.Less(t, code(t)(gwAdmin.UpdateEventGatewayEntities(ctx, left, contractEntities(contractBackendCluster(t, "kept")))), 300)

		assert.Equal(t, http.StatusNoContent, code(t)(gwAdmin.SetExpectedEventGateways(ctx, gwadminapi.ExpectedEventGateways{EventGatewayIds: []uuid.UUID{served}})))
		assert.Equal(t, []string{"kept"}, contractBackendClusterNames(t, gwAdmin, left))
		assert.Equal(t, http.StatusNoContent, code(t)(gwAdmin.SetExpectedEventGateways(ctx, gwadminapi.ExpectedEventGateways{EventGatewayIds: []uuid.UUID{}})), "the set can be emptied")
	})

	t.Run("a pod keeps nothing across a restart", func(t *testing.T) {
		t.Skip("not expressible through the API: the test can't restart a real gw-admin pod, and the fake keeps nothing by design. " +
			"The spike observed it (design, section 5.5); the operator side is covered by the envtest " +
			`"a new gw-admin pod gets the configuration" (TestOnPremEventGatewayPush), and no chainsaw test restarts a gw-admin pod yet`)
	})

	t.Run("a pod turns ready once every served gateway has entities and a data plane certificate", func(t *testing.T) {
		t.Skip("not expressible through the API: readiness is the probe of the gw-admin pod, which neither the API nor the fake serves. " +
			"Covered end to end by test/e2e/chainsaw/eventgateway/onprem-roundtrip, whose data plane only turns Ready once gw-admin serves its gateway")
	})

	t.Run("a data plane gets 204 without listeners and virtual clusters, and 401 with a certificate not registered on its gateway", func(t *testing.T) {
		t.Skip("not expressible through the API: the data plane endpoint (GET .../config, port 8099, with a data plane certificate) is not part of the gw-admin API client, and the fake doesn't serve it. " +
			"Only the happy path is covered end to end, by test/e2e/chainsaw/eventgateway/onprem-roundtrip (a data plane with a registered certificate, a listener and a virtual cluster turns Ready); no test covers the 204 and the 401")
	})
}

// contractInvalidFields returns a function returning the fields at fault of a
// response, which it requires to be a 400, and closes.
func contractInvalidFields(t *testing.T) func(*http.Response, error) []string {
	return func(resp *http.Response, err error) []string {
		t.Helper()
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
		var problem struct {
			InvalidParameters []struct {
				Field string `json:"field"`
			} `json:"invalid_parameters"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&problem))
		fields := make([]string, 0, len(problem.InvalidParameters))
		for _, p := range problem.InvalidParameters {
			fields = append(fields, p.Field)
		}
		return fields
	}
}

// withUnknownProperty returns v as a JSON object with a property no schema
// knows.
func withUnknownProperty(t *testing.T, v any) map[string]any {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	var object map[string]any
	require.NoError(t, json.Unmarshal(b, &object))
	object["unknown_property"] = true
	return object
}

func jsonBody(t *testing.T, v any) io.Reader {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return strings.NewReader(string(b))
}

func contractBackendClusterNames(t *testing.T, gwAdmin gwadminapi.ClientInterface, id uuid.UUID) []string {
	t.Helper()
	resp, err := gwAdmin.GetEventGatewayEntities(t.Context(), id)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var doc struct {
		Entities struct {
			BackendClusters []struct {
				Name string `json:"name"`
			} `json:"backend_clusters"`
		} `json:"entities"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&doc))
	names := make([]string, 0, len(doc.Entities.BackendClusters))
	for _, bc := range doc.Entities.BackendClusters {
		names = append(names, bc.Name)
	}
	return names
}

func contractCertificates(t *testing.T, gwAdmin gwadminapi.ClientInterface, id uuid.UUID) []gwadminapi.EventGatewayDataPlaneCertificateOnPrem {
	t.Helper()
	resp, err := gwAdmin.ListEventGatewayDataPlaneCertificates(t.Context(), id)
	require.NoError(t, err)
	defer resp.Body.Close()
	parsed, err := gwadminapi.ParseListEventGatewayDataPlaneCertificatesResponse(resp)
	require.NoError(t, err)
	require.NotNil(t, parsed.JSON200, "%s: %s", resp.Status, parsed.Body)
	return parsed.JSON200.Data
}
