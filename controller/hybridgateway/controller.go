package hybridgateway

import (
	"context"
	"fmt"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/kong/kong-operator/controller/hybridgateway/converter"
	"github.com/kong/kong-operator/controller/hybridgateway/route"
	"github.com/kong/kong-operator/controller/hybridgateway/watch"
	"github.com/kong/kong-operator/controller/pkg/log"
)

// HybridGatewayReconciler is a generic reconciler for handling Gateway API resources
// in a hybrid environment. It operates on objects implementing the RootObject and
// RootObjectPtr interfaces, allowing flexible reconciliation logic for different resource types.
type HybridGatewayReconciler[t converter.RootObject, tPtr converter.RootObjectPtr[t]] struct {
	client.Client

	sharedStatusMap *route.SharedRouteStatusMap
}

// NewHybridGatewayReconciler creates a new instance of GatewayAPIHybridReconciler for the specified
// generic types t and tPtr. It initializes the reconciler with the client from the provided manager.
func NewHybridGatewayReconciler[t converter.RootObject, tPtr converter.RootObjectPtr[t]](mgr ctrl.Manager, sharedStatusMap *route.SharedRouteStatusMap) *HybridGatewayReconciler[t, tPtr] {
	return &HybridGatewayReconciler[t, tPtr]{
		Client:          mgr.GetClient(),
		sharedStatusMap: sharedStatusMap,
	}
}

// SetupWithManager sets up the controller with the provided manager.
// It registers the reconciler to watch and manage resources of type 'u'.
func (r *HybridGatewayReconciler[t, tPtr]) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	obj := any(new(t)).(tPtr)
	filter, err := watch.FilterBy(r.Client, obj)
	if err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(obj).
		WithEventFilter(predicate.NewPredicateFuncs(filter)).
		Complete(r)
}

// Reconcile reconciles the state of a custom resource by fetching the object,
// converting it to the expected type, translating it, and enforcing its desired state.
func (r *HybridGatewayReconciler[t, tPtr]) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := ctrllog.FromContext(ctx)

	var obj tPtr = new(t)
	if err := r.Get(ctx, req.NamespacedName, obj); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	rootObj, ok := any(*obj).(t)
	if !ok {
		return ctrl.Result{}, fmt.Errorf("failed to convert object of type %T to route object type %T", obj, rootObj)
	}

	gvk := obj.GetObjectKind().GroupVersionKind()
	log.Debug(logger, "Reconciling object", "Group", gvk.Group, "Kind", gvk.Kind)

	conv, err := converter.NewConverter(rootObj, r.Client, r.sharedStatusMap)
	if err != nil {
		return ctrl.Result{}, err
	}

	if err := Translate(conv, ctx); err != nil {
		return ctrl.Result{}, err
	}

	requeue, stop, err := EnforceState(ctx, r.Client, logger, conv)
	if err != nil || requeue {
		return ctrl.Result{Requeue: true}, err
	}
	if stop {
		// TODO: workaround for not having watches in place yet
		// This requeue should be ensured by watches on the owned resources
		return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	}

	// TODO: workaround for not having watches in place yet
	return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
}
