package gateway

import (
	"context"
	"fmt"
	"maps"
	"slices"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/gatewayapi"
	"github.com/kong/kong-operator/v2/pkg/metadata"
)

const httpRoutePluginRefIndexKey = "httproute-pluginref"

func setupHTTPRouteIndices(mgr ctrl.Manager) error {
	if err := mgr.GetCache().IndexField(
		context.Background(),
		&gatewayapi.HTTPRoute{},
		httpRoutePluginRefIndexKey,
		indexHTTPRouteOnPluginReferences,
	); err != nil {
		return fmt.Errorf("failed to setup httproute indexers: %w", err)
	}
	return nil
}

func indexHTTPRouteOnPluginReferences(obj client.Object) []string {
	httproute, ok := obj.(*gatewayapi.HTTPRoute)
	if !ok {
		return []string{}
	}

	refs := make(map[string]struct{})
	for _, pluginRef := range metadata.ExtractPluginsNamespacedNames(httproute) {
		namespace := pluginRef.Namespace
		if namespace == "" {
			namespace = httproute.Namespace
		}
		refs[namespace+"/"+pluginRef.Name] = struct{}{}
	}

	for _, rule := range httproute.Spec.Rules {
		for _, filter := range rule.Filters {
			if filter.Type != gatewayapi.HTTPRouteFilterExtensionRef || filter.ExtensionRef == nil {
				continue
			}
			if string(filter.ExtensionRef.Group) != configurationv1.GroupVersion.Group || filter.ExtensionRef.Kind != "KongPlugin" {
				continue
			}
			refs[httproute.Namespace+"/"+string(filter.ExtensionRef.Name)] = struct{}{}
		}
	}

	if len(refs) == 0 {
		return []string{}
	}

	return slices.Collect(maps.Keys(refs))
}
