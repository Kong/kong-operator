package resources

import (
	"fmt"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// GenerateNewRoleForControlPlane generates a new Role in provided namespace for
// provided ControlPlane.
func GenerateNewRoleForControlPlane(
	cp *gwtypes.ControlPlane, namespace string, rules []rbacv1.PolicyRule,
) *rbacv1.Role {
	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: k8sutils.TrimGenerateName(fmt.Sprintf("%s-", cp.GetName())),
			Namespace:    namespace,
			Labels: map[string]string{
				"app": cp.GetName(),
			},
		},
		Rules: rules,
	}

	LabelObjectAsControlPlaneManaged(role)
	return role
}
