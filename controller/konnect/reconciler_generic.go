package konnect

import (
	"context"
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

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

const (
	// TODO(pmalek): make configurable https://github.com/Kong/gateway-operator/issues/451
	configurableSyncPeriod = 1 * time.Minute
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
) *KonnectEntityReconciler[T, TEnt] {
	return &KonnectEntityReconciler[T, TEnt]{
		sdkFactory:      sdkFactory,
		DevelopmentMode: developmentMode,
		Client:          client,
	}
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
			For(ent).
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

	res, err := handleControlPlaneRef(ctx, r.Client, ent)
	if err != nil || res.Requeue {
		return res, err
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

	if res, err := Update[T, TEnt](ctx, sdk, r.Client, ent); err != nil {
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
		RequeueAfter: configurableSyncPeriod,
	}, nil
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

func getAPIAuthRefNN[T SupportedKonnectEntityType, TEnt EntityType[T]](
	ctx context.Context,
	cl client.Client,
	ent TEnt,
) (types.NamespacedName, error) {
	// If the entity has a ControlPlaneRef, get the KonnectAPIAuthConfiguration
	// ref from the referenced ControlPlane.
	cpRef, ok := getControlPlaneRef(ent).Get()
	if ok {
		if cpRef.Type != configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef {
			return types.NamespacedName{}, fmt.Errorf("unsupported ControlPlane ref type %q", cpRef.Type)
		}
		nn := types.NamespacedName{
			Name:      cpRef.KonnectNamespacedRef.Name,
			Namespace: cpRef.KonnectNamespacedRef.Namespace,
		}
		if nn.Namespace == "" {
			nn.Namespace = ent.GetNamespace()
		}
		var cp konnectv1alpha1.KonnectControlPlane
		if err := cl.Get(ctx, nn, &cp); err != nil {
			return types.NamespacedName{}, fmt.Errorf("failed to get ControlPlane %s", nn)
		}
		return types.NamespacedName{
			Name: cp.GetKonnectAPIAuthConfigurationRef().Name,
			// TODO(pmalek): enable if cross namespace refs are allowed
			Namespace: cp.GetNamespace(),
		}, nil
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

func getControlPlaneRef[T SupportedKonnectEntityType, TEnt EntityType[T]](
	e TEnt,
) mo.Option[configurationv1alpha1.ControlPlaneRef] {
	switch e := any(e).(type) {
	case *konnectv1alpha1.KonnectControlPlane:
		return mo.None[configurationv1alpha1.ControlPlaneRef]()
	case *configurationv1alpha1.KongService:
		return mo.Some(e.Spec.ControlPlaneRef)
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
		nn := types.NamespacedName{
			Name:      cpRef.KonnectNamespacedRef.Name,
			Namespace: cpRef.KonnectNamespacedRef.Namespace,
		}
		if nn.Namespace == "" {
			nn.Namespace = ent.GetNamespace()
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

		type EntityWithControlPlaneRef interface {
			SetControlPlaneID(string)
			GetControlPlaneID() string
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
