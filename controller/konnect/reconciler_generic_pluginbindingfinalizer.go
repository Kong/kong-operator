package konnect

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	configurationv1 "github.com/kong/kong-operator/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kong-operator/api/configuration/v1beta1"
	"github.com/kong/kong-operator/controller/konnect/constraints"
	"github.com/kong/kong-operator/controller/pkg/log"
	"github.com/kong/kong-operator/internal/utils/index"
	"github.com/kong/kong-operator/modules/manager/logging"
	"github.com/kong/kong-operator/pkg/clientops"
	"github.com/kong/kong-operator/pkg/consts"
)

// KonnectEntityPluginBindingFinalizerReconciler reconciles Konnect entities that may be referenced by KongPluginBinding.
// It uses the generic type constraints to constrain the supported types.
type KonnectEntityPluginBindingFinalizerReconciler[
	T constraints.SupportedKonnectEntityPluginReferenceableType,
	TEnt constraints.EntityType[T],
] struct {
	ControllerOptions controller.Options
	LoggingMode       logging.Mode
	Client            client.Client
}

// NewKonnectEntityPluginReconciler returns a new KonnectEntityPluginReconciler
// for the given Konnect entity type.
func NewKonnectEntityPluginReconciler[
	T constraints.SupportedKonnectEntityPluginReferenceableType,
	TEnt constraints.EntityType[T],
](
	controllerOptions controller.Options,
	loggingMode logging.Mode,
	client client.Client,
) *KonnectEntityPluginBindingFinalizerReconciler[T, TEnt] {
	r := &KonnectEntityPluginBindingFinalizerReconciler[T, TEnt]{
		ControllerOptions: controllerOptions,
		LoggingMode:       loggingMode,
		Client:            client,
	}
	return r
}

// SetupWithManager sets up the controller with the given manager.
func (r *KonnectEntityPluginBindingFinalizerReconciler[T, TEnt]) SetupWithManager(
	ctx context.Context, mgr ctrl.Manager,
) error {
	var (
		entityTypeName = constraints.EntityTypeName[T]()
		b              = ctrl.NewControllerManagedBy(mgr).Named(entityTypeName + "PluginBindingCleanupFinalizer")
	)

	r.setControllerBuilderOptionsForKongPluginBinding(b)

	return b.Complete(r)
}

// enqueueObjectReferencedByKongPluginBinding watches for KongPluginBinding objects
// that reference the given Konnect entity.
// It returns the reconcile.Request slice containing the entity that the KongPluginBinding references.
func (r *KonnectEntityPluginBindingFinalizerReconciler[T, TEnt]) enqueueObjectReferencedByKongPluginBinding() func(ctx context.Context, obj client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		kpb, ok := obj.(*configurationv1alpha1.KongPluginBinding)
		if !ok {
			return nil
		}

		// If the KongPluginBinding is unmanaged (created not using an annotation,
		// and thus not having KongPlugin as an owner), skip it, do not delete it.
		if !ownerRefIsAnyKongPlugin(kpb) {
			return nil
		}

		var (
			name string
			e    T
			ent  = TEnt(&e)
		)

		switch any(ent).(type) {
		case *configurationv1alpha1.KongService:
			if kpb.Spec.Targets == nil ||
				kpb.Spec.Targets.ServiceReference == nil ||
				kpb.Spec.Targets.ServiceReference.Kind != "KongService" ||
				kpb.Spec.Targets.ServiceReference.Group != configurationv1alpha1.GroupVersion.Group {
				return nil
			}
			name = kpb.Spec.Targets.ServiceReference.Name

		case *configurationv1alpha1.KongRoute:
			if kpb.Spec.Targets == nil ||
				kpb.Spec.Targets.RouteReference == nil ||
				kpb.Spec.Targets.RouteReference.Kind != "KongRoute" ||
				kpb.Spec.Targets.RouteReference.Group != configurationv1alpha1.GroupVersion.Group {
				return nil
			}
			name = kpb.Spec.Targets.RouteReference.Name

		case *configurationv1.KongConsumer:
			if kpb.Spec.Targets == nil || kpb.Spec.Targets.ConsumerReference == nil {
				return nil
			}
			name = kpb.Spec.Targets.ConsumerReference.Name

		case *configurationv1beta1.KongConsumerGroup:
			if kpb.Spec.Targets == nil || kpb.Spec.Targets.ConsumerGroupReference == nil {
				return nil
			}
			name = kpb.Spec.Targets.ConsumerGroupReference.Name

		default:
			return nil
		}

		return []ctrl.Request{
			{
				NamespacedName: types.NamespacedName{
					Namespace: kpb.Namespace,
					Name:      name,
				},
			},
		}
	}
}

// Reconcile reconciles the Konnect entity that can be set as KongPluginBinding target.
// It reconciles only entities that are referenced by managed KongPluginBindings,
// i.e. those that are created by the controller out of konghq.com/plugins annotations.
//
// Its purpose is to:
//   - check if the entity is marked for deletion and mark KongPluginBindings
//     that reference it.
//   - add a finalizer to the entity if there are KongPluginBindings referencing it.
//     This finalizer designates that this entity needs to have its KongPluginBindings
//     removed upon deletion
//   - remove the finalizer if all KongPluginBindings referencing it are removed.
func (r *KonnectEntityPluginBindingFinalizerReconciler[T, TEnt]) Reconcile(
	ctx context.Context, req ctrl.Request,
) (ctrl.Result, error) {
	var (
		entityTypeName = constraints.EntityTypeName[T]()
		logger         = log.GetLogger(ctx, entityTypeName, r.LoggingMode)
	)

	var (
		e   T
		ent = TEnt(&e)
	)
	if err := r.Client.Get(ctx, req.NamespacedName, ent); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	ctx = ctrllog.IntoContext(ctx, logger)
	log.Debug(logger, "reconciling")

	cl := client.NewNamespacedClient(r.Client, ent.GetNamespace())
	kongPluginBindingList := configurationv1alpha1.KongPluginBindingList{}
	err := cl.List(
		ctx,
		&kongPluginBindingList,
		client.MatchingFields{
			r.getKongPluginBindingIndexFieldForType(): ent.GetName(),
		},
	)
	if err != nil {
		return ctrl.Result{}, err
	}

	var finalizersChangedAction string
	// if the entity is marked for deletion, we need to delete all the PluginBindings that reference it.
	if !ent.GetDeletionTimestamp().IsZero() {
		if err := clientops.DeleteAllFromList(ctx, cl, &kongPluginBindingList); err != nil {
			return ctrl.Result{}, err
		}
		// in case no KongPluginBindings are referencing the entity, but it has the finalizer,
		// we need to remove the finalizer.
		if controllerutil.RemoveFinalizer(ent, consts.CleanupPluginBindingFinalizer) {
			finalizersChangedAction = "removed"
		}
	} else {
		// if the entity is not marked for deletion, update the finalizers accordingly.
		switch len(kongPluginBindingList.Items) {
		case 0:
			// in case no KongPluginBindings are referencing the entity, but it has the finalizer,
			// we need to remove the finalizer.
			if controllerutil.RemoveFinalizer(ent, consts.CleanupPluginBindingFinalizer) {
				finalizersChangedAction = "removed"
			}

		default:
			// add a finalizer to the entity that means the associated
			// kongPluginBindings need to be cleaned up upon deletion.
			if controllerutil.AddFinalizer(ent, consts.CleanupPluginBindingFinalizer) {
				finalizersChangedAction = "added"
			}
		}
	}

	if finalizersChangedAction != "" {
		if err = r.Client.Update(ctx, ent); err != nil {
			if apierrors.IsConflict(err) {
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, err
		}
		log.Debug(logger, "finalizers changed",
			"action", finalizersChangedAction,
			"finalizer", consts.CleanupPluginBindingFinalizer,
		)
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
}

func (r *KonnectEntityPluginBindingFinalizerReconciler[T, TEnt]) getKongPluginBindingIndexFieldForType() string {
	var (
		e   T
		ent = TEnt(&e)
	)

	switch any(ent).(type) {
	case *configurationv1alpha1.KongService:
		return index.IndexFieldKongPluginBindingKongServiceReference
	case *configurationv1alpha1.KongRoute:
		return index.IndexFieldKongPluginBindingKongRouteReference
	case *configurationv1.KongConsumer:
		return index.IndexFieldKongPluginBindingKongConsumerReference
	case *configurationv1beta1.KongConsumerGroup:
		return index.IndexFieldKongPluginBindingKongConsumerGroupReference
	default:
		panic(fmt.Sprintf("unsupported entity type %s", constraints.EntityTypeName[T]()))
	}
}

func (r *KonnectEntityPluginBindingFinalizerReconciler[T, TEnt]) setControllerBuilderOptionsForKongPluginBinding(
	b *builder.TypedBuilder[ctrl.Request],
) {
	var (
		e   T
		ent = TEnt(&e)
	)

	var pred func(obj client.Object) bool

	switch any(ent).(type) {
	case *configurationv1alpha1.KongService:
		pred = objRefersToKonnectGatewayControlPlane[configurationv1alpha1.KongService]
	case *configurationv1alpha1.KongRoute:
		pred = kongRouteRefersToKonnectGatewayControlPlane(r.Client)
	case *configurationv1.KongConsumer:
		pred = objRefersToKonnectGatewayControlPlane[configurationv1.KongConsumer]
	case *configurationv1beta1.KongConsumerGroup:
		pred = objRefersToKonnectGatewayControlPlane[configurationv1beta1.KongConsumerGroup]
	default:
		panic(fmt.Sprintf("unsupported entity type %s", constraints.EntityTypeName[T]()))
	}

	b.
		WithOptions(r.ControllerOptions).
		For(ent,
			builder.WithPredicates(
				predicate.NewPredicateFuncs(pred),
				kongPluginsAnnotationChangedPredicate,
			),
		).
		Watches(
			&configurationv1alpha1.KongPluginBinding{},
			handler.EnqueueRequestsFromMapFunc(
				r.enqueueObjectReferencedByKongPluginBinding(),
			),
		)
}
