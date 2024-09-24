package konnect

import (
	"strings"

	"github.com/samber/lo"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/pkg/consts"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
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
	ann, ok := svc.Annotations[consts.PluginsAnnotationKey]
	if !ok {
		return nil
	}

	namespace := svc.GetNamespace()
	return lo.Map(strings.Split(ann, ","), func(p string, _ int) string {
		return namespace + "/" + p
	})
}
