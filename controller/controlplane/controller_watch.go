package controlplane

import (
	"context"
	"reflect"

	"github.com/samber/lo"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	operatorerrors "github.com/kong/kong-operator/v2/internal/errors"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/internal/utils/index"
)

func (r *Reconciler) listControlPlanesForWatchNamespaceGrants(
	ctx context.Context,
	obj client.Object,
) []reconcile.Request {
	rg, ok := obj.(*operatorv1alpha1.WatchNamespaceGrant)
	if !ok {
		ctrllog.FromContext(ctx).Error(
			operatorerrors.ErrUnexpectedObject,
			"failed to map WatchNamespaceGrant on ControlPlane",
			"expected", "WatchNamespaceGrant", "found", reflect.TypeOf(obj),
		)
		return nil
	}

	fromsForControlPlane := lo.Filter(rg.Spec.From,
		func(from operatorv1alpha1.WatchNamespaceGrantFrom, _ int) bool {
			return from.Group == gwtypes.ControlPlaneGVR().Group &&
				from.Kind == "ControlPlane"
		},
	)

	var recs []reconcile.Request
	for _, from := range fromsForControlPlane {
		var controlPlaneList gwtypes.ControlPlaneList
		if err := r.List(ctx, &controlPlaneList,
			client.InNamespace(from.Namespace),
		); err != nil {
			ctrllog.FromContext(ctx).Error(err, "failed to map WatchNamespaceGrant to ControlPlane")
			return nil
		}
		for _, cp := range controlPlaneList.Items {
			recs = append(recs, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: cp.Namespace,
					Name:      cp.Name,
				},
			})
		}
	}

	return recs
}

func (r *Reconciler) getControlPlanesFromDataPlaneDeployment(ctx context.Context, obj client.Object) []reconcile.Request {
	deployment, ok := obj.(*appsv1.Deployment)
	if !ok {
		ctrllog.FromContext(ctx).Error(
			operatorerrors.ErrUnexpectedObject,
			"failed to map ControlPlane on DataPlane Deployment",
			"expected", "Deployment", "found", reflect.TypeOf(obj),
		)
		return nil
	}

	var dataPlaneOwnerName string
	for _, ownRef := range deployment.OwnerReferences {
		if ownRef.APIVersion == operatorv1beta1.DataPlaneGVR().GroupVersion().String() && ownRef.Kind == "DataPlane" {
			dataPlaneOwnerName = ownRef.Name
			break
		}
	}
	if dataPlaneOwnerName == "" {
		return nil
	}

	var (
		dataPlane operatorv1beta1.DataPlane
		nn        = types.NamespacedName{
			Namespace: deployment.Namespace,
			Name:      dataPlaneOwnerName,
		}
	)
	if err := r.Get(ctx, nn, &dataPlane); err != nil {
		if !apierrors.IsNotFound(err) {
			ctrllog.FromContext(ctx).Error(err, "failed to map ControlPlane on DataPlane Deployment")
		}
		return nil
	}
	return r.getControlPlanesFromDataPlane(ctx, &dataPlane)
}

func (r *Reconciler) getControlPlanesFromDataPlane(ctx context.Context, obj client.Object) []reconcile.Request {
	dataplane, ok := obj.(*operatorv1beta1.DataPlane)
	if !ok {
		ctrllog.FromContext(ctx).Error(
			operatorerrors.ErrUnexpectedObject,
			"failed to map ControlPlane on DataPlane",
			"expected", "DataPlane", "found", reflect.TypeOf(obj),
		)
		return nil
	}

	var controlPlaneList gwtypes.ControlPlaneList
	if err := r.List(ctx, &controlPlaneList,
		client.InNamespace(dataplane.Namespace),
		client.MatchingFields{
			index.DataPlaneNameIndex: dataplane.Name,
		}); err != nil {
		ctrllog.FromContext(ctx).Error(err, "failed to map ControlPlane on DataPlane")
		return nil
	}

	recs := make([]reconcile.Request, 0, len(controlPlaneList.Items))
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
