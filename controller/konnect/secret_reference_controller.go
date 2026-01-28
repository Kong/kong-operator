package konnect

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	konnectv1alpha1 "github.com/kong/kong-operator/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/controller/pkg/log"
	"github.com/kong/kong-operator/controller/pkg/patch"
	"github.com/kong/kong-operator/internal/utils/index"
	"github.com/kong/kong-operator/modules/manager/logging"
	"github.com/kong/kong-operator/pkg/consts"
)

// KonnectSecretReferenceController reconciles Secret objects that are referenced by Konnect resources.
// It manages the SecretInUseFinalizer to prevent deletion of secrets while they are still referenced.
// by Konnect resources.
type KonnectSecretReferenceController struct {
	client            client.Client
	controllerOptions controller.Options
	loggingMode       logging.Mode
}

// NewKonnectSecretReferenceController creates a new KonnectSecretReferenceController.
func NewKonnectSecretReferenceController(
	client client.Client,
	controllerOptions controller.Options,
	loggingMode logging.Mode,
) *KonnectSecretReferenceController {
	return &KonnectSecretReferenceController{
		client:            client,
		controllerOptions: controllerOptions,
		loggingMode:       loggingMode,
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *KonnectSecretReferenceController) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	b := ctrl.NewControllerManagedBy(mgr).
		WithOptions(r.controllerOptions).
		For(&corev1.Secret{}).
		Named("KonnectSecretReference")

	setSecretReferenceWatches(b)

	return b.Complete(r)
}

// Reconcile reconciles a Secret object.
func (r *KonnectSecretReferenceController) Reconcile(
	ctx context.Context, req ctrl.Request,
) (ctrl.Result, error) {
	var secret corev1.Secret
	if err := r.client.Get(ctx, req.NamespacedName, &secret); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	logger := log.GetLogger(ctx, "KonnectSecretReference", r.loggingMode)

	// Handle secret deletion.
	if !secret.GetDeletionTimestamp().IsZero() {
		log.Debug(logger, "secret is being deleted")
		// Wait for termination grace period before cleaning up.
		if secret.GetDeletionTimestamp().After(time.Now()) {
			log.Debug(logger, "secret still under grace period, requeueing")
			return ctrl.Result{
				RequeueAfter: time.Until(secret.GetDeletionTimestamp().Time),
			}, nil
		}

		// Check if secret is still referenced by any Konnect resources.
		isReferenced, err := r.isSecretReferencedByKonnectResources(ctx, &secret)
		if err != nil {
			log.Debug(logger, "failed to check if secret is referenced", "error", err)
			return ctrl.Result{}, err
		}

		// Remove finalizer if not referenced.
		if !isReferenced {
			if removed, res, err := patch.WithoutFinalizer(ctx, r.client, &secret, consts.KonnectExtensionSecretInUseFinalizer); err != nil || !res.IsZero() {
				return res, err
			} else if removed {
				log.Debug(logger, "removed finalizer from secret as it's no longer referenced")
			}
		}
		return ctrl.Result{}, nil
	}

	// Check if secret is referenced by any Konnect resources.
	isReferenced, err := r.isSecretReferencedByKonnectResources(ctx, &secret)
	if err != nil {
		log.Debug(logger, "failed to check if secret is referenced", "error", err)
		return ctrl.Result{}, err
	}

	// Add finalizer if referenced, remove if not referenced.
	if isReferenced {
		if added, res, err := patch.WithFinalizer(ctx, r.client, &secret, consts.KonnectExtensionSecretInUseFinalizer); err != nil || !res.IsZero() {
			return res, err
		} else if added {
			log.Debug(logger, "added finalizer to secret as it's referenced by Konnect resources")
		}
	} else {
		if removed, res, err := patch.WithoutFinalizer(ctx, r.client, &secret, consts.KonnectExtensionSecretInUseFinalizer); err != nil || !res.IsZero() {
			return res, err
		} else if removed {
			log.Debug(logger, "removed finalizer from secret as it's not referenced")
		}
	}

	return ctrl.Result{}, nil
}

// isSecretReferencedByKonnectResources checks if the given secret is referenced by any Konnect resources.
// using the established indexes.
func (r *KonnectSecretReferenceController) isSecretReferencedByKonnectResources(ctx context.Context, secret *corev1.Secret) (bool, error) {
	secretKey := secret.Namespace + "/" + secret.Name

	// Check if secret is referenced by any KonnectAPIAuthConfiguration.
	apiAuthList := &konnectv1alpha1.KonnectAPIAuthConfigurationList{}
	err := r.client.List(ctx, apiAuthList, client.MatchingFields{
		index.IndexFieldKonnectAPIAuthConfigurationReferencesSecrets: secretKey,
	})
	if err != nil {
		return false, err
	}

	if len(apiAuthList.Items) > 0 {
		return true, nil
	}

	return false, nil
}
