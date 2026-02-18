package dataplane

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	kcfgkonnect "github.com/kong/kong-operator/v2/api/konnect"
	ctrlconsts "github.com/kong/kong-operator/v2/controller/consts"
	"github.com/kong/kong-operator/v2/controller/pkg/extensions"
	extensionserrors "github.com/kong/kong-operator/v2/controller/pkg/extensions/errors"
	extensionskonnect "github.com/kong/kong-operator/v2/controller/pkg/extensions/konnect"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	k8sresources "github.com/kong/kong-operator/v2/pkg/utils/kubernetes/resources"
)

// -----------------------------------------------------------------------------
// DataPlaneReconciler
// -----------------------------------------------------------------------------

// Reconciler reconciles a DataPlane object.
type Reconciler struct {
	client.Client

	ControllerOptions controller.Options

	eventRecorder            events.EventRecorder
	ClusterCASecretName      string
	ClusterCASecretNamespace string
	SecretLabelSelector      string
	// ConfigMapLabelSelector is the label selector configured at the oprator level.
	// When not empty, it is used as the config map label selector of all reconcilers.
	ConfigMapLabelSelector string
	DefaultImage           string
	KonnectEnabled         bool
	EnforceConfig          bool
	LoggingMode            logging.Mode
	ValidateDataPlaneImage bool
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	r.eventRecorder = mgr.GetEventRecorder("dataplane")

	return DataPlaneWatchBuilder(mgr, r.KonnectEnabled).
		WithOptions(r.ControllerOptions).
		Complete(r)
}

// -----------------------------------------------------------------------------
// DataPlaneReconciler - Reconciliation
// -----------------------------------------------------------------------------

// Reconcile moves the current state of an object to the intended state.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.GetLogger(ctx, "dataplane", r.LoggingMode)

	log.Trace(logger, "reconciling DataPlane resource")
	dpNn := req.NamespacedName
	dataplane := new(operatorv1beta1.DataPlane)
	if err := r.Get(ctx, dpNn, dataplane); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if k8sutils.InitReady(dataplane) {
		if patched, err := patchDataPlaneStatus(ctx, r.Client, logger, dataplane); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed initializing DataPlane Ready condition: %w", err)
		} else if patched {
			return ctrl.Result{}, nil
		}
	}

	if err := r.initSelectorInStatus(ctx, logger, dataplane); err != nil {
		if apierrors.IsNotFound(err) {
			log.Debug(logger, "DataPlane resource not found during status selector initialization, it might have been deleted")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed updating DataPlane with selector in Status: %w", err)
	}

	log.Trace(logger, "applying extensions")
	stop, result, err := extensions.ApplyExtensions(ctx, r.Client, dataplane, r.KonnectEnabled, &extensionskonnect.DataPlaneKonnectExtensionProcessor{})
	if err != nil {
		if extensionserrors.IsKonnectExtensionError(err) {
			log.Debug(logger, "failed to apply extensions", "err", err)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	if stop || !result.IsZero() {
		return result, nil
	}

	log.Trace(logger, "exposing DataPlane deployment admin API via headless service")
	res, dataplaneAdminService, err := ensureAdminServiceForDataPlane(ctx, r.Client, dataplane,
		client.MatchingLabels{
			consts.DataPlaneServiceStateLabel: consts.DataPlaneStateLabelValueLive,
		},
		k8sresources.LabelSelectorFromDataPlaneStatusSelectorServiceOpt(dataplane),
	)
	if err != nil {
		if apierrors.IsConflict(err) {
			return ctrl.Result{RequeueAfter: ctrlconsts.RequeueWithoutBackoff}, nil
		}
		return ctrl.Result{}, err
	}
	switch res {
	case op.Created, op.Updated:
		log.Debug(logger, "DataPlane admin service modified", "service", dataplaneAdminService.Name, "reason", res)
		return ctrl.Result{}, nil // dataplane admin service creation/update will trigger reconciliation
	case op.Noop:
	case op.Deleted: // This should not happen.
	}

	log.Trace(logger, "exposing DataPlane deployment via service")
	additionalServiceLabels := map[string]string{
		consts.DataPlaneServiceStateLabel: consts.DataPlaneStateLabelValueLive,
	}
	serviceRes, dataplaneIngressService, err := ensureIngressServiceForDataPlane(
		ctx,
		log.GetLogger(ctx, "dataplane_ingress_service", r.LoggingMode),
		r.Client,
		dataplane,
		additionalServiceLabels,
		k8sresources.LabelSelectorFromDataPlaneStatusSelectorServiceOpt(dataplane),
		k8sresources.ServicePortsFromDataPlaneIngressOpt(dataplane),
	)
	if err != nil {
		return ctrl.Result{}, err
	}
	if serviceRes == op.Created || serviceRes == op.Updated {
		log.Debug(logger, "DataPlane ingress service created/updated", "service", dataplaneIngressService.Name)
		return ctrl.Result{}, nil
	}

	dataplaneServiceChanged, err := r.ensureDataPlaneServiceStatus(ctx, logger, dataplane, dataplaneIngressService.Name)
	if err != nil {
		return ctrl.Result{}, err
	}
	if dataplaneServiceChanged {
		log.Debug(logger, "ingress service updated in the dataplane status")
		return ctrl.Result{}, nil // dataplane status update will trigger reconciliation
	}

	log.Trace(logger, "ensuring mTLS certificate")
	res, certSecret, err := ensureDataPlaneCertificate(
		ctx,
		r.Client,
		dataplane,
		types.NamespacedName{
			Namespace: r.ClusterCASecretNamespace,
			Name:      r.ClusterCASecretName,
		},
		types.NamespacedName{
			Namespace: dataplaneAdminService.Namespace,
			Name:      dataplaneAdminService.Name,
		},
		r.SecretLabelSelector,
	)
	if err != nil {
		return ctrl.Result{}, err
	}
	if res != op.Noop {
		log.Debug(logger, "mTLS certificate created/updated")
		return ctrl.Result{}, nil // requeue will be triggered by the creation or update of the owned object
	}

	log.Trace(logger, "checking readiness of DataPlane service", "service", dataplaneIngressService.Name)
	if dataplaneIngressService.Spec.ClusterIP == "" {
		return ctrl.Result{}, nil // no need to requeue, the update will trigger.
	}

	log.Trace(logger, "ensuring DataPlane has service addresses in status", "service", dataplaneIngressService.Name)
	if updated, err := r.ensureDataPlaneAddressesStatus(ctx, logger, dataplane, dataplaneIngressService); err != nil {
		return ctrl.Result{}, err
	} else if updated {
		log.Debug(logger, "dataplane status.Addresses updated")
		return ctrl.Result{}, nil // no need to requeue, the update will trigger.
	}

	deploymentLabels := client.MatchingLabels{
		consts.DataPlaneDeploymentStateLabel: consts.DataPlaneStateLabelValueLive,
	}
	deploymentOpts := []k8sresources.DeploymentOpt{
		labelSelectorFromDataPlaneStatusSelectorDeploymentOpt(dataplane),
	}

	// if the dataplane is configured with Konnect, the status/ready endpoint should be set as the readiness probe.
	if _, konnectApplied := k8sutils.GetCondition(kcfgkonnect.KonnectExtensionAppliedType, dataplane); konnectApplied {
		deploymentOpts = append(deploymentOpts, statusReadyEndpointDeploymentOpt(dataplane))
	}

	log.Trace(logger, "ensuring generation of deployment configuration for KongPluginInstallations configured for DataPlane")
	kpisForDeployment, requeue, err := ensureMappedConfigMapToKongPluginInstallationForDataPlane(ctx, logger, r.Client, dataplane, r.ConfigMapLabelSelector)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("cannot ensure KongPluginInstallation for DataPlane: %w", err)
	}
	if requeue {
		return ctrl.Result{Requeue: true}, nil
	}
	deploymentOpts = append(deploymentOpts, withCustomPlugins(kpisForDeployment...))

	deploymentBuilder := NewDeploymentBuilder(logger.WithName("deployment_builder"), r.Client).
		WithClusterCertificate(certSecret.Name).
		WithOpts(deploymentOpts...).
		WithDefaultImage(r.DefaultImage).
		WithAdditionalLabels(deploymentLabels).
		WithSecretLabelSelector(r.SecretLabelSelector)

	deployment, res, err := deploymentBuilder.BuildAndDeploy(ctx, dataplane, r.EnforceConfig, r.ValidateDataPlaneImage)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("could not build Deployment for DataPlane %s: %w", dpNn, err)
	}
	if res != op.Noop {
		return ctrl.Result{}, nil
	}

	res, _, err = ensureHPAForDataPlane(ctx, r.Client, logger, dataplane, deployment.Name)
	if err != nil {
		return ctrl.Result{}, err
	}
	if res != op.Noop {
		return ctrl.Result{}, nil
	}

	res, _, err = ensurePodDisruptionBudgetForDataPlane(ctx, r.Client, logger, dataplane)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("could not ensure PodDisruptionBudget for DataPlane %s: %w", dpNn, err)
	}
	if res != op.Noop {
		log.Debug(logger, "PodDisruptionBudget created/updated")
		return ctrl.Result{}, nil
	}

	if res, err := ensureDataPlaneReadyStatus(ctx, r.Client, logger, dataplane, dataplane.Generation); err != nil {
		return ctrl.Result{}, err
	} else if !res.IsZero() {
		return res, nil
	}

	log.Debug(logger, "reconciliation complete for DataPlane resource")
	return ctrl.Result{}, nil
}

func (r *Reconciler) initSelectorInStatus(ctx context.Context, logger logr.Logger, dataplane *operatorv1beta1.DataPlane) error {
	if dataplane.Status.Selector != "" {
		return nil
	}

	dataplane.Status.Selector = uuid.New().String()
	_, err := patchDataPlaneStatus(ctx, r.Client, logger, dataplane)
	return err
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

func statusReadyEndpointDeploymentOpt(_ *operatorv1beta1.DataPlane) func(s *appsv1.Deployment) {
	return func(d *appsv1.Deployment) {
		if container := k8sutils.GetPodContainerByName(&d.Spec.Template.Spec, consts.DataPlaneProxyContainerName); container != nil {
			container.ReadinessProbe = k8sresources.GenerateDataPlaneReadinessProbe(consts.DataPlaneStatusReadyEndpoint)
		}
	}
}
