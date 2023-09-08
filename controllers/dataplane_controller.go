package controllers

import (
	"context"
	"fmt"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1beta1 "github.com/kong/gateway-operator/apis/v1beta1"
	"github.com/kong/gateway-operator/internal/consts"
	dataplaneutils "github.com/kong/gateway-operator/internal/utils/dataplane"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
	k8sresources "github.com/kong/gateway-operator/internal/utils/kubernetes/resources"
)

// -----------------------------------------------------------------------------
// DataPlaneReconciler
// -----------------------------------------------------------------------------

type dataPlaneValidator interface {
	Validate(*operatorv1beta1.DataPlane) error
}

// DataPlaneReconciler reconciles a DataPlane object
type DataPlaneReconciler struct {
	client.Client
	Scheme                   *runtime.Scheme
	eventRecorder            record.EventRecorder
	ClusterCASecretName      string
	ClusterCASecretNamespace string
	DevelopmentMode          bool
	Validator                dataPlaneValidator
}

// SetupWithManager sets up the controller with the Manager.
func (r *DataPlaneReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.eventRecorder = mgr.GetEventRecorderFor("dataplane")

	return DataPlaneWatchBuilder(mgr).
		Complete(r)
}

// -----------------------------------------------------------------------------
// DataPlaneReconciler - Reconciliation
// -----------------------------------------------------------------------------

// Reconcile moves the current state of an object to the intended state.
func (r *DataPlaneReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := getLogger(ctx, "dataplane", r.DevelopmentMode)

	trace(log, "reconciling DataPlane resource", req)
	dataplane := new(operatorv1beta1.DataPlane)
	if err := r.Client.Get(ctx, req.NamespacedName, dataplane); err != nil {
		if k8serrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	k8sutils.InitReady(dataplane)
	if err := r.initSelectorInStatus(ctx, dataplane); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed updating DataPlane with selector in Status: %w", err)
	}

	trace(log, "validating DataPlane resource conditions", dataplane)
	if r.ensureIsMarkedScheduled(dataplane) {
		err := patchDataPlaneStatus(ctx, r.Client, log, dataplane)
		if err != nil {
			debug(log, "unable to update DataPlane resource", dataplane)
		}
		return ctrl.Result{}, err // requeue will be triggered by the creation or update of the owned object
	}

	{
		oldDataPlane := dataplane.DeepCopy()
		if updated := dataplaneutils.SetDataPlaneDefaults(&dataplane.Spec.DataPlaneOptions); updated {
			trace(log, "setting default ENVs", dataplane)
			if err := r.Client.Patch(ctx, dataplane, client.MergeFrom(oldDataPlane)); err != nil {
				if k8serrors.IsConflict(err) {
					debug(log, "conflict found when patching DataPlane, retrying", dataplane)
					return ctrl.Result{Requeue: true, RequeueAfter: requeueWithoutBackoff}, nil
				}
				return ctrl.Result{}, fmt.Errorf("failed patching DataPlane's environment variables: %w", err)
			}
			return ctrl.Result{}, nil // no need to requeue, the update will trigger.
		}
	}

	trace(log, "validating DataPlane configuration", dataplane)
	err := r.Validator.Validate(dataplane)
	if err != nil {
		info(log, "failed to validate dataplane: "+err.Error(), dataplane)
		r.eventRecorder.Event(dataplane, "Warning", "ValidationFailed", err.Error())
		markErr := r.ensureDataPlaneIsMarkedNotProvisioned(ctx, log, dataplane,
			DataPlaneConditionValidationFailed, err.Error())
		return ctrl.Result{}, markErr
	}

	trace(log, "exposing DataPlane deployment admin API via headless service", dataplane)
	res, dataplaneAdminService, err := ensureAdminServiceForDataPlane(ctx, r.Client, dataplane,
		client.MatchingLabels{
			consts.DataPlaneServiceStateLabel: consts.DataPlaneStateLabelValueLive,
		},
		labelSelectorFromDataPlaneStatusSelectorServiceOpt(dataplane),
	)
	if err != nil {
		return ctrl.Result{}, err
	}
	switch res {
	case Created, Updated:
		debug(log, "DataPlane admin service modified", dataplane, "service", dataplaneAdminService.Name, "reason", res)
		return ctrl.Result{}, nil // dataplane admin service creation/update will trigger reconciliation
	case Noop:
	}

	trace(log, "exposing DataPlane deployment via service", dataplane)
	additionalServiceLabels := map[string]string{
		consts.DataPlaneServiceStateLabel: consts.DataPlaneStateLabelValueLive,
	}
	serviceRes, dataplaneIngressService, err := ensureIngressServiceForDataPlane(
		ctx,
		getLogger(ctx, "dataplane_ingress_service", r.DevelopmentMode),
		r.Client,
		dataplane,
		additionalServiceLabels,
		labelSelectorFromDataPlaneStatusSelectorServiceOpt(dataplane),
	)
	if err != nil {
		return ctrl.Result{}, err
	}
	if serviceRes == Created || serviceRes == Updated {
		debug(log, "DataPlane ingress service created/updated", dataplane, "service", dataplaneIngressService.Name)
		return ctrl.Result{}, nil
	}

	dataplaneServiceChanged, err := r.ensureDataPlaneServiceStatus(ctx, log, dataplane, dataplaneIngressService.Name)
	if err != nil {
		return ctrl.Result{}, err
	}
	if dataplaneServiceChanged {
		debug(log, "ingress service updated in the dataplane status", dataplane)
		return ctrl.Result{}, nil // dataplane status update will trigger reconciliation
	}

	trace(log, "ensuring mTLS certificate", dataplane)
	certCreatedOrUpdated, certSecret, err := ensureDataPlaneCertificate(ctx, r.Client, dataplane,
		types.NamespacedName{
			Namespace: r.ClusterCASecretNamespace,
			Name:      r.ClusterCASecretName,
		},
		types.NamespacedName{
			Namespace: dataplaneAdminService.Namespace,
			Name:      dataplaneAdminService.Name,
		},
	)
	if err != nil {
		return ctrl.Result{}, err
	}
	if certCreatedOrUpdated {
		debug(log, "mTLS certificate created", dataplane)
		return ctrl.Result{}, nil // requeue will be triggered by the creation or update of the owned object
	}

	trace(log, "checking readiness of DataPlane service", dataplaneIngressService)
	if dataplaneIngressService.Spec.ClusterIP == "" {
		return ctrl.Result{}, nil // no need to requeue, the update will trigger.
	}

	trace(log, "ensuring DataPlane has service addesses in status", dataplaneIngressService)
	if updated, err := r.ensureDataPlaneAddressesStatus(ctx, log, dataplane, dataplaneIngressService); err != nil {
		return ctrl.Result{}, err
	} else if updated {
		debug(log, "dataplane status.Addresses updated", dataplane)
		return ctrl.Result{}, nil // no need to requeue, the update will trigger.
	}

	res, _, err = ensureDeploymentForDataPlane(ctx, r.Client, log, r.DevelopmentMode, dataplane,
		client.MatchingLabels{
			consts.DataPlaneDeploymentStateLabel: consts.DataPlaneStateLabelValueLive,
		},
		k8sresources.WithTLSVolumeFromSecret(consts.DataPlaneClusterCertificateVolumeName, certSecret.Name),
		k8sresources.WithClusterCertificateMount(consts.DataPlaneClusterCertificateVolumeName),
		labelSelectorFromDataPlaneStatusSelectorDeploymentOpt(dataplane),
	)
	if err != nil {
		return ctrl.Result{}, err
	}
	switch res {
	case Created, Updated:
		debug(log, "deployment modified", dataplane, "reason", res)
		return ctrl.Result{}, nil // requeue will be triggered by the creation of the owned object
	default:
		debug(log, "no need for deployment update", dataplane)
	}

	trace(log, "checking readiness of DataPlane deployments", dataplane)

	if res, err := ensureDataPlaneReadyStatus(ctx, r.Client, log, dataplane); err != nil {
		return ctrl.Result{}, err
	} else if res.Requeue {
		return res, nil
	}

	debug(log, "reconciliation complete for DataPlane resource", dataplane)
	return ctrl.Result{}, nil
}

func (r *DataPlaneReconciler) initSelectorInStatus(ctx context.Context, dataplane *operatorv1beta1.DataPlane) error {
	if dataplane.Status.Selector != "" {
		return nil
	}

	oldDataplane := dataplane.DeepCopy()
	dataplane.Status.Selector = uuid.New().String()
	if err := r.Client.Status().Patch(ctx, dataplane, client.MergeFrom(oldDataplane)); err != nil {
		return err
	}
	return nil
}

// labelSelectorFromDataPlaneStatusSelectorDeploymentOpt returns a DeploymentOpt
// function which will set Deployment's selector and spec template labels, based
// on provided DataPlane's Status selector field.
func labelSelectorFromDataPlaneStatusSelectorDeploymentOpt(dataplane *operatorv1beta1.DataPlane) func(s *appsv1.Deployment) {
	return func(d *appsv1.Deployment) {
		if dataplane.Status.Selector != "" {
			d.Labels[consts.OperatorLabelSelector] = dataplane.Status.Selector
			d.Spec.Selector.MatchLabels[consts.OperatorLabelSelector] = dataplane.Status.Selector
			d.Spec.Template.Labels[consts.OperatorLabelSelector] = dataplane.Status.Selector
		}
	}
}

// labelSelectorFromDataPlaneStatusSelectorServiceOpt returns a ServiceOpt function
// which will set Service's selector based on provided DataPlane's Status selector
// field.
func labelSelectorFromDataPlaneStatusSelectorServiceOpt(dataplane *operatorv1beta1.DataPlane) func(s *corev1.Service) {
	return func(s *corev1.Service) {
		if dataplane.Status.Selector != "" {
			s.Spec.Selector[consts.OperatorLabelSelector] = dataplane.Status.Selector
		}
	}
}

// addressesChanged returns a boolean indicating whether the addresses in provided
// DataPlane stauses differ.
func addressesChanged(current, updated *operatorv1beta1.DataPlane) bool {
	return !cmp.Equal(current.Status.Addresses, updated.Status.Addresses)
}

func readinessChanged(current, updated *operatorv1beta1.DataPlane) bool {
	return current.Status.ReadyReplicas != updated.Status.ReadyReplicas ||
		current.Status.Replicas != updated.Status.Replicas
}
