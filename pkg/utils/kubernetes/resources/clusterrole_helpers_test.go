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
		devMode             bool
		expectedClusterRole func() *rbacv1.ClusterRole
		expectedError       error
	}{
		{
			controlplane: "test_3.1.2",
			image:        "kong/kubernetes-ingress-controller:3.1.2",
			expectedClusterRole: func() *rbacv1.ClusterRole {
				cr := clusterroles.GenerateNewClusterRoleForControlPlane_ge3_1_lt3_2("test_3.1.2")
				resources.LabelObjectAsControlPlaneManaged(cr)
				return cr
			},
		},
		{
			controlplane: "test_3.1_dev",
			image:        "kong/kubernetes-ingress-controller:3.1",
			devMode:      true,
			expectedClusterRole: func() *rbacv1.ClusterRole {
				cr := clusterroles.GenerateNewClusterRoleForControlPlane_ge3_4("test_3.1_dev")
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
			controlplane: "test_3.0_dev",
			image:        "kong/kubernetes-ingress-controller:3.0.0",
			devMode:      true,
			expectedClusterRole: func() *rbacv1.ClusterRole {
				cr := clusterroles.GenerateNewClusterRoleForControlPlane_ge3_4("test_3.0_dev")
				resources.LabelObjectAsControlPlaneManaged(cr)
				return cr
			},
		},
		{
			controlplane:  "test_unsupported",
			image:         "kong/kubernetes-ingress-controller:1.0",
			expectedError: resources.ErrControlPlaneVersionNotSupported,
		},
		{
			controlplane: "test_unsupported_dev",
			image:        "kong/kubernetes-ingress-controller:1.0",
			devMode:      true,
			expectedClusterRole: func() *rbacv1.ClusterRole {
				cr := clusterroles.GenerateNewClusterRoleForControlPlane_ge3_4("test_unsupported_dev")
				resources.LabelObjectAsControlPlaneManaged(cr)
				return cr
			},
		},
		{
			controlplane:  "test_invalid_tag",
			image:         "test/development:main",
			expectedError: kgoerrors.ErrInvalidSemverVersion,
		},
		{
			controlplane: "test_invalid_tag_dev",
			image:        "test/development:main",
			devMode:      true,
			expectedClusterRole: func() *rbacv1.ClusterRole {
				cr := clusterroles.GenerateNewClusterRoleForControlPlane_ge3_4("test_invalid_tag_dev")
				resources.LabelObjectAsControlPlaneManaged(cr)
				return cr
			},
		},
		{
			controlplane: "cp-3-2-0",
			image:        "kong/kubernetes-ingress-controller:3.2.0",
			devMode:      false,
			expectedClusterRole: func() *rbacv1.ClusterRole {
				cr := clusterroles.GenerateNewClusterRoleForControlPlane_ge3_2_lt3_3("cp-3-2-0")
				resources.LabelObjectAsControlPlaneManaged(cr)
				return cr
			},
		},
		{
			controlplane: "cp-3-3-0",
			image:        "kong/kubernetes-ingress-controller:3.3.0",
			devMode:      false,
			expectedClusterRole: func() *rbacv1.ClusterRole {
				cr := clusterroles.GenerateNewClusterRoleForControlPlane_ge3_3_lt3_4("cp-3-3-0")
				resources.LabelObjectAsControlPlaneManaged(cr)
				return cr
			},
		},
		{
			controlplane: "cp-3-4-1",
			image:        "kong/kubernetes-ingress-controller:3.4.1",
			devMode:      false,
			expectedClusterRole: func() *rbacv1.ClusterRole {
				cr := clusterroles.GenerateNewClusterRoleForControlPlane_ge3_4("cp-3-4-1")
				resources.LabelObjectAsControlPlaneManaged(cr)
				return cr
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.controlplane, func(t *testing.T) {
			clusterRole, err := resources.GenerateNewClusterRoleForControlPlane(tc.controlplane, tc.image, tc.devMode)
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
