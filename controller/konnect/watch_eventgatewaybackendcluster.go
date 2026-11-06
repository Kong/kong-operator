package konnect

import (
	"cmp"
	"context"

	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
)

// eventGatewayBackendClusterReconciliationWatchOptions are the generated watch
// options of EventGatewayBackendCluster, and a watch of the listener policies:
// Konnect refuses to delete a backend cluster while listener policies forward
// to one of its virtual clusters, so the backend cluster is reconciled again
// once they stop, rather than on the fixed period of a blocked deletion.
func eventGatewayBackendClusterReconciliationWatchOptions(cl client.Client) []func(*ctrl.Builder) *ctrl.Builder {
	return append(EventGatewayBackendClusterReconciliationWatchOptions(cl),
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&configurationv1alpha1.EventGatewayListenerPolicy{},
				handler.EnqueueRequestsFromMapFunc(enqueueEventGatewayBackendClusterForEventGatewayListenerPolicy(cl)),
				builder.WithPredicates(referrerChanged()),
			)
		},
	)
}

// enqueueEventGatewayBackendClusterForEventGatewayListenerPolicy enqueues the
// backend clusters of the virtual clusters a listener policy forwards to.
func enqueueEventGatewayBackendClusterForEventGatewayListenerPolicy(
	cl client.Client,
) func(ctx context.Context, obj client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		policy, ok := obj.(*configurationv1alpha1.EventGatewayListenerPolicy)
		if !ok {
			return nil
		}
		var reqs []reconcile.Request
		for _, key := range configurationv1alpha1.EventGatewayListenerPolicyRefsToEventGatewayVirtualCluster(policy) {
			var vc configurationv1alpha1.EventGatewayVirtualCluster
			if err := cl.Get(ctx, key, &vc); err != nil {
				// A virtual cluster that doesn't exist has no backend cluster to
				// unblock.
				continue
			}
			ref := vc.Spec.EventGatewayBackendClusterRef.NamespacedRef
			if ref == nil {
				continue
			}
			reqs = append(reqs, reconcile.Request{
				Namespace: cmp.Or(ptr.Deref(ref.Namespace, ""), vc.Namespace),
				Name:      ref.Name})
		}
		return reqs
	}
}
