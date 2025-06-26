package resources_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	rbacv1 "k8s.io/api/rbac/v1"

	kgoerrors "github.com/kong/kong-operator/internal/errors"
	k8sresources "github.com/kong/kong-operator/pkg/utils/kubernetes/resources"
	"github.com/kong/kong-operator/pkg/utils/kubernetes/resources/clusterroles"
)

func TestClusterroleHelpers(t *testing.T) {
	testCases := []struct {
		controlplane              string
		image                     string
		validateControlPlaneImage bool
		expectedClusterRole       func() *rbacv1.ClusterRole
		expectedError             error
	}{
		{
			controlplane: "test_3.1.2",
			image:        "kong/kubernetes-ingress-controller:3.1.2",
			expectedClusterRole: func() *rbacv1.ClusterRole {
				cr := clusterroles.GenerateNewClusterRoleForControlPlane_ge3_1_lt3_2("test_3.1.2")
				k8sresources.LabelObjectAsControlPlaneManaged(cr)
				return cr
			},
		},
		{
			controlplane:              "test_3.1_dev",
			image:                     "kong/kubernetes-ingress-controller:3.1",
			validateControlPlaneImage: false,
			expectedClusterRole: func() *rbacv1.ClusterRole {
				cr := clusterroles.GenerateNewClusterRoleForControlPlane_ge3_4("test_3.1_dev")
				k8sresources.LabelObjectAsControlPlaneManaged(cr)
				return cr
			},
		},
		{
			controlplane:              "test_3.0",
			image:                     "kong/kubernetes-ingress-controller:3.0.0",
			validateControlPlaneImage: true,
			expectedError:             k8sresources.ErrControlPlaneVersionNotSupported,
		},
		{
			controlplane:              "test_3.0_dev",
			image:                     "kong/kubernetes-ingress-controller:3.0.0",
			validateControlPlaneImage: false,
			expectedClusterRole: func() *rbacv1.ClusterRole {
				cr := clusterroles.GenerateNewClusterRoleForControlPlane_ge3_4("test_3.0_dev")
				k8sresources.LabelObjectAsControlPlaneManaged(cr)
				return cr
			},
		},
		{
			controlplane:              "test_unsupported",
			image:                     "kong/kubernetes-ingress-controller:1.0",
			validateControlPlaneImage: true,
			expectedError:             k8sresources.ErrControlPlaneVersionNotSupported,
		},
		{
			controlplane:              "test_unsupported_dev",
			image:                     "kong/kubernetes-ingress-controller:1.0",
			validateControlPlaneImage: false,
			expectedClusterRole: func() *rbacv1.ClusterRole {
				cr := clusterroles.GenerateNewClusterRoleForControlPlane_ge3_4("test_unsupported_dev")
				k8sresources.LabelObjectAsControlPlaneManaged(cr)
				return cr
			},
		},
		{
			controlplane:              "test_invalid_tag",
			image:                     "test/development:main",
			validateControlPlaneImage: true,
			expectedError:             kgoerrors.ErrInvalidSemverVersion,
		},
		{
			controlplane:              "test_invalid_tag_dev",
			image:                     "test/development:main",
			validateControlPlaneImage: false,
			expectedClusterRole: func() *rbacv1.ClusterRole {
				cr := clusterroles.GenerateNewClusterRoleForControlPlane_ge3_4("test_invalid_tag_dev")
				k8sresources.LabelObjectAsControlPlaneManaged(cr)
				return cr
			},
		},
		{
			controlplane:              "cp-3-2-0",
			image:                     "kong/kubernetes-ingress-controller:3.2.0",
			validateControlPlaneImage: true,
			expectedClusterRole: func() *rbacv1.ClusterRole {
				cr := clusterroles.GenerateNewClusterRoleForControlPlane_ge3_2_lt3_3("cp-3-2-0")
				k8sresources.LabelObjectAsControlPlaneManaged(cr)
				return cr
			},
		},
		{
			controlplane:              "cp-3-3-0",
			image:                     "kong/kubernetes-ingress-controller:3.3.0",
			validateControlPlaneImage: true,
			expectedClusterRole: func() *rbacv1.ClusterRole {
				cr := clusterroles.GenerateNewClusterRoleForControlPlane_ge3_3_lt3_4("cp-3-3-0")
				k8sresources.LabelObjectAsControlPlaneManaged(cr)
				return cr
			},
		},
		{
			controlplane:              "cp-3-4-1",
			image:                     "kong/kubernetes-ingress-controller:3.4.1",
			validateControlPlaneImage: true,
			expectedClusterRole: func() *rbacv1.ClusterRole {
				cr := clusterroles.GenerateNewClusterRoleForControlPlane_ge3_4("cp-3-4-1")
				k8sresources.LabelObjectAsControlPlaneManaged(cr)
				return cr
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.controlplane, func(t *testing.T) {
			clusterRole, err := k8sresources.GenerateNewClusterRoleForControlPlane(tc.controlplane, tc.image, tc.validateControlPlaneImage)
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
