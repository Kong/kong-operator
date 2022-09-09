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

// GenerateNewClusterRoleBindingForCertificateConfig is a helper to generate a ClusterRoleBinding
// to be used by the certificateConfig jobs
func GenerateNewClusterRoleBindingForCertificateConfig(namespace, name, labelValue string) *rbacv1.ClusterRoleBinding {
	return &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"app": labelValue,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     name,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      name,
				Namespace: namespace,
			},
		},
	}
}
