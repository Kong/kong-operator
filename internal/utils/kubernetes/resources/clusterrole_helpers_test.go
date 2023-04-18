package resources_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	rbacv1 "k8s.io/api/rbac/v1"

	"github.com/kong/gateway-operator/internal/utils/kubernetes/resources"
	"github.com/kong/gateway-operator/internal/utils/kubernetes/resources/clusterroles"
)

func TestClusterroleHelpers(t *testing.T) {
	testCases := []struct {
		controlplane        string
		image               string
		version             string
		expectedClusterRole *rbacv1.ClusterRole
	}{
		{
			controlplane:        "test_2.7",
			image:               "kong/kubernetes-ingress-controller",
			version:             "2.7",
			expectedClusterRole: clusterroles.GenerateNewClusterRoleForControlPlane_lt2_9_ge2_7("test_2.7"),
		},
		{
			controlplane:        "test_2.9",
			image:               "kong/kubernetes-ingress-controller",
			version:             "2.9",
			expectedClusterRole: clusterroles.GenerateNewClusterRoleForControlPlane_ge2_9("test_2.9"),
		},
		{
			controlplane:        "test_development_untagged",
			image:               "test/development",
			expectedClusterRole: clusterroles.GenerateNewClusterRoleForControlPlane_ge2_9("test_development_untagged"),
		},
		{
			controlplane:        "test_development_tagged",
			image:               "test/development",
			version:             "main",
			expectedClusterRole: clusterroles.GenerateNewClusterRoleForControlPlane_ge2_9("test_development_tagged"),
		},
		{
			controlplane:        "test_empty",
			image:               "kong/kubernetes-ingress-controller",
			expectedClusterRole: clusterroles.GenerateNewClusterRoleForControlPlane_ge2_9("test_empty"),
		},
		{
			controlplane:        "test_unsupported",
			image:               "kong/kubernetes-ingress-controller",
			version:             "1.0",
			expectedClusterRole: clusterroles.GenerateNewClusterRoleForControlPlane_ge2_9("test_unsupported"),
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.controlplane, func(t *testing.T) {
			clusterRole, err := resources.GenerateNewClusterRoleForControlPlane(tc.controlplane, &tc.image, &tc.version)
			require.NoError(t, err)

			require.Equal(t, tc.expectedClusterRole, clusterRole)
		})
	}
}
