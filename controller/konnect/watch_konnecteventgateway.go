package konnect

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/index"
)

// KonnectEventGatewayReconciliationWatchOptions returns the watch options for KonnectEventGateway.
func KonnectEventGatewayReconciliationWatchOptions(
	cl client.Client,
) []func(*ctrl.Builder) *ctrl.Builder {
	return []func(*ctrl.Builder) *ctrl.Builder{
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.For(&konnectv1alpha1.KonnectEventGateway{})
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&konnectv1alpha1.KonnectAPIAuthConfiguration{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueKonnectEventGatewayForKonnectAPIAuthConfiguration(cl),
				),
			)
		},
	}
}

func enqueueKonnectEventGatewayForKonnectAPIAuthConfiguration(
	cl client.Client,
) func(ctx context.Context, obj client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		auth, ok := obj.(*konnectv1alpha1.KonnectAPIAuthConfiguration)
		if !ok {
			return nil
		}
		var l konnectv1alpha1.KonnectEventGatewayList
		if err := cl.List(ctx, &l,
			// TODO: change this when cross namespace refs are allowed.
			client.InNamespace(auth.GetNamespace()),
			client.MatchingFields{
				index.IndexFieldKonnectEventGatewayOnAPIAuthConfiguration: auth.Name,
			},
		); err != nil {
			return nil
		}
		return objectListToReconcileRequests(l.Items)
	}
}
