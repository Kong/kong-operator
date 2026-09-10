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
}
