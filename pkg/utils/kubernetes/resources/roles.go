package resources

import (
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// -----------------------------------------------------------------------------
// Role generator helpers
// -----------------------------------------------------------------------------

// GenerateNewRoleForCertificateConfig is a helper to generate a Role
// to be used by the certificateConfig jobs
func GenerateNewRoleForCertificateConfig(namespace, name, labelValue string) *rbacv1.Role {
	return &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"app": labelValue,
			},
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{
					"",
				},
				Resources: []string{
					"secrets",
				},
				Verbs: []string{
					"get", "create",
				},
			},
		},
	}
}
