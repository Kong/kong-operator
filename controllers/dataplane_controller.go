package controllers

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	operatorv1alpha1 "github.com/kong/gateway-operator/api/v1alpha1"
	"github.com/kong/gateway-operator/internal/consts"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
)

// -----------------------------------------------------------------------------
// DataPlaneReconciler
// -----------------------------------------------------------------------------

// DataPlaneReconciler reconciles a DataPlane object
type DataPlaneReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// SetupWithManager sets up the controller with the Manager.
func (r *DataPlaneReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorv1alpha1.DataPlane{}).
		Named("DataPlane").
		Complete(r)
}

// -----------------------------------------------------------------------------
// DataPlaneReconciler - Reconciliation
// -----------------------------------------------------------------------------

//+kubebuilder:rbac:groups=gateway-operator.konghq.com,resources=dataplanes,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=gateway-operator.konghq.com,resources=dataplanes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=gateway-operator.konghq.com,resources=dataplanes/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=create;get;list;watch;update;patch
//+kubebuilder:rbac:groups=apps,resources=deployments/status,verbs=get
//+kubebuilder:rbac:groups=core,resources=services,verbs=create;get;list;watch;update;patch
//+kubebuilder:rbac:groups=core,resources=services/status,verbs=get

// Reconcile moves the current state of an object to the intended state.
func (r *DataPlaneReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	debug(log, "reconciling dataplane resource", req)
	dataplane := new(operatorv1alpha1.DataPlane)
	if err := r.Client.Get(ctx, req.NamespacedName, dataplane); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	debug(log, "validating dataplane resource conditions", dataplane)
	changed, err := r.ensureDataPlaneIsMarkedScheduled(ctx, dataplane)
	if err != nil {
		return ctrl.Result{}, err
	}
	if changed {
		debug(log, "dataplane resource now marked as scheduled", dataplane)
		return ctrl.Result{}, nil // no need to requeue, status update will requeue
	}

	debug(log, "validating dataplane configuration", dataplane)
	if len(dataplane.Spec.Env) == 0 && len(dataplane.Spec.EnvFrom) == 0 {
		debug(log, "no ENV config found for dataplane resource, setting defaults", dataplane)
		setDataPlaneDefaults(dataplane)
		if err := r.Client.Update(ctx, dataplane); err != nil {
			if errors.IsConflict(err) {
				debug(log, "conflict found when updating dataplane resource, retrying", dataplane)
				return ctrl.Result{Requeue: true, RequeueAfter: time.Millisecond * 200}, nil
			}
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil // no need to requeue, the update will trigger.
	}

	debug(log, "looking for existing deployments for dataplane resource", dataplane)
	created, dataplaneDeployment, err := r.ensureDeploymentForDataPlane(ctx, dataplane)
	if err != nil {
		return ctrl.Result{}, err
	}
	if created {
		return ctrl.Result{Requeue: true, RequeueAfter: time.Millisecond * 200}, nil // TODO: remove after https://github.com/Kong/gateway-operator/issues/26
	}

	// TODO: updates need to update owned deployment https://github.com/Kong/gateway-operator/issues/27

	debug(log, "checking readiness of dataplane deployments", dataplane)
	if dataplaneDeployment.Status.Replicas == 0 || dataplaneDeployment.Status.AvailableReplicas < dataplaneDeployment.Status.Replicas {
		debug(log, "deployment for dataplane not yet ready, waiting", dataplane)
		return ctrl.Result{Requeue: true, RequeueAfter: time.Millisecond * 200}, nil
	}

	debug(log, "exposing dataplane deployment via service", dataplane)
	created, dataplaneService, err := r.ensureServiceForDataPlane(ctx, dataplane)
	if err != nil {
		return ctrl.Result{}, err
	}
	if created {
		return ctrl.Result{Requeue: true, RequeueAfter: time.Millisecond * 200}, nil // TODO: remove after https://github.com/Kong/gateway-operator/issues/26
	}

	// TODO: updates need to update owned service https://github.com/Kong/gateway-operator/issues/27

	debug(log, "checking readiness of dataplane service", dataplaneService)
	if dataplaneService.Spec.ClusterIP == "" {
		return ctrl.Result{Requeue: true, RequeueAfter: time.Millisecond * 200}, nil
	}

	debug(log, "reconciliation complete for dataplane resource", dataplane)
	return ctrl.Result{}, r.ensureDataPlaneIsMarkedProvisioned(ctx, dataplane)
}

// -----------------------------------------------------------------------------
// DataPlaneReconciler - Status Management
// -----------------------------------------------------------------------------

func (r *DataPlaneReconciler) ensureDataPlaneIsMarkedScheduled(
	ctx context.Context,
	dataplane *operatorv1alpha1.DataPlane,
) (bool, error) {
	isScheduled := false
	for _, condition := range dataplane.Status.Conditions {
		if condition.Type == string(DataPlaneConditionTypeProvisioned) {
			isScheduled = true
		}
	}

	if !isScheduled {
		dataplane.Status.Conditions = append(dataplane.Status.Conditions, metav1.Condition{
			Type:               string(DataPlaneConditionTypeProvisioned),
			Reason:             DataPlaneConditionReasonPodsNotReady,
			Status:             metav1.ConditionFalse,
			Message:            "dataplane resource is scheduled for provisioning",
			ObservedGeneration: dataplane.Generation,
			LastTransitionTime: metav1.Now(),
		})
		return true, r.Client.Status().Update(ctx, dataplane)
	}

	return false, nil
}

func (r *DataPlaneReconciler) ensureDataPlaneIsMarkedProvisioned(
	ctx context.Context,
	dataplane *operatorv1alpha1.DataPlane,
) error {
	updatedConditions := make([]metav1.Condition, 0)
	for _, condition := range dataplane.Status.Conditions {
		if condition.Type == string(DataPlaneConditionTypeProvisioned) {
			condition.Status = metav1.ConditionTrue
			condition.Reason = DataPlaneConditionReasonPodsReady
			condition.Message = "pods for all Deployments are ready"
			condition.ObservedGeneration = dataplane.Generation
			condition.LastTransitionTime = metav1.Now()
		}
		updatedConditions = append(updatedConditions, condition)
	}

	dataplane.Status.Conditions = updatedConditions
	return r.Status().Update(ctx, dataplane)
}

// -----------------------------------------------------------------------------
// DataPlaneReconciler - Owned Resource Management
// -----------------------------------------------------------------------------

func (r *DataPlaneReconciler) ensureDeploymentForDataPlane(
	ctx context.Context,
	dataplane *operatorv1alpha1.DataPlane,
) (bool, *appsv1.Deployment, error) {
	deployments, err := k8sutils.ListDeploymentsForOwner(
		ctx,
		r.Client,
		consts.GatewayOperatorControlledLabel,
		consts.DataPlaneManagedLabelValue,
		dataplane.Namespace,
		dataplane.UID,
	)
	if err != nil {
		return false, nil, err
	}

	count := len(deployments)
	if count > 1 {
		return false, nil, fmt.Errorf("found %d deployments for dataplane currently unsupported: expected 1 or less", count)
	}

	if count == 1 {
		return false, &deployments[0], nil
	}

	deployment := generateNewDeploymentForDataPlane(dataplane)
	setObjectOwner(dataplane, deployment)
	labelObjForDataplane(deployment)
	return true, deployment, r.Client.Create(ctx, deployment)
}

func (r *DataPlaneReconciler) ensureServiceForDataPlane(
	ctx context.Context,
	dataplane *operatorv1alpha1.DataPlane,
) (bool, *corev1.Service, error) {
	services, err := k8sutils.ListServicesForOwner(
		ctx,
		r.Client,
		consts.GatewayOperatorControlledLabel,
		consts.DataPlaneManagedLabelValue,
		dataplane.Namespace,
		dataplane.UID,
	)
	if err != nil {
		return false, nil, err
	}

	count := len(services)
	if count > 1 {
		return false, nil, fmt.Errorf("found %d services for dataplane currently unsupported: expected 1 or less", count)
	}

	if count == 1 {
		return false, &services[0], nil
	}

	service := generateNewServiceForDataplane(dataplane)
	labelObjForDataplane(service)

	return true, service, r.Client.Create(ctx, service)
}
