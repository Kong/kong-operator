package konnect

import (
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/stretchr/testify/assert"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
)

func TestControlPlaneSupportsWorkspaces(t *testing.T) {
	withSpec := func(ct sdkkonnectcomp.CreateControlPlaneRequestClusterType) *konnectv1alpha2.KonnectGatewayControlPlane {
		return &konnectv1alpha2.KonnectGatewayControlPlane{
			Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
				CreateControlPlaneRequest: &sdkkonnectcomp.CreateControlPlaneRequest{Name: "cp", ClusterType: &ct},
			},
		}
	}
	// A mirrored ControlPlane has no create request: only its status has the cluster type.
	mirrored := func(ct sdkkonnectcomp.ControlPlaneClusterType) *konnectv1alpha2.KonnectGatewayControlPlane {
		return &konnectv1alpha2.KonnectGatewayControlPlane{
			Status: konnectv1alpha2.KonnectGatewayControlPlaneStatus{ClusterType: ct},
		}
	}

	testCases := []struct {
		name                string
		cp                  *konnectv1alpha2.KonnectGatewayControlPlane
		expectedClusterType string
		expected            bool
	}{
		{
			name:     "unknown cluster type",
			cp:       &konnectv1alpha2.KonnectGatewayControlPlane{},
			expected: true,
		},
		{
			name:                "hybrid control plane",
			cp:                  withSpec(sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlane),
			expectedClusterType: "CLUSTER_TYPE_CONTROL_PLANE",
			expected:            true,
		},
		{
			name:                "Kong Ingress Controller control plane",
			cp:                  withSpec(sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeK8SIngressController),
			expectedClusterType: "CLUSTER_TYPE_K8S_INGRESS_CONTROLLER",
		},
		{
			name:                "control plane group",
			cp:                  withSpec(sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlaneGroup),
			expectedClusterType: "CLUSTER_TYPE_CONTROL_PLANE_GROUP",
		},
		{
			name:                "serverless control plane",
			cp:                  withSpec(sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeServerless),
			expectedClusterType: "CLUSTER_TYPE_SERVERLESS",
		},
		{
			name:                "mirrored hybrid control plane",
			cp:                  mirrored(sdkkonnectcomp.ControlPlaneClusterTypeClusterTypeControlPlane),
			expectedClusterType: "CLUSTER_TYPE_CONTROL_PLANE",
			expected:            true,
		},
		{
			name:                "mirrored Kong Ingress Controller control plane",
			cp:                  mirrored(sdkkonnectcomp.ControlPlaneClusterTypeClusterTypeK8SIngressController),
			expectedClusterType: "CLUSTER_TYPE_K8S_INGRESS_CONTROLLER",
		},
		{
			name:                "mirrored serverless control plane",
			cp:                  mirrored(sdkkonnectcomp.ControlPlaneClusterTypeClusterTypeServerless),
			expectedClusterType: "CLUSTER_TYPE_SERVERLESS",
		},
		{
			name: "status wins over spec",
			cp: func() *konnectv1alpha2.KonnectGatewayControlPlane {
				cp := withSpec(sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlane)
				cp.Status.ClusterType = sdkkonnectcomp.ControlPlaneClusterTypeClusterTypeControlPlaneGroup
				return cp
			}(),
			expectedClusterType: "CLUSTER_TYPE_CONTROL_PLANE_GROUP",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ct := controlPlaneClusterType(tc.cp)
			assert.Equal(t, tc.expectedClusterType, ct)
			assert.Equal(t, tc.expected, controlPlaneSupportsWorkspaces(ct))
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
