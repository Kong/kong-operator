package konnect

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"

	"github.com/kong/gateway-operator/pkg/annotations"
)

const (
	// IndexFieldKongServiceOnReferencedPluginNames is the index field for KongService -> KongPlugin.
	IndexFieldKongServiceOnReferencedPluginNames = "kongServiceKongPluginRef"
)

// IndexOptionsForKongService returns required Index options for KongService reconciler.
func IndexOptionsForKongService() []ReconciliationIndexOption {
	return []ReconciliationIndexOption{
		{
			IndexObject:  &configurationv1alpha1.KongService{},
			IndexField:   IndexFieldKongServiceOnReferencedPluginNames,
			ExtractValue: kongServiceUsesPlugins,
		},
	}
}

func kongServiceUsesPlugins(object client.Object) []string {
	svc, ok := object.(*configurationv1alpha1.KongService)
	if !ok {
		return nil
	}

	return annotations.ExtractPlugins(svc)
}
