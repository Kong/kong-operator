package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/pkg/consts"
	"github.com/kong/kong-operator/v2/pkg/ipfamily"
)

func TestClusterDataPlaneLabelStringFromLabels(t *testing.T) {
	testCases := []struct {
		name   string
		labels map[string]konnectv1alpha2.DataPlaneLabelValue
		want   string
	}{
		{
			name:   "empty labels",
			labels: map[string]konnectv1alpha2.DataPlaneLabelValue{},
			want:   "",
		},
		{
			name: "single label",
			labels: map[string]konnectv1alpha2.DataPlaneLabelValue{
				"region": "us-west",
			},
			want: "region:us-west",
		},
		{
			name: "multiple labels",
			labels: map[string]konnectv1alpha2.DataPlaneLabelValue{
				"region":      "us-west",
				"environment": "prod",
				"app":         "gateway",
			},
			want: "app:gateway,environment:prod,region:us-west",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, clusterDataPlaneLabelStringFromLabels(tc.labels))
		})
	}
}

func TestListenValue(t *testing.T) {
	t.Run("renders the wildcard address(es) per IP family", func(t *testing.T) {
		for _, tc := range []struct {
			family ipfamily.IPFamily
			want   string
		}{
			{family: ipfamily.IPv4, want: "0.0.0.0:8000 ssl"},
			{family: ipfamily.IPv6, want: "[::]:8000 ssl"},
			{family: ipfamily.Dual, want: "0.0.0.0:8000 ssl, [::]:8000 ssl"},
		} {
			got, err := ListenValue(tc.family, consts.DataPlaneProxyPort, "ssl")
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		}
	})

	t.Run("unresolved IP family returns an error", func(t *testing.T) {
		for _, family := range []ipfamily.IPFamily{ipfamily.Auto, ""} {
			_, err := ListenValue(family, consts.DataPlaneProxyPort)
			require.Error(t, err, "family %q should not silently fall back to IPv4", family)
		}
	})
}

func TestKongDefaults(t *testing.T) {
	t.Run("unresolved IP family returns an error", func(t *testing.T) {
		_, err := KongDefaults(ipfamily.Auto)
		require.Error(t, err)
	})

	t.Run("enables http2 on both proxy listens per IP family", func(t *testing.T) {
		for _, tc := range []struct {
			family ipfamily.IPFamily
			want   string
		}{
			{
				family: ipfamily.IPv4,
				want:   "0.0.0.0:8000 http2 reuseport backlog=16384, 0.0.0.0:8443 http2 ssl reuseport backlog=16384",
			},
			{
				family: ipfamily.IPv6,
				want:   "[::]:8000 http2 reuseport backlog=16384, [::]:8443 http2 ssl reuseport backlog=16384",
			},
			{
				family: ipfamily.Dual,
				want: "0.0.0.0:8000 http2 reuseport backlog=16384, [::]:8000 http2 reuseport backlog=16384, " +
					"0.0.0.0:8443 http2 ssl reuseport backlog=16384, [::]:8443 http2 ssl reuseport backlog=16384",
			},
		} {
			defaults, err := KongDefaults(tc.family)
			require.NoError(t, err)
			assert.Equal(t, tc.want, defaults["KONG_PROXY_LISTEN"], "family %q", tc.family)
		}
	})

	t.Run("sets the DNS order to prefer AAAA for IPv6 and Dual", func(t *testing.T) {
		for _, tc := range []struct {
			family       ipfamily.IPFamily
			wantDNSOrder string
			wantSet      bool
		}{
			{family: ipfamily.IPv6, wantDNSOrder: "LAST,SRV,AAAA,A,CNAME", wantSet: true},
			{family: ipfamily.Dual, wantDNSOrder: "LAST,SRV,AAAA,A,CNAME", wantSet: true},
			{family: ipfamily.IPv4, wantSet: false},
		} {
			defaults, err := KongDefaults(tc.family)
			require.NoError(t, err)
			got, ok := defaults["KONG_DNS_ORDER"]
			if tc.wantSet {
				require.True(t, ok)
				assert.Equal(t, tc.wantDNSOrder, got)
			} else {
				assert.NotContains(t, defaults, "KONG_DNS_ORDER")
			}
		}
	})
}

func konnectStatusWithEndpoints(
	clusterType konnectv1alpha2.KonnectExtensionClusterType,
	controlPlaneEndpoint, telemetryEndpoint string,
) konnectv1alpha2.KonnectExtensionStatus {
	return konnectv1alpha2.KonnectExtensionStatus{
		Konnect: &konnectv1alpha2.KonnectExtensionControlPlaneStatus{
			ControlPlaneID: "konnect-id",
			ClusterType:    clusterType,
			Endpoints: konnectv1alpha2.KonnectEndpoints{
				ControlPlaneEndpoint: controlPlaneEndpoint,
				TelemetryEndpoint:    telemetryEndpoint,
			},
		},
	}
}

func TestKongInKonnectDefaults(t *testing.T) {
	const (
		publicControlPlaneEndpoint  = "7078163243.us.cp.konghq.com"
		publicTelemetryEndpoint     = "7078163243.us.tp.konghq.com"
		privateControlPlaneEndpoint = "https://us.svc.konghq.com/cp/abc"
		privateTelemetryEndpoint    = "https://us.svc.konghq.com/tp/abc"
	)

	// The whole map the ControlPlane template renders for a public endpoint,
	// written out so any value that moves fails this test.
	publicEnv := map[string]string{
		"KONG_ROLE":                          "data_plane",
		"KONG_CLUSTER_MTLS":                  "pki",
		"KONG_CLUSTER_CONTROL_PLANE":         "7078163243.us.cp.konghq.com:443",
		"KONG_CLUSTER_SERVER_NAME":           "7078163243.us.cp.konghq.com",
		"KONG_CLUSTER_TELEMETRY_ENDPOINT":    "7078163243.us.tp.konghq.com:443",
		"KONG_CLUSTER_TELEMETRY_SERVER_NAME": "7078163243.us.tp.konghq.com",
		"KONG_CLUSTER_CERT":                  "/etc/secrets/kong-cluster-cert/tls.crt",
		"KONG_CLUSTER_CERT_KEY":              "/etc/secrets/kong-cluster-cert/tls.key",
		"KONG_INCREMENTAL_SYNC":              "on",
		"KONG_LUA_SSL_TRUSTED_CERTIFICATE":   "system",
		"KONG_KONNECT_MODE":                  "on",
		"KONG_VITALS":                        "off",
	}

	testCases := []struct {
		name                 string
		clusterType          konnectv1alpha2.KonnectExtensionClusterType
		controlPlaneEndpoint string
		telemetryEndpoint    string
		// wantEndpoints are the endpoint keys the returned map must carry.
		wantEndpoints map[string]string
		// wantAbsent are keys the returned map must not carry.
		wantAbsent []string
		// wantFull, when set, is the whole returned map.
		wantFull map[string]string
		// wantErr is set when the endpoints cannot be used.
		wantErr bool
	}{
		{
			name:                 "public endpoint without a scheme",
			clusterType:          konnectv1alpha2.ClusterTypeControlPlane,
			controlPlaneEndpoint: publicControlPlaneEndpoint,
			telemetryEndpoint:    publicTelemetryEndpoint,
			wantEndpoints: map[string]string{
				"KONG_CLUSTER_CONTROL_PLANE":         "7078163243.us.cp.konghq.com:443",
				"KONG_CLUSTER_SERVER_NAME":           "7078163243.us.cp.konghq.com",
				"KONG_CLUSTER_TELEMETRY_ENDPOINT":    "7078163243.us.tp.konghq.com:443",
				"KONG_CLUSTER_TELEMETRY_SERVER_NAME": "7078163243.us.tp.konghq.com",
			},
			wantFull: publicEnv,
		},
		{
			name:                 "public endpoint with a scheme",
			clusterType:          konnectv1alpha2.ClusterTypeControlPlane,
			controlPlaneEndpoint: "https://" + publicControlPlaneEndpoint,
			telemetryEndpoint:    "https://" + publicTelemetryEndpoint,
			wantEndpoints: map[string]string{
				"KONG_CLUSTER_CONTROL_PLANE":         "7078163243.us.cp.konghq.com:443",
				"KONG_CLUSTER_SERVER_NAME":           "7078163243.us.cp.konghq.com",
				"KONG_CLUSTER_TELEMETRY_ENDPOINT":    "7078163243.us.tp.konghq.com:443",
				"KONG_CLUSTER_TELEMETRY_SERVER_NAME": "7078163243.us.tp.konghq.com",
			},
			wantFull: publicEnv,
		},
		{
			name:                 "private endpoints carrying a path",
			clusterType:          konnectv1alpha2.ClusterTypeControlPlane,
			controlPlaneEndpoint: privateControlPlaneEndpoint,
			telemetryEndpoint:    privateTelemetryEndpoint,
			wantEndpoints: map[string]string{
				"KONG_CLUSTER_CONTROL_PLANE":         "us.svc.konghq.com/cp/abc",
				"KONG_CLUSTER_SERVER_NAME":           "us.svc.konghq.com",
				"KONG_CLUSTER_TELEMETRY_ENDPOINT":    "us.svc.konghq.com:443/tp/abc",
				"KONG_CLUSTER_TELEMETRY_SERVER_NAME": "us.svc.konghq.com",
			},
		},
		{
			name:                 "private endpoints on a K8s Ingress Controller cluster",
			clusterType:          konnectv1alpha2.ClusterTypeK8sIngressController,
			controlPlaneEndpoint: privateControlPlaneEndpoint,
			telemetryEndpoint:    privateTelemetryEndpoint,
			wantEndpoints: map[string]string{
				"KONG_CLUSTER_TELEMETRY_ENDPOINT":    "us.svc.konghq.com:443/tp/abc",
				"KONG_CLUSTER_TELEMETRY_SERVER_NAME": "us.svc.konghq.com",
			},
			wantAbsent: []string{"KONG_CLUSTER_CONTROL_PLANE", "KONG_CLUSTER_SERVER_NAME"},
		},
		{
			name:                 "control plane endpoint that cannot be used",
			clusterType:          konnectv1alpha2.ClusterTypeControlPlane,
			controlPlaneEndpoint: "https://host:bad",
			telemetryEndpoint:    publicTelemetryEndpoint,
			wantErr:              true,
		},
		{
			name:                 "unusable control plane endpoint on a K8s Ingress Controller cluster",
			clusterType:          konnectv1alpha2.ClusterTypeK8sIngressController,
			controlPlaneEndpoint: "https://host:bad",
			telemetryEndpoint:    privateTelemetryEndpoint,
			wantEndpoints: map[string]string{
				"KONG_CLUSTER_TELEMETRY_ENDPOINT":    "us.svc.konghq.com:443/tp/abc",
				"KONG_CLUSTER_TELEMETRY_SERVER_NAME": "us.svc.konghq.com",
			},
			wantAbsent: []string{"KONG_CLUSTER_CONTROL_PLANE", "KONG_CLUSTER_SERVER_NAME"},
		},
		{
			name:                 "unusable telemetry endpoint on a K8s Ingress Controller cluster",
			clusterType:          konnectv1alpha2.ClusterTypeK8sIngressController,
			controlPlaneEndpoint: privateControlPlaneEndpoint,
			telemetryEndpoint:    "https://host:bad",
			wantErr:              true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := KongInKonnectDefaults(nil, konnectStatusWithEndpoints(tc.clusterType, tc.controlPlaneEndpoint, tc.telemetryEndpoint))
			if tc.wantErr {
				require.Error(t, err)
				assert.Nil(t, got)
				return
			}
			require.NoError(t, err)

			for key, want := range tc.wantEndpoints {
				assert.Equal(t, want, got[key], "%s", key)
			}
			for _, key := range tc.wantAbsent {
				assert.NotContains(t, got, key)
			}
			if tc.wantFull != nil {
				assert.Equal(t, tc.wantFull, got)
			}
		})
	}
}

func TestDisableAdminAPI(t *testing.T) {
	testCases := []struct {
		name     string
		env      map[string]string
		expected map[string]string
	}{
		{
			name:     "nil map is a no-op",
			env:      nil,
			expected: nil,
		},
		{
			name: "turns off admin listen and drops admin TLS settings",
			env: map[string]string{
				"KONG_ADMIN_LISTEN":                       "0.0.0.0:8444 ssl",
				"KONG_ADMIN_SSL_CERT":                     "/var/cluster-certificate/tls.crt",
				"KONG_ADMIN_SSL_CERT_KEY":                 "/var/cluster-certificate/tls.key",
				"KONG_NGINX_ADMIN_SSL_CLIENT_CERTIFICATE": "/var/cluster-certificate/ca.crt",
				"KONG_NGINX_ADMIN_SSL_VERIFY_CLIENT":      "on",
				"KONG_NGINX_ADMIN_SSL_VERIFY_DEPTH":       "3",
				"KONG_ADMIN_ACCESS_LOG":                   "/dev/stdout",
				"KONG_DATABASE":                           "off",
			},
			expected: map[string]string{
				"KONG_ADMIN_LISTEN":     "off",
				"KONG_ADMIN_ACCESS_LOG": "/dev/stdout",
				"KONG_DATABASE":         "off",
			},
		},
		{
			name:     "sets admin listen off when not present",
			env:      map[string]string{},
			expected: map[string]string{"KONG_ADMIN_LISTEN": "off"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			DisableAdminAPI(tc.env)
			assert.Equal(t, tc.expected, tc.env)
		})
	}

	t.Run("does not affect subsequent KongDefaults calls", func(t *testing.T) {
		defaults, err := KongDefaults(ipfamily.IPv4)
		require.NoError(t, err)
		DisableAdminAPI(defaults)

		fresh, err := KongDefaults(ipfamily.IPv4)
		require.NoError(t, err)
		assert.NotEqual(t, "off", fresh["KONG_ADMIN_LISTEN"])
		assert.Contains(t, fresh, "KONG_ADMIN_SSL_CERT")
	})
}
