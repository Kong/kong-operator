package resources

import (
	"fmt"

	"github.com/samber/lo"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kong/kong-operator/pkg/consts"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
)

// -----------------------------------------------------------------------------
// ClusterRoleBinding generators
// -----------------------------------------------------------------------------

// GenerateNewClusterRoleBindingForControlPlane is a helper to generate a ClusterRoleBinding
// resource to bind roles to the service account used by the controlplane deployment.
func GenerateNewClusterRoleBindingForControlPlane(namespace, controlplaneName, serviceAccountName, clusterRoleName string) *rbacv1.ClusterRoleBinding {
	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: k8sutils.TrimGenerateName(fmt.Sprintf("%s-%s-", consts.ControlPlanePrefix, controlplaneName)),
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
	LabelObjectAsControlPlaneManaged(crb)
	return crb
}

// CompareClusterRoleName compares RoleRef in ClusterRoleBinding with given cluster role name.
// It returns true if the referenced role is the cluster role with the given name.
func CompareClusterRoleName(existingClusterRoleBinding *rbacv1.ClusterRoleBinding, clusterRoleName string) bool {
	return existingClusterRoleBinding.RoleRef.APIGroup == "rbac.authorization.k8s.io" &&
		existingClusterRoleBinding.RoleRef.Kind == "ClusterRole" &&
		existingClusterRoleBinding.RoleRef.Name == clusterRoleName
}

// ClusterRoleBindingContainsServiceAccount returns true if the subjects of the ClusterRoleBinding contains given service account.
func ClusterRoleBindingContainsServiceAccount(existingClusterRoleBinding *rbacv1.ClusterRoleBinding, namespace string, serviceAccountName string) bool {
	return lo.ContainsBy(existingClusterRoleBinding.Subjects, func(s rbacv1.Subject) bool {
		return s.Kind == "ServiceAccount" && s.Namespace == namespace && s.Name == serviceAccountName
	})
}
