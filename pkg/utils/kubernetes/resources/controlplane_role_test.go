package resources

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
)

func TestGenerateNewRoleForControlPlane(t *testing.T) {
	cp := &operatorv1beta1.ControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-controlplane",
			UID:  "12345",
		},
	}

	testCases := []struct {
		name             string
		controlplaneName string
		namespace        string
		rules            []rbacv1.PolicyRule
		expectedRole     *rbacv1.Role
	}{
		{
			name:             "generates role with empty rules",
			controlplaneName: "test-controlplane",
			namespace:        "test-namespace",
			rules:            []rbacv1.PolicyRule{},
			expectedRole: &rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: k8sutils.TrimGenerateName(fmt.Sprintf("%s-", "test-controlplane")),
					Namespace:    "test-namespace",
					Labels: map[string]string{
						"app":                                    "test-controlplane",
						"gateway-operator.konghq.com/managed-by": "controlplane",
					},
				},
				Rules: []rbacv1.PolicyRule{},
			},
		},
		{
			name:             "generates role with multiple rules",
			controlplaneName: "cp-with-rules",
			namespace:        "default",
			rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{""},
					Resources: []string{"pods"},
					Verbs:     []string{"get", "list", "watch"},
				},
				{
					APIGroups: []string{"apps"},
					Resources: []string{"deployments"},
					Verbs:     []string{"get", "update"},
				},
			},
			expectedRole: &rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: k8sutils.TrimGenerateName(fmt.Sprintf("%s-", "test-controlplane")),
					Namespace:    "default",
					Labels: map[string]string{
						"app":                                    "test-controlplane",
						"gateway-operator.konghq.com/managed-by": "controlplane",
					},
				},
				Rules: []rbacv1.PolicyRule{
					{
						APIGroups: []string{""},
						Resources: []string{"pods"},
						Verbs:     []string{"get", "list", "watch"},
					},
					{
						APIGroups: []string{"apps"},
						Resources: []string{"deployments"},
						Verbs:     []string{"get", "update"},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			role := GenerateNewRoleForControlPlane(cp, tc.namespace, tc.rules)
			require.Equal(t, tc.expectedRole, role)
		})
	}
}
