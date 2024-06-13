package controlplane

import (
	"context"
	"reflect"

	admregv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	operatorv1beta1 "github.com/kong/gateway-operator/api/v1beta1"
	operatorerrors "github.com/kong/gateway-operator/internal/errors"
	"github.com/kong/gateway-operator/internal/utils/index"
	"github.com/kong/gateway-operator/pkg/consts"
)

// -----------------------------------------------------------------------------
// Reconciler - Watch Predicates
// -----------------------------------------------------------------------------

func (r *Reconciler) clusterRoleHasControlPlaneOwner(obj client.Object) bool {
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

	return r.ClusterScopedObjHasControlPlaneOwner(ctx, clusterRole)
}

func (r *Reconciler) clusterRoleBindingHasControlPlaneOwner(obj client.Object) bool {
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

	return r.ClusterScopedObjHasControlPlaneOwner(ctx, clusterRoleBinding)
}

func (r *Reconciler) validatingWebhookConfigurationHasControlPlaneOwner(obj client.Object) bool {
	ctx := context.Background()

	validatingWebhookConfig, ok := obj.(*admregv1.ValidatingWebhookConfiguration)
	if !ok {
		log.FromContext(ctx).Error(
			operatorerrors.ErrUnexpectedObject,
			"failed to run predicate function",
			"expected", "ValidatingWebhookConfiguration", "found", reflect.TypeOf(obj),
		)
		return false
	}

	return r.ClusterScopedObjHasControlPlaneOwner(ctx, validatingWebhookConfig)
}

// ClusterScopedObjHasControlPlaneOwner checks if the cluster-scoped object has a control plane owner.
// The check is performed through the managed-by-name label.
func (r *Reconciler) ClusterScopedObjHasControlPlaneOwner(ctx context.Context, obj client.Object) bool {
	controlplaneList := &operatorv1beta1.ControlPlaneList{}
	if err := r.Client.List(ctx, controlplaneList); err != nil {
		// filtering here is just an optimization. If we fail here it's most likely because of some failure
		// of the Kubernetes API and it's technically better to enqueue the object
		// than to drop it for eventual consistency during cluster outages.
		log.FromContext(ctx).Error(err, "could not list controlplanes in map func")
		return true
	}

	for _, controlplane := range controlplaneList.Items {
		// When resources are cluster-scoped, the owner is set through a label.
		if obj.GetLabels()[consts.GatewayOperatorManagedByNameLabel] == controlplane.Name {
			return true
		}
	}

	return false
}

// -----------------------------------------------------------------------------
// Reconciler - Watch Map Funcs
// -----------------------------------------------------------------------------

func (r *Reconciler) getControlPlaneForClusterRole(ctx context.Context, obj client.Object) (recs []reconcile.Request) {
	clusterRole, ok := obj.(*rbacv1.ClusterRole)
	if !ok {
		log.FromContext(ctx).Error(
			operatorerrors.ErrUnexpectedObject,
			"failed to run map funcs",
			"expected", "ClusterRole", "found", reflect.TypeOf(obj),
		)
		return
	}

	return r.getControlPlaneRequestFromManagedByNameLabel(ctx, clusterRole)
}

func (r *Reconciler) getControlPlaneForClusterRoleBinding(ctx context.Context, obj client.Object) (recs []reconcile.Request) {
	clusterRoleBinding, ok := obj.(*rbacv1.ClusterRoleBinding)
	if !ok {
		log.FromContext(ctx).Error(
			operatorerrors.ErrUnexpectedObject,
			"failed to run map funcs",
			"expected", "ClusterRoleBinding", "found", reflect.TypeOf(obj),
		)
		return
	}

	return r.getControlPlaneRequestFromManagedByNameLabel(ctx, clusterRoleBinding)
}

func (r *Reconciler) getControlPlaneForValidatingWebhookConfiguration(ctx context.Context, obj client.Object) []reconcile.Request {
	validatingWebhookConfig, ok := obj.(*admregv1.ValidatingWebhookConfiguration)
	if !ok {
		log.FromContext(ctx).Error(
			operatorerrors.ErrUnexpectedObject,
			"failed to run map funcs",
			"expected", "ValidatingWebhookConfiguration", "found", reflect.TypeOf(obj),
		)
		return nil
	}

	return r.getControlPlaneRequestFromManagedByNameLabel(ctx, validatingWebhookConfig)
}

func (r *Reconciler) getControlPlaneRequestFromManagedByNameLabel(ctx context.Context, obj client.Object) (recs []reconcile.Request) {
	controlplanes := &operatorv1beta1.ControlPlaneList{}
	if err := r.Client.List(ctx, controlplanes); err != nil {
		log.FromContext(ctx).Error(err, "could not list controlplanes in map func")
		return
	}

	for _, controlplane := range controlplanes.Items {
		// For cluster-scoped resources, the owner is set through a label.
		if obj.GetLabels()[consts.GatewayOperatorManagedByNameLabel] == controlplane.Name {
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

func (r *Reconciler) getControlPlanesFromDataPlaneDeployment(ctx context.Context, obj client.Object) (recs []reconcile.Request) {
	deployment, ok := obj.(*appsv1.Deployment)
	if !ok {
		log.FromContext(ctx).Error(
			operatorerrors.ErrUnexpectedObject,
			"failed to map ControlPlane on DataPlane Deployment",
			"expected", "Deployment", "found", reflect.TypeOf(obj),
		)
		return
	}

	var dataPlaneOwnerName string
	for _, ownRef := range deployment.OwnerReferences {
		if ownRef.APIVersion == operatorv1beta1.SchemeGroupVersion.String() && ownRef.Kind == "DataPlane" {
			dataPlaneOwnerName = ownRef.Name
			break
		}
	}
	if dataPlaneOwnerName == "" {
		return
	}

	dataPlane := &operatorv1beta1.DataPlane{}
	if err := r.Client.Get(ctx, types.NamespacedName{Namespace: deployment.Namespace, Name: dataPlaneOwnerName}, dataPlane); err != nil {
		if !k8serrors.IsNotFound(err) {
			log.FromContext(ctx).Error(err, "failed to map ControlPlane on DataPlane Deployment")
		}
		return
	}
	return r.getControlPlanesFromDataPlane(ctx, dataPlane)
}

func (r *Reconciler) getControlPlanesFromDataPlane(ctx context.Context, obj client.Object) (recs []reconcile.Request) {
	dataplane, ok := obj.(*operatorv1beta1.DataPlane)
	if !ok {
		log.FromContext(ctx).Error(
			operatorerrors.ErrUnexpectedObject,
			"failed to map ControlPlane on DataPlane",
			"expected", "DataPlane", "found", reflect.TypeOf(obj),
		)
		return
	}

	controlPlaneList := &operatorv1beta1.ControlPlaneList{}
	if err := r.Client.List(ctx, controlPlaneList,
		client.InNamespace(dataplane.Namespace),
		client.MatchingFields{
			index.DataPlaneNameIndex: dataplane.Name,
		}); err != nil {
		log.FromContext(ctx).Error(err, "failed to map ControlPlane on DataPlane")
		return
	}

	recs = make([]reconcile.Request, 0, len(controlPlaneList.Items))
	for _, cp := range controlPlaneList.Items {
		recs = append(recs, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: cp.Namespace,
				Name:      cp.Name,
			},
		})
	}
	return recs
}
