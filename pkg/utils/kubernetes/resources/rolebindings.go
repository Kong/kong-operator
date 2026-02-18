package resources

import (
	"fmt"

	"github.com/samber/lo"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"

	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// -----------------------------------------------------------------------------
// RoleBinding generators
// -----------------------------------------------------------------------------

// GenerateNewRoleBindingForControlPlane is a helper to generate a RoleBinding
// resource to bind roles to the service account used by the controlplane deployment.
func GenerateNewRoleBindingForControlPlane(
	cp *gwtypes.ControlPlane,
	serviceAccountName string,
	roleNN k8stypes.NamespacedName,
) *rbacv1.RoleBinding {
	crb := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: k8sutils.TrimGenerateName(fmt.Sprintf("%s-%s-", consts.ControlPlanePrefix, cp.GetName())),
			Namespace:    roleNN.Namespace,
			Labels: map[string]string{
				"app": cp.GetName(),
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     roleNN.Name,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      serviceAccountName,
				Namespace: cp.GetNamespace(),
			},
		},
	}
	LabelObjectAsControlPlaneManaged(crb)
	return crb
}

// CompareRoleName compares RoleRef in RoleBinding with given cluster role name.
// It returns true if the referenced role is the cluster role with the given name.
func CompareRoleName(existingRoleBinding *rbacv1.RoleBinding, roleName string) bool {
	return existingRoleBinding.RoleRef.APIGroup == "rbac.authorization.k8s.io" &&
		existingRoleBinding.RoleRef.Kind == "Role" &&
		existingRoleBinding.RoleRef.Name == roleName
}

// RoleBindingContainsServiceAccount returns true if the subjects of the RoleBinding contains given service account.
func RoleBindingContainsServiceAccount(existingRoleBinding *rbacv1.RoleBinding, namespace string, serviceAccountName string) bool {
	return lo.ContainsBy(existingRoleBinding.Subjects, func(s rbacv1.Subject) bool {
		return s.Kind == "ServiceAccount" && s.Namespace == namespace && s.Name == serviceAccountName
	})
}
