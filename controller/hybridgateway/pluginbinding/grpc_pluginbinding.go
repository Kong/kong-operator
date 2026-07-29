package pluginbinding

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/builder"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/metadata"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/namegen"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/translator"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

// BindingForGRPCPluginAndRoute creates or updates a KongPluginBinding for the given plugin and
// GRPCRoute-derived KongRoute. Mirrors BindingForPluginAndRoute for GRPCRoute.
func BindingForGRPCPluginAndRoute(
	ctx context.Context,
	logger logr.Logger,
	cl client.Client,
	grpcRoute *gwtypes.GRPCRoute,
	pRef *gwtypes.ParentReference,
	cp *commonv1alpha1.ControlPlaneRef,
	pluginName string,
	routeName string,
) (kongPluginBinding *configurationv1alpha1.KongPluginBinding, err error) {
	bindingName := namegen.NewKongPluginBindingName(routeName, pluginName)
	logger = logger.WithValues("kongpluginbinding", bindingName)
	log.Debug(logger, "Generating KongPluginBinding for KongPlugin and KongRoute")

	binding, err := builder.NewKongPluginBinding().
		WithName(bindingName).
		WithNamespace(metadata.NamespaceFromParentRef(grpcRoute, pRef)).
		WithLabels(grpcRoute, pRef).
		WithAnnotations(grpcRoute, pRef).
		WithPluginRef(pluginName).
		WithControlPlaneRef(*cp).
		WithRouteRef(routeName).
		Build()
	if err != nil {
		log.Error(logger, err, "Failed to build KongPluginBinding resource")
		return nil, fmt.Errorf("failed to build KongPluginBinding %s: %w", bindingName, err)
	}

	if _, err = translator.VerifyAndUpdate(ctx, logger, cl, &binding, grpcRoute, true); err != nil {
		return nil, err
	}

	return &binding, nil
}

// BindingForGRPCPluginAndService creates or updates a KongPluginBinding for the given plugin and
// GRPCRoute-derived KongService. Mirrors BindingForPluginAndService for GRPCRoute.
func BindingForGRPCPluginAndService(
	ctx context.Context,
	logger logr.Logger,
	cl client.Client,
	grpcRoute *gwtypes.GRPCRoute,
	pRef *gwtypes.ParentReference,
	cp *commonv1alpha1.ControlPlaneRef,
	pluginName string,
	serviceName string,
) (kongPluginBinding *configurationv1alpha1.KongPluginBinding, err error) {
	bindingName := namegen.NewKongPluginBindingName(serviceName, pluginName)
	logger = logger.WithValues("kongpluginbinding", bindingName)
	log.Debug(logger, "Generating KongPluginBinding for KongPlugin and KongService")

	binding, err := builder.NewKongPluginBinding().
		WithName(bindingName).
		WithNamespace(metadata.NamespaceFromParentRef(grpcRoute, pRef)).
		WithLabels(grpcRoute, pRef).
		WithAnnotations(grpcRoute, pRef).
		WithPluginRef(pluginName).
		WithControlPlaneRef(*cp).
		WithServiceRef(serviceName).
		Build()
	if err != nil {
		log.Error(logger, err, "Failed to build KongPluginBinding resource")
		return nil, fmt.Errorf("failed to build KongPluginBinding %s: %w", bindingName, err)
	}

	if _, err = translator.VerifyAndUpdate(ctx, logger, cl, &binding, grpcRoute, true); err != nil {
		return nil, err
	}

	return &binding, nil
}
