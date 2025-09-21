package metadata

import (
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/pkg/consts"
)

// BuildLabels creates the standard labels map for Kong resources managed by HTTPRoute.
func BuildLabels(route *gwtypes.HTTPRoute) map[string]string {
	return map[string]string{
		consts.GatewayOperatorManagedByLabel:          consts.HTTPRouteManagedByLabel,
		consts.GatewayOperatorManagedByNameLabel:      route.GetName(),
		consts.GatewayOperatorManagedByNamespaceLabel: route.GetNamespace(),
	}
}
