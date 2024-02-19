package resources_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	rbacv1 "k8s.io/api/rbac/v1"

	kgoerrors "github.com/kong/gateway-operator/internal/errors"
	"github.com/kong/gateway-operator/pkg/utils/kubernetes/resources"
	"github.com/kong/gateway-operator/pkg/utils/kubernetes/resources/clusterroles"
)

func TestClusterroleHelpers(t *testing.T) {
	testCases := []struct {
		controlplane        string
		image               string
		expectedClusterRole func() *rbacv1.ClusterRole
		expectedError       error
	}{
		{
			controlplane: "test_3.1",
			image:        "kong/kubernetes-ingress-controller:3.1",
			expectedClusterRole: func() *rbacv1.ClusterRole {
				cr := clusterroles.GenerateNewClusterRoleForControlPlane_ge3_1("test_3.1")
				resources.LabelObjectAsControlPlaneManaged(cr)
				return cr
			},
		},
		{
			controlplane:  "test_3.0",
			image:         "kong/kubernetes-ingress-controller:3.0.0",
			expectedError: resources.ErrControlPlaneVersionNotSupported,
		},
		{
			controlplane:  "test_unsupported",
			image:         "kong/kubernetes-ingress-controller:1.0",
			expectedError: resources.ErrControlPlaneVersionNotSupported,
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
				require.Error(t, err)
				require.ErrorIs(t, err, tc.expectedError)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.expectedClusterRole(), clusterRole)
			}
		})
	}
}
