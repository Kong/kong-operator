package controllers

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	dataplaneutils "github.com/kong/gateway-operator/internal/utils/dataplane"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
	dataplanevalidation "github.com/kong/gateway-operator/internal/validation/dataplane"
)

// -----------------------------------------------------------------------------
// DataPlaneReconciler
// -----------------------------------------------------------------------------

// DataPlaneReconciler reconciles a DataPlane object
type DataPlaneReconciler struct {
	client.Client
	Scheme                   *runtime.Scheme
	eventRecorder            record.EventRecorder
	ClusterCASecretName      string
	ClusterCASecretNamespace string
	DevelopmentMode          bool
}

// SetupWithManager sets up the controller with the Manager.
func (r *DataPlaneReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.eventRecorder = mgr.GetEventRecorderFor("dataplane")

	return ctrl.NewControllerManagedBy(mgr).
		// watch Dataplane objects
		For(&operatorv1alpha1.DataPlane{}).
		// watch for changes in Secrets created by the dataplane controller
		Owns(&corev1.Secret{}).
		// watch for changes in Services created by the dataplane controller
		Owns(&corev1.Service{}).
		// watch for changes in Deployments created by the dataplane controller
		Owns(&appsv1.Deployment{}).
		Complete(r)
}

// -----------------------------------------------------------------------------
// DataPlaneReconciler - Reconciliation
// -----------------------------------------------------------------------------

// Reconcile moves the current state of an object to the intended state.
func (r *DataPlaneReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := getLogger(ctx, "dataplane", r.DevelopmentMode)

	trace(log, "reconciling DataPlane resource", req)
	dataplane := new(operatorv1alpha1.DataPlane)
	if err := r.Client.Get(ctx, req.NamespacedName, dataplane); err != nil {
		if k8serrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	k8sutils.InitReady(dataplane)

	trace(log, "validating DataPlane resource conditions", dataplane)

	if r.ensureIsMarkedScheduled(dataplane) {
		err := r.patchStatus(ctx, dataplane)
		if err != nil {
			debug(log, "unable to update DataPlane resource", dataplane)
		}
		return ctrl.Result{}, err // requeue will be triggered by the creation or update of the owned object
	}

	trace(log, "exposing DataPlane deployment via service", dataplane)
	createdOrUpdated, dataplaneService, err := r.ensureServiceForDataPlane(ctx, dataplane)
	if err != nil {
		return ctrl.Result{}, err
	}
	if createdOrUpdated {
		debug(log, "service updated", dataplane)
		return ctrl.Result{}, r.ensureDataPlaneServiceStatus(ctx, dataplane, dataplaneService.Name)
	}

	trace(log, "checking readiness of DataPlane service", dataplaneService)
	if dataplaneService.Spec.ClusterIP == "" {
		return ctrl.Result{}, nil // no need to requeue, the update will trigger.
	}

	trace(log, "validating DataPlane configuration", dataplane)

	updated := dataplaneutils.SetDataPlaneDefaults(&dataplane.Spec.DataPlaneDeploymentOptions)
	if updated {
		trace(log, "setting default ENVs", dataplane)
		if err := r.Client.Update(ctx, dataplane); err != nil {
			if k8serrors.IsConflict(err) {
				debug(log, "conflict found when updating DataPlane resource, retrying", dataplane)
				return ctrl.Result{Requeue: true, RequeueAfter: requeueWithoutBackoff}, nil
			}
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil // no need to requeue, the update will trigger.
	}

	// validate dataplane
	err = dataplanevalidation.NewValidator(r.Client).Validate(dataplane)
	if err != nil {
		info(log, "failed to validate dataplane: "+err.Error(), dataplane)
		r.eventRecorder.Event(dataplane, "Warning", "ValidationFailed", err.Error())
		markErr := r.ensureDataPlaneIsMarkedNotProvisioned(ctx, dataplane,
			DataPlaneConditionValidationFailed, err.Error())
		return ctrl.Result{}, markErr
	}

	trace(log, "ensuring mTLS certificate", dataplane)
	createdOrUpdated, certSecret, err := r.ensureCertificate(ctx, dataplane, dataplaneService.Name)
	if err != nil {
		return ctrl.Result{}, err
	}
	if createdOrUpdated {
		debug(log, "mTLS certificate created", dataplane)
		return ctrl.Result{}, nil // requeue will be triggered by the creation or update of the owned object
	}

	trace(log, "looking for existing deployments for DataPlane resource", dataplane)
	createdOrUpdated, dataplaneDeployment, err := r.ensureDeploymentForDataPlane(ctx, dataplane, certSecret.Name)
	if err != nil {
		return ctrl.Result{}, err
	}
	if createdOrUpdated {
		debug(log, "deployment updated", dataplane)
		return ctrl.Result{}, nil // requeue will be triggered by the creation or update of the owned object
	}

	trace(log, "checking readiness of DataPlane deployments", dataplane)

	if dataplaneDeployment.Status.Replicas == 0 || dataplaneDeployment.Status.AvailableReplicas < dataplaneDeployment.Status.Replicas {
		debug(log, "deployment for DataPlane not ready yet", dataplane)
		// Set Ready to false for dataplane as the underlying deployment is not ready.
		k8sutils.SetCondition(
			k8sutils.NewCondition(k8sutils.ReadyType, metav1.ConditionFalse, k8sutils.WaitingToBecomeReadyReason, k8sutils.WaitingToBecomeReadyMessage),
			dataplane,
		)
		return ctrl.Result{}, r.patchStatus(ctx, dataplane)
	}

	r.ensureIsMarkedProvisioned(dataplane)

	if err = r.patchStatus(ctx, dataplane); err != nil {
		debug(log, "unable to reconcile the DataPlane resource", dataplane)
		return ctrl.Result{}, err
	}

	debug(log, "reconciliation complete for DataPlane resource", dataplane)
	return ctrl.Result{}, nil
}

// patchStatus Patches the resource status only when there are changes in the Conditions
func (r *DataPlaneReconciler) patchStatus(ctx context.Context, updated *operatorv1alpha1.DataPlane) error {
	current := &operatorv1alpha1.DataPlane{}

	err := r.Client.Get(ctx, client.ObjectKeyFromObject(updated), current)
	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	}

	if k8sutils.NeedsUpdate(current, updated) {
		return r.Client.Status().Patch(ctx, updated, client.MergeFrom(current))
	}

	return nil
}
