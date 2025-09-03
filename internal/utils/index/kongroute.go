package index

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kubernetes-configuration/v2/pkg/metadata"

	configurationv1alpha1 "github.com/kong/kong-operator/apis/configuration/v1alpha1"
)

const (
	// IndexFieldKongRouteOnReferencedPluginNames is the index field for KongRoute -> KongPlugin.
	IndexFieldKongRouteOnReferencedPluginNames = "kongRouteKongPluginRef"
	// IndexFieldKongRouteOnReferencedKongService is the index field for KongRoute -> KongService.
	IndexFieldKongRouteOnReferencedKongService = "kongRouteKongServiceRef"
	// IndexFieldKongRouteOnKonnectGatewayControlPlane is the index field for KongRoute -> KonnectGatewayControlPlane.
	IndexFieldKongRouteOnKonnectGatewayControlPlane = "kongRouteKonnectGatewayControlPlaneRef"
)

// OptionsForKongRoute returns required Index options for KongRoute reconciler.
func OptionsForKongRoute(cl client.Client) []Option {
	return []Option{
		{
			Object:         &configurationv1alpha1.KongRoute{},
			Field:          IndexFieldKongRouteOnReferencedPluginNames,
			ExtractValueFn: kongRouteUsesPlugins,
		},
		{
			Object:         &configurationv1alpha1.KongRoute{},
			Field:          IndexFieldKongRouteOnReferencedKongService,
			ExtractValueFn: kongRouteRefersToKongService,
		},
		{
			Object:         &configurationv1alpha1.KongRoute{},
			Field:          IndexFieldKongRouteOnKonnectGatewayControlPlane,
			ExtractValueFn: indexKonnectGatewayControlPlaneRef[configurationv1alpha1.KongRoute](cl),
		},
	}
}

func kongRouteUsesPlugins(object client.Object) []string {
	route, ok := object.(*configurationv1alpha1.KongRoute)
	if !ok {
		return nil
	}
	return metadata.ExtractPluginsWithNamespaces(route)
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

	// NOTE: We currently do not allow cross namespace references between KongRoute and KongService.
	// https://github.com/Kong/kubernetes-configuration/issues/106 tracks the implementation.

	return []string{route.Namespace + "/" + svcRef.NamespacedRef.Name}
}
