package konnect

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kong/kong-operator/internal/utils/index"

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha1"
)

// KonnectCloudGatewayTransitGatewayWatchOptions returns the watch options for KonnectCloudGatewayTransitGateway controller.
func KonnectCloudGatewayTransitGatewayWatchOptions(cl client.Client) []func(*ctrl.Builder) *ctrl.Builder {
	return []func(*ctrl.Builder) *ctrl.Builder{
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.For(&konnectv1alpha1.KonnectCloudGatewayTransitGateway{})
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&konnectv1alpha1.KonnectCloudGatewayNetwork{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueTransitGatewayForKonnectNetwork(cl),
				),
			)
		},
	}
}

func enqueueTransitGatewayForKonnectNetwork(cl client.Client) func(ctx context.Context, obj client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		n, ok := obj.(*konnectv1alpha1.KonnectCloudGatewayNetwork)
		if !ok {
			return nil
		}
		var l konnectv1alpha1.KonnectCloudGatewayTransitGatewayList
		if err := cl.List(ctx, &l,
			// TODO: change this when cross namespace refs are allowed.
			client.InNamespace(n.GetNamespace()),
			client.MatchingFields{
				index.IndexFieldKonnectCloudGatewayTransitGatewayOnKonnectNetworkRef: n.Name,
			},
		); err != nil {
			return nil
		}
		return objectListToReconcileRequests(l.Items)
	}
}
