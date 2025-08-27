package fullhybrid

import (
	"context"
	"fmt"

	"github.com/kong/kong-operator/controller/fullhybrid/converter"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type GatewayAPIHybridReconciler[t converter.RootObject, u client.Object] struct {
	client.Client
}

func NewGatewayAPIHybridReconciler[t converter.RootObject](mgr ctrl.Manager) *GatewayAPIHybridReconciler[t, client.Object] {
	return &GatewayAPIHybridReconciler[t, client.Object]{
		Client: mgr.GetClient(),
	}
}

func (r *GatewayAPIHybridReconciler[t, u]) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	var obj u
	return ctrl.NewControllerManagedBy(mgr).
		For(obj).
		Complete(r)
}

func (r *GatewayAPIHybridReconciler[t, u]) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var obj u
	if err := r.Get(ctx, req.NamespacedName, obj); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	rootObj, ok := any(obj).(t)
	if !ok {
		return ctrl.Result{}, fmt.Errorf("failed to convert obj to expected type")
	}
	logger.Info("Reconciling Object", "Group", obj.GetObjectKind().GroupVersionKind().Group, "Kind", obj.GetObjectKind().GroupVersionKind().Kind, "namespace", obj.GetNamespace(), "name", obj.GetName())

	conv, err := converter.NewConverter(rootObj, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}

	if err := Translate(conv, ctx); err != nil {
		return ctrl.Result{}, err
	}

	requeue, err := EnforceState(ctx, r.Client, conv)
	if err != nil || requeue {
		return ctrl.Result{Requeue: true}, err
	}

	return ctrl.Result{}, nil
}
