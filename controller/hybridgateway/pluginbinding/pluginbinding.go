package pluginbinding

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/controller/hybridgateway/builder"
	"github.com/kong/kong-operator/controller/hybridgateway/metadata"
	"github.com/kong/kong-operator/controller/hybridgateway/namegen"
	"github.com/kong/kong-operator/controller/pkg/log"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/pkg/consts"
)

// BindingForPluginAndRoute creates or updates a KongPluginBinding for the given plugin and route.
//
// The function performs the following operations:
// 1. Generates the KongPluginBinding name using the namegen package
// 2. Checks if a KongPluginBinding with that name already exists in the cluster
// 3. If it exists, updates the KongPluginBinding annotations to include the current HTTPRoute
// 4. If it doesn't exist, creates a new KongPluginBinding
// 5. Returns the KongPluginBinding resource for use by the caller
//
// Parameters:
//   - ctx: The context for API calls and cancellation
//   - logger: Logger for structured logging
//   - cl: Kubernetes client for API operations
//   - httpRoute: The HTTPRoute resource from which the KongPluginBinding is derived
//   - pRef: The parent reference (Gateway) for the HTTPRoute
//   - cp: The control plane reference for the KongPluginBinding
//   - pluginName: The name of the KongPlugin to bind
//   - routeName: The name of the KongRoute to bind to
//
// Returns:
//   - kongPluginBinding: The created or updated KongPluginBinding resource
//   - exists: A boolean indicating whether the KongPluginBinding already exists (true) or must be created (false)
//   - err: Any error that occurred during the process
func BindingForPluginAndRoute(
	ctx context.Context,
	logger logr.Logger,
	cl client.Client,
	httpRoute *gwtypes.HTTPRoute,
	pRef *gwtypes.ParentReference,
	cp *commonv1alpha1.ControlPlaneRef,
	pluginName string,
	routeName string,
) (kongPluginBinding *configurationv1alpha1.KongPluginBinding, exists bool, err error) {
	bindingName := namegen.NewKongPluginBindingName(routeName, pluginName)
	logger = logger.WithValues("kongpluginbinding", bindingName)
	log.Debug(logger, "Generating KongPluginBinding for KongPlugin and KongRoute")

	binding, err := builder.NewKongPluginBinding().
		WithName(bindingName).
		WithNamespace(metadata.NamespaceFromParentRef(httpRoute, pRef)).
		WithLabels(httpRoute, pRef).
		WithAnnotations(httpRoute, pRef).
		WithPluginRef(pluginName).
		WithControlPlaneRef(*cp).
		WithRouteRef(routeName).
		Build()
	if err != nil {
		log.Error(logger, err, "Failed to build KongPluginBinding resource")
		return nil, false, fmt.Errorf("failed to build KongPluginBinding %s: %w", bindingName, err)
	}

	// Check if KongPluginBinding already exists
	existingBinding := &configurationv1alpha1.KongPluginBinding{}
	namespacedName := types.NamespacedName{
		Name:      bindingName,
		Namespace: httpRoute.Namespace,
	}
	if err = cl.Get(ctx, namespacedName, existingBinding); err != nil && !apierrors.IsNotFound(err) {
		log.Error(logger, err, "Failed to check for existing KongPluginBinding")
		return nil, false, fmt.Errorf("failed to check for existing KongPluginBinding %s: %w", bindingName, err)
	}

	if apierrors.IsNotFound(err) {
		// KongPluginBinding doesn't exist, create a new one
		log.Debug(logger, "New KongPluginBinding generated successfully")
		return &binding, false, nil
	}

	// KongPluginBinding exists, update annotations to include current HTTPRoute
	log.Debug(logger, "KongPluginBinding found")
	binding.Annotations[consts.GatewayOperatorHybridRoutesAnnotation] = existingBinding.Annotations[consts.GatewayOperatorHybridRoutesAnnotation]
	annotationManager := metadata.NewAnnotationManager(logger)
	annotationManager.AppendRouteToAnnotation(&binding, httpRoute)

	// TODO: we should check that the existingBinding.Spec matches what we expect
	// https://github.com/Kong/kong-operator/issues/2687
	log.Debug(logger, "Successfully updated existing KongPluginBinding")

	return &binding, true, nil
}
