package resources

import (
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// -----------------------------------------------------------------------------
// ClusterRole generator helpers
// -----------------------------------------------------------------------------

// GenerateNewClusterRoleForCertificateConfig is a helper to generate a ClusterRole
// to be used by the certificateConfig jobs
func GenerateNewClusterRoleForCertificateConfig(namespace, name, labelValue string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"app": labelValue,
			},
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{
					"admissionregistration.k8s.io",
				},
				Resources: []string{
					"validatingwebhookconfigurations",
				},
				Verbs: []string{
					"get", "update",
				},
			},
		},
	}
}
