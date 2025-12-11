package pluginbinding

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/controller/hybridgateway/builder"
	"github.com/kong/kong-operator/controller/hybridgateway/metadata"
	"github.com/kong/kong-operator/controller/hybridgateway/namegen"
	"github.com/kong/kong-operator/controller/hybridgateway/translator"
	"github.com/kong/kong-operator/controller/pkg/log"
	gwtypes "github.com/kong/kong-operator/internal/types"
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

	exists, err = translator.VerifyAndUpdate(ctx, logger, cl, &binding, httpRoute, true)
	if err != nil {
		return nil, false, err
	}

	return &binding, exists, nil
}
