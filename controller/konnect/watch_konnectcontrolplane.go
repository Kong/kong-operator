package konnect

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	konnectv1alpha1 "github.com/kong/kong-operator/apis/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/apis/v1alpha2"
	"github.com/kong/kong-operator/internal/utils/index"
)

// TODO(pmalek): this can be extracted and used in reconciler.go
// as every Konnect entity will have a reference to the KonnectAPIAuthConfiguration.
// This would require:
// - mapping function from non List types to List types
// - a function on each Konnect entity type to get the API Auth configuration
//   reference from the object
// - lists have their items stored in Items field, not returned via a method

// KonnectGatewayControlPlaneReconciliationWatchOptions returns the watch options for
// the KonnectGatewayControlPlane.
func KonnectGatewayControlPlaneReconciliationWatchOptions(
	cl client.Client,
) []func(*ctrl.Builder) *ctrl.Builder {
	return []func(*ctrl.Builder) *ctrl.Builder{
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.For(&konnectv1alpha2.KonnectGatewayControlPlane{})
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&konnectv1alpha1.KonnectAPIAuthConfiguration{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueObjectsForKonnectAPIAuthConfiguration[konnectv1alpha2.KonnectGatewayControlPlaneList](
						cl,
						index.IndexFieldKonnectGatewayControlPlaneOnAPIAuthConfiguration,
					),
				),
			)
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&konnectv1alpha2.KonnectGatewayControlPlane{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueKonnectGatewayControlPlaneGroupForMembers(cl),
				),
			)
		},
	}
}

func enqueueKonnectGatewayControlPlaneGroupForMembers(
	cl client.Client,
) func(ctx context.Context, obj client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		cp, ok := obj.(*konnectv1alpha2.KonnectGatewayControlPlane)
		if !ok {
			return nil
		}
		var l konnectv1alpha2.KonnectGatewayControlPlaneList
		if err := cl.List(ctx, &l,
			// TODO: change this when cross namespace refs are allowed.
			client.InNamespace(cp.GetNamespace()),
			client.MatchingFields{
				// List groups that this control plane is a member of.
				index.IndexFieldKonnectGatewayControlPlaneGroupOnMembers: cp.Name,
			},
		); err != nil {
			return nil
		}

		return objectListToReconcileRequests(l.Items)
	}
}
