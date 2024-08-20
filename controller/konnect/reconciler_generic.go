package konnect

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/samber/mo"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kong/gateway-operator/controller/pkg/log"
	"github.com/kong/gateway-operator/pkg/consts"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kubernetes-configuration/api/configuration/v1beta1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

const (
	// KonnectCleanupFinalizer is the finalizer that is added to the Konnect
	// entities when they are created in Konnect, and which is removed when
	// the CR and Konnect entity are deleted.
	KonnectCleanupFinalizer = "gateway.konghq.com/konnect-cleanup"
)

// KonnectEntityReconciler reconciles a Konnect entities.
// It uses the generic type constraints to constrain the supported types.
type KonnectEntityReconciler[T SupportedKonnectEntityType, TEnt EntityType[T]] struct {
	sdkFactory      SDKFactory
	DevelopmentMode bool
	Client          client.Client
	SyncPeriod      time.Duration
}

// KonnectEntityReconcilerOption is a functional option for the KonnectEntityReconciler.
type KonnectEntityReconcilerOption[
	T SupportedKonnectEntityType,
	TEnt EntityType[T],
] func(*KonnectEntityReconciler[T, TEnt])

// WithKonnectEntitySyncPeriod sets the sync period for the reconciler.
func WithKonnectEntitySyncPeriod[T SupportedKonnectEntityType, TEnt EntityType[T]](
	d time.Duration,
) KonnectEntityReconcilerOption[T, TEnt] {
	return func(r *KonnectEntityReconciler[T, TEnt]) {
		r.SyncPeriod = d
	}
}

// NewKonnectEntityReconciler returns a new KonnectEntityReconciler for the given
// Konnect entity type.
func NewKonnectEntityReconciler[
	T SupportedKonnectEntityType,
	TEnt EntityType[T],
](
	sdkFactory SDKFactory,
	developmentMode bool,
	client client.Client,
	opts ...KonnectEntityReconcilerOption[T, TEnt],
) *KonnectEntityReconciler[T, TEnt] {
	r := &KonnectEntityReconciler[T, TEnt]{
		sdkFactory:      sdkFactory,
		DevelopmentMode: developmentMode,
		Client:          client,
		SyncPeriod:      consts.DefaultKonnectSyncPeriod,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

const (
	// MaxConcurrentReconciles is the maximum number of concurrent reconciles
	// that the controller will allow.
	MaxConcurrentReconciles = 8
)

// SetupWithManager sets up the controller with the given manager.
func (r *KonnectEntityReconciler[T, TEnt]) SetupWithManager(mgr ctrl.Manager) error {
	var (
		e   T
		ent = TEnt(&e)
		b   = ctrl.NewControllerManagedBy(mgr).
			Named(entityTypeName[T]()).
			WithOptions(controller.Options{
				MaxConcurrentReconciles: MaxConcurrentReconciles,
			})
	)

	for _, dep := range ReconciliationWatchOptionsForEntity(r.Client, ent) {
		b = dep(b)
	}
	return b.Complete(r)
}

// Reconcile reconciles the given Konnect entity.
func (r *KonnectEntityReconciler[T, TEnt]) Reconcile(
	ctx context.Context, req ctrl.Request,
) (ctrl.Result, error) {
	var (
		entityTypeName = entityTypeName[T]()
		logger         = log.GetLogger(ctx, entityTypeName, r.DevelopmentMode)
	)

	var (
		e   T
		ent = TEnt(&e)
	)
	if err := r.Client.Get(ctx, req.NamespacedName, ent); err != nil {
		if k8serrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	ctx = ctrllog.IntoContext(ctx, logger)
	log.Debug(logger, "reconciling", ent)

	// If a type has a ControlPlane ref, handle it.
	res, err := handleControlPlaneRef(ctx, r.Client, ent)
	if err != nil || res.Requeue {
		// If the referenced ControlPlane is not found and the object is deleted,
		// remove the finalizer and update the status.
		// There's no need to remove the entity on Konnect because the ControlPlane
		// does not exist anymore.
		if !ent.GetDeletionTimestamp().IsZero() && errors.As(err, &ReferencedControlPlaneDoesNotExistError{}) {
			if controllerutil.RemoveFinalizer(ent, KonnectCleanupFinalizer) {
				if err := r.Client.Update(ctx, ent); err != nil {
					if k8serrors.IsConflict(err) {
						return ctrl.Result{Requeue: true}, nil
					}
					return ctrl.Result{}, fmt.Errorf("failed to remove finalizer %s: %w", KonnectCleanupFinalizer, err)
				}
			}
		}
		// Status update will requeue the entity.
		return ctrl.Result{}, nil
	}
	// If a type has a KongService ref, handle it.
	res, err = handleKongServiceRef(ctx, r.Client, ent)
	if err != nil {
		if !errors.As(err, &ReferencedKongServiceIsBeingDeleted{}) {
			return ctrl.Result{}, err
		}

		// If the referenced KongService is being deleted (has a non zero deletion timestamp)
		// then we remove the entity if it has not been deleted yet (deletion timestamp is zero).
		// We do this because Konnect blocks deletion of entities like Services
		// if they contain entities like Routes.
		if ent.GetDeletionTimestamp().IsZero() {
			if err := r.Client.Delete(ctx, ent); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to delete %s: %w", client.ObjectKeyFromObject(ent), err)
			}
			return ctrl.Result{}, nil
		}
	} else if res.Requeue {
		return res, nil
	}

	apiAuthRef, err := getAPIAuthRefNN(ctx, r.Client, ent)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get APIAuth ref for %s: %w", client.ObjectKeyFromObject(ent), err)
	}

	var apiAuth konnectv1alpha1.KonnectAPIAuthConfiguration
	if err := r.Client.Get(ctx, apiAuthRef, &apiAuth); err != nil {
		if k8serrors.IsNotFound(err) {
			if res, err := updateStatusWithCondition(
				ctx, r.Client, ent,
				KonnectEntityAPIAuthConfigurationResolvedRefConditionType,
				metav1.ConditionFalse,
				KonnectEntityAPIAuthConfigurationResolvedRefReasonRefNotFound,
				fmt.Sprintf("Referenced KonnectAPIAuthConfiguration %s not found", apiAuthRef),
			); err != nil || res.Requeue {
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, nil
		}

		if res, err := updateStatusWithCondition(
			ctx, r.Client, ent,
			KonnectEntityAPIAuthConfigurationResolvedRefConditionType,
			metav1.ConditionFalse,
			KonnectEntityAPIAuthConfigurationResolvedRefReasonRefInvalid,
			fmt.Sprintf("KonnectAPIAuthConfiguration reference %s is invalid: %v", apiAuthRef, err),
		); err != nil || res.Requeue {
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, fmt.Errorf("failed to get KonnectAPIAuthConfiguration: %w", err)
	}

	// Update the status if the reference is resolved and it's not as expected.
	if cond, present := k8sutils.GetCondition(KonnectEntityAPIAuthConfigurationResolvedRefConditionType, ent); !present ||
		cond.Status != metav1.ConditionTrue ||
		cond.ObservedGeneration != ent.GetGeneration() ||
		cond.Reason != KonnectEntityAPIAuthConfigurationResolvedRefReasonResolvedRef {
		if res, err := updateStatusWithCondition(
			ctx, r.Client, ent,
			KonnectEntityAPIAuthConfigurationResolvedRefConditionType,
			metav1.ConditionTrue,
			KonnectEntityAPIAuthConfigurationResolvedRefReasonResolvedRef,
			fmt.Sprintf("KonnectAPIAuthConfiguration reference %s is resolved", apiAuthRef),
		); err != nil || res.Requeue {
			return res, err
		}
		return ctrl.Result{}, nil
	}

	// Check if the referenced APIAuthConfiguration is valid.
	if cond, present := k8sutils.GetCondition(KonnectEntityAPIAuthConfigurationValidConditionType, &apiAuth); !present ||
		cond.Status != metav1.ConditionTrue ||
		cond.Reason != KonnectEntityAPIAuthConfigurationReasonValid {

		// If it's invalid then set the "APIAuthValid" status condition on
		// the entity to False with "Invalid" reason.
		if res, err := updateStatusWithCondition(
			ctx, r.Client, ent,
			KonnectEntityAPIAuthConfigurationValidConditionType,
			metav1.ConditionFalse,
			KonnectEntityAPIAuthConfigurationReasonInvalid,
			conditionMessageReferenceKonnectAPIAuthConfigurationInvalid(apiAuthRef),
		); err != nil || res.Requeue {
			return res, err
		}

		return ctrl.Result{}, nil
	}

	// If the referenced APIAuthConfiguration is valid, set the "APIAuthValid"
	// condition to True with "Valid" reason.
	// Only perform the update if the condition is not as expected.
	if cond, present := k8sutils.GetCondition(KonnectEntityAPIAuthConfigurationValidConditionType, ent); !present ||
		cond.Status != metav1.ConditionTrue ||
		cond.Reason != KonnectEntityAPIAuthConfigurationReasonValid ||
		cond.ObservedGeneration != ent.GetGeneration() ||
		cond.Message != conditionMessageReferenceKonnectAPIAuthConfigurationValid(apiAuthRef) {

		if res, err := updateStatusWithCondition(
			ctx, r.Client, ent,
			KonnectEntityAPIAuthConfigurationValidConditionType,
			metav1.ConditionTrue,
			KonnectEntityAPIAuthConfigurationReasonValid,
			conditionMessageReferenceKonnectAPIAuthConfigurationValid(apiAuthRef),
		); err != nil || res.Requeue {
			return res, err
		}
		return ctrl.Result{}, nil
	}

	token, err := getTokenFromKonnectAPIAuthConfiguration(ctx, r.Client, &apiAuth)
	if err != nil {
		if res, errStatus := updateStatusWithCondition(
			ctx, r.Client, &apiAuth,
			KonnectEntityAPIAuthConfigurationValidConditionType,
			metav1.ConditionFalse,
			KonnectEntityAPIAuthConfigurationReasonInvalid,
			err.Error(),
		); errStatus != nil || res.Requeue {
			return res, errStatus
		}
		return ctrl.Result{}, err
	}

	// NOTE: We need to create a new SDK instance for each reconciliation
	// because the token is retrieved in runtime through KonnectAPIAuthConfiguration.
	sdk := r.sdkFactory.NewKonnectSDK(
		"https://"+apiAuth.Spec.ServerURL,
		SDKToken(token),
	)

	if delTimestamp := ent.GetDeletionTimestamp(); !delTimestamp.IsZero() {
		logger.Info("resource is being deleted")
		// wait for termination grace period before cleaning up
		if delTimestamp.After(time.Now()) {
			logger.Info("resource still under grace period, requeueing")
			return ctrl.Result{
				// Requeue when grace period expires.
				// If deletion timestamp is changed,
				// the update will trigger another round of reconciliation.
				// so we do not consider updates of deletion timestamp here.
				RequeueAfter: time.Until(delTimestamp.Time),
			}, nil
		}

		if controllerutil.RemoveFinalizer(ent, KonnectCleanupFinalizer) {
			if err := Delete[T, TEnt](ctx, sdk, r.Client, ent); err != nil {
				if res, errStatus := updateStatusWithCondition(
					ctx, r.Client, ent,
					KonnectEntityProgrammedConditionType,
					metav1.ConditionFalse,
					KonnectEntityProgrammedReasonKonnectAPIOpFailed,
					err.Error(),
				); errStatus != nil || res.Requeue {
					return res, errStatus
				}
				return ctrl.Result{}, err
			}
			if err := r.Client.Update(ctx, ent); err != nil {
				if k8serrors.IsConflict(err) {
					return ctrl.Result{Requeue: true}, nil
				}
				return ctrl.Result{}, fmt.Errorf("failed to remove finalizer %s: %w", KonnectCleanupFinalizer, err)
			}
		}

		return ctrl.Result{}, nil
	}

	// TODO: relying on status ID is OK but we need to rethink this because
	// we're using a cached client so after creating the resource on Konnect it might
	// happen that we've just created the resource but the status ID is not there yet.
	//
	// We should look at the "expectations" for this:
	// https://github.com/kubernetes/kubernetes/blob/master/pkg/controller/controller_utils.go
	if status := ent.GetKonnectStatus(); status == nil || status.GetKonnectID() == "" {
		_, err := Create[T, TEnt](ctx, sdk, r.Client, ent)
		if err != nil {
			// TODO(pmalek): this is actually not 100% error prone because when status
			// update fails we don't store the Konnect ID and hence the reconciler
			// will try to create the resource again on next reconciliation.
			if err := r.Client.Status().Update(ctx, ent); err != nil {
				if k8serrors.IsConflict(err) {
					return ctrl.Result{Requeue: true}, nil
				}
				return ctrl.Result{}, fmt.Errorf("failed to update status after creating object: %w", err)
			}

			return ctrl.Result{}, FailedKonnectOpError[T]{
				Op:  CreateOp,
				Err: err,
			}
		}

		ent.GetKonnectStatus().ServerURL = apiAuth.Spec.ServerURL
		ent.GetKonnectStatus().OrgID = apiAuth.Status.OrganizationID
		if err := r.Client.Status().Update(ctx, ent); err != nil {
			if k8serrors.IsConflict(err) {
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, fmt.Errorf("failed to update status: %w", err)
		}

		if controllerutil.AddFinalizer(ent, KonnectCleanupFinalizer) {
			if err := r.Client.Update(ctx, ent); err != nil {
				if k8serrors.IsConflict(err) {
					return ctrl.Result{Requeue: true}, nil
				}
				return ctrl.Result{}, fmt.Errorf("failed to update finalizer: %w", err)
			}
		}

		// NOTE: we don't need to requeue here because the object update will trigger another reconciliation.
		return ctrl.Result{}, nil
	}

	if res, err := Update[T, TEnt](ctx, sdk, r.SyncPeriod, r.Client, ent); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update object: %w", err)
	} else if res.Requeue || res.RequeueAfter > 0 {
		return res, nil
	}

	ent.GetKonnectStatus().ServerURL = apiAuth.Spec.ServerURL
	ent.GetKonnectStatus().OrgID = apiAuth.Status.OrganizationID
	if err := r.Client.Status().Update(ctx, ent); err != nil {
		if k8serrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to update in cluster resource after Konnect update: %w", err)
	}

	// NOTE: We requeue here to keep enforcing the state of the resource in Konnect.
	// Konnect does not allow subscribing to changes so we need to keep pushing the
	// desired state periodically.
	return ctrl.Result{
		RequeueAfter: r.SyncPeriod,
	}, nil
}

// EntityWithControlPlaneRef is an interface for entities that have a ControlPlaneRef.
type EntityWithControlPlaneRef interface {
	SetControlPlaneID(string)
	GetControlPlaneID() string
}

func updateStatusWithCondition[T interface {
	client.Object
	k8sutils.ConditionsAware
}](
	ctx context.Context,
	client client.Client,
	ent T,
	conditionType consts.ConditionType,
	conditionStatus metav1.ConditionStatus,
	conditionReason consts.ConditionReason,
	conditionMessage string,
) (ctrl.Result, error) {
	k8sutils.SetCondition(
		k8sutils.NewConditionWithGeneration(
			conditionType,
			conditionStatus,
			conditionReason,
			conditionMessage,
			ent.GetGeneration(),
		),
		ent,
	)

	if err := client.Status().Update(ctx, ent); err != nil {
		if k8serrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf(
			"failed to update status with %s condition: %w",
			KonnectEntityAPIAuthConfigurationResolvedRefConditionType, err,
		)
	}

	return ctrl.Result{}, nil
}

func getCPForRef(
	ctx context.Context,
	cl client.Client,
	cpRef configurationv1alpha1.ControlPlaneRef,
	namespace string,
) (*konnectv1alpha1.KonnectControlPlane, error) {
	if cpRef.Type != configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef {
		return nil, fmt.Errorf("unsupported ControlPlane ref type %q", cpRef.Type)
	}
	// TODO(pmalek): handle cross namespace refs
	nn := types.NamespacedName{
		Name:      cpRef.KonnectNamespacedRef.Name,
		Namespace: namespace,
	}

	var cp konnectv1alpha1.KonnectControlPlane
	if err := cl.Get(ctx, nn, &cp); err != nil {
		return nil, fmt.Errorf("failed to get ControlPlane %s", nn)
	}
	return &cp, nil
}

func getCPAuthRefForRef(
	ctx context.Context,
	cl client.Client,
	cpRef configurationv1alpha1.ControlPlaneRef,
	namespace string,
) (types.NamespacedName, error) {
	cp, err := getCPForRef(ctx, cl, cpRef, namespace)
	if err != nil {
		return types.NamespacedName{}, err
	}

	return types.NamespacedName{
		Name: cp.GetKonnectAPIAuthConfigurationRef().Name,
		// TODO(pmalek): enable if cross namespace refs are allowed
		Namespace: cp.GetNamespace(),
	}, nil
}

func getAPIAuthRefNN[T SupportedKonnectEntityType, TEnt EntityType[T]](
	ctx context.Context,
	cl client.Client,
	ent TEnt,
) (types.NamespacedName, error) {
	// If the entity has a ControlPlaneRef, get the KonnectAPIAuthConfiguration
	// ref from the referenced ControlPlane.
	cpRef, ok := getControlPlaneRef(ent).Get()
	if ok {
		return getCPAuthRefForRef(ctx, cl, cpRef, ent.GetNamespace())
	}

	// If the entity has a KongServiceRef, get the KonnectAPIAuthConfiguration
	// ref from the referenced KongService.
	svcRef, ok := getServiceRef(ent).Get()
	if ok {
		if svcRef.Type != configurationv1alpha1.ServiceRefNamespacedRef {
			return types.NamespacedName{}, fmt.Errorf("unsupported KongService ref type %q", svcRef.Type)
		}
		// TODO(pmalek): handle cross namespace refs
		nn := types.NamespacedName{
			Name:      svcRef.NamespacedRef.Name,
			Namespace: ent.GetNamespace(),
		}

		var svc configurationv1alpha1.KongService
		if err := cl.Get(ctx, nn, &svc); err != nil {
			return types.NamespacedName{}, fmt.Errorf("failed to get KongService %s", nn)
		}

		cpRef, ok := getControlPlaneRef(&svc).Get()
		if !ok {
			return types.NamespacedName{}, fmt.Errorf("KongService %s does not have a ControlPlaneRef", nn)
		}
		return getCPAuthRefForRef(ctx, cl, cpRef, ent.GetNamespace())
	}

	if ref, ok := any(ent).(EntityWithKonnectAPIAuthConfigurationRef); ok {
		return types.NamespacedName{
			Name: ref.GetKonnectAPIAuthConfigurationRef().Name,
			// TODO(pmalek): enable if cross namespace refs are allowed
			Namespace: ent.GetNamespace(),
		}, nil
	}

	return types.NamespacedName{}, fmt.Errorf(
		"cannot get KonnectAPIAuthConfiguration for entity type %T %s",
		client.ObjectKeyFromObject(ent), ent,
	)
}

func getServiceRef[T SupportedKonnectEntityType, TEnt EntityType[T]](
	e TEnt,
) mo.Option[configurationv1alpha1.ServiceRef] {
	switch e := any(e).(type) {
	case *configurationv1alpha1.KongService,
		*configurationv1.KongConsumer,
		*configurationv1beta1.KongConsumerGroup,
		*konnectv1alpha1.KonnectControlPlane:
		return mo.None[configurationv1alpha1.ServiceRef]()
	case *configurationv1alpha1.KongRoute:
		if e.Spec.ServiceRef == nil {
			return mo.None[configurationv1alpha1.ServiceRef]()
		}
		return mo.Some(*e.Spec.ServiceRef)
	default:
		panic(fmt.Sprintf("unsupported entity type %T", e))
	}
}

// handleKongServiceRef handles the ServiceRef for the given entity.
// It sets the owner reference to the referenced KongService and updates the
// status of the entity based on the referenced KongService status.
func handleKongServiceRef[T SupportedKonnectEntityType, TEnt EntityType[T]](
	ctx context.Context,
	cl client.Client,
	ent TEnt,
) (ctrl.Result, error) {
	kongServiceRef, ok := getServiceRef(ent).Get()
	if !ok {
		return ctrl.Result{}, nil
	}
	switch kongServiceRef.Type {
	case configurationv1alpha1.ServiceRefNamespacedRef:
		svc := configurationv1alpha1.KongService{}
		nn := types.NamespacedName{
			Name: kongServiceRef.NamespacedRef.Name,
			// TODO: handle cross namespace refs
			Namespace: ent.GetNamespace(),
		}

		if err := cl.Get(ctx, nn, &svc); err != nil {
			if res, errStatus := updateStatusWithCondition(
				ctx, cl, ent,
				KongServiceRefValidConditionType,
				metav1.ConditionFalse,
				KongServiceRefReasonInvalid,
				err.Error(),
			); errStatus != nil || res.Requeue {
				return res, errStatus
			}

			return ctrl.Result{}, fmt.Errorf("Can't get the referenced KongService %s: %w", nn, err)
		}

		// If referenced KongService is being deleted, return an error so that we
		// can remove the entity from Konnect first.
		if delTimestamp := svc.GetDeletionTimestamp(); !delTimestamp.IsZero() {
			return ctrl.Result{}, ReferencedKongServiceIsBeingDeleted{
				Reference: nn,
			}
		}

		cond, ok := k8sutils.GetCondition(KonnectEntityProgrammedConditionType, &svc)
		if !ok || cond.Status != metav1.ConditionTrue {
			ent.SetKonnectID("")
			if res, err := updateStatusWithCondition(
				ctx, cl, ent,
				KongServiceRefValidConditionType,
				metav1.ConditionFalse,
				KongServiceRefReasonInvalid,
				fmt.Sprintf("Referenced KongService %s is not programmed yet", nn),
			); err != nil || res.Requeue {
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}

		old := ent.DeepCopyObject().(TEnt)
		if err := controllerutil.SetOwnerReference(&svc, ent, cl.Scheme(), controllerutil.WithBlockOwnerDeletion(true)); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to set owner reference: %w", err)
		}
		if err := cl.Patch(ctx, ent, client.MergeFrom(old)); err != nil {
			if k8serrors.IsConflict(err) {
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, fmt.Errorf("failed to update status: %w", err)
		}

		// TODO(pmalek): make this generic.
		// Service ID is not stored in KonnectEntityStatus because not all entities
		// have a ServiceRef, hence the type constraints in the reconciler can't be used.
		if route, ok := any(ent).(*configurationv1alpha1.KongRoute); ok {
			if route.Status.Konnect == nil {
				route.Status.Konnect = &konnectv1alpha1.KonnectEntityStatusWithControlPlaneAndServiceRefs{}
			}
			route.Status.Konnect.ServiceID = svc.Status.Konnect.GetKonnectID()
		}

		if res, errStatus := updateStatusWithCondition(
			ctx, cl, ent,
			KongServiceRefValidConditionType,
			metav1.ConditionTrue,
			KongServiceRefReasonValid,
			fmt.Sprintf("Referenced KongService %s programmed", nn),
		); errStatus != nil || res.Requeue {
			return res, errStatus
		}

		cpRef, ok := getControlPlaneRef(&svc).Get()
		if !ok {
			return ctrl.Result{}, fmt.Errorf(
				"KongRoute references a KongService %s which does not have a ControlPlane ref",
				client.ObjectKeyFromObject(&svc),
			)
		}
		cp, err := getCPForRef(ctx, cl, cpRef, ent.GetNamespace())
		if err != nil {
			if res, errStatus := updateStatusWithCondition(
				ctx, cl, ent,
				ControlPlaneRefValidConditionType,
				metav1.ConditionFalse,
				ControlPlaneRefReasonInvalid,
				err.Error(),
			); errStatus != nil || res.Requeue {
				return res, errStatus
			}
			if k8serrors.IsNotFound(err) {
				return ctrl.Result{}, ReferencedControlPlaneDoesNotExistError{
					Reference: nn,
					Err:       err,
				}
			}
			return ctrl.Result{}, err
		}

		cond, ok = k8sutils.GetCondition(KonnectEntityProgrammedConditionType, cp)
		if !ok || cond.Status != metav1.ConditionTrue || cond.ObservedGeneration != cp.GetGeneration() {
			if res, errStatus := updateStatusWithCondition(
				ctx, cl, ent,
				ControlPlaneRefValidConditionType,
				metav1.ConditionFalse,
				ControlPlaneRefReasonInvalid,
				fmt.Sprintf("Referenced ControlPlane %s is not programmed yet", nn),
			); errStatus != nil || res.Requeue {
				return res, errStatus
			}

			return ctrl.Result{Requeue: true}, nil
		}

		// TODO(pmalek): make this generic.
		// CP ID is not stored in KonnectEntityStatus because not all entities
		// have a ControlPlaneRef, hence the type constraints in the reconciler can't be used.
		if resource, ok := any(ent).(EntityWithControlPlaneRef); ok {
			resource.SetControlPlaneID(cp.Status.ID)
		}

		if res, errStatus := updateStatusWithCondition(
			ctx, cl, ent,
			ControlPlaneRefValidConditionType,
			metav1.ConditionTrue,
			ControlPlaneRefReasonValid,
			fmt.Sprintf("Referenced ControlPlane %s is programmed", nn),
		); errStatus != nil || res.Requeue {
			return res, errStatus
		}

	default:
		return ctrl.Result{}, fmt.Errorf("unimplemented KongService ref type %q", kongServiceRef.Type)
	}

	return ctrl.Result{}, nil
}

func getControlPlaneRef[T SupportedKonnectEntityType, TEnt EntityType[T]](
	e TEnt,
) mo.Option[configurationv1alpha1.ControlPlaneRef] {
	switch e := any(e).(type) {
	case *konnectv1alpha1.KonnectControlPlane, *configurationv1alpha1.KongRoute:
		return mo.None[configurationv1alpha1.ControlPlaneRef]()
	case *configurationv1.KongConsumer:
		if e.Spec.ControlPlaneRef == nil {
			return mo.None[configurationv1alpha1.ControlPlaneRef]()
		}
		return mo.Some(*e.Spec.ControlPlaneRef)
	case *configurationv1beta1.KongConsumerGroup:
		if e.Spec.ControlPlaneRef == nil {
			return mo.None[configurationv1alpha1.ControlPlaneRef]()
		}
		return mo.Some(*e.Spec.ControlPlaneRef)
	case *configurationv1alpha1.KongService:
		if e.Spec.ControlPlaneRef == nil {
			return mo.None[configurationv1alpha1.ControlPlaneRef]()
		}
		return mo.Some(*e.Spec.ControlPlaneRef)
	default:
		panic(fmt.Sprintf("unsupported entity type %T", e))
	}
}

// handleControlPlaneRef handles the ControlPlaneRef for the given entity.
// It sets the owner reference to the referenced ControlPlane and updates the
// status of the entity based on the referenced ControlPlane status.
func handleControlPlaneRef[T SupportedKonnectEntityType, TEnt EntityType[T]](
	ctx context.Context,
	cl client.Client,
	ent TEnt,
) (ctrl.Result, error) {
	cpRef, ok := getControlPlaneRef(ent).Get()
	if !ok {
		return ctrl.Result{}, nil
	}

	switch cpRef.Type {
	case configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef:
		cp := konnectv1alpha1.KonnectControlPlane{}
		// TODO(pmalek): handle cross namespace refs
		nn := types.NamespacedName{
			Name:      cpRef.KonnectNamespacedRef.Name,
			Namespace: ent.GetNamespace(),
		}
		if err := cl.Get(ctx, nn, &cp); err != nil {
			if res, errStatus := updateStatusWithCondition(
				ctx, cl, ent,
				ControlPlaneRefValidConditionType,
				metav1.ConditionFalse,
				ControlPlaneRefReasonInvalid,
				err.Error(),
			); errStatus != nil || res.Requeue {
				return res, errStatus
			}
			if k8serrors.IsNotFound(err) {
				return ctrl.Result{}, ReferencedControlPlaneDoesNotExistError{
					Reference: nn,
					Err:       err,
				}
			}
			return ctrl.Result{}, err
		}

		cond, ok := k8sutils.GetCondition(KonnectEntityProgrammedConditionType, &cp)
		if !ok || cond.Status != metav1.ConditionTrue || cond.ObservedGeneration != cp.GetGeneration() {
			if res, errStatus := updateStatusWithCondition(
				ctx, cl, ent,
				ControlPlaneRefValidConditionType,
				metav1.ConditionFalse,
				ControlPlaneRefReasonInvalid,
				fmt.Sprintf("Referenced ControlPlane %s is not programmed yet", nn),
			); errStatus != nil || res.Requeue {
				return res, errStatus
			}

			return ctrl.Result{Requeue: true}, nil
		}

		old := ent.DeepCopyObject().(TEnt)
		if err := controllerutil.SetOwnerReference(&cp, ent, cl.Scheme(), controllerutil.WithBlockOwnerDeletion(true)); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to set owner reference: %w", err)
		}
		if err := cl.Patch(ctx, ent, client.MergeFrom(old)); err != nil {
			if k8serrors.IsConflict(err) {
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, fmt.Errorf("failed to update status: %w", err)
		}

		// TODO(pmalek): make this generic.
		// CP ID is not stored in KonnectEntityStatus because not all entities
		// have a ControlPlaneRef, hence the type constraints in the reconciler can't be used.
		if resource, ok := any(ent).(EntityWithControlPlaneRef); ok {
			resource.SetControlPlaneID(cp.Status.ID)
		}

		if res, errStatus := updateStatusWithCondition(
			ctx, cl, ent,
			ControlPlaneRefValidConditionType,
			metav1.ConditionTrue,
			ControlPlaneRefReasonValid,
			fmt.Sprintf("Referenced ControlPlane %s is programmed", nn),
		); errStatus != nil || res.Requeue {
			return res, errStatus
		}

		return ctrl.Result{}, nil

	default:
		return ctrl.Result{}, fmt.Errorf("unimplemented ControlPlane ref type %q", cpRef.Type)
	}
}

func conditionMessageReferenceKonnectAPIAuthConfigurationInvalid(apiAuthRef types.NamespacedName) string {
	return fmt.Sprintf("referenced KonnectAPIAuthConfiguration %s is invalid", apiAuthRef)
}

func conditionMessageReferenceKonnectAPIAuthConfigurationValid(apiAuthRef types.NamespacedName) string {
	return fmt.Sprintf("referenced KonnectAPIAuthConfiguration %s is valid", apiAuthRef)
}
