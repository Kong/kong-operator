package hybridgateway

import (
	"context"
	"fmt"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kong/kong-operator/controller/hybridgateway/route"
	"github.com/kong/kong-operator/controller/pkg/log"
	"github.com/kong/kong-operator/controller/pkg/op"
)

// RouteStatusReconciler is a generic reconciler for route objects, allowing creation of controllers for different route types by specifying type parameters.
type RouteStatusReconciler[t route.RouteObject, tPtr route.RouteObjectPtr[t]] struct {
	client.Client

	sharedStatusMap *route.SharedRouteStatusMap
}

// NewRouteStatusReconciler returns a new RouteStatusReconciler for the given route type, using the provided manager and shared status map.
func NewRouteStatusReconciler[t route.RouteObject, tPtr route.RouteObjectPtr[t]](mgr ctrl.Manager, sharedStatusMap *route.SharedRouteStatusMap) *RouteStatusReconciler[t, tPtr] {
	return &RouteStatusReconciler[t, tPtr]{
		Client:          mgr.GetClient(),
		sharedStatusMap: sharedStatusMap,
	}
}

// SetupWithManager registers the reconciler with the controller manager for the specific route type.
func (r *RouteStatusReconciler[t, tPtr]) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	obj := any(new(t)).(tPtr)
	return ctrl.NewControllerManagedBy(mgr).
		For(obj).
		Complete(r)
}

// Reconcile fetches the route object, computes and enforces its status, and schedules requeueing. Used by controllers for different route types.
func (r *RouteStatusReconciler[t, tPtr]) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := ctrllog.FromContext(ctx)

	var obj tPtr = new(t)
	if err := r.Get(ctx, req.NamespacedName, obj); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	var routeObj t
	routeObj, ok := any(*obj).(t)
	if !ok {
		return ctrl.Result{}, fmt.Errorf("failed to convert object of type %T to route object type %T", obj, routeObj)
	}

	gvk := obj.GetObjectKind().GroupVersionKind()
	log.Debug(logger, "Reconciling object", "Group", gvk.Group, "Kind", gvk.Kind)

	statusUpdater, err := route.NewRouteStatusUpdater(routeObj, r.Client, logger, r.sharedStatusMap)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to create status updater: %w", err)
	}

	statusUpdater.ComputeStatus()

	res, err := statusUpdater.EnforceStatus(ctx)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to enforce status: %w", err)
	}
	if res != op.Noop {
		logger.Info("route status updated", "namespace", obj.GetNamespace(), "name", obj.GetName())
	}

	// TODO: workaround for not having watches in place yet
	return ctrl.Result{}, nil
}
