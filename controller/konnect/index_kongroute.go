package konnect

import (
	"strings"

	"github.com/samber/lo"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/pkg/consts"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

const (
	// IndexFieldKongRouteOnReferencedPluginNames is the index field for KongRoute -> KongPlugin.
	IndexFieldKongRouteOnReferencedPluginNames = "kongRouteKongPluginRef"
	// IndexFieldKongRouteOnServiceReference is the index field for KongRoute -> Service.
	IndexFieldKongRouteOnServiceReference = "kongRouteServiceRef"
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
	ann, ok := route.Annotations[consts.PluginsAnnotationKey]
	if !ok {
		return nil
	}

	namespace := route.GetNamespace()
	return lo.Map(strings.Split(ann, ","), func(p string, _ int) string {
		return namespace + "/" + p
	})
}
