package controlplane

import (
	"context"
	"reflect"

	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	operatorv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1alpha1"
	operatorv2alpha1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v2alpha1"

	operatorerrors "github.com/kong/kong-operator/internal/errors"
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
			return from.Group == operatorv2alpha1.ControlPlaneGVR().Group &&
				from.Kind == "ControlPlane"
		},
	)

	var recs []reconcile.Request
	for _, from := range fromsForControlPlane {
		var controlPlaneList operatorv2alpha1.ControlPlaneList
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
