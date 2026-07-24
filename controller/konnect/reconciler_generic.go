package konnect

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apiconsts "github.com/kong/kong-operator/v2/api/common/consts"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/controller/konnect/constraints"
	"github.com/kong/kong-operator/v2/controller/konnect/ops"
	sdkops "github.com/kong/kong-operator/v2/controller/konnect/ops/sdk"
	"github.com/kong/kong-operator/v2/controller/konnect/server"
	"github.com/kong/kong-operator/v2/controller/pkg/controlplane"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	"github.com/kong/kong-operator/v2/controller/pkg/patch"
	"github.com/kong/kong-operator/v2/internal/metrics"
	"github.com/kong/kong-operator/v2/internal/utils/crossnamespace"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

const (
	// KonnectCleanupFinalizer is the finalizer that is added to the Konnect
	// entities when they are created in Konnect, and which is removed when
	// the CR and Konnect entity are deleted.
	KonnectCleanupFinalizer = "gateway.konghq.com/konnect-cleanup"
)

// KonnectEntityReconciler reconciles a Konnect entities.
// It uses the generic type constraints to constrain the supported types.
type KonnectEntityReconciler[T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T]] struct {
	sdkFactory        sdkops.SDKFactory
	ControllerOptions controller.Options
	Client            client.Client
	LoggingMode       logging.Mode
	SyncPeriod        time.Duration

	MetricRecorder metrics.Recorder

	// pendingKonnectIDs holds the Konnect ID of entities created in Konnect whose
	// ID has not yet been persisted to their status. It lets the cleanup logic
	// recover and delete a Konnect entity even if the status update that would
	// persist its ID fails, preventing orphaned entities.
	pendingKonnectIDs *pendingKonnectIDStore
}

// KonnectEntityReconcilerOption is a functional option for the KonnectEntityReconciler.
type KonnectEntityReconcilerOption[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
] func(*KonnectEntityReconciler[T, TEnt])

// WithKonnectEntitySyncPeriod sets the sync period for the reconciler.
func WithKonnectEntitySyncPeriod[T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T]](
	d time.Duration,
) KonnectEntityReconcilerOption[T, TEnt] {
	return func(r *KonnectEntityReconciler[T, TEnt]) {
		r.SyncPeriod = d
	}
}

// WithControllerOptions sets the controller options for the reconciler.
func WithControllerOptions[T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T]](
	controllerOptions controller.Options,
) KonnectEntityReconcilerOption[T, TEnt] {
	return func(r *KonnectEntityReconciler[T, TEnt]) {
		r.ControllerOptions = controllerOptions
	}
}

// WithMetricRecorder sets the metric recorder to record metrics of Konnect entity operations of the reconciler.
func WithMetricRecorder[T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T]](
	metricRecorder metrics.Recorder,
) KonnectEntityReconcilerOption[T, TEnt] {
	return func(r *KonnectEntityReconciler[T, TEnt]) {
		r.MetricRecorder = metricRecorder
	}
}

// NewKonnectEntityReconciler returns a new KonnectEntityReconciler for the given
// Konnect entity type.
func NewKonnectEntityReconciler[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](
	sdkFactory sdkops.SDKFactory,
	loggingMode logging.Mode,
	client client.Client,
	opts ...KonnectEntityReconcilerOption[T, TEnt],
) *KonnectEntityReconciler[T, TEnt] {
	r := &KonnectEntityReconciler[T, TEnt]{
		sdkFactory:        sdkFactory,
		LoggingMode:       loggingMode,
		Client:            client,
		SyncPeriod:        consts.DefaultKonnectSyncPeriod,
		MetricRecorder:    nil,
		pendingKonnectIDs: newPendingKonnectIDStore(),
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// SetupWithManager sets up the controller with the given manager.
func (r *KonnectEntityReconciler[T, TEnt]) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	var (
		e              T
		ent            = TEnt(&e)
		entityTypeName = constraints.EntityTypeName[T]()
		b              = ctrl.
				NewControllerManagedBy(mgr).
				Named(entityTypeName).
				WithOptions(r.ControllerOptions)
	)

	for _, dep := range ReconciliationWatchOptionsForEntity(r.Client, ent) {
		b = dep(b)
	}
	return b.Complete(reconcile.AsReconciler(r.Client, r))
}

// Reconcile reconciles the given Konnect entity.
func (r *KonnectEntityReconciler[T, TEnt]) Reconcile(ctx context.Context, ent TEnt) (ctrl.Result, error) {
	var (
		entityTypeName = constraints.EntityTypeName[T]()
		logger         = log.GetLogger(ctx, entityTypeName, r.LoggingMode)
	)

	if id := ent.GetKonnectStatus().GetKonnectID(); id != "" {
		logger = logger.WithValues("konnect_id", id)
	}
	ctx = ctrllog.IntoContext(ctx, logger)
	log.Debug(logger, "reconciling")

	// If a type has a ControlPlane ref, handle it.
	res, err := handleControlPlaneRef(ctx, r.Client, ent)
	if err != nil || !res.IsZero() {
		// If the referenced ControlPlane is not found, remove the finalizer and update the status.
		// There's no need to remove the entity on Konnect because the ControlPlane
		// does not exist anymore.
		if _, ok := errors.AsType[controlplane.ReferencedControlPlaneDoesNotExistError](err); ok {
			if controllerutil.RemoveFinalizer(ent, KonnectCleanupFinalizer) {
				if err := r.Client.Update(ctx, ent); err != nil {
					if apierrors.IsConflict(err) {
						return ctrl.Result{Requeue: true}, nil
					}
					// in case the finalizer removal fails because the resource does not exist, ignore the error.
					if apierrors.IsNotFound(err) {
						return ctrl.Result{}, nil
					}
					return ctrl.Result{}, fmt.Errorf("failed to remove finalizer %s: %w", KonnectCleanupFinalizer, err)
				}
			}
		}
		return patchWithProgrammedStatusConditionBasedOnOtherConditions(ctx, r.Client, ent)
	}

	if stop, res, err := r.handleGeneratedTypeParentReferences(ctx, ent); stop || err != nil {
		return res, err
	}

	// For KongPluginBinding, verify the pluginRef early: check plugin existence for all refs,
	// and additionally check the KongReferenceGrant for cross-namespace refs. Running this
	// before the Konnect SDK ops layer ensures that grant/plugin changes are reflected
	// immediately via the watch-triggered enqueue rather than waiting for the sync period.
	if res, stop, err := handlePluginRef(ctx, r.Client, ent); err != nil || !res.IsZero() {
		return res, err
	} else if stop {
		return patchWithProgrammedStatusConditionBasedOnOtherConditions(ctx, r.Client, ent)
	}

	// If a type has a KongService ref, handle it.
	res, err = handleKongServiceRef(ctx, r.Client, ent)
	if err != nil {
		_, kongServiceIsBeingDeleted := errors.AsType[ReferencedKongServiceIsBeingDeletedError](err)
		_, referencedObjectDoesNotExist := errors.AsType[ReferencedObjectDoesNotExistError](err)
		_, referencedCPDoesNotExist := errors.AsType[controlplane.ReferencedControlPlaneDoesNotExistError](err)
		switch {
		// In case the referenced KongService is being deleted, disregard the error
		// and continue.
		case kongServiceIsBeingDeleted:
			log.Info(logger, "referenced KongService is being deleted, proceeding with reconciliation", "error", err.Error())
		case referencedObjectDoesNotExist, referencedCPDoesNotExist:
			if controllerutil.RemoveFinalizer(ent, KonnectCleanupFinalizer) {
				if err := r.Client.Update(ctx, ent); err != nil {
					if apierrors.IsConflict(err) {
						return ctrl.Result{RequeueAfter: time.Second}, nil
					}
					// in case the finalizer removal fails because the resource does not exist, ignore the error.
					if apierrors.IsNotFound(err) {
						return ctrl.Result{}, nil
					}
					return ctrl.Result{}, fmt.Errorf("failed to remove finalizer %s: %w", KonnectCleanupFinalizer, err)
				}
			}
			return ctrl.Result{}, nil

		case crossnamespace.IsReferenceNotGranted(err):
			if res, errStatus := patch.StatusWithCondition(
				ctx, r.Client, ent,
				apiconsts.ConditionType(configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs),
				metav1.ConditionFalse,
				configurationv1alpha1.KongReferenceGrantReasonRefNotPermitted,
				err.Error(),
			); errStatus != nil || !res.IsZero() {
				return res, errStatus
			}
			return patchWithProgrammedStatusConditionBasedOnOtherConditions(ctx, r.Client, ent)

		default:
			log.Error(logger, err, "error handling KongService ref")
			return ctrl.Result{}, err
		}
	} else if !res.IsZero() {
		// If the result is not zero (e.g., requeue), we still need to update the Programmed
		// status condition based on other conditions.
		if _, errStatus := patchWithProgrammedStatusConditionBasedOnOtherConditions(ctx, r.Client, ent); errStatus != nil {
			return ctrl.Result{}, errStatus
		}
		return res, nil
	}
	// If a type has a KongConsumer ref, handle it.
	res, err = handleKongConsumerRef(ctx, r.Client, ent)
	if err != nil {
		// If the referenced KongConsumer is being deleted and the object
		// is not being deleted yet then requeue until it will
		// get the deletion timestamp set due to having the owner set to KongConsumer.
		if errDel, ok := errors.AsType[ReferencedKongConsumerIsBeingDeletedError](err); ok &&
			ent.GetDeletionTimestamp().IsZero() {
			return ctrl.Result{
				RequeueAfter: time.Until(errDel.DeletionTimestamp),
			}, nil
		}

		_, referencedCPDoesNotExist := errors.AsType[controlplane.ReferencedControlPlaneDoesNotExistError](err)
		_, referencedKongConsumerDoesNotExist := errors.AsType[ReferencedKongConsumerDoesNotExistError](err)
		// If the referenced KongConsumer is not found or is being deleted
		// then remove the finalizer and let the deletion proceed without trying to delete the entity from Konnect
		// as the KongConsumer deletion will (or already has - in case of the consumer being gone)
		// take care of it on the Konnect side.
		if referencedCPDoesNotExist || referencedKongConsumerDoesNotExist {
			if controllerutil.RemoveFinalizer(ent, KonnectCleanupFinalizer) {
				if err := r.Client.Update(ctx, ent); err != nil {
					if apierrors.IsConflict(err) {
						return ctrl.Result{Requeue: true}, nil
					}
					// in case the finalizer removal fails because the resource does not exist, ignore the error.
					if apierrors.IsNotFound(err) {
						return ctrl.Result{}, nil
					}
					return ctrl.Result{}, fmt.Errorf("failed to remove finalizer %s: %w", KonnectCleanupFinalizer, err)
				}
				log.Debug(logger, "finalizer removed as the owning KongConsumer is being deleted or is already gone",
					"finalizer", KonnectCleanupFinalizer,
				)
			}
			return ctrl.Result{}, nil
		}

		return patchWithProgrammedStatusConditionBasedOnOtherConditions(ctx, r.Client, ent)
	} else if !res.IsZero() {
		// If the result is not zero (e.g., requeue), we still need to update the Programmed
		// status condition based on other conditions.
		if _, errStatus := patchWithProgrammedStatusConditionBasedOnOtherConditions(ctx, r.Client, ent); errStatus != nil {
			return ctrl.Result{}, errStatus
		}
		return res, nil
	}

	// If a type has a KongUpstream ref (KongTarget), handle it.
	res, err = handleKongUpstreamRef(ctx, r.Client, ent)
	if err != nil {
		// If the referenced KongUpstream is being deleted and the object
		// is not being deleted yet then requeue until it will
		// get the deletion timestamp set due to having the owner set to KongUpstream.
		if errDel, ok := errors.AsType[ReferencedKongUpstreamIsBeingDeletedError](err); ok &&
			ent.GetDeletionTimestamp().IsZero() {
			return ctrl.Result{
				RequeueAfter: time.Until(errDel.DeletionTimestamp),
			}, nil
		}

		// If the referenced KongUpstream is not found then remove the finalizer
		// and let the deletion proceed without trying to delete the entity from Konnect
		// as the KongUpstream deletion will (or already has - in case of the upstream being gone)
		// take care of it on the Konnect side.
		// In case the ControlPlane referenced by the KongUpstream is not found, do the same.
		_, upstreamNotExist := errors.AsType[ReferencedKongUpstreamDoesNotExistError](err)
		_, cpNotExist := errors.AsType[controlplane.ReferencedControlPlaneDoesNotExistError](err)
		if upstreamNotExist || cpNotExist {
			if controllerutil.RemoveFinalizer(ent, KonnectCleanupFinalizer) {
				if err := r.Client.Update(ctx, ent); err != nil {
					if apierrors.IsConflict(err) {
						return ctrl.Result{Requeue: true}, nil
					}
					return ctrl.Result{}, fmt.Errorf("failed to remove finalizer %s: %w", KonnectCleanupFinalizer, err)
				}
				log.Debug(logger, "finalizer removed as the owning KongUpstream is being deleted or is already gone",
					"finalizer", KonnectCleanupFinalizer,
				)
			}
		}

		return patchWithProgrammedStatusConditionBasedOnOtherConditions(ctx, r.Client, ent)
	} else if !res.IsZero() {
		// If the result is not zero (e.g., requeue), we still need to update the Programmed
		// status condition based on other conditions.
		if _, errStatus := patchWithProgrammedStatusConditionBasedOnOtherConditions(ctx, r.Client, ent); errStatus != nil {
			return ctrl.Result{}, errStatus
		}
		return res, nil
	}

	// If a type has a KongCertificateRef (KongCertificate), handle it.
	res, err = handleKongCertificateRef(ctx, r.Client, ent)
	if err != nil {
		// If the referenced KongCertificate is being deleted and the object
		// is not being deleted yet then requeue until it will
		// get the deletion timestamp set due to having the owner set to KongCertificate.
		if errDel, ok := errors.AsType[ReferencedKongCertificateIsBeingDeletedError](err); ok &&
			ent.GetDeletionTimestamp().IsZero() {
			return ctrl.Result{
				RequeueAfter: time.Until(errDel.DeletionTimestamp),
			}, nil
		}

		_, referencedCPDoesNotExist := errors.AsType[controlplane.ReferencedControlPlaneDoesNotExistError](err)
		_, referencedKongCertificateDoesNotExist := errors.AsType[ReferencedKongCertificateDoesNotExistError](err)

		// If the referenced KongCertificate is not found or is being deleted
		// and the object is being deleted, remove the finalizer and let the
		// deletion proceed without trying to delete the entity from Konnect
		// as the KongCertificate deletion will take care of it on the Konnect side.
		if referencedKongCertificateDoesNotExist || referencedCPDoesNotExist {
			if controllerutil.RemoveFinalizer(ent, KonnectCleanupFinalizer) {
				if err := r.Client.Update(ctx, ent); err != nil {
					if apierrors.IsConflict(err) {
						return ctrl.Result{Requeue: true}, nil
					}
					// in case the finalizer removal fails because the resource does not exist, ignore the error.
					if apierrors.IsNotFound(err) {
						return ctrl.Result{}, nil
					}
					return ctrl.Result{}, fmt.Errorf("failed to remove finalizer %s: %w", KonnectCleanupFinalizer, err)
				}
				log.Debug(logger, "finalizer removed as the owning KongCertificate is being deleted or is already gone",
					"finalizer", KonnectCleanupFinalizer,
				)
			}
		}

		if crossnamespace.IsReferenceNotGranted(err) {
			if res, errStatus := patch.StatusWithCondition(
				ctx, r.Client, ent,
				apiconsts.ConditionType(configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs),
				metav1.ConditionFalse,
				configurationv1alpha1.KongReferenceGrantReasonRefNotPermitted,
				err.Error(),
			); errStatus != nil || !res.IsZero() {
				return res, errStatus
			}
			return ctrl.Result{}, err
		}

		return patchWithProgrammedStatusConditionBasedOnOtherConditions(ctx, r.Client, ent)
	} else if !res.IsZero() {
		// If the result is not zero (e.g., requeue), we still need to update the Programmed
		// status condition based on other conditions (e.g., KongCertificateRefValid set to False
		// when referenced KongCertificate is not programmed yet).
		// We patch the status but still return the original requeue result.
		if _, errStatus := patchWithProgrammedStatusConditionBasedOnOtherConditions(ctx, r.Client, ent); errStatus != nil {
			return ctrl.Result{}, errStatus
		}
		return res, nil
	}

	// If a type has a KongKeySet ref, handle it.
	res, err = handleKongKeySetRef(ctx, r.Client, ent)
	if err != nil || !res.IsZero() {
		// If the referenced KongKeySet is being deleted and the object
		// is not being deleted yet then requeue until it will
		// get the deletion timestamp set due to having the owner set to KongKeySet.
		if errDel, ok := errors.AsType[ReferencedKongKeySetIsBeingDeletedError](err); ok &&
			ent.GetDeletionTimestamp().IsZero() {
			return ctrl.Result{
				RequeueAfter: time.Until(errDel.DeletionTimestamp),
			}, nil
		}

		// If the referenced KongKeySet is not found, remove the finalizer and let the
		// user delete the resource without trying to delete the entity from Konnect
		// as the KongKeySet deletion will take care of it on the Konnect side.
		if _, ok := errors.AsType[ReferencedKongKeySetDoesNotExistError](err); ok {
			if controllerutil.RemoveFinalizer(ent, KonnectCleanupFinalizer) {
				if err := r.Client.Update(ctx, ent); err != nil {
					if apierrors.IsConflict(err) {
						return ctrl.Result{Requeue: true}, nil
					}
					return ctrl.Result{}, fmt.Errorf("failed to remove finalizer %s: %w", KonnectCleanupFinalizer, err)
				}
				log.Debug(logger, "finalizer removed as the owning KongKeySet is being deleted or is already gone",
					"finalizer", KonnectCleanupFinalizer,
				)
				return ctrl.Result{}, nil
			}
		}

		return patchWithProgrammedStatusConditionBasedOnOtherConditions(ctx, r.Client, ent)
	}

	// If a type has KongCACertificate refs (KongService), handle them.
	res, err = handleKongCACertificateRefs(ctx, r.Client, ent)
	if err != nil {
		if errDel, ok := errors.AsType[ReferencedObjectIsBeingDeletedError](err); ok &&
			ent.GetDeletionTimestamp().IsZero() {
			return ctrl.Result{
				RequeueAfter: time.Until(errDel.DeletionTimestamp),
			}, nil
		}

		_, referencedDoesNotExist := errors.AsType[ReferencedKongCACertificateDoesNotExistError](err)
		if referencedDoesNotExist {
			if controllerutil.RemoveFinalizer(ent, KonnectCleanupFinalizer) {
				if err := r.Client.Update(ctx, ent); err != nil {
					if apierrors.IsConflict(err) {
						return ctrl.Result{Requeue: true}, nil
					}
					if apierrors.IsNotFound(err) {
						return ctrl.Result{}, nil
					}
					return ctrl.Result{}, fmt.Errorf("failed to remove finalizer %s: %w", KonnectCleanupFinalizer, err)
				}
				log.Debug(logger, "finalizer removed as the owning KongCACertificate is being deleted or is already gone",
					"finalizer", KonnectCleanupFinalizer,
				)
			}
		}

		if crossnamespace.IsReferenceNotGranted(err) {
			if res, errStatus := patch.StatusWithCondition(
				ctx, r.Client, ent,
				apiconsts.ConditionType(configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs),
				metav1.ConditionFalse,
				configurationv1alpha1.KongReferenceGrantReasonRefNotPermitted,
				err.Error(),
			); errStatus != nil || !res.IsZero() {
				return res, errStatus
			}
			return ctrl.Result{}, err
		}

		return patchWithProgrammedStatusConditionBasedOnOtherConditions(ctx, r.Client, ent)
	} else if !res.IsZero() {
		if _, errStatus := patchWithProgrammedStatusConditionBasedOnOtherConditions(ctx, r.Client, ent); errStatus != nil {
			return ctrl.Result{}, errStatus
		}
		return res, nil
	}

	// If a type has a Secret ref, handle it.
	res, stop, err := handleSecretRef(ctx, r.Client, ent)
	if err != nil || !res.IsZero() {
		return res, err
	}
	if stop {
		return patchWithProgrammedStatusConditionBasedOnOtherConditions(ctx, r.Client, ent)
	}

	programmedFalseCondition := metav1.Condition{
		Type:    konnectv1alpha1.KonnectEntityProgrammedConditionType,
		Status:  metav1.ConditionFalse,
		Reason:  konnectv1alpha1.KonnectEntityProgrammedReasonConditionWithStatusFalseExists,
		Message: "Some conditions have status set to False",
	}

	apiAuthRef, err := GetAPIAuthRefNN(ctx, r.Client, ent)
	if err != nil {
		// Entities such as KongTargets resolve API authentication through their
		// parent resource. The parent ControlPlane can disappear between the
		// reference checks above and this lookup. In that case the ControlPlane's
		// deletion has already removed its Konnect entities, so keeping the child
		// cleanup finalizer would only leave the Kubernetes resource terminating
		// forever.
		if handled, res, finalizerErr := removeCleanupFinalizerIfControlPlaneIsGone(ctx, r.Client, ent, err); handled {
			return res, finalizerErr
		}

		if crossnamespace.IsReferenceNotGranted(err) {
			log.Info(logger, "cross-namespace reference to KonnectAPIAuthConfiguration is not granted", "error", err.Error())
			if requeue, res, retErr := handleAPIAuthStatusCondition(ctx, r.Client, ent, konnectv1alpha1.KonnectAPIAuthConfiguration{}, apiAuthRef, err, programmedFalseCondition); requeue {
				return res, retErr
			}
		}

		return ctrl.Result{}, fmt.Errorf("failed to get APIAuth ref for %s: %w", client.ObjectKeyFromObject(ent), err)
	}

	var apiAuth konnectv1alpha1.KonnectAPIAuthConfiguration
	err = r.Client.Get(ctx, apiAuthRef, &apiAuth)
	if requeue, res, retErr := handleAPIAuthStatusCondition(ctx, r.Client, ent, apiAuth, apiAuthRef, err, programmedFalseCondition); requeue {
		return res, retErr
	}

	token, err := GetTokenFromKonnectAPIAuthConfiguration(ctx, r.Client, &apiAuth)
	if err != nil {
		if res, errStatus := patch.StatusWithCondition(
			ctx, r.Client, &apiAuth,
			konnectv1alpha1.KonnectEntityAPIAuthConfigurationValidConditionType,
			metav1.ConditionFalse,
			konnectv1alpha1.KonnectEntityAPIAuthConfigurationReasonInvalid,
			err.Error(),
		); errStatus != nil || !res.IsZero() {
			return res, errStatus
		}
		return ctrl.Result{}, err
	}

	// NOTE: We need to create a new SDK instance for each reconciliation
	// because the token is retrieved in runtime through KonnectAPIAuthConfiguration.
	server, err := server.NewServer[T](apiAuth.Spec.ServerURL)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to parse server URL: %w", err)
	}
	sdk := r.sdkFactory.NewKonnectSDK(server, sdkops.SDKToken(token))

	// If a type has a KonnectCloudGatewayNetwork ref, handle it.
	res, err = handleKonnectNetworkRef(ctx, r.Client, ent, sdk)
	if err != nil || !res.IsZero() {
		// NOTE: If the referenced network is being deleted and the object
		// is being deleted then allow the reconciliation to continue as we want to
		// proceed with object's deletion.
		// Otherwise, just return the error and requeue.
		if _, ok := errors.AsType[ReferencedObjectIsBeingDeletedError](err); !ok ||
			ent.GetDeletionTimestamp().IsZero() {
			log.Debug(logger, "error handling KonnectNetwork ref", "error", err)
			return patchWithProgrammedStatusConditionBasedOnOtherConditions(ctx, r.Client, ent)
		}
		if !res.IsZero() {
			return res, err
		}
	}

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
			// If the Konnect ID was never persisted to the status (e.g. the status
			// update failed after the entity was created in Konnect), try to recover
			// it from the in-memory store so the entity can still be cleaned up.
			r.restorePendingKonnectIDForDeletion(ent)

			if err := ops.Delete(ctx, sdk, r.Client, r.MetricRecorder, ent); err != nil {
				// If the error was a network error, handle it here, there's no need to proceed,
				// as no state has changed.
				// Status conditions are updated in handleOpsErr.
				if errURL, ok := errors.AsType[*url.Error](err); ok {
					return r.handleOpsErr(ctx, ent, errURL)
				}

				// If the error is a rate limit error, requeue after the retry-after duration
				// instead of returning an error.
				if retryAfter, isRateLimited := ops.GetRetryAfterFromRateLimitError(err); isRateLimited {
					logger.Info("rate limited by Konnect API during delete, requeueing", "retry_after", retryAfter.String())
					return ctrl.Result{RequeueAfter: retryAfter}, nil
				}
				if res, errStatus := patch.StatusWithCondition(
					ctx, r.Client, ent,
					konnectv1alpha1.KonnectEntityProgrammedConditionType,
					metav1.ConditionFalse,
					konnectv1alpha1.KonnectEntityProgrammedReasonKonnectAPIOpFailed,
					err.Error(),
				); errStatus != nil || !res.IsZero() {
					return res, errStatus
				}
				return ctrl.Result{}, err
			}

			// The Konnect entity has been deleted (or there was nothing to delete);
			// drop any in-memory copy of its ID.
			r.pendingKonnectIDs.Delete(client.ObjectKeyFromObject(ent))
		}

		// For KonnectGatewayControlPlane resources, also remove the finalizer added
		// by the Gateway controller. This handles the race condition where a Gateway
		// is deleted before the Konnect reconciler adds its own finalizer, leaving
		// only the Gateway controller's finalizer on the KonnectGatewayControlPlane
		// with no controller able to remove it (since the parent Gateway is gone).
		if _, ok := any(ent).(*konnectv1alpha2.KonnectGatewayControlPlane); ok {
			controllerutil.RemoveFinalizer(ent, consts.KonnectGatewayControlPlaneFinalizer)
		}

		if err := r.Client.Update(ctx, ent); err != nil {
			if apierrors.IsConflict(err) {
				return ctrl.Result{Requeue: true}, nil
			}
			if apierrors.IsNotFound(err) {
				return ctrl.Result{}, nil
			}
			return ctrl.Result{}, fmt.Errorf("failed to update resource after finalizer removal: %w", err)
		}

		return ctrl.Result{}, nil
	}

	// Add the cleanup finalizer before creating anything in Konnect. This
	// guarantees that, even if the operator is interrupted right after the
	// Konnect entity is created, there is always a finalizer that triggers the
	// cleanup logic on deletion, so the Konnect entity can never be orphaned.
	if _, res, err := patch.WithFinalizer(ctx, r.Client, ent, KonnectCleanupFinalizer); err != nil || !res.IsZero() {
		return res, err
	}

	// Handle type specific operations and stop reconciliation if needed.
	// This can happen for instance when KongConsumer references credentials Secrets
	// that do not exist or populate some Status fields based on Konnect API.
	if stop, res, err := handleTypeSpecific(ctx, sdk, r.Client, ent); err != nil {
		// If the error was a network error, handle it here, there's no need to proceed,
		// as no state has changed.
		// Status conditions are updated in handleOpsErr.
		if errURL, ok := errors.AsType[*url.Error](err); ok {
			return r.handleOpsErr(ctx, ent, errURL)
		}
		if errMatchingIDNotFound, ok := errors.AsType[ops.EntityWithMatchingIDNotFoundError](err); ok {
			// If the error is that the entity with the matching ID was not found,
			// in Konnect, it means that it was deleted from Konnect.
			// We continue with the reconciliation which will recreate the entity on Konnect
			// and update the status with the new Konnect ID.
			logger.Info(
				"Konnect entity with matching ID not found on Konnect, it might have been deleted. Continuing reconciliation to recreate it.",
				"konnect_id", errMatchingIDNotFound.ID,
			)
			ent.SetKonnectID("")
		} else {
			return ctrl.Result{}, err
		}
	} else if !res.IsZero() || stop {
		return res, nil
	}

	// Reconcile the cached view of the object with what we know we have already
	// created in Konnect, so a stale cache cannot drive a duplicate create.
	if res, stop, err := r.reconcilePendingKonnectID(ctx, ent); stop {
		return res, err
	}

	if shouldCreateKonnectEntity(ent) {

		// Check if the object is adopting an existing Konnect entity.
		if adoptable, ok := any(ent).(constraints.KonnectEntityTypeSupportingAdoption); ok {
			if adoptOptions := adoptable.GetAdoptOptions(); adoptOptions != nil && adoptOptions.Konnect != nil {
				return r.adoptFromExistingEntity(ctx, sdk, ent, adoptOptions, &apiAuth, server)
			}
		}

		obj := ent.DeepCopyObject().(client.Object)

		_, err := ops.Create(ctx, sdk, r.Client, r.MetricRecorder, ent)

		// Add the Org ID and Server URL to the status so that the status can
		// indicate where the corresponding Konnect entity is located. The cleanup
		// finalizer has already been added before the create above.
		setStatusServerURLAndOrgID(ent, server, apiAuth.Status.OrganizationID)

		// If the entity was created in Konnect, record its Konnect ID in the
		// in-memory store. The entry is intentionally kept (not purged here) until a
		// later reconcile observes the persisted ID on the cached status (see the
		// reconciliation block above) or the entity is deleted. This serves two
		// purposes:
		//   - cleanup: the deletion logic can find and delete the Konnect entity even
		//     if the status update below fails (preventing orphaned entities);
		//   - de-duplication: a subsequent reconcile reading a stale, ID-less cached
		//     status will restore the ID from here instead of creating a duplicate.
		// Only the entity's own ID can be lost here; parent references are persisted
		// by their reference handlers in earlier reconcile passes.
		if id := ent.GetKonnectStatus().GetKonnectID(); id != "" {
			r.pendingKonnectIDs.Store(client.ObjectKeyFromObject(ent), id)
		}

		// Regardless of the error, patch the status as it can contain the Konnect ID,
		// Org ID, Server URL and status conditions.
		// Konnect ID will be needed for the finalizer to work.
		if _, err := patch.ApplyStatusPatchIfNotEmpty(ctx, r.Client, logger, any(ent).(client.Object), obj); err != nil {
			if apierrors.IsConflict(err) {
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, fmt.Errorf("failed to update status after creating object: %w", err)
		}

		if err != nil {
			var (
				errURL, okURL                = errors.AsType[*url.Error](err)
				rateLimitErr, okRateLimitErr = errors.AsType[ops.RateLimitError](err)
				referenceErr, okReferenceErr = errors.AsType[ops.ReferenceError](err)
			)
			switch {
			// If the error was a network error, handle it here, there's no need to proceed,
			// as no state has changed.
			// Status conditions are updated in handleOpsErr.
			case okURL:
				return r.handleOpsErr(ctx, ent, errURL)

			// If the error is a rate limit error, requeue after the retry-after duration
			// instead of returning an error.
			case okRateLimitErr:
				return ctrl.Result{RequeueAfter: rateLimitErr.RetryAfter}, nil

			// A ReferenceError means Konnect returned a 400 with only ERROR_TYPE_REFERENCE
			// details — transient for HybridGateway-managed resources (old entities being
			// cleaned up, e.g. after an HTTPRoute/TLSRoute change). For user-created
			// resources the same error shape can represent a permanent misconfiguration, so
			// we suppress it instead of requeueing to avoid hammering the Konnect API.
			case okReferenceErr:
				if isHybridGatewayManaged(ent) {
					return ctrl.Result{RequeueAfter: referenceErr.RetryAfter}, nil
				}
				return ctrl.Result{}, nil
			}

			return ctrl.Result{}, ops.FailedKonnectOpError[T]{
				Op:  ops.CreateOp,
				Err: err,
			}
		}

		// NOTE: we don't need to requeue here because the object update will trigger another reconciliation.
		return ctrl.Result{}, nil
	}

	res, err = ops.Update(ctx, sdk, r.SyncPeriod, r.Client, r.MetricRecorder, ent)

	// Set the server URL and org ID regardless of the error.
	setStatusServerURLAndOrgID(ent, server, apiAuth.Status.OrganizationID)
	// Update the status of the object regardless of the error.
	if errUpd := r.Client.Status().Update(ctx, ent); errUpd != nil {
		if apierrors.IsConflict(errUpd) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to update in cluster resource after Konnect update: %w %w", errUpd, err)
	}
	if err != nil {
		logger.Error(err, "failed to update")

		var (
			errURL, okURL                = errors.AsType[*url.Error](err)
			rateLimitErr, okRateLimitErr = errors.AsType[ops.RateLimitError](err)
			referenceErr, okReferenceErr = errors.AsType[ops.ReferenceError](err)
		)
		switch {
		// If the error was a network error, handle it here, there's no need to proceed,
		// as no state has changed.
		// Status conditions are updated in handleOpsErr.
		case okURL:
			return r.handleOpsErr(ctx, ent, errURL)

		// If the error is a rate limit error, requeue after the retry-after duration
		// instead of returning an error.
		case okRateLimitErr:
			return ctrl.Result{RequeueAfter: rateLimitErr.RetryAfter}, nil

		// A ReferenceError means Konnect returned a 400 with only ERROR_TYPE_REFERENCE
		// details — transient for HybridGateway-managed resources (old entities being
		// cleaned up, e.g. after an HTTPRoute/TLSRoute change). For user-created
		// resources the same error shape can represent a permanent misconfiguration, so
		// we suppress it instead of requeueing to avoid hammering the Konnect API.
		case okReferenceErr:
			if isHybridGatewayManaged(ent) {
				return ctrl.Result{RequeueAfter: referenceErr.RetryAfter}, nil
			}
			return ctrl.Result{}, nil
		}

	} else if !res.IsZero() {
		return res, nil
	}

	// NOTE: We requeue here to keep enforcing the state of the resource in Konnect.
	// Konnect does not allow subscribing to changes so we need to keep pushing the
	// desired state periodically.
	return ctrl.Result{
		RequeueAfter: r.SyncPeriod,
	}, nil
}

func removeCleanupFinalizerIfControlPlaneIsGone[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](
	ctx context.Context,
	cl client.Client,
	ent TEnt,
	err error,
) (bool, ctrl.Result, error) {
	if _, ok := errors.AsType[controlplane.ReferencedControlPlaneDoesNotExistError](err); !ok {
		return false, ctrl.Result{}, nil
	}

	if controllerutil.RemoveFinalizer(ent, KonnectCleanupFinalizer) {
		if err := cl.Update(ctx, ent); err != nil {
			switch {
			case apierrors.IsConflict(err):
				return true, ctrl.Result{Requeue: true}, nil
			case apierrors.IsNotFound(err):
				return true, ctrl.Result{}, nil
			default:
				return true, ctrl.Result{}, fmt.Errorf("failed to remove finalizer %s: %w", KonnectCleanupFinalizer, err)
			}
		}
	}

	return true, ctrl.Result{}, nil
}

// reconcilePendingKonnectID reconciles the cached view of the object with the
// in-memory record of Konnect entities we have already created. Reads go through
// a cache, so right after creating an entity and persisting its Konnect ID to the
// status, a later reconcile may still observe a stale, ID-less status; creating
// again on that basis would produce a duplicate Konnect entity. The in-memory
// store records the Konnect ID as soon as the entity is created, so:
//   - if the cached status lacks the ID but the store has it, restore and persist
//     it, then return. A stale cached object can otherwise continue into update
//     logic with the recovered ID, causing nondeterministic Konnect updates before
//     the cache has observed the status write. Re-persisting is idempotent and
//     also heals the case where the post-create status write failed.
//   - once the cached status carries the ID, the bridge entry has served its
//     purpose and is dropped.
//
// It returns stop=true when the caller should return the provided result/error (a
// conflict requeue, a persist error, or a successful pending-ID recovery);
// otherwise reconciliation continues.
func (r *KonnectEntityReconciler[T, TEnt]) reconcilePendingKonnectID(
	ctx context.Context,
	ent TEnt,
) (ctrl.Result, bool, error) {
	pendingKey := client.ObjectKeyFromObject(ent)
	if ent.GetKonnectStatus().GetKonnectID() == "" {
		if id, ok := r.pendingKonnectIDs.Get(pendingKey); ok {
			old := ent.DeepCopyObject().(client.Object)
			ent.SetKonnectID(id)
			if _, err := patch.ApplyStatusPatchIfNotEmpty(ctx, r.Client, ctrllog.FromContext(ctx), any(ent).(client.Object), old); err != nil {
				if apierrors.IsConflict(err) {
					return ctrl.Result{Requeue: true}, true, nil
				}
				return ctrl.Result{}, true, fmt.Errorf("failed to persist recovered Konnect ID for %s: %w", pendingKey, err)
			}
			return ctrl.Result{}, true, nil
		}
	} else {
		r.pendingKonnectIDs.Delete(pendingKey)
	}
	return ctrl.Result{}, false, nil
}

// restorePendingKonnectIDForDeletion restores the Konnect ID from the in-memory
// store onto the object when its status does not carry one, so the deletion logic
// can clean up the corresponding Konnect entity. If the ID is not in memory either
// (e.g. after an operator restart), ops.Delete falls back to probing Konnect by
// Kubernetes UID. Parent references needed for deletion are already on the object,
// persisted by their reference handlers in earlier reconcile passes.
func (r *KonnectEntityReconciler[T, TEnt]) restorePendingKonnectIDForDeletion(ent TEnt) {
	if ent.GetKonnectStatus().GetKonnectID() != "" {
		return
	}
	if id, ok := r.pendingKonnectIDs.Get(client.ObjectKeyFromObject(ent)); ok {
		ent.SetKonnectID(id)
	}
}

func shouldCreateKonnectEntity[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](ent TEnt) bool {
	status := ent.GetKonnectStatus()
	if status == nil {
		return true
	}
	if status.GetKonnectID() != "" {
		return false
	}
	if ops.EntityPersistsKonnectID(ent) {
		return true
	}
	return !entityHasProgrammedStatusTrue(ent)
}

func entityHasProgrammedStatusTrue[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](ent TEnt) bool {
	cond, ok := k8sutils.GetCondition(konnectv1alpha1.KonnectEntityProgrammedConditionType, ent)
	return ok && cond.Status == metav1.ConditionTrue
}

// adoptFromExistingEntity adopts the existing entity from Konnect based on reconciled object
// if it is not attached to the existing entity yet, and sets the status and finalizers.
func (r *KonnectEntityReconciler[T, TEnt]) adoptFromExistingEntity(
	ctx context.Context,
	sdk sdkops.SDKWrapper,
	ent TEnt,
	adoptOptions *commonv1alpha1.AdoptOptions,
	apiAuth *konnectv1alpha1.KonnectAPIAuthConfiguration,
	server server.Server,
) (ctrl.Result, error) {
	var (
		entityTypeName = constraints.EntityTypeName[T]()
		logger         = log.GetLogger(ctx, entityTypeName, r.LoggingMode)
		obj            = ent.DeepCopyObject().(client.Object)
		retErr         error
	)
	status := ent.GetKonnectStatus()
	logger.Info("Adopting from existing entity",
		"type", ent.GetTypeName(), "konnect_id", adoptOptions.Konnect.ID)
	_, err := ops.Adopt(ctx, sdk, r.SyncPeriod, r.Client, r.MetricRecorder, ent, *adoptOptions)
	if err != nil {
		logger.Error(err, "failed to adopt entity", "type", ent.GetTypeName(), "konnect_id", adoptOptions.Konnect.ID)
		retErr = err
	}

	// Regardless of the error reported from Adopt(), if the Konnect ID has been
	// set then:
	// - add the finalizer so that the resource can be cleaned up from Konnect on deletion...
	if status != nil && status.ID != "" {
		if _, res, err := patch.WithFinalizer(ctx, r.Client, ent, KonnectCleanupFinalizer); err != nil || !res.IsZero() {
			return res, err
		}

		// ...
		// - add the Org ID and Server URL to the status so that the resource can be
		//   cleaned up from Konnect on deletion and also so that the status can
		//   indicate where the corresponding Konnect entity is located.
		setStatusServerURLAndOrgID(ent, server, apiAuth.Status.OrganizationID)
	}

	// Regardless of the error, patch the status as it can contain the Konnect ID,
	// Org ID, Server URL and status conditions.
	// Konnect ID will be needed for the finalizer to work.
	if res, err := patch.ApplyStatusPatchIfNotEmpty(ctx, r.Client, logger, any(ent).(client.Object), obj); err != nil {
		if apierrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to update status after creating object: %w", err)
	} else if res != op.Noop {
		return ctrl.Result{}, nil
	}

	if retErr != nil {
		// If the error is a rate limit error, requeue after the retry-after duration
		// instead of returning an error.
		if rateLimitErr, ok := errors.AsType[ops.RateLimitError](retErr); ok {
			return ctrl.Result{RequeueAfter: rateLimitErr.RetryAfter}, nil
		}
		return ctrl.Result{}, ops.FailedKonnectOpError[T]{
			Op:  ops.AdoptOp,
			Err: retErr,
		}
	}

	// NOTE: we don't need to requeue here because the object update will trigger another reconciliation.
	return ctrl.Result{}, nil
}

func setStatusServerURLAndOrgID(
	ent interface {
		GetKonnectStatus() *konnectv1alpha2.KonnectEntityStatus
	},
	serverURL server.Server,
	orgID string,
) {
	ent.GetKonnectStatus().ServerURL = serverURL.URL()
	ent.GetKonnectStatus().OrgID = orgID
}

func patchWithProgrammedStatusConditionBasedOnOtherConditions[
	T k8sutils.ConditionsAwareObject,
](
	ctx context.Context,
	cl client.Client,
	ent T,
) (ctrl.Result, error) {
	if k8sutils.AreAllConditionsHaveTrueStatus(ent) {
		return ctrl.Result{}, nil
	}

	if res, errStatus := patch.StatusWithCondition(
		ctx, cl, ent,
		konnectv1alpha1.KonnectEntityProgrammedConditionType,
		metav1.ConditionFalse,
		konnectv1alpha1.KonnectEntityProgrammedReasonConditionWithStatusFalseExists,
		"Some conditions have status set to False",
	); errStatus != nil || !res.IsZero() {
		return res, errStatus
	}
	return ctrl.Result{}, nil
}

// isHybridGatewayManaged returns true if the entity was generated by the
// HybridGateway controller, as indicated by the presence of the corresponding ownership annotation.
func isHybridGatewayManaged(obj client.Object) bool {
	annotations := obj.GetAnnotations()
	_, ok := annotations[consts.GatewayOperatorHybridGatewaysAnnotation]
	return ok
}
