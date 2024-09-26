package konnect

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kong/gateway-operator/modules/manager/logging"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kubernetes-configuration/api/configuration/v1beta1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

// TODO(pmalek): this can be extracted and used in reconciler.go
// as every Konnect entity will have a reference to the KonnectAPIAuthConfiguration.
// This would require:
// - mapping function from non List types to List types
// - a function on each Konnect entity type to get the API Auth configuration
//   reference from the object
// - lists have their items stored in Items field, not returned via a method

// KongConsumerGroupReconciliationWatchOptions returns the watch options for
// the KongConsumerGroup.
func KongConsumerGroupReconciliationWatchOptions(
	cl client.Client,
) []func(*ctrl.Builder) *ctrl.Builder {
	return []func(*ctrl.Builder) *ctrl.Builder{
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.For(&configurationv1beta1.KongConsumerGroup{},
				builder.WithPredicates(
					predicate.NewPredicateFuncs(objRefersToKonnectGatewayControlPlane[configurationv1beta1.KongConsumerGroup]),
				),
			)
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&konnectv1alpha1.KonnectAPIAuthConfiguration{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueKongConsumerGroupForKonnectAPIAuthConfiguration(cl),
				),
			)
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&konnectv1alpha1.KonnectGatewayControlPlane{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueKongConsumerGroupForKonnectGatewayControlPlane(cl),
				),
			)
		},
	}
}

func enqueueKongConsumerGroupForKonnectAPIAuthConfiguration(
	cl client.Client,
) func(ctx context.Context, obj client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		auth, ok := obj.(*konnectv1alpha1.KonnectAPIAuthConfiguration)
		if !ok {
			return nil
		}
		var l configurationv1beta1.KongConsumerGroupList
		if err := cl.List(ctx, &l, &client.ListOptions{
			// TODO: change this when cross namespace refs are allowed.
			Namespace: auth.GetNamespace(),
		}); err != nil {
			return nil
		}

		var ret []reconcile.Request
		for _, group := range l.Items {
			cpRef, ok := getControlPlaneRef(&group).Get()
			if !ok {
				continue
			}

			switch cpRef.Type {
			case configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef:
				nn := types.NamespacedName{
					Name:      cpRef.KonnectNamespacedRef.Name,
					Namespace: group.Namespace,
				}
				// TODO: change this when cross namespace refs are allowed.
				if nn.Namespace != auth.Namespace {
					continue
				}
				var cp konnectv1alpha1.KonnectGatewayControlPlane
				if err := cl.Get(ctx, nn, &cp); err != nil {
					ctrllog.FromContext(ctx).Error(
						err,
						"failed to get KonnectGatewayControlPlane",
						"KonnectGatewayControlPlane", nn,
					)
					continue
				}

				// TODO: change this when cross namespace refs are allowed.
				if cp.GetKonnectAPIAuthConfigurationRef().Name != auth.Name {
					continue
				}

				ret = append(ret, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: group.Namespace,
						Name:      group.Name,
					},
				})

			case configurationv1alpha1.ControlPlaneRefKonnectID:
				ctrllog.FromContext(ctx).Error(
					fmt.Errorf("unimplemented ControlPlaneRef type %q", cpRef.Type),
					"unimplemented ControlPlaneRef for KongConsumerGroup",
					"KongConsumerGroup", group, "refType", cpRef.Type,
				)
				continue

			default:
				ctrllog.FromContext(ctx).V(logging.DebugLevel.Value()).Info(
					"unsupported ControlPlaneRef for KongConsumerGroup",
					"KongConsumerGroup", group, "refType", cpRef.Type,
				)
				continue
			}
		}
		return ret
	}
}

func enqueueKongConsumerGroupForKonnectGatewayControlPlane(
	cl client.Client,
) func(ctx context.Context, obj client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		cp, ok := obj.(*konnectv1alpha1.KonnectGatewayControlPlane)
		if !ok {
			return nil
		}
		var l configurationv1beta1.KongConsumerGroupList
		if err := cl.List(ctx, &l, &client.ListOptions{
			// TODO: change this when cross namespace refs are allowed.
			Namespace: cp.GetNamespace(),
		}); err != nil {
			return nil
		}

		var ret []reconcile.Request
		for _, group := range l.Items {
			cpRef, ok := getControlPlaneRef(&group).Get()
			if !ok {
				continue
			}
			switch cpRef.Type {
			case configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef:
				// TODO: change this when cross namespace refs are allowed.
				if cpRef.KonnectNamespacedRef.Name != cp.Name {
					continue
				}

				ret = append(ret, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: group.Namespace,
						Name:      group.Name,
					},
				})

			case configurationv1alpha1.ControlPlaneRefKonnectID:
				ctrllog.FromContext(ctx).Error(
					fmt.Errorf("unimplemented ControlPlaneRef type %q", cpRef.Type),
					"unimplemented ControlPlaneRef for KongConsumerGroup",
					"KongConsumerGroup", group, "refType", cpRef.Type,
				)
				continue

			default:
				ctrllog.FromContext(ctx).V(logging.DebugLevel.Value()).Info(
					"unsupported ControlPlaneRef for KongConsumerGroup",
					"KongConsumerGroup", group, "refType", cpRef.Type,
				)
				continue
			}
		}
		return ret
	}
}
