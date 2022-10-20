package resources_test

import (
	"reflect"
	"testing"

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
			expectedClusterRole: clusterroles.GenerateNewClusterRoleForControlPlane_ge2_7("test_2.7"),
		},
		{
			controlplane:        "test_development_untagged",
			image:               "test/development",
			expectedClusterRole: clusterroles.GenerateNewClusterRoleForControlPlane_ge2_7("test_development_untagged"),
		},
		{
			controlplane:        "test_development_tagged",
			image:               "test/development",
			version:             "main",
			expectedClusterRole: clusterroles.GenerateNewClusterRoleForControlPlane_ge2_7("test_development_tagged"),
		},
		{
			controlplane:        "test_empty",
			image:               "kong/kubernetes-ingress-controller",
			expectedClusterRole: clusterroles.GenerateNewClusterRoleForControlPlane_ge2_7("test_empty"),
		},
		{
			controlplane:        "test_unsupported",
			image:               "kong/kubernetes-ingress-controller",
			version:             "1.0",
			expectedClusterRole: clusterroles.GenerateNewClusterRoleForControlPlane_ge2_7("test_unsupported"),
		},
	}

	for _, tc := range testCases {
		clusterRole, err := resources.GenerateNewClusterRoleForControlPlane(tc.controlplane, &tc.image, &tc.version)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(clusterRole, tc.expectedClusterRole) {
			t.Fatalf("clusterRole %s different from the expected one", clusterRole.Name)
		}
	}
}
