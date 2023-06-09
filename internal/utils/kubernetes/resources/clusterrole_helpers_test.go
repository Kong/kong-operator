package resources_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	rbacv1 "k8s.io/api/rbac/v1"

	kgoerrors "github.com/kong/gateway-operator/internal/errors"
	"github.com/kong/gateway-operator/internal/utils/kubernetes/resources"
	"github.com/kong/gateway-operator/internal/utils/kubernetes/resources/clusterroles"
)

func TestClusterroleHelpers(t *testing.T) {
	testCases := []struct {
		controlplane        string
		image               string
		version             string
		expectedClusterRole *rbacv1.ClusterRole
		expectedError       error
	}{
		{
			controlplane:        "test_2.10",
			image:               "kong/kubernetes-ingress-controller",
			version:             "2.10",
			expectedClusterRole: clusterroles.GenerateNewClusterRoleForControlPlane_ge2_10("test_2.10"),
		},
		{
			controlplane:        "test_2.9",
			image:               "kong/kubernetes-ingress-controller",
			version:             "2.9",
			expectedClusterRole: clusterroles.GenerateNewClusterRoleForControlPlane_lt2_10_ge2_9("test_2.9"),
		},
		{
			controlplane:        "test_development_untagged",
			image:               "test/development",
			expectedClusterRole: clusterroles.GenerateNewClusterRoleForControlPlane_ge2_10("test_development_untagged"),
		},
		{
			controlplane:        "test_empty",
			image:               "kong/kubernetes-ingress-controller",
			expectedClusterRole: clusterroles.GenerateNewClusterRoleForControlPlane_ge2_10("test_empty"),
		},
		{
			controlplane:        "test_unsupported",
			image:               "kong/kubernetes-ingress-controller",
			version:             "1.0",
			expectedClusterRole: clusterroles.GenerateNewClusterRoleForControlPlane_ge2_10("test_unsupported"),
		},
		{
			controlplane:  "test_invalid_tag",
			image:         "test/development",
			version:       "main",
			expectedError: kgoerrors.ErrInvalidSemverVersion,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.controlplane, func(t *testing.T) {
			clusterRole, err := resources.GenerateNewClusterRoleForControlPlane(tc.controlplane, &tc.image, &tc.version)
			if tc.expectedError != nil {
				require.ErrorIs(t, err, kgoerrors.ErrInvalidSemverVersion)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.expectedClusterRole, clusterRole)
			}
		})
	}
}
