package konnect

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/pkg/annotations"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

const (
	// IndexFieldKongRouteOnReferencedPluginNames is the index field for KongRoute -> KongPlugin.
	IndexFieldKongRouteOnReferencedPluginNames = "kongRouteKongPluginRef"
	// IndexFieldKongRouteOnReferencedKongService is the index field for KongRoute -> KongService.
	IndexFieldKongRouteOnReferencedKongService = "kongRouteKongServiceRef"
)

// IndexOptionsForKongRoute returns required Index options for KongRoute reconciler.
func IndexOptionsForKongRoute() []ReconciliationIndexOption {
	return []ReconciliationIndexOption{
		{
			IndexObject:  &configurationv1alpha1.KongRoute{},
			IndexField:   IndexFieldKongRouteOnReferencedPluginNames,
			ExtractValue: kongRouteUsesPlugins,
		},
		{
			IndexObject:  &configurationv1alpha1.KongRoute{},
			IndexField:   IndexFieldKongRouteOnReferencedKongService,
			ExtractValue: kongRouteRefersToKongService,
		},
	}
}

func kongRouteUsesPlugins(object client.Object) []string {
	route, ok := object.(*configurationv1alpha1.KongRoute)
	if !ok {
		return nil
	}
	return annotations.ExtractPluginsWithNamespaces(route)
}

func kongRouteRefersToKongService(object client.Object) []string {
	route, ok := object.(*configurationv1alpha1.KongRoute)
	if !ok {
		return nil
	}
	svcRef := route.Spec.ServiceRef
	if svcRef == nil ||
		svcRef.Type != configurationv1alpha1.ServiceRefNamespacedRef ||
		svcRef.NamespacedRef == nil {
		return nil
	}

	namespace := route.Namespace
	if svcRef.NamespacedRef.Namespace != "" {
		namespace = svcRef.NamespacedRef.Namespace
	}

	return []string{namespace + "/" + svcRef.NamespacedRef.Name}
}
