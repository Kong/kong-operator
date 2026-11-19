package konnect

import (
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/stretchr/testify/assert"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
)

func TestControlPlaneSupportsWorkspaces(t *testing.T) {
	testCases := []struct {
		name        string
		clusterType *sdkkonnectcomp.CreateControlPlaneRequestClusterType
		expected    bool
	}{
		{
			name:     "unknown cluster type",
			expected: true,
		},
		{
			name:        "hybrid control plane",
			clusterType: new(sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlane),
			expected:    true,
		},
		{
			name:        "Kong Ingress Controller control plane",
			clusterType: new(sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeK8SIngressController),
		},
		{
			name:        "control plane group",
			clusterType: new(sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlaneGroup),
		},
		{
			name:        "serverless control plane",
			clusterType: new(sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeServerless),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, controlPlaneSupportsWorkspaces(tc.clusterType))
		})
	}
}

func TestSetWorkspace(t *testing.T) {
	t.Run("sets the workspace from the ControlPlaneRef", func(t *testing.T) {
		svc := &configurationv1alpha1.KongService{}
		setWorkspace(svc, commonv1alpha1.ControlPlaneRef{
			Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
			KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
				Name: "cp",
			},
			Workspace: "team-a",
		})
		assert.Equal(t, "team-a", svc.GetWorkspace())
	})

	t.Run("clears the workspace when the ControlPlaneRef has none", func(t *testing.T) {
		svc := &configurationv1alpha1.KongService{}
		svc.SetWorkspace("team-a")
		setWorkspace(svc, commonv1alpha1.ControlPlaneRef{
			Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
			KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
				Name: "cp",
			},
		})
		assert.Empty(t, svc.GetWorkspace())
	})

	t.Run("ignores entities without workspace support", func(t *testing.T) {
		assert.NotPanics(t, func() {
			setWorkspace(struct{}{}, commonv1alpha1.ControlPlaneRef{Workspace: "team-a"})
		})
	})
}
