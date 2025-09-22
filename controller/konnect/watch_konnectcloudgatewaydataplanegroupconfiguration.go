package konnect

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	konnectv1alpha1 "github.com/kong/kong-operator/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/internal/utils/index"
)

// TODO(pmalek): this can be extracted and used in reconciler.go
// as every Konnect entity will have a reference to the KonnectAPIAuthConfiguration.
// This would require:
// - mapping function from non List types to List types
// - a function on each Konnect entity type to get the API Auth configuration
//   reference from the object
// - lists have their items stored in Items field, not returned via a method

// KonnectCloudGatewayDataPlaneGroupConfigurationReconciliationWatchOptions returns
// the watch options for the KonnectCloudGatewayDataPlaneGroupConfiguration.
func KonnectCloudGatewayDataPlaneGroupConfigurationReconciliationWatchOptions(
	cl client.Client,
) []func(*ctrl.Builder) *ctrl.Builder {
	return []func(*ctrl.Builder) *ctrl.Builder{
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.For(&konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration{})
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&konnectv1alpha2.KonnectGatewayControlPlane{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueObjectForKonnectGatewayControlPlane[konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfigurationList](
						cl, index.IndexFieldKonnectCloudGatewayDataPlaneGroupConfigurationOnKonnectGatewayControlPlane,
					),
				),
			)
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&konnectv1alpha1.KonnectCloudGatewayNetwork{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueKonnectCloudGatewayNetworkForKonnectNetwork(cl),
				),
			)
		},
	}
}

func enqueueKonnectCloudGatewayNetworkForKonnectNetwork(
	cl client.Client,
) func(ctx context.Context, obj client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		n, ok := obj.(*konnectv1alpha1.KonnectCloudGatewayNetwork)
		if !ok {
			return nil
		}
		var l konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfigurationList
		if err := cl.List(ctx, &l,
			// TODO: change this when cross namespace refs are allowed.
			client.InNamespace(n.GetNamespace()),
			client.MatchingFields{
				index.IndexFieldKonnectCloudGatewayDataPlaneGroupConfigurationOnKonnectNetworkRef: n.Name,
			},
		); err != nil {
			return nil
		}
		return objectListToReconcileRequests(l.Items)
	}
}
