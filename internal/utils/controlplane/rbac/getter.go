package rbac

import (
	"github.com/blang/semver/v4"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// FIXME delete this
func GetRBACRolesForControlPlaneVersion(version semver.Version) ([]rbacv1.Role, []rbacv1.ClusterRole) {
	// FIXME - this file should be _generated_ and should do a lookup of the version
	return []rbacv1.Role{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "kong-leader-election",
				},
				Rules: []rbacv1.PolicyRule{
					{
						APIGroups: []string{
							"",
							"coordination.k8s.io",
						},
						Resources: []string{
							"configmaps",
							"leases",
						},
						Verbs: []string{
							"get",
							"list",
							"watch",
							"create",
							"update",
							"patch",
							"delete",
						},
					},
					{
						APIGroups: []string{
							"",
						},
						Resources: []string{
							"events",
						},
						Verbs: []string{
							"create",
							"patch",
						},
					},
				},
			},
		},
		[]rbacv1.ClusterRole{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "kong-ingress",
				},
				Rules: []rbacv1.PolicyRule{
					{
						APIGroups: []string{
							"",
						},
						Resources: []string{
							"endpoints",
						},
						Verbs: []string{
							"list",
							"watch",
						},
					},
					{
						APIGroups: []string{
							"",
						},
						Resources: []string{
							"endpoints/status",
						},
						Verbs: []string{
							"get",
							"patch",
							"update",
						},
					},
					{
						APIGroups: []string{
							"",
						},
						Resources: []string{
							"events",
						},
						Verbs: []string{
							"create",
							"patch",
						},
					},
					{
						APIGroups: []string{
							"",
						},
						Resources: []string{
							"nodes",
						},
						Verbs: []string{
							"list",
							"watch",
						},
					},
					{
						APIGroups: []string{
							"",
						},
						Resources: []string{
							"pods",
						},
						Verbs: []string{
							"list",
							"watch",
						},
					},
					{
						APIGroups: []string{
							"",
						},
						Resources: []string{
							"services",
						},
						Verbs: []string{
							"get",
							"list",
							"watch",
						},
					},
					{
						APIGroups: []string{
							"",
						},
						Resources: []string{
							"services/status",
						},
						Verbs: []string{
							"get",
							"patch",
							"update",
						},
					},
					{
						APIGroups: []string{
							"configuration.konghq.com",
						},
						Resources: []string{
							"kongclusterplugins",
						},
						Verbs: []string{
							"get",
							"list",
							"watch",
						},
					},
					{
						APIGroups: []string{
							"configuration.konghq.com",
						},
						Resources: []string{
							"kongclusterplugins/status",
						},
						Verbs: []string{
							"get",
							"patch",
							"update",
						},
					},
					{
						APIGroups: []string{
							"configuration.konghq.com",
						},
						Resources: []string{
							"kongconsumers",
						},
						Verbs: []string{
							"get",
							"list",
							"watch",
						},
					},
					{
						APIGroups: []string{
							"configuration.konghq.com",
						},
						Resources: []string{
							"kongconsumers/status",
						},
						Verbs: []string{
							"get",
							"patch",
							"update",
						},
					},

					{
						APIGroups: []string{
							"configuration.konghq.com",
						},
						Resources: []string{
							"kongingresses",
						},
						Verbs: []string{
							"get",
							"list",
							"watch",
						},
					},
					{
						APIGroups: []string{
							"configuration.konghq.com",
						},
						Resources: []string{
							"kongingresses/status",
						},
						Verbs: []string{
							"get",
							"patch",
							"update",
						},
					},
					{
						APIGroups: []string{
							"configuration.konghq.com",
						},
						Resources: []string{
							"kongplugins",
						},
						Verbs: []string{
							"get",
							"list",
							"watch",
						},
					},
					{
						APIGroups: []string{
							"configuration.konghq.com",
						},
						Resources: []string{
							"kongplugins/status",
						},
						Verbs: []string{
							"get",
							"patch",
							"update",
						},
					},
					{
						APIGroups: []string{
							"configuration.konghq.com",
						},
						Resources: []string{
							"tcpingresses",
						},
						Verbs: []string{
							"get",
							"list",
							"watch",
						},
					},
					{
						APIGroups: []string{
							"configuration.konghq.com",
						},
						Resources: []string{
							"tcpingresses/status",
						},
						Verbs: []string{
							"get",
							"patch",
							"update",
						},
					},
					{
						APIGroups: []string{
							"configuration.konghq.com",
						},
						Resources: []string{
							"udpingresses",
						},
						Verbs: []string{
							"get",
							"list",
							"watch",
						},
					},
					{
						APIGroups: []string{
							"configuration.konghq.com",
						},
						Resources: []string{
							"udpingresses/status",
						},
						Verbs: []string{
							"get",
							"patch",
							"update",
						},
					},
					{
						APIGroups: []string{
							"extensions",
						},
						Resources: []string{
							"ingresses",
						},
						Verbs: []string{
							"get",
							"list",
							"watch",
						},
					},
					{
						APIGroups: []string{
							"extensions",
						},
						Resources: []string{
							"ingresses/status",
						},
						Verbs: []string{
							"get",
							"patch",
							"update",
						},
					},
					{
						APIGroups: []string{
							"gateway.networking.k8s.io",
						},
						Resources: []string{
							"gatewayclasses",
						},
						Verbs: []string{
							"get",
							"list",
							"watch",
						},
					},
					{
						APIGroups: []string{
							"gateway.networking.k8s.io",
						},
						Resources: []string{
							"gatewayclasses/status",
						},
						Verbs: []string{
							"get",
							"patch",
							"update",
						},
					},
					{
						APIGroups: []string{
							"gateway.networking.k8s.io",
						},
						Resources: []string{
							"gateways",
						},
						Verbs: []string{
							"get",
							"list",
							"update",
							"watch",
						},
					},
					{
						APIGroups: []string{
							"gateway.networking.k8s.io",
						},
						Resources: []string{
							"gateways/status",
						},
						Verbs: []string{
							"get",
							"patch",
							"update",
						},
					},
					{
						APIGroups: []string{
							"gateway.networking.k8s.io",
						},
						Resources: []string{
							"httproutes",
						},
						Verbs: []string{
							"get",
							"list",
							"watch",
						},
					},
					{
						APIGroups: []string{
							"gateway.networking.k8s.io",
						},
						Resources: []string{
							"httproutes/status",
						},
						Verbs: []string{
							"get",
							"patch",
							"update",
						},
					},
					{
						APIGroups: []string{
							"gateway.networking.k8s.io",
						},
						Resources: []string{
							"tcproutes",
						},
						Verbs: []string{
							"get",
							"list",
							"watch",
						},
					},
					{
						APIGroups: []string{
							"gateway.networking.k8s.io",
						},
						Resources: []string{
							"tcproutes/status",
						},
						Verbs: []string{
							"get",
							"patch",
							"update",
						},
					},
					{
						APIGroups: []string{
							"gateway.networking.k8s.io",
						},
						Resources: []string{
							"udproutes",
						},
						Verbs: []string{
							"get",
							"list",
							"watch",
						},
					},
					{
						APIGroups: []string{
							"gateway.networking.k8s.io",
						},
						Resources: []string{
							"udproutes/status",
						},
						Verbs: []string{
							"get",
							"patch",
							"update",
						},
					},
					{
						APIGroups: []string{
							"networking.internal.knative.dev",
						},
						Resources: []string{
							"ingresses",
						},
						Verbs: []string{
							"get",
							"list",
							"watch",
						},
					},
					{
						APIGroups: []string{
							"networking.internal.knative.dev",
						},
						Resources: []string{
							"ingresses/status",
						},
						Verbs: []string{
							"get",
							"patch",
							"update",
						},
					},
					{
						APIGroups: []string{
							"networking.k8s.io",
						},
						Resources: []string{
							"ingressclasses",
						},
						Verbs: []string{
							"get",
							"list",
							"watch",
						},
					},
					{
						APIGroups: []string{
							"networking.k8s.io",
						},
						Resources: []string{
							"ingresses",
						},
						Verbs: []string{
							"get",
							"list",
							"watch",
						},
					},
					{
						APIGroups: []string{
							"networking.k8s.io",
						},
						Resources: []string{
							"ingresses/status",
						},
						Verbs: []string{
							"get",
							"patch",
							"update",
						},
					},
					{
						APIGroups: []string{
							"",
						},
						Resources: []string{
							"secrets",
						},
						Verbs: []string{
							"get",
							"list",
							"watch",
						},
					},
					{
						APIGroups: []string{
							"",
						},
						Resources: []string{
							"secrets/status",
						},
						Verbs: []string{
							"get",
							"patch",
							"update",
						},
					},
				},
			},
		}
}
