package konnect

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/pkg/annotations"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

const (
	// IndexFieldKongRouteOnReferencedPluginNames is the index field for KongRoute -> KongPlugin.
	IndexFieldKongRouteOnReferencedPluginNames = "kongRouteKongPluginRef"
)

// IndexOptionsForKongRoute returns required Index options for KongRoute reconciler.
func IndexOptionsForKongRoute() []ReconciliationIndexOption {
	return []ReconciliationIndexOption{
		{
			IndexObject:  &configurationv1alpha1.KongRoute{},
			IndexField:   IndexFieldKongRouteOnReferencedPluginNames,
			ExtractValue: kongRouteUsesPlugins,
		},
	}
}

func kongRouteUsesPlugins(object client.Object) []string {
	route, ok := object.(*configurationv1alpha1.KongRoute)
	if !ok {
		return nil
	}
	return annotations.ExtractPlugins(route)
}
