package plugin

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1 "github.com/kong/kong-operator/api/configuration/v1"
	"github.com/kong/kong-operator/controller/hybridgateway/builder"
	"github.com/kong/kong-operator/controller/hybridgateway/metadata"
	"github.com/kong/kong-operator/controller/hybridgateway/namegen"
	"github.com/kong/kong-operator/controller/pkg/log"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/pkg/consts"
)

// PluginForFilter creates or updates a KongPlugin for the given HTTPRoute filter.
//
// The function performs the following operations:
// 1. Generates the KongPlugin name using the namegen package
// 2. Checks if a KongPlugin with that name already exists in the cluster
// 3. If it exists, updates the KongPlugin annotations to include the current HTTPRoute
// 4. If it doesn't exist, creates a new KongPlugin
// 5. Returns the KongPlugin resource for use by the caller
//
// Parameters:
//   - ctx: The context for API calls and cancellation
//   - logger: Logger for structured logging
//   - cl: Kubernetes client for API operations
//   - httpRoute: The HTTPRoute resource from which the KongPlugin is derived
//   - filter: The specific filter within the HTTPRoute rule
//   - pRef: The parent reference (Gateway) for the HTTPRoute
//
// Returns:
//   - *configurationv1.KongPlugin: The created or updated KongPlugin resource
//   - error: Any error that occurred during the process
func PluginForFilter(
	ctx context.Context,
	logger logr.Logger,
	cl client.Client,
	httpRoute *gwtypes.HTTPRoute,
	filter gwtypes.HTTPRouteFilter,
	pRef *gwtypes.ParentReference,
) (*configurationv1.KongPlugin, error) {
	pluginName := namegen.NewKongPluginName(filter)
	logger = logger.WithValues("kongplugin", pluginName)
	log.Debug(logger, "Generating KongPlugin for HTTPRoute filter")

	plugin, err := builder.NewKongPlugin().
		WithName(pluginName).
		WithNamespace(metadata.NamespaceFromParentRef(httpRoute, pRef)).
		WithLabels(httpRoute, pRef).
		WithAnnotations(httpRoute, pRef).
		WithFilter(filter).
		Build()
	if err != nil {
		log.Error(logger, err, "Failed to build KongPlugin resource")
		return nil, fmt.Errorf("failed to build KongPlugin %s: %w", pluginName, err)
	}

	// Check if KongPlugin already exists
	existingPlugin := &configurationv1.KongPlugin{}
	namespacedName := types.NamespacedName{
		Name:      pluginName,
		Namespace: httpRoute.Namespace,
	}
	if err = cl.Get(ctx, namespacedName, existingPlugin); err != nil && !apierrors.IsNotFound(err) {
		log.Error(logger, err, "Failed to check for existing KongPlugin")
		return nil, fmt.Errorf("failed to check for existing KongPlugin %s: %w", pluginName, err)
	}

	if apierrors.IsNotFound(err) {
		// KongPlugin doesn't exist, create a new one
		log.Debug(logger, "New KongPlugin generated successfully")
		return &plugin, nil
	}

	// KongPlugin exists, update annotations to include current HTTPRoute
	log.Debug(logger, "KongPlugin found")
	plugin.Annotations[consts.GatewayOperatorHybridRoutesAnnotation] = existingPlugin.Annotations[consts.GatewayOperatorHybridRoutesAnnotation]
	annotationManager := metadata.NewAnnotationManager(logger)
	annotationManager.AppendRouteToAnnotation(&plugin, httpRoute)

	// TODO: we should check that the existingPlugin.Spec matches what we expect
	// https://github.com/Kong/kong-operator/issues/2687
	log.Debug(logger, "Successfully updated existing KongPlugin")

	return &plugin, nil
}
