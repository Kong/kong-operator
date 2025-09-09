package konnect

import (
	"context"
	"errors"
	"fmt"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	konnectv1alpha1 "github.com/kong/kong-operator/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/controller/konnect/constraints"
	"github.com/kong/kong-operator/controller/konnect/ops"
	sdkops "github.com/kong/kong-operator/controller/konnect/ops/sdk"
	"github.com/kong/kong-operator/controller/konnect/server"
	"github.com/kong/kong-operator/controller/pkg/controlplane"
	"github.com/kong/kong-operator/controller/pkg/log"
	"github.com/kong/kong-operator/controller/pkg/op"
	"github.com/kong/kong-operator/controller/pkg/patch"
	"github.com/kong/kong-operator/internal/metrics"
	"github.com/kong/kong-operator/modules/manager/logging"
	"github.com/kong/kong-operator/pkg/consts"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
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
	sdkFactory              sdkops.SDKFactory
	CacheSyncTimeout        time.Duration
	Client                  client.Client
	LoggingMode             logging.Mode
	MaxConcurrentReconciles uint
	SyncPeriod              time.Duration

	MetricRecorder metrics.Recorder
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

// WithKonnectMaxConcurrentReconciles sets the max concurrent reconciles for the reconciler.
func WithKonnectMaxConcurrentReconciles[T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T]](
	maxConcurrent uint,
) KonnectEntityReconcilerOption[T, TEnt] {
	return func(r *KonnectEntityReconciler[T, TEnt]) {
		r.MaxConcurrentReconciles = maxConcurrent
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
		sdkFactory:              sdkFactory,
		LoggingMode:             loggingMode,
		Client:                  client,
		SyncPeriod:              consts.DefaultKonnectSyncPeriod,
		MaxConcurrentReconciles: consts.DefaultKonnectMaxConcurrentReconciles,
		MetricRecorder:          nil,
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
				WithOptions(
				controller.Options{
					CacheSyncTimeout:        r.CacheSyncTimeout,
					MaxConcurrentReconciles: int(r.MaxConcurrentReconciles),
				},
			)
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
		if errors.As(err, &controlplane.ReferencedControlPlaneDoesNotExistError{}) {
			if controllerutil.RemoveFinalizer(ent, KonnectCleanupFinalizer) {
				if err := r.Client.Update(ctx, ent); err != nil {
					if k8serrors.IsConflict(err) {
						return ctrl.Result{Requeue: true}, nil
					}
					return ctrl.Result{}, fmt.Errorf("failed to remove finalizer %s: %w", KonnectCleanupFinalizer, err)
				}
			}
		}
		return patchWithProgrammedStatusConditionBasedOnOtherConditions(ctx, r.Client, ent)
	}

	// If a type has a KongService ref, handle it.
	res, err = handleKongServiceRef(ctx, r.Client, ent)
	if err != nil {
		if !errors.As(err, &ReferencedKongServiceIsBeingDeleted{}) {
			log.Error(logger, err, "error handling KongService ref")
		}
	} else if !res.IsZero() {
		return res, nil
	}
	// If a type has a KongConsumer ref, handle it.
	res, err = handleKongConsumerRef(ctx, r.Client, ent)
	if err != nil {
		// If the referenced KongConsumer is being deleted and the object
		// is not being deleted yet then requeue until it will
		// get the deletion timestamp set due to having the owner set to KongConsumer.
		if errDel := (&ReferencedKongConsumerIsBeingDeleted{}); errors.As(err, errDel) &&
			ent.GetDeletionTimestamp().IsZero() {
			return ctrl.Result{
				RequeueAfter: time.Until(errDel.DeletionTimestamp),
			}, nil
		}

		// If the referenced KongConsumer is not found or is being deleted
		// then remove the finalizer and let the deletion proceed without trying to delete the entity from Konnect
		// as the KongConsumer deletion will (or already has - in case of the consumer being gone)
		// take care of it on the Konnect side.
		if errors.As(err, &ReferencedKongConsumerDoesNotExist{}) {
			if controllerutil.RemoveFinalizer(ent, KonnectCleanupFinalizer) {
				if err := r.Client.Update(ctx, ent); err != nil {
					if k8serrors.IsConflict(err) {
						return ctrl.Result{Requeue: true}, nil
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
		return res, nil
	}

	// If a type has a KongUpstream ref (KongTarget), handle it.
	res, err = handleKongUpstreamRef(ctx, r.Client, ent)
	if err != nil {
		// If the referenced KongUpstream is being deleted and the object
		// is not being deleted yet then requeue until it will
		// get the deletion timestamp set due to having the owner set to KongUpstream.
		if errDel := (&ReferencedKongUpstreamIsBeingDeleted{}); errors.As(err, errDel) &&
			ent.GetDeletionTimestamp().IsZero() {
			return ctrl.Result{
				RequeueAfter: time.Until(errDel.DeletionTimestamp),
			}, nil
		}

		// If the referenced KongUpstream is not found then remove the finalizer
		// and let the deletion proceed without trying to delete the entity from Konnect
		// as the KongUpstream deletion will (or already has - in case of the upstream being gone)
		// take care of it on the Konnect side.
		if errors.As(err, &ReferencedKongUpstreamDoesNotExist{}) {
			if controllerutil.RemoveFinalizer(ent, KonnectCleanupFinalizer) {
				if err := r.Client.Update(ctx, ent); err != nil {
					if k8serrors.IsConflict(err) {
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
		return res, nil
	}

	// If a type has a KongCertificateRef (KongCertificate), handle it.
	res, err = handleKongCertificateRef(ctx, r.Client, ent)
	if err != nil {
		// If the referenced KongCertificate is being deleted and the object
		// is not being deleted yet then requeue until it will
		// get the deletion timestamp set due to having the owner set to KongCertificate.
		if errDel := (&ReferencedKongCertificateIsBeingDeleted{}); errors.As(err, errDel) &&
			ent.GetDeletionTimestamp().IsZero() {
			return ctrl.Result{
				RequeueAfter: time.Until(errDel.DeletionTimestamp),
			}, nil
		}

		// If the referenced KongCertificate is not found or is being deleted
		// and the object is being deleted, remove the finalizer and let the
		// deletion proceed without trying to delete the entity from Konnect
		// as the KongCertificate deletion will take care of it on the Konnect side.
		if errors.As(err, &ReferencedKongCertificateDoesNotExist{}) {
			if controllerutil.RemoveFinalizer(ent, KonnectCleanupFinalizer) {
				if err := r.Client.Update(ctx, ent); err != nil {
					if k8serrors.IsConflict(err) {
						return ctrl.Result{Requeue: true}, nil
					}
					return ctrl.Result{}, fmt.Errorf("failed to remove finalizer %s: %w", KonnectCleanupFinalizer, err)
				}
				log.Debug(logger, "finalizer removed as the owning KongCertificate is being deleted or is already gone",
					"finalizer", KonnectCleanupFinalizer,
				)
			}
		}

		return patchWithProgrammedStatusConditionBasedOnOtherConditions(ctx, r.Client, ent)
	} else if !res.IsZero() {
		return res, nil
	}

	// If a type has a KongKeySet ref, handle it.
	res, err = handleKongKeySetRef(ctx, r.Client, ent)
	if err != nil || !res.IsZero() {
		// If the referenced KongKeySet is being deleted and the object
		// is not being deleted yet then requeue until it will
		// get the deletion timestamp set due to having the owner set to KongKeySet.
		if errDel := (&ReferencedKongKeySetIsBeingDeleted{}); errors.As(err, errDel) &&
			ent.GetDeletionTimestamp().IsZero() {
			return ctrl.Result{
				RequeueAfter: time.Until(errDel.DeletionTimestamp),
			}, nil
		}

		// If the referenced KongKeySet is not found, remove the finalizer and let the
		// user delete the resource without trying to delete the entity from Konnect
		// as the KongKeySet deletion will take care of it on the Konnect side.
		if errors.As(err, &ReferencedKongKeySetDoesNotExist{}) {
			if controllerutil.RemoveFinalizer(ent, KonnectCleanupFinalizer) {
				if err := r.Client.Update(ctx, ent); err != nil {
					if k8serrors.IsConflict(err) {
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

	apiAuthRef, err := getAPIAuthRefNN(ctx, r.Client, ent)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get APIAuth ref for %s: %w", client.ObjectKeyFromObject(ent), err)
	}

	var apiAuth konnectv1alpha1.KonnectAPIAuthConfiguration
	err = r.Client.Get(ctx, apiAuthRef, &apiAuth)
	if requeue, res, retErr := handleAPIAuthStatusCondition(ctx, r.Client, ent, apiAuth, err); requeue {
		return res, retErr
	}

	token, err := getTokenFromKonnectAPIAuthConfiguration(ctx, r.Client, &apiAuth)
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
		if errDel := (&ReferencedObjectIsBeingDeleted{}); !errors.As(err, errDel) ||
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
			if err := ops.Delete(ctx, sdk, r.Client, r.MetricRecorder, ent); err != nil {
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
			if err := r.Client.Update(ctx, ent); err != nil {
				if k8serrors.IsConflict(err) {
					return ctrl.Result{Requeue: true}, nil
				}
				return ctrl.Result{}, fmt.Errorf("failed to remove finalizer %s: %w", KonnectCleanupFinalizer, err)
			}
		}

		return ctrl.Result{}, nil
	}

	// Handle type specific operations and stop reconciliation if needed.
	// This can happen for instance when KongConsumer references credentials Secrets
	// that do not exist or populate some Status fields based on Konnect API.
	if stop, res, err := handleTypeSpecific(ctx, sdk, r.Client, ent); err != nil || !res.IsZero() || stop {
		return res, err
	}

	// TODO: relying on status ID is OK but we need to rethink this because
	// we're using a cached client so after creating the resource on Konnect it might
	// happen that we've just created the resource but the status ID is not there yet.
	//
	// We should look at the "expectations" for this:
	// https://github.com/kubernetes/kubernetes/blob/master/pkg/controller/controller_utils.go
	if status := ent.GetKonnectStatus(); status == nil || status.GetKonnectID() == "" {
		obj := ent.DeepCopyObject().(client.Object)
		_, err := ops.Create(ctx, sdk, r.Client, r.MetricRecorder, ent)

		// TODO: this is actually not 100% error prone because when status
		// update fails we don't store the Konnect ID and hence the reconciler
		// will try to create the resource again on next reconciliation.

		// Regardless of the error reported from Create(), if the Konnect ID has been
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
			if k8serrors.IsConflict(err) {
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, fmt.Errorf("failed to update status after creating object: %w", err)
		} else if res != op.Noop {
			return ctrl.Result{}, nil
		}

		if err != nil {
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
		if k8serrors.IsConflict(errUpd) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to update in cluster resource after Konnect update: %w %w", errUpd, err)
	}
	if err != nil {
		logger.Error(err, "failed to update")
	} else if !res.IsZero() {
		return res, nil
	}

	// Ensure that successfully reconciled object has the cleanup finalizer.
	// This can happen when the finalizer was removed e.g. when the referenced
	// object was removed, breaking the reference chain in Konnect and thus making
	// the delete operation on the Konnect side impossible.
	if _, res, err := patch.WithFinalizer(ctx, r.Client, ent, KonnectCleanupFinalizer); err != nil || !res.IsZero() {
		return res, err
	}

	// NOTE: We requeue here to keep enforcing the state of the resource in Konnect.
	// Konnect does not allow subscribing to changes so we need to keep pushing the
	// desired state periodically.
	return ctrl.Result{
		RequeueAfter: r.SyncPeriod,
	}, nil
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
	T interface {
		client.Object
		k8sutils.ConditionsAware
	},
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
