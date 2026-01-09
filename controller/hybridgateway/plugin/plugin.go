package plugin

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	configurationv1 "github.com/kong/kong-operator/api/configuration/v1"
	"github.com/kong/kong-operator/controller/hybridgateway/builder"
	"github.com/kong/kong-operator/controller/hybridgateway/metadata"
	"github.com/kong/kong-operator/controller/hybridgateway/namegen"
	"github.com/kong/kong-operator/controller/hybridgateway/translator"
	"github.com/kong/kong-operator/controller/pkg/log"
	gwtypes "github.com/kong/kong-operator/internal/types"
)

// PluginsForFilter creates or retrieves KongPlugins for the given HTTPRoute filter.
//
// Workflow:
//  1. If the filter type is ExtensionRef retrieve the referenced KongPlugin.
//  2. Otherwise, translates the filter into one or more plugin configurations.
//  3. For each plugin configuration:
//     - Derives the KongPlugin name using namegen.
//     - Builds a new KongPlugin resource with appropriate name, namespace, labels, annotations, and config.
//     - Calls translator.VerifyAndUpdate to handle existing plugin reconciliation.
//  4. Returns all translated plugins.
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
//   - filter: The HTTPRouteFilter being processed.
//   - pRef: Parent (Gateway) reference.
//
// Returns:
//   - kongPlugins: The translated plugin(s).
//   - selfManaged: True if sourced from ExtensionRef.
//   - err: Any error encountered.
func PluginsForFilter(
	ctx context.Context,
	logger logr.Logger,
	cl client.Client,
	httpRoute *gwtypes.HTTPRoute,
	rule gwtypes.HTTPRouteRule,
	filter gwtypes.HTTPRouteFilter,
	pRef *gwtypes.ParentReference,
) ([]configurationv1.KongPlugin, bool, error) {
	logger = logger.WithValues("filter-type", filter.Type)
	plugins := []configurationv1.KongPlugin{}

	// In case the filter is an ExtensionRef, retrieve the referenced KongPlugin and return early
	if filter.Type == gatewayv1.HTTPRouteFilterExtensionRef {
		log.Debug(logger, "Filter is an ExtensionRef, retrieving referenced KongPlugin")
		plugin, err := getReferencedKongPlugin(ctx, cl, httpRoute.Namespace, filter)
		pluginName := plugin.Name
		if err != nil {
			log.Error(logger, err, "Failed to retrieve referenced KongPlugin")
			return nil, false, fmt.Errorf("failed to retrieve referenced KongPlugin %s: %w", pluginName, err)
		}
		log.Debug(logger, "Successfully retrieved referenced KongPlugin")
		plugins = append(plugins, *plugin)
		return plugins, true, nil
	}

	pluginConfs, err := translateFromFilter(rule, filter)
	if err != nil {
		return nil, false, fmt.Errorf("translating filter to KongPlugins: %w", err)
	}
	for i := range pluginConfs {
		pConf := &pluginConfs[i]
		pluginName := namegen.NewKongPluginName(filter, pConf.name)
		logger := logger.WithValues("kongplugin", pluginName)
		log.Debug(logger, "Generating KongPlugin for HTTPRoute filter")

		plugin, err := builder.NewKongPlugin().
			WithName(pluginName).
			WithNamespace(metadata.NamespaceFromParentRef(httpRoute, pRef)).
			WithLabels(httpRoute, pRef).
			WithPluginName(pConf.name).
			WithPluginConfig(pConf.config).
			WithAnnotations(httpRoute, pRef).
			Build()
		if err != nil {
			log.Error(logger, err, "Failed to build KongPlugin resource")
			return nil, false, fmt.Errorf("failed to build KongPlugin %s: %w", pluginName, err)
		}

		if _, err = translator.VerifyAndUpdate(ctx, logger, cl, &plugin, httpRoute, false); err != nil {
			return nil, false, err
		}
		plugins = append(plugins, plugin)
	}

	return plugins, false, nil
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
		err = fmt.Errorf("failed to get KongPlugin for ExtensionRef %s: %w", filter.ExtensionRef.Name, err)
		return nil, err
	}

	return plugin, nil
}
