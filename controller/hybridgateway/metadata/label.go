package metadata

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/pkg/consts"
)

// BuildLabels creates the standard labels map for Kong resources managed by the given object and ParentReference.
// It generates labels that identify which route and gateway the Kong resources belong to, enabling
// proper resource tracking and cleanup.
func BuildLabels(obj client.Object, pRef *gwtypes.ParentReference) map[string]string {
	return buildManagedByLabels(obj, pRef)
}

// buildManagedByLabels returns the identifying labels for resources managed by a given object and its ParentReference.
// The labels include information about both the managing route and the target gateway.
func buildManagedByLabels(obj client.Object, pRef *gwtypes.ParentReference) map[string]string {
	// If no ParentReference is provided, only include labels for the managing object.
	if pRef == nil {
		return map[string]string{
			consts.GatewayOperatorManagedByLabel:          obj.GetObjectKind().GroupVersionKind().Kind,
			consts.GatewayOperatorManagedByNameLabel:      obj.GetName(),
			consts.GatewayOperatorManagedByNamespaceLabel: obj.GetNamespace(),
		}
	}

	gwObjKey := client.ObjectKey{
		Name: string(pRef.Name),
	}
	if pRef.Namespace != nil && *pRef.Namespace != "" {
		gwObjKey.Namespace = string(*pRef.Namespace)
	} else {
		gwObjKey.Namespace = obj.GetNamespace()
	}

	return map[string]string{
		consts.GatewayOperatorManagedByLabel:               obj.GetObjectKind().GroupVersionKind().Kind,
		consts.GatewayOperatorManagedByNameLabel:           obj.GetName(),
		consts.GatewayOperatorManagedByNamespaceLabel:      obj.GetNamespace(),
		consts.GatewayOperatorHybridGatewaysNameLabel:      gwObjKey.Name,
		consts.GatewayOperatorHybridGatewaysNamespaceLabel: gwObjKey.Namespace,
	}
}

// LabelSelectorForOwnedResources returns a label selector for listing resources managed by the given object and ParentReference.
// This can be used to find all Kong resources associated with a specific route-gateway pair.
func LabelSelectorForOwnedResources(obj client.Object, pRef *gwtypes.ParentReference) client.ListOption {
	return client.MatchingLabels(buildManagedByLabels(obj, pRef))
}
