package service

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

// ServiceForRule creates or updates a KongService for the given HTTPRoute rule.
// This function handles the creation of services with proper annotations that track
// which HTTPRoutes reference the KongService. If the KongService already exists, it appends
// the current HTTPRoute name to the existing hybrid-routes annotation.
//
// The function performs the following operations:
// 1. Generates the KongService name using the namegen package
// 2. Checks if a KongService with that name already exists in the cluster
// 3. If it exists, appends the current HTTPRoute name to the existing hybrid-routes annotation
// 4. If it doesn't exist, creates a new KongService
// 5. Returns the KongService resource for use by the caller
//
// Parameters:
//   - ctx: The context for API calls and cancellation
//   - logger: Logger for structured logging
//   - cl: Kubernetes client for API operations
//   - httpRoute: The HTTPRoute resource that needs the service
//   - rule: The specific rule within the HTTPRoute
//   - pRef: The parent reference (Gateway) for the HTTPRoute
//   - cp: The control plane reference for the service
//   - upstreamName: The name of the KongUpstream this service should point to
//
// Returns:
//   - *configurationv1alpha1.KongService: The created or updated service resource
//   - error: Any error that occurred during the process
func ServiceForRule(
	ctx context.Context,
	logger logr.Logger,
	cl client.Client,
	httpRoute *gwtypes.HTTPRoute,
	rule gwtypes.HTTPRouteRule,
	pRef *gwtypes.ParentReference,
	cp *commonv1alpha1.ControlPlaneRef,
	upstreamName string,
) (*configurationv1alpha1.KongService, error) {
	serviceName := namegen.NewKongServiceName(cp, rule)
	logger = logger.WithValues("kongservice", serviceName)
	log.Debug(logger, "Generating KongService for HTTPRoute rule")

	service, err := builder.NewKongService().
		WithName(serviceName).
		WithNamespace(httpRoute.Namespace).
		WithLabels(httpRoute, pRef).
		WithAnnotations(httpRoute, pRef).
		WithSpecName(serviceName).
		WithSpecHost(upstreamName).
		WithControlPlaneRef(*cp).Build()
	if err != nil {
		log.Error(logger, err, "Failed to build KongService resource")
		return nil, fmt.Errorf("failed to build KongService %s: %w", serviceName, err)
	}

	// Check if the service already exists
	existingService := &configurationv1alpha1.KongService{}
	namespacedName := types.NamespacedName{
		Name:      serviceName,
		Namespace: httpRoute.Namespace,
	}
	if err = cl.Get(ctx, namespacedName, existingService); err != nil && !apierrors.IsNotFound(err) {
		log.Error(logger, err, "Failed to check for existing KongService")
		return nil, fmt.Errorf("failed to check for existing KongService %s: %w", serviceName, err)
	}

	if apierrors.IsNotFound(err) {
		// KongService doesn't exist, create a new one
		log.Debug(logger, "New KongService generated successfully")
		return &service, nil
	}

	// KongService exists, update annotations to include current HTTPRoute
	log.Debug(logger, "KongService found")
	annotationManager := metadata.NewAnnotationManager(logger)
	service.Annotations[consts.GatewayOperatorHybridRoutesAnnotation] = existingService.Annotations[consts.GatewayOperatorHybridRoutesAnnotation]
	annotationManager.AppendRouteToAnnotation(&service, httpRoute)

	// TODO: we should check that the existingService.Spec matches what we expect
	// https://github.com/Kong/kong-operator/issues/2687
	log.Debug(logger, "Successfully updated existing KongService")

	return &service, nil
}
