package resources_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	rbacv1 "k8s.io/api/rbac/v1"

	kgoerrors "github.com/kong/gateway-operator/internal/errors"
	"github.com/kong/gateway-operator/internal/utils/kubernetes/resources"
	"github.com/kong/gateway-operator/internal/utils/kubernetes/resources/clusterroles"
	"github.com/kong/gateway-operator/internal/versions"
)

func TestClusterroleHelpers(t *testing.T) {
	testCases := []struct {
		controlplane        string
		image               string
		expectedClusterRole *rbacv1.ClusterRole
		expectedError       error
	}{
		{
			controlplane:        "test_2.10",
			image:               "kong/kubernetes-ingress-controller:2.10",
			expectedClusterRole: clusterroles.GenerateNewClusterRoleForControlPlane_ge2_10("test_2.10"),
		},
		{
			controlplane:        "test_2.9",
			image:               "kong/kubernetes-ingress-controller:2.9",
			expectedClusterRole: clusterroles.GenerateNewClusterRoleForControlPlane_lt2_10_ge2_9("test_2.9"),
		},
		{
			controlplane:        "test_development_untagged",
			image:               "test/development",
			expectedClusterRole: clusterroles.GenerateNewClusterRoleForControlPlane_ge2_10("test_development_untagged"),
			expectedError:       versions.ErrExpectedSemverVersion,
		},
		{
			controlplane:        "test_empty",
			image:               "kong/kubernetes-ingress-controller",
			expectedClusterRole: clusterroles.GenerateNewClusterRoleForControlPlane_ge2_10("test_empty"),
			expectedError:       versions.ErrExpectedSemverVersion,
		},
		{
			controlplane:        "test_unsupported",
			image:               "kong/kubernetes-ingress-controller:1.0",
			expectedClusterRole: clusterroles.GenerateNewClusterRoleForControlPlane_ge2_10("test_unsupported"),
		},
		{
			controlplane:  "test_invalid_tag",
			image:         "test/development:main",
			expectedError: kgoerrors.ErrInvalidSemverVersion,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.controlplane, func(t *testing.T) {
			clusterRole, err := resources.GenerateNewClusterRoleForControlPlane(tc.controlplane, tc.image)
			if tc.expectedError != nil {
				require.ErrorIs(t, err, tc.expectedError)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.expectedClusterRole, clusterRole)
			}
		})
	}
}
