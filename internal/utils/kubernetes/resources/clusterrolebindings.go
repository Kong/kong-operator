package resources

import (
	"fmt"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kong/gateway-operator/internal/consts"
)

// -----------------------------------------------------------------------------
// ClusterRoleBinding generators
// -----------------------------------------------------------------------------

// GenerateNewClusterRoleBindingForControlPlane is a helper to generate a ClusterRoleBinding
// resource to bind roles to the service account used by the controlplane deployment.
func GenerateNewClusterRoleBindingForControlPlane(namespace, controlplaneName, serviceAccountName, clusterRoleName string) *rbacv1.ClusterRoleBinding {
	return &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("%s-%s-", consts.ControlPlanePrefix, controlplaneName),
			Labels: map[string]string{
				"app": controlplaneName,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     clusterRoleName,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      serviceAccountName,
				Namespace: namespace,
			},
		},
	}
}
