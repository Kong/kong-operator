package config

import (
	"testing"

	"github.com/stretchr/testify/assert"

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

func TestClusterDataPlaneLabelStringFromLabels(t *testing.T) {
	testCases := []struct {
		name   string
		labels map[string]konnectv1alpha1.DataPlaneLabelValue
		want   string
	}{
		{
			name:   "empty labels",
			labels: map[string]konnectv1alpha1.DataPlaneLabelValue{},
			want:   "",
		},
		{
			name: "single label",
			labels: map[string]konnectv1alpha1.DataPlaneLabelValue{
				"region": "us-west",
			},
			want: "region:us-west",
		},
		{
			name: "multiple labels",
			labels: map[string]konnectv1alpha1.DataPlaneLabelValue{
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
