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

// PluginsForRule creates or retrieves the KongPlugins for all filters of the given HTTPRoute rule.
//
// Workflow:
//  1. Translates the rule's built-in filters into plugin configurations, merging filters that map
//     to the same Kong plugin type into a single configuration. This is required because Kong
//     enforces a unique-plugin-per-entity constraint: a route cannot have two plugins of the same
//     type bound to it (e.g. URLRewrite and RequestHeaderModifier both map to request-transformer).
//  2. For each resulting plugin configuration:
//     - Derives the KongPlugin name using namegen (from the contributing filters).
//     - Builds a new KongPlugin resource with appropriate name, namespace, labels, annotations, and config.
//     - Calls translator.VerifyAndUpdate to handle existing plugin reconciliation.
//  3. Retrieves the KongPlugin referenced by each ExtensionRef filter.
//  4. Returns all plugins.
//
// Self-managed plugin:
//
//	A plugin referenced via ExtensionRef. These are returned as-is without rebuild,
//	annotation modification, or existence checks. The user is responsible for managing
//	the plugin lifecycle.
//
// Parameters:
//
//   - ctx: Context for API calls.
//   - logger: Structured logger.
//   - cl: Kubernetes client.
//   - httpRoute: Source HTTPRoute.
//   - rule: The HTTPRouteRule being processed.
//   - pRef: Parent (Gateway) reference.
//
// Returns:
//   - kongPlugins: The translated plugin(s).
//   - err: Any error encountered.
func PluginsForRule(
	ctx context.Context,
	logger logr.Logger,
	cl client.Client,
	httpRoute *gwtypes.HTTPRoute,
	rule gwtypes.HTTPRouteRule,
	pRef *gwtypes.ParentReference,
) ([]configurationv1.KongPlugin, error) {
	plugins := []configurationv1.KongPlugin{}

	// Translate the built-in filters, merging filters that map to the same Kong plugin type.
	pluginConfs, err := translateRuleFilters(rule)
	if err != nil {
		return nil, fmt.Errorf("translating filters to KongPlugins: %w", err)
	}
	for i := range pluginConfs {
		pConf := &pluginConfs[i]
		pluginName := namegen.NewKongPluginNameForFilters(pConf.filters, httpRoute.Namespace, pConf.name)
		logger := logger.WithValues("kongplugin", pluginName)
		log.Debug(logger, "Generating KongPlugin for HTTPRoute filters")

		plugin, err := builder.NewKongPlugin().
			WithName(pluginName).
			WithNamespace(metadata.NamespaceFromParentRef(httpRoute, pRef)).
			WithLabels(httpRoute, pRef).
			WithPluginName(pConf.name).
			WithPluginConfig(pConf.config).
			WithAnnotations(httpRoute, pRef).
			Build()
		if err != nil {
			return nil, fmt.Errorf("failed to build KongPlugin %s: %w", pluginName, err)
		}

		if _, err = translator.VerifyAndUpdate(ctx, logger, cl, &plugin, httpRoute, false); err != nil {
			return nil, err
		}
		plugins = append(plugins, plugin)
	}

	// ExtensionRef filters reference user-managed KongPlugins; retrieve each one as-is.
	for _, filter := range rule.Filters {
		if filter.Type != gatewayv1.HTTPRouteFilterExtensionRef {
			continue
		}

		logger := logger.WithValues("filter-type", filter.Type)
		log.Debug(logger, "Filter is an ExtensionRef, retrieving referenced KongPlugin")
		plugin, err := getReferencedKongPlugin(ctx, cl, httpRoute.Namespace, filter)
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
			WithName(namegen.NewKongPluginName(filter, httpRoute.Namespace, plugin.PluginName)).
			WithNamespace(metadata.NamespaceFromParentRef(httpRoute, pRef)).
			WithLabels(httpRoute, pRef).
			WithPluginName(plugin.PluginName).
			WithPluginConfig(plugin.Config.Raw).
			WithAnnotations(httpRoute, pRef).
			Build()
		if err != nil {
			return nil, fmt.Errorf("failed to build KongPlugin %s: %w", pluginName, err)
		}
		plugins = append(plugins, pluginCopy)
	}

	return plugins, nil
}

func getReferencedKongPlugin(ctx context.Context, cl client.Client, namespace string, filter gwtypes.HTTPRouteFilter) (*configurationv1.KongPlugin, error) {
	var err error
	plugin := &configurationv1.KongPlugin{}

	if filter.ExtensionRef == nil {
		err = errors.New("ExtensionRef filter is missing")
		return nil, err
	}

	if filter.ExtensionRef.Group != gatewayv1.Group(configurationv1.GroupVersion.Group) || filter.ExtensionRef.Kind != "KongPlugin" {
		err = fmt.Errorf("unsupported ExtensionRef: %s/%s", filter.ExtensionRef.Group, filter.ExtensionRef.Kind)
		return nil, err
	}

	err = cl.Get(ctx, types.NamespacedName{
		Name:      string(filter.ExtensionRef.Name),
		Namespace: namespace,
	}, plugin)
	if err != nil {
		return nil, err
	}

	return plugin, nil
}
