package plugin

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/builder"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/metadata"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/namegen"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/translator"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

// GRPCPluginsForRule creates or retrieves the KongPlugins for all filters of the given GRPCRoute
// rule. Mirrors PluginsForRule for GRPCRoute's own filter type (GRPCRouteFilter), but unlike the
// HTTPRoute path it doesn't need a merge step: GRPCRouteRule's Filters are CEL-validated to at
// most one RequestHeaderModifier and at most one ResponseHeaderModifier per rule (see
// translateGRPCFromFilter), so each translated filter maps 1:1 to its own KongPlugin.
// RequestMirror is not yet translated (see translateGRPCFromFilter), and ExtensionRef filters are
// retrieved as-is exactly like the HTTPRoute path.
func GRPCPluginsForRule(
	ctx context.Context,
	logger logr.Logger,
	cl client.Client,
	grpcRoute *gwtypes.GRPCRoute,
	rule gwtypes.GRPCRouteRule,
	pRef *gwtypes.ParentReference,
) ([]configurationv1.KongPlugin, error) {
	plugins := []configurationv1.KongPlugin{}

	for _, filter := range rule.Filters {
		if filter.Type == gatewayv1.GRPCRouteFilterExtensionRef {
			continue
		}

		confs, err := translateGRPCFromFilter(filter)
		if err != nil {
			return nil, fmt.Errorf("translating filters to KongPlugins: %w", err)
		}

		for _, conf := range confs {
			pluginName := namegen.NewKongPluginNameForGRPCRouteFilter(filter, grpcRoute.Namespace, conf.name)
			logger := logger.WithValues("kongplugin", pluginName)
			log.Debug(logger, "Generating KongPlugin for GRPCRoute filter")

			plugin, err := builder.NewKongPlugin().
				WithName(pluginName).
				WithNamespace(metadata.NamespaceFromParentRef(grpcRoute, pRef)).
				WithLabels(grpcRoute, pRef).
				WithPluginName(conf.name).
				WithPluginConfig(conf.config).
				WithAnnotations(grpcRoute, pRef).
				WithTagsAnnotation(grpcRoute).
				Build()
			if err != nil {
				return nil, fmt.Errorf("failed to build KongPlugin %s: %w", pluginName, err)
			}

			if _, err = translator.VerifyAndUpdate(ctx, logger, cl, &plugin, grpcRoute, false); err != nil {
				return nil, err
			}
			plugins = append(plugins, plugin)
		}
	}

	// ExtensionRef filters reference user-managed KongPlugins; retrieve each one as-is.
	for _, filter := range rule.Filters {
		if filter.Type != gatewayv1.GRPCRouteFilterExtensionRef {
			continue
		}

		logger := logger.WithValues("filter-type", filter.Type)
		log.Debug(logger, "Filter is an ExtensionRef, retrieving referenced KongPlugin")
		plugin, err := getReferencedKongPluginForGRPCFilter(ctx, cl, grpcRoute.Namespace, filter)
		if err != nil {
			if apierrors.IsNotFound(err) {
				log.Debug(logger, "Referenced KongPlugin not found")
				continue
			}
			return nil, fmt.Errorf("failed to retrieve referenced KongPlugin: %w", err)
		}
		pluginName := plugin.Name
		log.Debug(logger, "Successfully retrieved referenced KongPlugin")
		pluginCopy, err := builder.NewKongPlugin().
			WithName(namegen.NewKongPluginNameForGRPCRouteFilter(filter, grpcRoute.Namespace, plugin.PluginName)).
			WithNamespace(metadata.NamespaceFromParentRef(grpcRoute, pRef)).
			WithLabels(grpcRoute, pRef).
			WithPluginName(plugin.PluginName).
			WithPluginConfig(plugin.Config.Raw).
			WithAnnotations(grpcRoute, pRef).
			WithTagsAnnotation(grpcRoute, plugin).
			Build()
		if err != nil {
			return nil, fmt.Errorf("failed to build KongPlugin %s: %w", pluginName, err)
		}
		plugins = append(plugins, pluginCopy)
	}

	return plugins, nil
}

func getReferencedKongPluginForGRPCFilter(ctx context.Context, cl client.Client, namespace string, filter gatewayv1.GRPCRouteFilter) (*configurationv1.KongPlugin, error) {
	if filter.ExtensionRef == nil {
		return nil, errors.New("ExtensionRef filter is missing")
	}

	if filter.ExtensionRef.Group != gatewayv1.Group(configurationv1.GroupVersion.Group) || filter.ExtensionRef.Kind != "KongPlugin" {
		return nil, fmt.Errorf("unsupported ExtensionRef: %s/%s", filter.ExtensionRef.Group, filter.ExtensionRef.Kind)
	}

	plugin := &configurationv1.KongPlugin{}
	if err := cl.Get(ctx, types.NamespacedName{
		Name:      string(filter.ExtensionRef.Name),
		Namespace: namespace,
	}, plugin); err != nil {
		return nil, err
	}

	return plugin, nil
}
