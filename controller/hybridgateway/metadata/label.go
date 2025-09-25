package metadata

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/pkg/consts"
)

// BuildLabels creates the standard labels map for Kong resources managed by HTTPRoute.
func BuildLabels(obj client.Object) map[string]string {
	return buildManagedByLabels(obj)
}

// buildManagedByLabels returns the identifying labels for resources managed by a given object.
func buildManagedByLabels(obj client.Object) map[string]string {
	return map[string]string{
		consts.GatewayOperatorManagedByLabel:          obj.GetObjectKind().GroupVersionKind().Kind,
		consts.GatewayOperatorManagedByNameLabel:      obj.GetName(),
		consts.GatewayOperatorManagedByNamespaceLabel: obj.GetNamespace(),
	}
}

// LabelSelectorForOwnedResources returns a label selector for listing resources managed by the given object.
func LabelSelectorForOwnedResources(obj client.Object) client.ListOption {
	return client.MatchingLabels(buildManagedByLabels(obj))
}
