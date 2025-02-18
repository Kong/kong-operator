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

	commonv1alpha1 "github.com/kong/kubernetes-configuration/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

// KongKeyReconciliationWatchOptions returns the watch options for the KongKey.
func KongKeyReconciliationWatchOptions(cl client.Client) []func(*ctrl.Builder) *ctrl.Builder {
	return []func(*ctrl.Builder) *ctrl.Builder{
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.For(&configurationv1alpha1.KongKey{},
				builder.WithPredicates(
					predicate.NewPredicateFuncs(objRefersToKonnectGatewayControlPlane[configurationv1alpha1.KongKey]),
				),
			)
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&configurationv1alpha1.KongKeySet{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueKongKeyForKongKeySet(cl),
				),
			)
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&konnectv1alpha1.KonnectAPIAuthConfiguration{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueKongKeyForKonnectAPIAuthConfiguration(cl),
				),
			)
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&konnectv1alpha1.KonnectGatewayControlPlane{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueObjectForKonnectGatewayControlPlane[configurationv1alpha1.KongKeyList](
						cl, IndexFieldKongKeyOnKonnectGatewayControlPlane,
					),
				),
			)
		},
	}
}

func enqueueKongKeyForKonnectAPIAuthConfiguration(cl client.Client) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		auth, ok := obj.(*konnectv1alpha1.KonnectAPIAuthConfiguration)
		if !ok {
			return nil
		}
		var l configurationv1alpha1.KongKeyList
		if err := cl.List(ctx, &l, &client.ListOptions{
			// TODO: change this when cross namespace refs are allowed.
			Namespace: auth.GetNamespace(),
		}); err != nil {
			return nil
		}

		var ret []reconcile.Request
		for _, key := range l.Items {
			cpRef, ok := getControlPlaneRef(&key).Get()
			if !ok {
				continue
			}

			switch cpRef.Type {
			case commonv1alpha1.ControlPlaneRefKonnectNamespacedRef:
				nn := types.NamespacedName{
					Name:      cpRef.KonnectNamespacedRef.Name,
					Namespace: key.Namespace,
				}
				// TODO: change this when cross namespace refs are allowed.
				if nn.Namespace != auth.Namespace {
					continue
				}
				var cp konnectv1alpha1.KonnectGatewayControlPlane
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
						Namespace: key.Namespace,
						Name:      key.Name,
					},
				})

			case commonv1alpha1.ControlPlaneRefKonnectID:
				ctrllog.FromContext(ctx).Error(
					fmt.Errorf("unimplemented ControlPlaneRef type %q", cpRef.Type),
					"unimplemented ControlPlaneRef for KongKey",
					"KongKey", key, "refType", cpRef.Type,
				)
				continue

			default:
				ctrllog.FromContext(ctx).V(logging.DebugLevel.Value()).Info(
					"unsupported ControlPlaneRef for KongKey",
					"KongKey", key, "refType", cpRef.Type,
				)
				continue
			}
		}
		return ret
	}
}

func enqueueKongKeyForKongKeySet(cl client.Client) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		keySet, ok := obj.(*configurationv1alpha1.KongKeySet)
		if !ok {
			return nil
		}
		var l configurationv1alpha1.KongKeyList
		if err := cl.List(ctx, &l,
			client.InNamespace(keySet.GetNamespace()),
			client.MatchingFields{
				IndexFieldKongKeyOnKongKeySetReference: keySet.GetNamespace() + "/" + keySet.GetName(),
			},
		); err != nil {
			return nil
		}

		return objectListToReconcileRequests(l.Items)
	}
}
