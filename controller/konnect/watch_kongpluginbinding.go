package konnect

import (
	"context"
	"fmt"
	"reflect"

	operatorerrors "github.com/kong/gateway-operator/internal/errors"
	"github.com/kong/gateway-operator/modules/manager/logging"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// -----------------------------------------------------------------------------
// KongPluginBinding reconciler - Watch Builder
// -----------------------------------------------------------------------------

// TODO(pmalek): this can be extracted and used in reconciler.go
// as every Konnect entity will have a reference to the KonnectAPIAuthConfiguration.
// This would require:
// - mapping function from non List types to List types
// - a function on each Konnect entity type to get the API Auth configuration
//   reference from the object
// - lists have their items stored in Items field, not returned via a method

// KongPluginBindingReconciliationWatchOptions returns the watch options for
// the KongPluginBinding.
func KongPluginBindingReconciliationWatchOptions(
	cl client.Client,
) []func(*ctrl.Builder) *ctrl.Builder {
	return []func(*ctrl.Builder) *ctrl.Builder{
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.For(&configurationv1alpha1.KongPluginBinding{},
				builder.WithPredicates(
					predicate.NewPredicateFuncs(kongPluginBindingRefersToKonnectControlPlane),
				),
			)
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&konnectv1alpha1.KonnectAPIAuthConfiguration{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueKongPluginBindingForKonnectAPIAuthConfiguration(cl),
				),
			)
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&konnectv1alpha1.KonnectControlPlane{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueKongPluginBindingForKonnectControlPlane(cl),
				),
			)
		},

		// TODO(mlavacca): add KongPlugin watch
		// TODO(mlavacca): add KongClusterPlugin watch
		// TODO(mlavacca): add KongService watch
		// TODO(mlavacca): add KongConsumer watch
		// TODO(mlavacca): add KongRoute watch
	}
}

// -----------------------------------------------------------------------------
// KongPluginBinding reconciler - Watch Predicates
// -----------------------------------------------------------------------------

// kongPluginBindingRefersToKonnectControlPlane returns true if the KongPluginBinding
// refers to a KonnectControlPlane.
func kongPluginBindingRefersToKonnectControlPlane(obj client.Object) bool {
	kongPB, ok := obj.(*configurationv1alpha1.KongPluginBinding)
	if !ok {
		ctrllog.FromContext(context.Background()).Error(
			operatorerrors.ErrUnexpectedObject,
			"failed to run predicate function",
			"expected", "KongPluginBinding", "found", reflect.TypeOf(obj),
		)
		return false
	}

	cpRef := kongPB.Spec.ControlPlaneRef
	return cpRef != nil && cpRef.Type == configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef
}

// -----------------------------------------------------------------------------
// KongPluginBinding reconciler - Watch Mappers
// -----------------------------------------------------------------------------

func enqueueKongPluginBindingForKonnectAPIAuthConfiguration(
	cl client.Client,
) func(ctx context.Context, obj client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		auth, ok := obj.(*konnectv1alpha1.KonnectAPIAuthConfiguration)
		if !ok {
			return nil
		}
		var l configurationv1alpha1.KongPluginBindingList
		if err := cl.List(ctx, &l, &client.ListOptions{
			// TODO: change this when cross namespace refs are allowed.
			Namespace: auth.GetNamespace(),
		}); err != nil {
			return nil
		}

		var ret []reconcile.Request
		for _, pb := range l.Items {
			cpRef, ok := getControlPlaneRef(&pb).Get()
			if !ok {
				continue
			}

			switch cpRef.Type {
			case configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef:
				nn := types.NamespacedName{
					Name:      cpRef.KonnectNamespacedRef.Name,
					Namespace: pb.Namespace,
				}
				// TODO: change this when cross namespace refs are allowed.
				if nn.Namespace != auth.Namespace {
					continue
				}
				var cp konnectv1alpha1.KonnectControlPlane
				if err := cl.Get(ctx, nn, &cp); err != nil {
					ctrllog.FromContext(ctx).Error(
						err,
						"failed to get KonnectControlPlane",
						"KonnectControlPlane", nn,
					)
					continue
				}

				// TODO: change this when cross namespace refs are allowed.
				if cp.GetKonnectAPIAuthConfigurationRef().Name != auth.Name {
					continue
				}

				ret = append(ret, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: pb.Namespace,
						Name:      pb.Name,
					},
				})

			case configurationv1alpha1.ControlPlaneRefKonnectID:
				ctrllog.FromContext(ctx).Error(
					fmt.Errorf("unimplemented ControlPlaneRef type %q", cpRef.Type),
					"unimplemented ControlPlaneRef for KongPluginBinding",
					"KongPluginBinding", pb, "refType", cpRef.Type,
				)
				continue

			default:
				ctrllog.FromContext(ctx).V(logging.DebugLevel.Value()).Info(
					"unsupported ControlPlaneRef for KongPluginBinding",
					"KongPluginBinding", pb, "refType", cpRef.Type,
				)
				continue
			}
		}
		return ret
	}
}

func enqueueKongPluginBindingForKonnectControlPlane(
	cl client.Client,
) func(ctx context.Context, obj client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		cp, ok := obj.(*konnectv1alpha1.KonnectControlPlane)
		if !ok {
			return nil
		}
		var l configurationv1alpha1.KongPluginBindingList
		if err := cl.List(ctx, &l, &client.ListOptions{
			// TODO: change this when cross namespace refs are allowed.
			Namespace: cp.GetNamespace(),
		}); err != nil {
			return nil
		}

		var ret []reconcile.Request
		for _, pb := range l.Items {
			switch pb.Spec.ControlPlaneRef.Type {
			case configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef:
				// TODO: change this when cross namespace refs are allowed.
				if pb.Spec.ControlPlaneRef.KonnectNamespacedRef.Name != cp.Name {
					continue
				}

				ret = append(ret, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: pb.Namespace,
						Name:      pb.Name,
					},
				})

			case configurationv1alpha1.ControlPlaneRefKonnectID:
				ctrllog.FromContext(ctx).Error(
					fmt.Errorf("unimplemented ControlPlaneRef type %q", pb.Spec.ControlPlaneRef.Type),
					"unimplemented ControlPlaneRef for KongPluginBinding",
					"KongPluginBinding", pb, "refType", pb.Spec.ControlPlaneRef.Type,
				)
				continue

			default:
				ctrllog.FromContext(ctx).V(logging.DebugLevel.Value()).Info(
					"unsupported ControlPlaneRef for KongPluginBinding",
					"KongPluginBinding", pb, "refType", pb.Spec.ControlPlaneRef.Type,
				)
				continue
			}
		}
		return ret
	}
}
