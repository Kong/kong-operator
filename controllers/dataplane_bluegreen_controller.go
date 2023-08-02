package controllers

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	operatorv1beta1 "github.com/kong/gateway-operator/apis/v1beta1"
	"github.com/kong/gateway-operator/internal/consts"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
)

// -----------------------------------------------------------------------------
// DataPlaneBlueGreenReconciler
// -----------------------------------------------------------------------------

// DataPlaneBlueGreenReconciler reconciles a DataPlane objects for purposes
// of Blue Green rollouts.
type DataPlaneBlueGreenReconciler struct {
	client.Client

	// DataPlaneController contains the DataPlaneReconciler to which we delegate
	// the DataPlane reconciliation when it's not yet ready to accept BlueGreen
	// rollout changes or BlueGreen rollout has not been configured.
	DataPlaneController reconcile.Reconciler

	// ClusterCASecretName contains the name of the Secret that contains the CA
	// certificate data which will be used when generating certificates for DataPlane's
	// Deployment.
	ClusterCASecretName string
	// ClusterCASecretName contains the namespace of the Secret that contains the CA
	// certificate data which will be used when generating certificates for DataPlane's
	// Deployment.
	ClusterCASecretNamespace string

	// DevelopmentMode indicates if the controller should run in development mode,
	// which causes it to e.g. perform less validations.
	DevelopmentMode bool
}

// SetupWithManager sets up the controller with the Manager.
func (r *DataPlaneBlueGreenReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return DataPlaneWatchBuilder(mgr).
		Complete(r)
}

// -----------------------------------------------------------------------------
// DataPlaneBlueGreenReconciler - Reconciliation
// -----------------------------------------------------------------------------

// Reconcile moves the current state of an object to the intended state.
func (r *DataPlaneBlueGreenReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var dataplane operatorv1beta1.DataPlane
	if err := r.Client.Get(ctx, req.NamespacedName, &dataplane); err != nil {
		if k8serrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	log := getLogger(ctx, "dataplaneBlueGreen", r.DevelopmentMode)

	// Blue Green rollout strategy is not enabled, delegate to DataPlane controller.
	if dataplane.Spec.Deployment.Rollout == nil || dataplane.Spec.Deployment.Rollout.Strategy.BlueGreen == nil {
		trace(log, "no Rollout with BlueGreen strategy specified, delegating to DataPlaneReconciler", req)
		return r.DataPlaneController.Reconcile(ctx, req)
	}

	if !k8sutils.IsReady(&dataplane) {
		debug(log, "DataPlane is not ready yet to proceed with BlueGreen rollout, delegating to DataPlaneReconciler", req)
		return r.DataPlaneController.Reconcile(ctx, req)
	}

	// DataPlane is ready and we can proceed with deploying preview resources.
	res, dataplaneAdminService, err := ensureAdminServiceForDataPlane(ctx, r.Client, &dataplane,
		client.MatchingLabels{
			consts.DataPlaneServiceStateLabel: consts.DataPlaneServiceStatePreview,
		},
	)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed ensuring that preview Admin API Service exists for DataPlane %s/%s: %w", dataplane.Namespace, dataplane.Name, err)
	}
	switch res {
	case Created, Updated:
		debug(log, "DataPlane preview admin service created/updated", dataplane, "service", dataplaneAdminService.Name)
		return ctrl.Result{}, nil // dataplane admin service creation/update will trigger reconciliation
	case Noop:
	}

	if updated, err := r.ensureDataPlaneAdminAPIInRolloutStatus(ctx, log, &dataplane, dataplaneAdminService); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed updating rollout status with preview Admin API service addresses: %w", err)
	} else if updated {
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
}

func (r *DataPlaneBlueGreenReconciler) ensureDataPlaneAdminAPIInRolloutStatus(
	ctx context.Context,
	log logr.Logger,
	dataplane *operatorv1beta1.DataPlane,
	dataplaneAdminAPIService *corev1.Service,
) (bool, error) {
	addresses, err := addressesFromService(dataplaneAdminAPIService)
	if err != nil {
		return false, fmt.Errorf("failed getting addresses for Admin API service %s: %w", dataplaneAdminAPIService, err)
	}
	rolloutStatus := dataplane.Status.RolloutStatus

	// If there's nothing to update then bail.
	if len(addresses) == 0 && (dataplaneAdminAPIService == nil || dataplaneAdminAPIService.Name == "") {
		return false, nil
	}

	// If the status is already in place and is as expected then don't update.
	if rolloutStatus != nil && rolloutStatus.Services != nil && rolloutStatus.Services.AdminAPI != nil &&
		cmp.Equal(addresses, rolloutStatus.Services.AdminAPI.Addresses, cmpopts.EquateEmpty()) &&
		dataplaneAdminAPIService.Name == rolloutStatus.Services.AdminAPI.Name {
		return false, nil
	}

	old := dataplane.DeepCopy()
	if dataplane.Status.RolloutStatus == nil {
		dataplane.Status.RolloutStatus = &operatorv1beta1.DataPlaneRolloutStatus{
			Services: &operatorv1beta1.DataPlaneRolloutStatusServices{
				AdminAPI: &operatorv1beta1.RolloutStatusService{},
			},
		}
	}

	dataplane.Status.RolloutStatus.Services.AdminAPI.Addresses = addresses
	dataplane.Status.RolloutStatus.Services.AdminAPI.Name = dataplaneAdminAPIService.Name
	return true, r.patchRolloutStatus(ctx, log, old, dataplane)
}

// patchRolloutStatus Patches the resource status only when there are changes in the Conditions
func (r *DataPlaneBlueGreenReconciler) patchRolloutStatus(ctx context.Context, log logr.Logger, old, updated *operatorv1beta1.DataPlane) error {
	if rolloutStatusChanged(old, updated) {
		debug(log, "patching DataPlane status", updated, "status", updated.Status)
		return r.Client.Status().Patch(ctx, updated, client.MergeFrom(old))
	}

	return nil
}

func rolloutStatusChanged(old, updated *operatorv1beta1.DataPlane) bool {
	return !cmp.Equal(old.Status.RolloutStatus, updated.Status.RolloutStatus)
}
