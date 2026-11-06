package metricsscraper

import (
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"

	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	adminapi "github.com/kong/kong-operator/v2/internal/adminapi"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
)

func TestMetricsEnricher_UsesEndpointAddressAndTLSServerName(t *testing.T) {
	const serverName = "pod.dataplane-admin.default.svc"
	type requestInfo struct {
		path string
		sni  string
	}
	requests := make(chan requestInfo, 1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- requestInfo{path: r.URL.Path, sni: r.TLS.ServerName}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[],"next":null}`)
	}))
	t.Cleanup(server.Close)

	enricher := metricsEnricher{
		dataplane: &operatorv1beta1.DataPlane{Name: "dataplane-1", Namespace: "default"},
		adminAPIAddressProvider: &metricsmocks.MockAdminAPIAddressProvider{
			Endpoints: []adminapi.DiscoveredAdminAPI{{
				Address:       server.URL,
				TLSServerName: serverName,
			}},
		},
		httpClient: &http.Client{Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // test server certificate
		}},
		logger: logr.Discard(),
	}

	err := enricher.Consume(t.Context(), Metrics{metrics: metricsMap{}})
	require.NoError(t, err)

	request := <-requests
	require.True(t, strings.HasPrefix(request.path, "/services"))
	require.Equal(t, serverName, request.sni)
}
