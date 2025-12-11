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
	"github.com/kong/kong-operator/controller/pkg/log"
	gwtypes "github.com/kong/kong-operator/internal/types"
)

// PluginForFilter creates or retrieves a KongPlugin for the given HTTPRoute filter.
//
// Workflow:
//  1. Derives the KongPlugin name (namegen) and enriches the logger context.
//  2. If the filter type is ExtensionRef:
//     - Validates group/kind.
//     - Retrieves the referenced KongPlugin directly (no build, no mutation).
//     - Returns it with selfManaged=true.
//  3. Otherwise builds a new KongPlugin (builder chain: name, namespace, labels, annotations, filter).
//  4. Attempts to GET an existing KongPlugin with the same name/namespace:
//     - If an API error (other than NotFound) occurs, returns the error.
//     - If NotFound, returns the freshly built plugin (selfManaged=false) for creation.
//     - If found:
//     a. Preserves the existing hybrid routes annotation value.
//     b. Uses AnnotationManager to append the current HTTPRoute reference.
//     c. Returns the updated (rebuilt) plugin (selfManaged=false).
//  5. Spec reconciliation for existing plugins is not performed yet (see TODO / issue 2687).
//
// Self-managed plugin:
//
//	A plugin referenced via ExtensionRef. These are returned as-is without rebuild,
//	annotation modification, or existence checks.
//
// Parameters:
//
//   - ctx: Context for API calls.
//   - logger: Structured logger.
//   - cl: Kubernetes client.
//   - httpRoute: Source HTTPRoute.
//   - filter: The HTTPRouteFilter being processed.
//   - pRef: Parent (Gateway) reference.
//
// Returns:
//   - kongPlugin: The translated plugin.
//   - exists: True if the KongPlugin already exists.
//   - selfManaged: True if sourced from ExtensionRef.
//   - err: Any error encountered.
func PluginForFilter(
	ctx context.Context,
	logger logr.Logger,
	cl client.Client,
	httpRoute *gwtypes.HTTPRoute,
	filter gwtypes.HTTPRouteFilter,
	pRef *gwtypes.ParentReference,
) (kongPlugin *configurationv1.KongPlugin, exists, selfManaged bool, err error) {
	pluginName := namegen.NewKongPluginName(filter)
	logger = logger.WithValues("kongplugin", pluginName)
	log.Debug(logger, "Generating KongPlugin for HTTPRoute filter")

	// In case the filter is an ExtensionRef, retrieve the referenced KongPlugin and return early
	if filter.Type == gatewayv1.HTTPRouteFilterExtensionRef {
		log.Debug(logger, "Filter is an ExtensionRef, retrieving referenced KongPlugin")
		plugin, err := getReferencedKongPlugin(ctx, cl, httpRoute.Namespace, filter)
		if err != nil {
			log.Error(logger, err, "Failed to retrieve referenced KongPlugin")
			return nil, false, false, fmt.Errorf("failed to retrieve referenced KongPlugin %s: %w", pluginName, err)
		}
		log.Debug(logger, "Successfully retrieved referenced KongPlugin")
		return plugin, true, true, nil
	}

	plugin, err := builder.NewKongPlugin().
		WithName(pluginName).
		WithNamespace(metadata.NamespaceFromParentRef(httpRoute, pRef)).
		WithLabels(httpRoute, pRef).
		WithAnnotations(httpRoute, pRef).
		WithFilter(filter).
		Build()
	if err != nil {
		log.Error(logger, err, "Failed to build KongPlugin resource")
		return nil, false, false, fmt.Errorf("failed to build KongPlugin %s: %w", pluginName, err)
	}

	exists, err = metadata.AppendRouteToAnnotationIfObjExists(ctx, logger, cl, &plugin, httpRoute, false)
	if err != nil {
		return nil, false, false, err
	}

	return &plugin, exists, false, nil
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
