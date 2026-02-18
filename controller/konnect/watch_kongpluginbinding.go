package konnect

import (
	"context"
	"fmt"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	configurationv1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1beta1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha2"

	"github.com/kong/kong-operator/v2/controller/konnect/constraints"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/internal/utils/index"
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
					predicate.NewPredicateFuncs(objRefersToKonnectGatewayControlPlane[configurationv1alpha1.KongPluginBinding]),
				),
			)
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&konnectv1alpha1.KonnectAPIAuthConfiguration{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueObjectForAPIAuthThroughControlPlaneRef[configurationv1alpha1.KongPluginBindingList](
						cl, index.IndexFieldKongPluginBindingKonnectGatewayControlPlane,
					),
				),
			)
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&konnectv1alpha2.KonnectGatewayControlPlane{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueObjectForKonnectGatewayControlPlane[configurationv1alpha1.KongPluginBindingList](
						cl, index.IndexFieldKongPluginBindingKonnectGatewayControlPlane,
					),
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
				&configurationv1alpha1.KongService{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueKongPluginBindingFor[configurationv1alpha1.KongService](cl),
				),
				builder.WithPredicates(
					predicate.NewPredicateFuncs(objRefersToKonnectGatewayControlPlane[configurationv1alpha1.KongService]),
				),
			)
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&configurationv1alpha1.KongRoute{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueKongPluginBindingFor[configurationv1alpha1.KongRoute](cl),
				),
				builder.WithPredicates(
					predicate.NewPredicateFuncs(objRefersToKonnectGatewayControlPlane[configurationv1alpha1.KongRoute]),
				),
			)
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&configurationv1.KongConsumer{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueKongPluginBindingFor[configurationv1.KongConsumer](cl),
				),
				builder.WithPredicates(
					predicate.NewPredicateFuncs(objRefersToKonnectGatewayControlPlane[configurationv1.KongConsumer]),
				),
			)
		},
		func(b *ctrl.Builder) *ctrl.Builder {
			return b.Watches(
				&configurationv1beta1.KongConsumerGroup{},
				handler.EnqueueRequestsFromMapFunc(
					enqueueKongPluginBindingFor[configurationv1beta1.KongConsumerGroup](cl),
				),
				builder.WithPredicates(
					predicate.NewPredicateFuncs(objRefersToKonnectGatewayControlPlane[configurationv1beta1.KongConsumerGroup]),
				),
			)
		},
	}
}

// -----------------------------------------------------------------------------
// KongPluginBinding reconciler - Watch Mappers
// -----------------------------------------------------------------------------

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
				index.IndexFieldKongPluginBindingKongPluginReference: plugin.Namespace + "/" + plugin.Name,
			},
		)
		if err != nil {
			ctrllog.FromContext(ctx).Error(err, "failed to list KongPluginBindings referencing KongPlugin")
			return nil
		}

		return objectListToReconcileRequests(pluginBindingList.Items, kongPluginBindingIsBoundToKonnectGatewayControlPlane)
	}
}

func enqueueKongPluginBindingFor[
	T constraints.SupportedKonnectEntityPluginReferenceableType,
	TEnt constraints.EntityType[T],
](
	cl client.Client,
) func(ctx context.Context, obj client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		ent, ok := obj.(TEnt)
		if !ok {
			return nil
		}

		logger := ctrllog.FromContext(ctx)
		var indexName string
		switch any(ent).(type) {
		case *configurationv1alpha1.KongService:
			indexName = index.IndexFieldKongPluginBindingKongServiceReference
		case *configurationv1alpha1.KongRoute:
			indexName = index.IndexFieldKongPluginBindingKongRouteReference
		case *configurationv1.KongConsumer:
			indexName = index.IndexFieldKongPluginBindingKongConsumerReference
		case *configurationv1beta1.KongConsumerGroup:
			indexName = index.IndexFieldKongPluginBindingKongConsumerGroupReference
		default:
			log.Error(
				logger,
				fmt.Errorf("unsupported entity type %s in KongPluginBinding watch", constraints.EntityTypeName[T]()),
				"unsupported entity type",
			)
			return nil

		}

		var pluginBindingList configurationv1alpha1.KongPluginBindingList
		err := cl.List(ctx, &pluginBindingList,
			client.InNamespace(ent.GetNamespace()),
			client.MatchingFields{
				indexName: ent.GetName(),
			},
		)
		if err != nil {
			log.Error(
				logger,
				err,
				"failed to list KongPluginBindings referencing %ss", constraints.EntityTypeName[T](),
			)
			return nil
		}

		return objectListToReconcileRequests(pluginBindingList.Items, kongPluginBindingIsBoundToKonnectGatewayControlPlane)
	}
}

func kongPluginBindingIsBoundToKonnectGatewayControlPlane(kpb *configurationv1alpha1.KongPluginBinding) bool {
	return objHasControlPlaneRef(kpb)
}
