package resources

import (
	"fmt"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
)

// GenerateNewRoleForControlPlane generates a new Role in provided namespace for
// provided ControlPlane.
func GenerateNewRoleForControlPlane(
	cp *operatorv1beta1.ControlPlane, namespace string, rules []rbacv1.PolicyRule,
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
