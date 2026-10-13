package konnect

import (
	"testing"

	"github.com/stretchr/testify/assert"

	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
)

func TestIsHybridClusterType(t *testing.T) {
	tests := []struct {
		name        string
		clusterType konnectv1alpha2.KonnectExtensionClusterType
		want        bool
	}{
		{name: "ControlPlane", clusterType: konnectv1alpha2.ClusterTypeControlPlane, want: true},
		{name: "ControlPlaneGroup", clusterType: konnectv1alpha2.ClusterTypeControlPlaneGroup, want: true},
		{name: "K8sIngressController", clusterType: konnectv1alpha2.ClusterTypeK8sIngressController, want: false},
		{name: "empty", clusterType: "", want: false},
		{name: "unknown", clusterType: "Unknown", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsHybridClusterType(tt.clusterType))
		})
	}
}
