package konnect

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

// TODO(pmalek): this can be extracted and used in reconciler.go
// as every Konnect entity will have a reference to the KonnectAPIAuthConfiguration.
// This would require:
// - mapping function from non List types to List types
// - a function on each Konnect entity type to get the API Auth configuration
//   reference from the object
// - lists have their items stored in Items field, not returned via a method

// KonnectControlPlaneReconciliationWatchOptions returns the watch options for
// the KonnectControlPlane.
func KonnectControlPlaneReconciliationWatchOptions(
	cl client.Client,
) []func(*ctrl.Builder) *ctrl.Builder {
	return []func(*ctrl.Builder) *ctrl.Builder{
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&konnectv1alpha1.KonnectAPIAuthConfiguration{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueKonnectControlPlaneForKonnectAPIAuthConfiguration(cl),
				),
			)
		},
	}
}

func enqueueKonnectControlPlaneForKonnectAPIAuthConfiguration(
	cl client.Client,
) func(ctx context.Context, obj client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		auth, ok := obj.(*konnectv1alpha1.KonnectAPIAuthConfiguration)
		if !ok {
			return nil
		}
		var l konnectv1alpha1.KonnectControlPlaneList
		if err := cl.List(ctx, &l, &client.ListOptions{
			// TODO: change this when cross namespace refs are allowed.
			Namespace: auth.GetNamespace(),
		}); err != nil {
			return nil
		}
		var ret []reconcile.Request
		for _, cp := range l.Items {
			authRef := cp.GetKonnectAPIAuthConfigurationRef()
			if authRef.Name != auth.Name {
				// TODO: change this when cross namespace refs are allowed.
				// authRef.Namespace != auth.Namespace {
				continue
			}
			ret = append(ret, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: cp.Namespace,
					Name:      cp.Name,
				},
			})
		}
		return ret
	}
}
