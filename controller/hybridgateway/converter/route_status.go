package converter

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	hybridgatewayerrors "github.com/kong/kong-operator/v2/controller/hybridgateway/errors"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/refs"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/route"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/pkg/vars"
)

type buildResolvedRefsConditionFunc[T gwtypes.SupportedRoute, TPtr gwtypes.SupportedRoutePtr[T]] func(
	context.Context,
	logr.Logger,
	client.Client,
	TPtr,
) (*metav1.Condition, error)

func updateRouteStatus[T gwtypes.SupportedRoute, TPtr gwtypes.SupportedRoutePtr[T]](
	ctx context.Context,
	logger logr.Logger,
	cl client.Client,
	routeObject TPtr,
	expectedGVKs []schema.GroupVersionKind,
	buildResolvedRefsCondition buildResolvedRefsConditionFunc[T, TPtr],
) (updated bool, stop bool, err error) {
	routeKind := routeKindForStatusPhase(*routeObject)
	logger = logger.WithValues("phase", strings.ToLower(routeKind)+"-status")
	log.Debug(logger, "Starting UpdateRootObjectStatus")

	log.Debug(logger, "Building ResolvedRefs condition", "routeKind", routeKind)
	resolvedRefsCond, err := buildResolvedRefsCondition(ctx, logger, cl, routeObject)
	if err != nil {
		return false, stop, fmt.Errorf("failed to build resolvedRefs condition for %s %s: %w", routeKind, routeObject.GetName(), err)
	}

	for _, pRef := range gwtypes.GetSpecParentRefs(*routeObject) {
		log.Debug(logger, "Processing ParentReference", "parentRef", pRef)
		gateway, found, err := refs.GetSupportedGatewayForParentRef(ctx, logger, cl, pRef, routeObject.GetNamespace())
		if err != nil {
			switch {
			case errors.Is(err, hybridgatewayerrors.ErrNoGatewayClassFound),
				errors.Is(err, hybridgatewayerrors.ErrNoGatewayController),
				errors.Is(err, hybridgatewayerrors.ErrNoGatewayFound),
				errors.Is(err, hybridgatewayerrors.ErrUnsupportedKind),
				errors.Is(err, hybridgatewayerrors.ErrUnsupportedGroup):
				log.Debug(logger, "Skipping status update Gateway", "parentRef", pRef, "reason", err)
				if route.RemoveStatusForParentRef(logger, routeObject, pRef, vars.ControllerName()) {
					log.Debug(logger, "Removed ParentStatus for unsupported ParentReference", "parentRef", pRef)
					updated = true
					stop = false
				}
				continue
			default:
				log.Error(logger, err, "Failed to get supported gateway for ParentReference", "parentRef", pRef)
				return false, stop, fmt.Errorf("failed to get supported gateway for parentRef %s: %w", pRef.Name, err)
			}
		}
		if !found {
			continue
		}

		log.Debug(logger, "Building Accepted condition", "parentRef", pRef, "gateway", gateway.Name)
		acceptedCondition, err := route.BuildAcceptedCondition(ctx, logger, cl, gateway, routeObject, pRef)
		if err != nil {
			return false, stop, fmt.Errorf("failed to build accepted condition for parentRef %s: %w", pRef.Name, err)
		}
		// Unresolved backend references should still translate and be applied so the
		// data plane returns a Gateway API-compliant 500 instead of falling through to 404.
		// Only an unaccepted route must halt state enforcement.
		if acceptedCondition.Status == metav1.ConditionFalse {
			stop = true
		}

		log.Debug(logger, "Building Programmed conditions", "parentRef", pRef, "gateway", gateway.Name)
		programmedConditions, err := route.BuildProgrammedCondition(ctx, logger, cl, routeObject, pRef, expectedGVKs)
		if err != nil {
			return false, stop, fmt.Errorf("failed to build programmed condition for parentRef %s: %w", pRef.Name, err)
		}

		programmedConditions = append(programmedConditions, *acceptedCondition, *resolvedRefsCond)

		log.Debug(logger, "Setting status conditions", "parentRef", pRef, "conditionsCount", len(programmedConditions))
		if route.SetStatusConditions(routeObject, pRef, vars.ControllerName(), programmedConditions...) {
			log.Debug(logger, "Status conditions updated for ParentReference", "parentRef", pRef)
			updated = true
		}
	}

	log.Debug(logger, "Cleaning up orphaned ParentStatus entries")
	if route.CleanupOrphanedParentStatus(logger, routeObject, vars.ControllerName()) {
		log.Debug(logger, "Orphaned ParentStatus entries cleaned up")
		updated = true
	}

	if updated {
		log.Debug(logger, "Updating route status in cluster", "routeKind", routeKind)
		if err := cl.Status().Update(ctx, routeObject); err != nil {
			if apierrors.IsConflict(err) {
				return false, true, err
			}
			log.Error(logger, err, "Failed to update route status in cluster", "routeKind", routeKind)
			return false, stop, fmt.Errorf("failed to update %s status: %w", routeKind, err)
		}
	} else {
		log.Debug(logger, "No status update required", "routeKind", routeKind)
	}

	log.Debug(logger, "Finished UpdateRootObjectStatus", "updated", updated)
	return updated, stop, nil
}

func routeKindForStatusPhase[T gwtypes.SupportedRoute](routeObject T) string {
	switch any(routeObject).(type) {
	case gwtypes.HTTPRoute:
		return "HTTPRoute"
	case gwtypes.TLSRoute:
		return "TLSRoute"
	}
	return "Route"
}
