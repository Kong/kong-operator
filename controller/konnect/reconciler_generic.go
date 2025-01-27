package konnect

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/samber/mo"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kong/gateway-operator/controller/konnect/constraints"
	"github.com/kong/gateway-operator/controller/konnect/ops"
	sdkops "github.com/kong/gateway-operator/controller/konnect/ops/sdk"
	"github.com/kong/gateway-operator/controller/pkg/log"
	"github.com/kong/gateway-operator/controller/pkg/patch"
	"github.com/kong/gateway-operator/internal/metrics"
	"github.com/kong/gateway-operator/pkg/consts"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
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
type KonnectEntityReconciler[T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T]] struct {
	sdkFactory              sdkops.SDKFactory
	DevelopmentMode         bool
	Client                  client.Client
	SyncPeriod              time.Duration
	MaxConcurrentReconciles uint

	MetricRecoder metrics.Recorder
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

// WithMetricRecoder sets the metric recorder to record metrics of Konnect entity operations of the reconciler.
func WithMetricRecorder[T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T]](
	metricRecorder metrics.Recorder,
) KonnectEntityReconcilerOption[T, TEnt] {
	return func(r *KonnectEntityReconciler[T, TEnt]) {
		r.MetricRecoder = metricRecorder
	}
}

// NewKonnectEntityReconciler returns a new KonnectEntityReconciler for the given
// Konnect entity type.
func NewKonnectEntityReconciler[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](
	sdkFactory sdkops.SDKFactory,
	developmentMode bool,
	client client.Client,
	opts ...KonnectEntityReconcilerOption[T, TEnt],
) *KonnectEntityReconciler[T, TEnt] {
	r := &KonnectEntityReconciler[T, TEnt]{
		sdkFactory:              sdkFactory,
		DevelopmentMode:         developmentMode,
		Client:                  client,
		SyncPeriod:              consts.DefaultKonnectSyncPeriod,
		MaxConcurrentReconciles: consts.DefaultKonnectMaxConcurrentReconciles,
		MetricRecoder:           &metrics.MockRecorder{},
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
func (r *KonnectEntityReconciler[T, TEnt]) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	var (
		e              T
		ent            = TEnt(&e)
		entityTypeName = constraints.EntityTypeName[T]()
		b              = ctrl.NewControllerManagedBy(mgr).
				Named(entityTypeName).
				WithOptions(
				controller.Options{
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
		entityTypeName = constraints.EntityTypeName[T]()
		logger         = log.GetLogger(ctx, entityTypeName, r.DevelopmentMode)
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
		if errors.As(err, &ReferencedControlPlaneDoesNotExistError{}) {
			if controllerutil.RemoveFinalizer(ent, KonnectCleanupFinalizer) {
				if err := r.Client.Update(ctx, ent); err != nil {
					if k8serrors.IsConflict(err) {
						return ctrl.Result{Requeue: true}, nil
					}
					return ctrl.Result{}, fmt.Errorf("failed to remove finalizer %s: %w", KonnectCleanupFinalizer, err)
				}
			}
		}

		if res, err := setProgrammedStatusConditionBasedOnOtherConditions(ctx, r.Client, ent); err != nil {
			return res, err
		} else if !res.IsZero() {
			return res, nil
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
		// and the object is being deleted, remove the finalizer and let the
		// deletion proceed without trying to delete the entity from Konnect
		// as the KongConsumer deletion will (or already has - in case of the consumer being gone)
		// take care of it on the Konnect side.
		if errors.As(err, &ReferencedKongConsumerDoesNotExist{}) ||
			errors.As(err, &ReferencedKongConsumerIsBeingDeleted{}) {
			if !ent.GetDeletionTimestamp().IsZero() {
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
			}
			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, err
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

		// If the referenced KongUpstream is not found or is being deleted
		// and the object is being deleted, remove the finalizer and let the
		// deletion proceed without trying to delete the entity from Konnect
		// as the KongUpstream deletion will take care of it on the Konnect side.
		if errors.As(err, &ReferencedKongUpstreamIsBeingDeleted{}) ||
			errors.As(err, &ReferencedKongUpstreamDoesNotExist{}) {
			if !ent.GetDeletionTimestamp().IsZero() {
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
		}

		return ctrl.Result{}, err
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
		if errors.As(err, &ReferencedKongCertificateIsBeingDeleted{}) ||
			errors.As(err, &ReferencedKongCertificateDoesNotExist{}) {
			if !ent.GetDeletionTimestamp().IsZero() {
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
		}
		return ctrl.Result{}, nil
	} else if res.Requeue {
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

		// If the referenced KongKeySet is not found or is being deleted
		// and the object is being deleted, remove the finalizer and let the
		// deletion proceed without trying to delete the entity from Konnect
		// as the KongKeySet deletion will take care of it on the Konnect side.
		if errors.As(err, &ReferencedKongKeySetIsBeingDeleted{}) ||
			errors.As(err, &ReferencedKongKeySetDoesNotExist{}) {
			if !ent.GetDeletionTimestamp().IsZero() {
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
		}

		return res, err
	}

	apiAuthRef, err := getAPIAuthRefNN(ctx, r.Client, ent)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get APIAuth ref for %s: %w", client.ObjectKeyFromObject(ent), err)
	}

	var apiAuth konnectv1alpha1.KonnectAPIAuthConfiguration
	if err := r.Client.Get(ctx, apiAuthRef, &apiAuth); err != nil {
		if k8serrors.IsNotFound(err) {
			if res, err := patch.StatusWithCondition(
				ctx, r.Client, ent,
				konnectv1alpha1.KonnectEntityAPIAuthConfigurationResolvedRefConditionType,
				metav1.ConditionFalse,
				konnectv1alpha1.KonnectEntityAPIAuthConfigurationResolvedRefReasonRefNotFound,
				fmt.Sprintf("Referenced KonnectAPIAuthConfiguration %s not found", apiAuthRef),
			); err != nil || !res.IsZero() {
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, nil
		}

		if res, err := patch.StatusWithCondition(
			ctx, r.Client, ent,
			konnectv1alpha1.KonnectEntityAPIAuthConfigurationResolvedRefConditionType,
			metav1.ConditionFalse,
			konnectv1alpha1.KonnectEntityAPIAuthConfigurationResolvedRefReasonRefInvalid,
			fmt.Sprintf("KonnectAPIAuthConfiguration reference %s is invalid: %v", apiAuthRef, err),
		); err != nil || !res.IsZero() {
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, fmt.Errorf("failed to get KonnectAPIAuthConfiguration: %w", err)
	}

	// Update the status if the reference is resolved and it's not as expected.
	if cond, present := k8sutils.GetCondition(konnectv1alpha1.KonnectEntityAPIAuthConfigurationResolvedRefConditionType, ent); !present ||
		cond.Status != metav1.ConditionTrue ||
		cond.ObservedGeneration != ent.GetGeneration() ||
		cond.Reason != konnectv1alpha1.KonnectEntityAPIAuthConfigurationResolvedRefReasonResolvedRef {
		if res, err := patch.StatusWithCondition(
			ctx, r.Client, ent,
			konnectv1alpha1.KonnectEntityAPIAuthConfigurationResolvedRefConditionType,
			metav1.ConditionTrue,
			konnectv1alpha1.KonnectEntityAPIAuthConfigurationResolvedRefReasonResolvedRef,
			fmt.Sprintf("KonnectAPIAuthConfiguration reference %s is resolved", apiAuthRef),
		); err != nil || !res.IsZero() {
			return res, err
		}
		return ctrl.Result{}, nil
	}

	// Check if the referenced APIAuthConfiguration is valid.
	if cond, present := k8sutils.GetCondition(konnectv1alpha1.KonnectEntityAPIAuthConfigurationValidConditionType, &apiAuth); !present ||
		cond.Status != metav1.ConditionTrue ||
		cond.Reason != konnectv1alpha1.KonnectEntityAPIAuthConfigurationReasonValid {

		// If it's invalid then set the "APIAuthValid" status condition on
		// the entity to False with "Invalid" reason.
		if res, err := patch.StatusWithCondition(
			ctx, r.Client, ent,
			konnectv1alpha1.KonnectEntityAPIAuthConfigurationValidConditionType,
			metav1.ConditionFalse,
			konnectv1alpha1.KonnectEntityAPIAuthConfigurationReasonInvalid,
			conditionMessageReferenceKonnectAPIAuthConfigurationInvalid(apiAuthRef),
		); err != nil || !res.IsZero() {
			return res, err
		}

		return ctrl.Result{}, nil
	}

	// If the referenced APIAuthConfiguration is valid, set the "APIAuthValid"
	// condition to True with "Valid" reason.
	// Only perform the update if the condition is not as expected.
	if cond, present := k8sutils.GetCondition(konnectv1alpha1.KonnectEntityAPIAuthConfigurationValidConditionType, ent); !present ||
		cond.Status != metav1.ConditionTrue ||
		cond.Reason != konnectv1alpha1.KonnectEntityAPIAuthConfigurationReasonValid ||
		cond.ObservedGeneration != ent.GetGeneration() ||
		cond.Message != conditionMessageReferenceKonnectAPIAuthConfigurationValid(apiAuthRef) {

		if res, err := patch.StatusWithCondition(
			ctx, r.Client, ent,
			konnectv1alpha1.KonnectEntityAPIAuthConfigurationValidConditionType,
			metav1.ConditionTrue,
			konnectv1alpha1.KonnectEntityAPIAuthConfigurationReasonValid,
			conditionMessageReferenceKonnectAPIAuthConfigurationValid(apiAuthRef),
		); err != nil || !res.IsZero() {
			return res, err
		}
		return ctrl.Result{}, nil
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
	serverURL := ops.NewServerURL(apiAuth.Spec.ServerURL)
	sdk := r.sdkFactory.NewKonnectSDK(
		serverURL.String(),
		sdkops.SDKToken(token),
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
			if err := ops.Delete[T, TEnt](ctx, sdk, r.Client, r.MetricRecoder, ent); err != nil {
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

	// TODO: relying on status ID is OK but we need to rethink this because
	// we're using a cached client so after creating the resource on Konnect it might
	// happen that we've just created the resource but the status ID is not there yet.
	//
	// We should look at the "expectations" for this:
	// https://github.com/kubernetes/kubernetes/blob/master/pkg/controller/controller_utils.go
	if status := ent.GetKonnectStatus(); status == nil || status.GetKonnectID() == "" {
		obj := ent.DeepCopyObject().(client.Object)
		_, err := ops.Create[T, TEnt](ctx, sdk, r.Client, r.MetricRecoder, ent)

		// TODO: this is actually not 100% error prone because when status
		// update fails we don't store the Konnect ID and hence the reconciler
		// will try to create the resource again on next reconciliation.

		// Regardless of the error reported from Create(), if the Konnect ID has been
		// set then:
		// - add the finalizer so that the resource can be cleaned up from Konnect on deletion...
		if status != nil && status.ID != "" {
			objWithFinalizer := ent.DeepCopyObject().(client.Object)
			if controllerutil.AddFinalizer(objWithFinalizer, KonnectCleanupFinalizer) {
				if errUpd := r.Client.Patch(ctx, objWithFinalizer, client.MergeFrom(ent)); errUpd != nil {
					if k8serrors.IsConflict(errUpd) {
						return ctrl.Result{Requeue: true}, nil
					}
					if err != nil {
						return ctrl.Result{}, fmt.Errorf(
							"failed to update finalizer %s: %w, object create operation failed against Konnect API: %w",
							KonnectCleanupFinalizer, errUpd, err,
						)
					}
					return ctrl.Result{}, fmt.Errorf(
						"failed to update finalizer %s: %w",
						KonnectCleanupFinalizer, errUpd,
					)
				}
			}

			// ...
			// - add the Org ID and Server URL to the status so that the resource can be
			//   cleaned up from Konnect on deletion and also so that the status can
			//   indicate where the corresponding Konnect entity is located.
			setServerURLAndOrgID(ent, serverURL, apiAuth.Status.OrganizationID)
		}

		// Regardless of the error, patch the status as it can contain the Konnect ID,
		// Org ID, Server URL and status conditions.
		// Konnect ID will be needed for the finalizer to work.
		if err := r.Client.Status().Patch(ctx, ent, client.MergeFrom(obj)); err != nil {
			if k8serrors.IsConflict(err) {
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, fmt.Errorf("failed to update status after creating object: %w", err)
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

	if res, err := ops.Update[T, TEnt](ctx, sdk, r.SyncPeriod, r.Client, r.MetricRecoder, ent); err != nil {
		setServerURLAndOrgID(ent, serverURL, apiAuth.Status.OrganizationID)
		if errUpd := r.Client.Status().Update(ctx, ent); errUpd != nil {
			if k8serrors.IsConflict(errUpd) {
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, fmt.Errorf("failed to update in cluster resource after Konnect update: %w %w", errUpd, err)
		}

		return ctrl.Result{}, fmt.Errorf("failed to update object: %w", err)
	} else if !res.IsZero() {
		return res, nil
	}

	setServerURLAndOrgID(ent, serverURL, apiAuth.Status.OrganizationID)
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

func setServerURLAndOrgID(
	ent interface {
		GetKonnectStatus() *konnectv1alpha1.KonnectEntityStatus
	},
	serverURL ops.ServerURL,
	orgID string,
) {
	ent.GetKonnectStatus().ServerURL = serverURL.String()
	ent.GetKonnectStatus().OrgID = orgID
}

// EntityWithControlPlaneRef is an interface for entities that have a ControlPlaneRef.
type EntityWithControlPlaneRef interface {
	SetControlPlaneID(string)
	GetControlPlaneID() string
}

func getCPForRef(
	ctx context.Context,
	cl client.Client,
	cpRef configurationv1alpha1.ControlPlaneRef,
	namespace string,
) (*konnectv1alpha1.KonnectGatewayControlPlane, error) {
	switch cpRef.Type {
	case configurationv1alpha1.ControlPlaneRefKonnectID:
		return getCPForKonnectID(ctx, cl, cpRef)
	case configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef:
		return getCPForNamespacedRef(ctx, cl, cpRef, namespace)
	default:
		return nil, ReferencedKongGatewayControlPlaneIsUnsupported{Reference: cpRef}
	}
}

func getCPForKonnectID(
	ctx context.Context,
	cl client.Client,
	cpRef configurationv1alpha1.ControlPlaneRef,
) (*konnectv1alpha1.KonnectGatewayControlPlane, error) {
	var l konnectv1alpha1.KonnectGatewayControlPlaneList
	if err := cl.List(ctx, &l,
		client.MatchingFields{
			IndexFieldKonnectGatewayControlPlaneOnKonnectID: *cpRef.KonnectID,
		},
	); err != nil {
		return nil, fmt.Errorf("failed to list ControlPlanes: %w", err)
	}

	if len(l.Items) == 0 {
		return nil, ReferencedControlPlaneDoesNotExistError{
			Reference: cpRef,
			Err:       errors.New("no KonnectControlPlane with given status.konnectID found"),
		}
	}
	return &l.Items[0], nil
}

func getCPForNamespacedRef(
	ctx context.Context,
	cl client.Client,
	ref configurationv1alpha1.ControlPlaneRef,
	namespace string,
) (*konnectv1alpha1.KonnectGatewayControlPlane, error) {
	// TODO(pmalek): handle cross namespace refs
	if namespace != "" && ref.KonnectNamespacedRef.Namespace != "" && ref.KonnectNamespacedRef.Namespace != namespace {
		return nil, fmt.Errorf("%s ControlPlaneRef from different namespace than %s", ref.KonnectNamespacedRef.Namespace, namespace)
	}

	nn := types.NamespacedName{
		Name:      ref.KonnectNamespacedRef.Name,
		Namespace: namespace,
	}

	// Set namespace of control plane when it is non-empty. Only applies for cluster-scoped resources (KongVault).
	if namespace == "" && ref.KonnectNamespacedRef.Namespace != "" {
		nn.Namespace = ref.KonnectNamespacedRef.Namespace
	}

	var cp konnectv1alpha1.KonnectGatewayControlPlane
	if err := cl.Get(ctx, nn, &cp); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, ReferencedControlPlaneDoesNotExistError{
				Reference: ref,
				Err:       err,
			}
		}
		return nil, fmt.Errorf("failed to get ControlPlane %s: %w", nn, err)
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

func getAPIAuthRefNN[T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T]](
	ctx context.Context,
	cl client.Client,
	ent TEnt,
) (types.NamespacedName, error) {
	// If the entity has a ControlPlaneRef, get the KonnectAPIAuthConfiguration
	// ref from the referenced ControlPlane.
	cpRef, ok := getControlPlaneRef(ent).Get()
	if ok {
		cp, err := getCPForRef(ctx, cl, cpRef, ent.GetNamespace())
		if err != nil {
			return types.NamespacedName{}, fmt.Errorf("failed to get ControlPlane for %s: %w", client.ObjectKeyFromObject(ent), err)
		}

		cpNamespace := ent.GetNamespace()
		if ent.GetNamespace() == "" && cp.GetNamespace() != "" {
			cpNamespace = cp.GetNamespace()
		}
		return getCPAuthRefForRef(ctx, cl, cpRef, cpNamespace)
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

	// If the entity has a KongConsumerRef, get the KonnectAPIAuthConfiguration
	// ref from the referenced KongConsumer.
	consumerRef, ok := getConsumerRef(ent).Get()
	if ok {
		// TODO(pmalek): handle cross namespace refs
		nn := types.NamespacedName{
			Name:      consumerRef.Name,
			Namespace: ent.GetNamespace(),
		}

		var consumer configurationv1.KongConsumer
		if err := cl.Get(ctx, nn, &consumer); err != nil {
			return types.NamespacedName{}, fmt.Errorf("failed to get KongConsumer %s", nn)
		}

		cpRef, ok := getControlPlaneRef(&consumer).Get()
		if !ok {
			return types.NamespacedName{}, fmt.Errorf("KongConsumer %s does not have a ControlPlaneRef", nn)
		}
		return getCPAuthRefForRef(ctx, cl, cpRef, ent.GetNamespace())
	}

	// If the entity has a KongUpstreamRef, get the KonnectAPIAuthConfiguration
	// ref from the referenced KongUpstream.
	upstreamRef, ok := getKongUpstreamRef(ent).Get()
	if ok {
		nn := types.NamespacedName{
			Name:      upstreamRef.Name,
			Namespace: ent.GetNamespace(),
		}

		var upstream configurationv1alpha1.KongUpstream
		if err := cl.Get(ctx, nn, &upstream); err != nil {
			return types.NamespacedName{}, fmt.Errorf("failed to get KongUpstream %s", nn)
		}

		cpRef, ok := getControlPlaneRef(&upstream).Get()
		if !ok {
			return types.NamespacedName{}, fmt.Errorf("KongUpstream %s does not have a ControlPlaneRef", nn)
		}
		return getCPAuthRefForRef(ctx, cl, cpRef, ent.GetNamespace())
	}

	if ref, ok := any(ent).(constraints.EntityWithKonnectAPIAuthConfigurationRef); ok {
		return types.NamespacedName{
			Name: ref.GetKonnectAPIAuthConfigurationRef().Name,
			// TODO: enable if cross namespace refs are allowed
			Namespace: ent.GetNamespace(),
		}, nil
	}

	// If the entity has a KongCertificateRef, get the KonnectAPIAuthConfiguration
	// ref from the referenced KongUpstream.
	certificateRef, ok := getKongCertificateRef(ent).Get()
	if ok {
		nn := types.NamespacedName{
			Name:      certificateRef.Name,
			Namespace: ent.GetNamespace(),
		}

		var cert configurationv1alpha1.KongCertificate
		if err := cl.Get(ctx, nn, &cert); err != nil {
			return types.NamespacedName{}, fmt.Errorf("failed to get KongCertificate %s", nn)
		}

		cpRef, ok := getControlPlaneRef(&cert).Get()
		if !ok {
			return types.NamespacedName{}, fmt.Errorf("KongCertificate %s does not have a ControlPlaneRef", nn)
		}
		return getCPAuthRefForRef(ctx, cl, cpRef, ent.GetNamespace())
	}

	return types.NamespacedName{}, fmt.Errorf(
		"cannot get KonnectAPIAuthConfiguration for entity type %T %s",
		client.ObjectKeyFromObject(ent), ent,
	)
}

func getConsumerRef[T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T]](
	e TEnt,
) mo.Option[corev1.LocalObjectReference] {
	switch e := any(e).(type) {
	case *configurationv1alpha1.KongCredentialBasicAuth:
		return mo.Some(e.Spec.ConsumerRef)
	case *configurationv1alpha1.KongCredentialAPIKey:
		return mo.Some(e.Spec.ConsumerRef)
	case *configurationv1alpha1.KongCredentialACL:
		return mo.Some(e.Spec.ConsumerRef)
	case *configurationv1alpha1.KongCredentialJWT:
		return mo.Some(e.Spec.ConsumerRef)
	case *configurationv1alpha1.KongCredentialHMAC:
		return mo.Some(e.Spec.ConsumerRef)
	default:
		return mo.None[corev1.LocalObjectReference]()
	}
}

// handleKongConsumerRef handles the ConsumerRef for the given entity.
// It sets the owner reference to the referenced KongConsumer and updates the
// status of the entity based on the referenced KongConsumer status.
func handleKongConsumerRef[T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T]](
	ctx context.Context,
	cl client.Client,
	ent TEnt,
) (ctrl.Result, error) {
	kongConsumerRef, ok := getConsumerRef(ent).Get()
	if !ok {
		return ctrl.Result{}, nil
	}
	consumer := configurationv1.KongConsumer{}
	nn := types.NamespacedName{
		Name:      kongConsumerRef.Name,
		Namespace: ent.GetNamespace(),
	}

	if err := cl.Get(ctx, nn, &consumer); err != nil {
		if res, errStatus := patch.StatusWithCondition(
			ctx, cl, ent,
			konnectv1alpha1.KongConsumerRefValidConditionType,
			metav1.ConditionFalse,
			konnectv1alpha1.KongConsumerRefReasonInvalid,
			err.Error(),
		); errStatus != nil || !res.IsZero() {
			return res, errStatus
		}

		return ctrl.Result{}, ReferencedKongConsumerDoesNotExist{
			Reference: nn,
			Err:       err,
		}
	}

	// If referenced KongConsumer is being deleted, return an error so that we
	// can remove the entity from Konnect first.
	if delTimestamp := consumer.GetDeletionTimestamp(); !delTimestamp.IsZero() {
		return ctrl.Result{}, ReferencedKongConsumerIsBeingDeleted{
			Reference:         nn,
			DeletionTimestamp: delTimestamp.Time,
		}
	}

	cond, ok := k8sutils.GetCondition(konnectv1alpha1.KonnectEntityProgrammedConditionType, &consumer)
	if !ok || cond.Status != metav1.ConditionTrue {
		ent.SetKonnectID("")
		if res, err := patch.StatusWithCondition(
			ctx, cl, ent,
			konnectv1alpha1.KongConsumerRefValidConditionType,
			metav1.ConditionFalse,
			konnectv1alpha1.KongConsumerRefReasonInvalid,
			fmt.Sprintf("Referenced KongConsumer %s is not programmed yet", nn),
		); err != nil || !res.IsZero() {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	old := ent.DeepCopyObject().(TEnt)
	if err := controllerutil.SetOwnerReference(&consumer, ent, cl.Scheme(), controllerutil.WithBlockOwnerDeletion(true)); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to set owner reference: %w", err)
	}
	if err := cl.Patch(ctx, ent, client.MergeFrom(old)); err != nil {
		if k8serrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to update status: %w", err)
	}

	type EntityWithConsumerRef interface {
		SetKonnectConsumerIDInStatus(string)
	}
	if cred, ok := any(ent).(EntityWithConsumerRef); ok {
		cred.SetKonnectConsumerIDInStatus(consumer.Status.Konnect.GetKonnectID())
	} else {
		return ctrl.Result{}, fmt.Errorf(
			"cannot set referenced Consumer %s KonnectID in %s %sstatus",
			client.ObjectKeyFromObject(&consumer), constraints.EntityTypeName[T](), client.ObjectKeyFromObject(ent),
		)
	}

	if res, errStatus := patch.StatusWithCondition(
		ctx, cl, ent,
		konnectv1alpha1.KongConsumerRefValidConditionType,
		metav1.ConditionTrue,
		konnectv1alpha1.KongConsumerRefReasonValid,
		fmt.Sprintf("Referenced KongConsumer %s programmed", nn),
	); errStatus != nil || !res.IsZero() {
		return res, errStatus
	}

	cpRef, ok := getControlPlaneRef(&consumer).Get()
	if !ok {
		return ctrl.Result{}, fmt.Errorf(
			"KongRoute references a KongConsumer %s which does not have a ControlPlane ref",
			client.ObjectKeyFromObject(&consumer),
		)
	}
	cp, err := getCPForRef(ctx, cl, cpRef, ent.GetNamespace())
	if err != nil {
		if res, errStatus := patch.StatusWithCondition(
			ctx, cl, ent,
			konnectv1alpha1.ControlPlaneRefValidConditionType,
			metav1.ConditionFalse,
			konnectv1alpha1.ControlPlaneRefReasonInvalid,
			err.Error(),
		); errStatus != nil || !res.IsZero() {
			return res, errStatus
		}
		if k8serrors.IsNotFound(err) {
			return ctrl.Result{}, ReferencedControlPlaneDoesNotExistError{
				Reference: cpRef,
				Err:       err,
			}
		}
		return ctrl.Result{}, err
	}

	cond, ok = k8sutils.GetCondition(konnectv1alpha1.KonnectEntityProgrammedConditionType, cp)
	if !ok || cond.Status != metav1.ConditionTrue || cond.ObservedGeneration != cp.GetGeneration() {
		if res, errStatus := patch.StatusWithCondition(
			ctx, cl, ent,
			konnectv1alpha1.ControlPlaneRefValidConditionType,
			metav1.ConditionFalse,
			konnectv1alpha1.ControlPlaneRefReasonInvalid,
			fmt.Sprintf("Referenced ControlPlane %s is not programmed yet", nn),
		); errStatus != nil || !res.IsZero() {
			return res, errStatus
		}

		return ctrl.Result{Requeue: true}, nil
	}

	if resource, ok := any(ent).(EntityWithControlPlaneRef); ok {
		old := ent.DeepCopyObject().(TEnt)
		resource.SetControlPlaneID(cp.Status.ID)
		_, err := patch.ApplyStatusPatchIfNotEmpty(ctx, cl, ctrllog.FromContext(ctx), ent, old)
		if err != nil {
			if k8serrors.IsConflict(err) {
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, err
		}
	}

	if res, errStatus := patch.StatusWithCondition(
		ctx, cl, ent,
		konnectv1alpha1.ControlPlaneRefValidConditionType,
		metav1.ConditionTrue,
		konnectv1alpha1.ControlPlaneRefReasonValid,
		fmt.Sprintf("Referenced ControlPlane %s is programmed", nn),
	); errStatus != nil || !res.IsZero() {
		return res, errStatus
	}

	return ctrl.Result{}, nil
}

func setProgrammedStatusConditionBasedOnOtherConditions[
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
		konnectv1alpha1.KonnectEntityProgrammedReasonExistsConditionWithStatusFalse,
		"Some conditions have status set to False",
	); errStatus != nil || !res.IsZero() {
		return res, errStatus
	}
	return ctrl.Result{}, nil
}
