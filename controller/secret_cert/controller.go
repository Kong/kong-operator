package secretcert

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kong/kong-operator/v2/controller/pkg/finalizer"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/modules/manager/config"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

// Reconciler reconciles TLS Secrets managed by DataPlane or ControlPlane.
// Certs in these Secrets expires after a certain time, and the controller
// is responsible for renewing them by deleting the expiring Secret,
// which will trigger the creation of a new Secret with renewed certs by the
// respective owner controllers (CP/DP).
type Reconciler struct {
	client.Client

	ControllerOptions    controller.Options
	LoggingMode          logging.Mode
	CertExpirationMargin time.Duration
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(r.ControllerOptions).
		For(&corev1.Secret{},
			builder.WithPredicates(
				predicate.NewPredicateFuncs(secretMatchesFilter),
			),
		).
		Complete(reconcile.AsReconciler[*corev1.Secret](r.Client, r))
}

// secretMatchesFilter returns true if the Secret has the required labels and type.
func secretMatchesFilter(obj client.Object) bool {
	labels := obj.GetLabels()
	if labels[config.DefaultSecretLabelSelector] != config.LabelValueForSelectorTrue {
		return false
	}
	if managedBy := labels[consts.GatewayOperatorManagedByLabel]; managedBy != consts.DataPlaneManagedLabelValue && managedBy != consts.ControlPlaneManagedLabelValue {
		return false
	}

	secret, ok := obj.(*corev1.Secret)
	if !ok {
		return false
	}

	return secret.Type == corev1.SecretTypeTLS
}

// Reconcile handles certificate Secret reconciliation.
func (r *Reconciler) Reconcile(ctx context.Context, cert *corev1.Secret) (ctrl.Result, error) {
	logger := log.GetLogger(ctx, "secret_cert", r.LoggingMode).WithValues()

	certExpirationAnnotation, ok := cert.Annotations[consts.CertExpiresAtAnnotation]
	if !ok {
		log.Debug(
			logger,
			"secret %s/%s created before introducing mechanism for autorotation, force rotation by deleting it manually",
			cert.Namespace, cert.Name,
		)
		return ctrl.Result{}, nil
	}
	expiresAt, err := time.Parse(time.RFC3339, certExpirationAnnotation)
	if err != nil {
		log.Error(
			logger,
			err,
			fmt.Sprintf("failed to parse %s annotation for secret %s/%s", consts.CertExpiresAtAnnotation, cert.Namespace, cert.Name),
		)
		return ctrl.Result{}, err
	}
	if timeUntilExpire := expiresAt.Sub(time.Now().UTC()); timeUntilExpire > r.CertExpirationMargin {
		log.Debug(logger, "cert is still valid", "expires_at", expiresAt.String(), "time_until_expiry", timeUntilExpire.String())
		return ctrl.Result{RequeueAfter: timeUntilExpire - r.CertExpirationMargin}, nil
	}

	log.Info(logger, "cert is expiring soon, renewing")

	// Remove the wait-for-owner finalizer if present so that the secret can be deleted.
	if controllerutil.RemoveFinalizer(cert, consts.DataPlaneOwnedWaitForOwnerFinalizer) {
		if err := r.Update(ctx, cert); err != nil {
			return finalizer.HandlePatchOrUpdateError(err, logger)
		}
		log.Debug(logger, "removed wait-for-owner finalizer from secret")
	}
	// Delete the expiring Secret, which will trigger the creation
	// of a new secret with renewed certs by the respective owner controllers (CP/DP).
	if err := r.Delete(ctx, cert); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to delete secret %s/%s: %w", cert.Namespace, cert.Name, err)
	}
	log.Info(logger, "expiring cert secret has been deleted, new will be created immediately")

	return ctrl.Result{}, nil
}
