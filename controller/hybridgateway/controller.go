package hybridgateway

import (
	"context"
	"fmt"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/kong/kong-operator/controller/hybridgateway/converter"
	"github.com/kong/kong-operator/controller/hybridgateway/watch"
	"github.com/kong/kong-operator/controller/pkg/log"
)

//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongroutes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongroutes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongservices,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongservices/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongupstreams,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongupstreams/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongtargets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongtargets/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongplugins,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongplugins/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongpluginbindings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongpluginbindings/status,verbs=get;update;patch

// HybridGatewayReconciler is a generic reconciler for handling Gateway API resources
// in a hybrid environment. It operates on objects implementing the RootObject and
// RootObjectPtr interfaces, allowing flexible reconciliation logic for different resource types.
type HybridGatewayReconciler[t converter.RootObject, tPtr converter.RootObjectPtr[t]] struct {
	client.Client
}

// NewHybridGatewayReconciler creates a new instance of GatewayAPIHybridReconciler for the specified
// generic types t and tPtr. It initializes the reconciler with the client from the provided manager.
func NewHybridGatewayReconciler[t converter.RootObject, tPtr converter.RootObjectPtr[t]](mgr ctrl.Manager) *HybridGatewayReconciler[t, tPtr] {
	return &HybridGatewayReconciler[t, tPtr]{
		Client: mgr.GetClient(),
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
	builder := ctrl.NewControllerManagedBy(mgr).
		For(obj).
		WithEventFilter(
			predicate.Funcs{
				CreateFunc: func(e event.CreateEvent) bool {
					return filter(e.Object)
				},
				UpdateFunc: func(e event.UpdateEvent) bool {
					// If either the old or new object passes the filter, we want to reconcile.
					// This ensures we handle cases where the object starts or stops matching the filter criteria.
					if filter(e.ObjectNew) {
						return true
					}
					return filter(e.ObjectOld)
				},
				DeleteFunc: func(e event.DeleteEvent) bool {
					return filter(e.Object)
				},
				GenericFunc: func(e event.GenericEvent) bool {
					return filter(e.Object)
				},
			})

	// Add watches for owned resources.
	for _, owned := range watch.Owns(obj) {
		builder = builder.Owns(owned)
	}

	return builder.Complete(r)
}

// Reconcile reconciles the state of a custom resource by fetching the object,
// converting it to the expected type, translating it, and enforcing its desired state.
func (r *HybridGatewayReconciler[t, tPtr]) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var obj tPtr = new(t)

	logger := ctrllog.FromContext(ctx).WithName("HybridGateway")

	if err := r.Get(ctx, req.NamespacedName, obj); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	rootObj, ok := any(*obj).(t)
	if !ok {
		return ctrl.Result{}, fmt.Errorf("failed to convert object of type %T to route object type %T", obj, rootObj)
	}

	gvk := obj.GetObjectKind().GroupVersionKind()
	log.Debug(logger, "Reconciling object", "Group", gvk.Group, "Kind", gvk.Kind)

	conv, err := converter.NewConverter(rootObj, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}

	if stop, err := EnforceStatus(ctx, logger, conv); err != nil && !k8serrors.IsConflict(err) {
		return ctrl.Result{}, err
	} else if k8serrors.IsConflict(err) {
		return ctrl.Result{Requeue: true}, nil
	} else if stop {
		return ctrl.Result{}, nil
	}

	if err := Translate(conv, ctx); err != nil {
		return ctrl.Result{}, err
	}

	requeue, _, err := EnforceState(ctx, r.Client, logger, conv)
	if err != nil || requeue {
		return ctrl.Result{Requeue: true}, err
	}

	if err := CleanOrphanedResources[t, tPtr](ctx, r.Client, logger, conv); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}
