package controllers

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"github.com/samber/mo"
	ctrl "sigs.k8s.io/controller-runtime"

	internalcontrollers "github.com/kong/kong-operator/v2/ingress-controller/internal/controllers"
	ctrllicense "github.com/kong/kong-operator/v2/ingress-controller/internal/controllers/license"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/license"
)

// DataPlane re-exports internalcontrollers.DataPlane so that reconcilers
// living outside the ingress-controller module tree can consume it.
type DataPlane = internalcontrollers.DataPlane

// Reconciler re-exports internalcontrollers.Reconciler so that reconcilers
// living outside the ingress-controller module tree can implement it.
type Reconciler = internalcontrollers.Reconciler

// LicenseGetter re-exports license.Getter so that consumers outside the
// ingress-controller module tree can consume the effective Kong license.
type LicenseGetter = license.Getter

// SetupKongLicense runs the KongLicense reconciler on the given manager and
// returns the license getter it provides. It is exported so that the
// operator's manager can run the same controller the embedded KIC runs inside
// ControlPlanes, reusing its license picking and status reporting. It reports
// under the operator's own controller type (LicenseControllerTypeKongOperator)
// so entries in the KongLicense status do not collide with the embedded KIC's.
// The reconciler only starts once the KongLicense CRD exists, so a cluster
// without the CRD does not abort the manager.
// NOTE: no license validator is wired, so the license validity is not
// verified; only its availability is reported.
func SetupKongLicense(
	ctx context.Context,
	mgr ctrl.Manager,
	cacheSyncTimeout time.Duration,
	log logr.Logger,
) (LicenseGetter, error) {
	licenseController := ctrllicense.NewKongV1Alpha1KongLicenseReconciler(
		mgr.GetClient(),
		log,
		mgr.GetScheme(),
		ctrllicense.NewLicenseCache(),
		cacheSyncTimeout,
		nil, // statusQueue: only used by KIC's status updater.
		ctrllicense.LicenseControllerTypeKongOperator,
		mo.None[string](),
		mo.None[ctrllicense.ValidatorFunc](),
	)
	if err := ctrllicense.WrapKongLicenseReconcilerToDynamicCRDController(
		ctx, mgr, licenseController,
	).SetupWithManager(mgr); err != nil {
		return nil, fmt.Errorf("failed to start KongLicense controller: %w", err)
	}
	return licenseController, nil
}
