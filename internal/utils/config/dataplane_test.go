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
