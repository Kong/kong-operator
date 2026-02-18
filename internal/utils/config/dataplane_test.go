package config

import (
	"testing"

	"github.com/stretchr/testify/assert"

	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
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
