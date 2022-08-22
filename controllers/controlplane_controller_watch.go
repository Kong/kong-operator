package controllers

import (
	"context"
	"reflect"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	operatorerrors "github.com/kong/gateway-operator/internal/errors"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
)

// -----------------------------------------------------------------------------
// ControlplaneReconciler - Watch Predicates
// -----------------------------------------------------------------------------

func (r *ControlPlaneReconciler) clusterRoleHasControlplaneOwner(obj client.Object) bool {
	ctx := context.Background()

	clusterRole, ok := obj.(*rbacv1.ClusterRole)
	if !ok {
		log.FromContext(ctx).Error(
			operatorerrors.ErrUnexpectedObject,
			"failed to run predicate function",
			"expected", "ClusterRole", "found", reflect.TypeOf(obj),
		)
		return false
	}

	return r.objHasControlplaneOwner(ctx, clusterRole)
}

func (r *ControlPlaneReconciler) clusterRoleBindingHasControlplaneOwner(obj client.Object) bool {
	ctx := context.Background()

	clusterRoleBinding, ok := obj.(*rbacv1.ClusterRoleBinding)
	if !ok {
		log.FromContext(ctx).Error(
			operatorerrors.ErrUnexpectedObject,
			"failed to run predicate function",
			"expected", "ClusterRoleBinding", "found", reflect.TypeOf(obj),
		)
		return false
	}

	return r.objHasControlplaneOwner(ctx, clusterRoleBinding)
}

func (r *ControlPlaneReconciler) objHasControlplaneOwner(ctx context.Context, obj client.Object) bool {
	controlplaneList := &operatorv1alpha1.ControlPlaneList{}
	if err := r.Client.List(ctx, controlplaneList); err != nil {
		// filtering here is just an optimization. If we fail here it's most likely because of some failure
		// of the Kubernetes API and it's technically better to enqueue the object
		// than to drop it for eventual consistency during cluster outages.
		log.FromContext(ctx).Error(err, "could not list controlplanes in map func")
		return true
	}

	for _, controlplane := range controlplaneList.Items {
		if k8sutils.IsOwnedByRefUID(obj, controlplane.GetUID()) {
			return true
		}
	}

	return false
}

// -----------------------------------------------------------------------------
// ControlplaneReconciler - Watch Map Funcs
// -----------------------------------------------------------------------------

func (r *ControlPlaneReconciler) getControlplaneForClusterRole(obj client.Object) (recs []reconcile.Request) {
	ctx := context.Background()

	clusterRole, ok := obj.(*rbacv1.ClusterRole)
	if !ok {
		log.FromContext(ctx).Error(
			operatorerrors.ErrUnexpectedObject,
			"failed to run map funcs",
			"expected", "ClusterRole", "found", reflect.TypeOf(obj),
		)
		return
	}

	return r.getControlplaneRequestFromRefUID(ctx, clusterRole)
}

func (r *ControlPlaneReconciler) getControlplaneForClusterRoleBinding(obj client.Object) (recs []reconcile.Request) {
	ctx := context.Background()

	clusterRoleBinding, ok := obj.(*rbacv1.ClusterRoleBinding)
	if !ok {
		log.FromContext(ctx).Error(
			operatorerrors.ErrUnexpectedObject,
			"failed to run map funcs",
			"expected", "ClusterRoleBinding", "found", reflect.TypeOf(obj),
		)
		return
	}

	return r.getControlplaneRequestFromRefUID(ctx, clusterRoleBinding)
}

func (r *ControlPlaneReconciler) getControlplaneRequestFromRefUID(ctx context.Context, obj client.Object) (recs []reconcile.Request) {
	controlplanes := &operatorv1alpha1.ControlPlaneList{}
	if err := r.Client.List(ctx, controlplanes); err != nil {
		log.FromContext(ctx).Error(err, "could not list controlplanes in map func")
		return
	}

	for _, controlplane := range controlplanes.Items {
		if k8sutils.IsOwnedByRefUID(obj, controlplane.GetUID()) {
			return []reconcile.Request{
				{
					NamespacedName: types.NamespacedName{
						Namespace: controlplane.Namespace,
						Name:      controlplane.Name,
					},
				},
			}
		}
	}

	return
}
