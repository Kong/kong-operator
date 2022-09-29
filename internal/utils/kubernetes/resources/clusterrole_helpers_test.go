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
		expectedClusterRole *rbacv1.ClusterRole
	}{
		{
			controlplane:        "test_2.1",
			image:               "kong/kubernetes-ingress-controller:2.1",
			expectedClusterRole: clusterroles.GenerateNewClusterRoleForControlPlane_ge2_1_lt2_2("test_2.1"),
		},
		{
			controlplane:        "test_2.2.1",
			image:               "kong/kubernetes-ingress-controller:2.2",
			expectedClusterRole: clusterroles.GenerateNewClusterRoleForControlPlane_ge2_2_lt2_3("test_2.2.1"),
		},
		{
			controlplane:        "test_2.3",
			image:               "kong/kubernetes-ingress-controller:2.3",
			expectedClusterRole: clusterroles.GenerateNewClusterRoleForControlPlane_ge2_3_lt2_4("test_2.3"),
		},
		{
			controlplane:        "test_2.4.2",
			image:               "kong/kubernetes-ingress-controller:2.4.2",
			expectedClusterRole: clusterroles.GenerateNewClusterRoleForControlPlane_ge2_4_lt2_6("test_2.4.2"),
		},
		{
			controlplane:        "test_2.5",
			image:               "kong/kubernetes-ingress-controller:2.5",
			expectedClusterRole: clusterroles.GenerateNewClusterRoleForControlPlane_ge2_4_lt2_6("test_2.5"),
		},
		{
			controlplane:        "test_2.6",
			image:               "kong/kubernetes-ingress-controller:2.6",
			expectedClusterRole: clusterroles.GenerateNewClusterRoleForControlPlane_ge2_6_lt2_7("test_2.6"),
		},
		{
			controlplane:        "test_2.7",
			image:               "kong/kubernetes-ingress-controller:2.7",
			expectedClusterRole: clusterroles.GenerateNewClusterRoleForControlPlane_ge2_6_lt2_7("test_2.7"),
		},
		{
			controlplane:        "test_latest",
			image:               "kong/kubernetes-ingress-controller:latest",
			expectedClusterRole: clusterroles.GenerateNewClusterRoleForControlPlane_ge2_7("test_latest"),
		},
		{
			controlplane:        "test_empty",
			image:               "kong/kubernetes-ingress-controller",
			expectedClusterRole: clusterroles.GenerateNewClusterRoleForControlPlane_ge2_7("test_empty"),
		},
	}

	for _, tc := range testCases {
		clusterRole, err := resources.GenerateNewClusterRoleForControlPlane(tc.controlplane, &tc.image)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(clusterRole, tc.expectedClusterRole) {
			t.Fatalf("clusterRole %s different from the expected one", clusterRole.Name)
		}
	}
}
