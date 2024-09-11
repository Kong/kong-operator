package konnect

import (
	"context"
	"fmt"
	"reflect"

	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	operatorerrors "github.com/kong/gateway-operator/internal/errors"
	"github.com/kong/gateway-operator/modules/manager/logging"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kubernetes-configuration/api/configuration/v1beta1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
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
					predicate.NewPredicateFuncs(kongPluginBindingRefersToKonnectGatewayControlPlane),
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
				&konnectv1alpha1.KonnectGatewayControlPlane{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueKongPluginBindingForKonnectGatewayControlPlane(cl),
				),
			)
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&configurationv1.KongPlugin{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueKongPluginBindingForKongPlugin(cl),
				),
			)
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&configurationv1.KongClusterPlugin{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueKongPluginBindingForKongClusterPlugin(cl),
				),
			)
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&configurationv1alpha1.KongService{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueKongPluginBindingForKongService(cl),
				),
			)
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&configurationv1alpha1.KongRoute{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueKongPluginBindingForKongRoute(cl),
				),
			)
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&configurationv1.KongConsumer{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueKongPluginBindingForKongConsumer(cl),
				),
			)
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&configurationv1beta1.KongConsumerGroup{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueKongPluginBindingForKongConsumerGroup(cl),
				),
			)
		},
	}
}

// -----------------------------------------------------------------------------
// KongPluginBinding reconciler - Watch Predicates
// -----------------------------------------------------------------------------

// kongPluginBindingRefersToKonnectGatewayControlPlane returns true if the KongPluginBinding
// refers to a KonnectGatewayControlPlane.
func kongPluginBindingRefersToKonnectGatewayControlPlane(obj client.Object) bool {
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

func enqueueKongPluginBindingForKonnectGatewayControlPlane(
	cl client.Client,
) func(ctx context.Context, obj client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		cp, ok := obj.(*konnectv1alpha1.KonnectGatewayControlPlane)
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

func enqueueKongPluginBindingForKongPlugin(cl client.Client) func(
	ctx context.Context, obj client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		plugin, ok := obj.(*configurationv1.KongPlugin)
		if !ok {
			return nil
		}

		pluginBindingList := configurationv1alpha1.KongPluginBindingList{}
		err := cl.List(ctx, &pluginBindingList,
			// Currently KongPlugin and KongPluginBinding must be in the same namespace to reference the plugin.
			client.InNamespace(plugin.Namespace),
			client.MatchingFields{
				IndexFieldKongPluginBindingKongPluginReference: plugin.Namespace + "/" + plugin.Name,
			},
		)
		if err != nil {
			ctrllog.FromContext(ctx).Error(err, "failed to list KongPluginBindings referencing KongPlugin")
			return nil
		}

		return lo.FilterMap(pluginBindingList.Items, func(pb configurationv1alpha1.KongPluginBinding, _ int) (reconcile.Request, bool) {
			// Only put KongPluginBindings referencing to a Konnect control plane,
			if pb.Spec.ControlPlaneRef == nil || pb.Spec.ControlPlaneRef.Type != configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef {
				return reconcile.Request{}, false
			}
			return reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: pb.Namespace,
					Name:      pb.Name,
				},
			}, true
		})
	}
}

func enqueueKongPluginBindingForKongClusterPlugin(cl client.Client) func(
	ctx context.Context, obj client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		plugin, ok := obj.(*configurationv1.KongClusterPlugin)
		if !ok {
			return nil
		}

		pluginBindingList := configurationv1alpha1.KongPluginBindingList{}
		err := cl.List(ctx, &pluginBindingList,
			client.MatchingFields{
				IndexFieldKongPluginBindingKongClusterPluginReference: plugin.Name,
			},
		)
		if err != nil {
			ctrllog.FromContext(ctx).Error(err, "failed to list KongPluginBindings referencing KongClusterPlugin")
		}

		return lo.FilterMap(pluginBindingList.Items, func(pb configurationv1alpha1.KongPluginBinding, _ int) (reconcile.Request, bool) {
			// Only put KongPluginBindings referencing to a Konnect control plane,
			if pb.Spec.ControlPlaneRef == nil || pb.Spec.ControlPlaneRef.Type != configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef {
				return reconcile.Request{}, false
			}
			return reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: pb.Namespace,
					Name:      pb.Name,
				},
			}, true
		})
	}
}

func enqueueKongPluginBindingForKongService(cl client.Client) func(
	ctx context.Context, obj client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		kongService, ok := obj.(*configurationv1alpha1.KongService)
		if !ok {
			return nil
		}

		pluginBindingList := configurationv1alpha1.KongPluginBindingList{}
		err := cl.List(ctx, &pluginBindingList,
			client.InNamespace(kongService.Namespace),
			client.MatchingFields{
				IndexFieldKongPluginBindingKongServiceReference: kongService.Name,
			},
		)
		if err != nil {
			ctrllog.FromContext(ctx).Error(err, "failed to list KongPluginBindings referencing KongServices")
		}

		return lo.FilterMap(pluginBindingList.Items, func(pb configurationv1alpha1.KongPluginBinding, _ int) (reconcile.Request, bool) {
			// Only put KongPluginBindings referencing to a Konnect control plane,
			if pb.Spec.ControlPlaneRef == nil || pb.Spec.ControlPlaneRef.Type != configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef {
				return reconcile.Request{}, false
			}
			return reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: pb.Namespace,
					Name:      pb.Name,
				},
			}, true
		})
	}
}

func enqueueKongPluginBindingForKongRoute(cl client.Client) func(
	ctx context.Context, obj client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		kongRoute, ok := obj.(*configurationv1alpha1.KongRoute)
		if !ok {
			return nil
		}

		pluginBindingList := configurationv1alpha1.KongPluginBindingList{}
		err := cl.List(ctx, &pluginBindingList,
			client.InNamespace(kongRoute.Namespace),
			client.MatchingFields{
				IndexFieldKongPluginBindingKongRouteReference: kongRoute.Name,
			},
		)
		if err != nil {
			ctrllog.FromContext(ctx).Error(err, "failed to list KongPluginBindings referencing KongRoutes")
		}

		return lo.FilterMap(pluginBindingList.Items, func(pb configurationv1alpha1.KongPluginBinding, _ int) (reconcile.Request, bool) {
			// Only put KongPluginBindings referencing to a Konnect control plane,
			if pb.Spec.ControlPlaneRef == nil || pb.Spec.ControlPlaneRef.Type != configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef {
				return reconcile.Request{}, false
			}
			return reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: pb.Namespace,
					Name:      pb.Name,
				},
			}, true
		})
	}
}

func enqueueKongPluginBindingForKongConsumer(cl client.Client) func(
	ctx context.Context, obj client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		kongConsumer, ok := obj.(*configurationv1.KongConsumer)
		if !ok {
			return nil
		}

		pluginBindingList := configurationv1alpha1.KongPluginBindingList{}
		err := cl.List(ctx, &pluginBindingList,
			client.InNamespace(kongConsumer.Namespace),
			client.MatchingFields{
				IndexFieldKongPluginBindingKongConsumerReference: kongConsumer.Name,
			},
		)
		if err != nil {
			ctrllog.FromContext(ctx).Error(err, "failed to list KongPluginBindings referencing KongConsumers")
		}

		return lo.FilterMap(pluginBindingList.Items, func(pb configurationv1alpha1.KongPluginBinding, _ int) (reconcile.Request, bool) {
			// Only put KongPluginBindings referencing to a Konnect control plane,
			if pb.Spec.ControlPlaneRef == nil || pb.Spec.ControlPlaneRef.Type != configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef {
				return reconcile.Request{}, false
			}
			return reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: pb.Namespace,
					Name:      pb.Name,
				},
			}, true
		})
	}
}

func enqueueKongPluginBindingForKongConsumerGroup(cl client.Client) func(
	ctx context.Context, obj client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		kongConsumerGroup, ok := obj.(*configurationv1beta1.KongConsumerGroup)
		if !ok {
			return nil
		}

		pluginBindingList := configurationv1alpha1.KongPluginBindingList{}
		err := cl.List(ctx, &pluginBindingList,
			client.InNamespace(kongConsumerGroup.Namespace),
			client.MatchingFields{
				IndexFieldKongPluginBindingKongConsumerGroupReference: kongConsumerGroup.Name,
			},
		)
		if err != nil {
			ctrllog.FromContext(ctx).Error(err, "failed to list KongPluginBindings referencing KongConsumerGroups")
		}

		return lo.FilterMap(pluginBindingList.Items, func(pb configurationv1alpha1.KongPluginBinding, _ int) (reconcile.Request, bool) {
			// Only put KongPluginBindings referencing to a Konnect control plane,
			if pb.Spec.ControlPlaneRef == nil || pb.Spec.ControlPlaneRef.Type != configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef {
				return reconcile.Request{}, false
			}
			return reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: pb.Namespace,
					Name:      pb.Name,
				},
			}, true
		})
	}
}
