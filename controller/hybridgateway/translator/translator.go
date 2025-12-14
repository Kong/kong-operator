package translator

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/controller/hybridgateway/metadata"
	"github.com/kong/kong-operator/controller/pkg/log"
	"github.com/kong/kong-operator/pkg/consts"
)

// VerifyAndUpdate verifies if the object passaed as parameter already exists or not in the cluster.
// If it exists, VerifyAndUpdate updates the hybrid-routes annotation in the object to include the provided route.
// For more information about the hybrid-routes annotation, see https://github.com/Kong/kong-operator/blob/main/docs/internal/hybridgateway/autogen-resource-naming.md .
//
// Parameters:
//   - ctx: Context for API calls.
//   - logger: Structured logger.
//   - cl: Kubernetes client.
//   - obj: The object to verify and potentially update.
//   - route: The HTTPRoute object to associate with the obj.
//   - exclusiveRoute: If true, ensures that the obj is only associated with the provided route.
//
// Returns:
//   - exists: True if the object already exists in the cluster.
//   - err: Any error encountered during the process.
func VerifyAndUpdate[T client.Object](
	ctx context.Context,
	logger logr.Logger,
	cl client.Client,
	obj T,
	route client.Object,
	exclusiveRoute bool,
) (exists bool, err error) {
	existingObj := obj.DeepCopyObject().(T)
	// Verify: check if obj already exists
	if err = cl.Get(ctx, client.ObjectKeyFromObject(obj), existingObj); err != nil {
		if apierrors.IsNotFound(err) {
			log.Debug(logger, "Object not found")
			return false, nil
		}
		log.Error(logger, err, "Failed to get existing object",
			"object-type", obj.GetObjectKind().GroupVersionKind().Kind,
			"object-name", obj.GetName(),
			"object-namespace", obj.GetNamespace(),
		)
		return false, err
	}

	// Update: verify and update the hybrid-routes annotation
	routesCSV := existingObj.GetAnnotations()[consts.GatewayOperatorHybridRoutesAnnotation]
	routes := strings.Split(routesCSV, ",")
	if len(routes) == 0 {
		err = fmt.Errorf("existing %s object %s/%s has empty hybrid-routes annotation",
			obj.GetObjectKind().GroupVersionKind().Kind, existingObj.GetNamespace(), existingObj.GetName())
		log.Error(logger, err, "Tracking annotation check failed")
		return true, err
	}
	// If exclusiveRoute is true, ensure that the existing object is only associated with the provided route
	// A resource is exclusive to a route if it cannot be shared among multiple routes, e.g., each KongRoute is derived
	// from a single HTTPRoute only and is not shared with other HTTPRoutes by design.
	// See https://github.com/Kong/kong-operator/blob/main/docs/internal/hybridgateway/autogen-resource-naming.md for more details of
	// how the resource are generated and associated with routes.
	if exclusiveRoute {
		if len(routes) > 1 || strings.TrimSpace(routes[0]) != metadata.ObjectToNameString(route) {
			err = fmt.Errorf("existing %s object %s/%s is associated with multiple routes %s",
				obj.GetObjectKind().GroupVersionKind().Kind, existingObj.GetNamespace(), existingObj.GetName(), routesCSV)
			log.Error(logger, err, "Tracking annotation exclusive source Route check failed")
			return true, err
		}
	}

	// Object exists, update annotation to include current route
	am := metadata.NewAnnotationManager(logger)
	am.SetRoutes(obj, am.GetRoutes(existingObj))
	am.AppendRouteToAnnotation(obj, route)

	return true, nil
}
