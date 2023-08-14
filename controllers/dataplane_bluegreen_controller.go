package controllers

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/uuid"
	"github.com/samber/lo"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	operatorv1beta1 "github.com/kong/gateway-operator/apis/v1beta1"
	"github.com/kong/gateway-operator/internal/consts"
	dataplaneutils "github.com/kong/gateway-operator/internal/utils/dataplane"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
	k8sresources "github.com/kong/gateway-operator/internal/utils/kubernetes/resources"
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

	if err := r.reduceLiveDeployments(ctx, log, &dataplane); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to reduce live deployments: %w", err)
	}

	if err := r.initSelectorInRolloutStatus(ctx, &dataplane); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed updating DataPlane with selector in Rollout Status: %w", err)
	}

	c, ok := k8sutils.GetCondition(consts.DataPlaneConditionTypeRolledOut, dataplane.Status.RolloutStatus)
	if !ok || c.ObservedGeneration != dataplane.Generation {
		err := r.ensureRolledOutCondition(ctx, log, &dataplane, metav1.ConditionFalse, consts.DataPlaneConditionReasonRolloutProgressing, consts.DataPlaneConditionMessageRolledOutRolloutInitialied)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	{
		oldDataPlane := dataplane.DeepCopy()
		if updated := dataplaneutils.SetDataPlaneDefaults(&dataplane.Spec.DataPlaneOptions); updated {
			trace(log, "setting default ENVs", dataplane)
			if err := r.Client.Patch(ctx, &dataplane, client.MergeFrom(oldDataPlane)); err != nil {
				if k8serrors.IsConflict(err) {
					debug(log, "conflict found when patching DataPlane, retrying", dataplane)
					return ctrl.Result{Requeue: true, RequeueAfter: requeueWithoutBackoff}, nil
				}
				return ctrl.Result{}, fmt.Errorf("failed patching DataPlane's environment variables: %w", err)
			}
			return ctrl.Result{}, nil // no need to requeue, the patch will trigger.
		}
	}

	// DataPlane is ready and we can proceed with deploying preview resources.

	// Ensure "preview" Admin API service.
	res, dataplaneAdminService, err := r.ensurePreviewAdminAPIService(ctx, &dataplane)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed ensuring that preview Admin API Service exists for DataPlane %s/%s: %w", dataplane.Namespace, dataplane.Name, err)
	}
	switch res {
	case Created, Updated:
		debug(log, "DataPlane preview admin service modified", dataplane, "service", dataplaneAdminService.Name, "reason", res)
		return ctrl.Result{}, nil // dataplane admin service creation/update will trigger reconciliation
	case Noop:
		debug(log, "no need for preview Admin API service update", dataplane)
	}

	if updated, err := r.ensureDataPlaneAdminAPIInRolloutStatus(ctx, log, &dataplane, dataplaneAdminService); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed updating rollout status with preview Admin API service addresses: %w", err)
	} else if updated {
		return ctrl.Result{}, nil
	}

	trace(log, "ensuring mTLS certificate", dataplane)
	createdOrUpdated, certSecret, err := ensureCertificate(ctx, r.Client, &dataplane,
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
	if createdOrUpdated {
		debug(log, "mTLS certificate created", dataplane)
		return ctrl.Result{}, nil // requeue will be triggered by the creation or update of the owned object
	}

	// Ensure "preview" Ingress service.
	res, previewIngressService, err := r.ensurePreviewIngressService(ctx, log, &dataplane)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed ensuring preview Ingress service for DataPlane %s/%s: %w", dataplane.Namespace, dataplane.Name, err)
	}
	switch res {
	case Created, Updated:
		debug(log, "preview ingress service modified", dataplane, "reason", res)
		return ctrl.Result{}, nil
	default:
		debug(log, "no need for preview ingress service update", dataplane)
	}
	// ensure status of "preview" service is updated in status.rollout.services.
	if updated, err := r.ensureDataPlaneRolloutIngressServiceStatus(ctx, log, &dataplane, previewIngressService); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed updating rollout status with preview ingress service addresses: %w", err)
	} else if updated {
		return ctrl.Result{}, nil
	}

	// Ensure "preview" Deployment.
	res, deployment, err := ensureDeploymentForDataPlane(ctx, r.Client, log, r.DevelopmentMode, &dataplane,
		client.MatchingLabels{
			consts.DataPlaneDeploymentStateLabel: consts.DataPlaneStateLabelValuePreview,
		},
		k8sresources.WithTLSVolumeFromSecret(consts.DataPlaneClusterCertificateVolumeName, certSecret.Name),
		k8sresources.WithClusterCertificateMount(consts.DataPlaneClusterCertificateVolumeName),
		labelSelectorFromDataPlaneRolloutStatusSelectorDeploymentOpt(&dataplane),
	)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to ensure Deployment for DataPlane: %w", err)
	}
	switch res {
	case Created, Updated:
		debug(log, "deployment modified", dataplane, "reason", res)
		return ctrl.Result{}, nil // requeue will be triggered by the creation or update of the owned object
	default:
		debug(log, "no need for deployment update", dataplane)
	}

	// TODO: check if the preview service is available.
	if deployment.Status.Replicas == 0 ||
		deployment.Status.AvailableReplicas != deployment.Status.Replicas ||
		deployment.Status.ReadyReplicas != deployment.Status.Replicas {
		trace(log, "preview deployment for DataPlane not ready yet", dataplane)
		err := r.ensureRolledOutCondition(ctx, log, &dataplane, metav1.ConditionFalse, consts.DataPlaneConditionReasonRolloutProgressing, consts.DataPlaneConditionMessageRolledOutPreviewDeploymentNotYetReady)
		return ctrl.Result{}, err
	}

	// TODO: Perform promotion condition checks to verify we can proceed
	// Ref: https://github.com/Kong/gateway-operator/issues/974

	if proceedWithPromotion, err := canProceedWithPromotion(dataplane); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed checking if DataPlane %s/%s can be promoted: %w", dataplane.Namespace, dataplane.Name, err)
	} else if !proceedWithPromotion {
		debug(log, "DataPlane preview resources cannot be promoted yet", dataplane,
			"promotion_strategy", dataplane.Spec.Deployment.Rollout.Strategy.BlueGreen.Promotion.Strategy)

		err := r.ensureRolledOutCondition(ctx, log, &dataplane, metav1.ConditionFalse, consts.DataPlaneConditionReasonRolloutAwaitingPromotion, "")
		return ctrl.Result{}, err
	}

	if err = r.ensureRolledOutCondition(ctx, log, &dataplane, metav1.ConditionFalse, consts.DataPlaneConditionReasonRolloutPromotionInProgress, ""); err != nil {
		return ctrl.Result{}, err
	}

	// Ensure that the live deployment selector is equal to the preview deployment selector to trigger the promotion.
	if updated, err := r.ensurePreviewSelectorOverridesLive(ctx, &dataplane); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update DataPlane %s/%s: %w", dataplane.Namespace, dataplane.Name, err)
	} else if updated {
		debug(log, "preview deployment selector assigned to a live selector, promotion in progress", dataplane)
		return ctrl.Result{}, nil
	}

	// Wait until the live services have the expected selector - current preview deployment selector.
	expectedLiveSelector := map[string]string{
		"app":                        dataplane.Name,
		consts.OperatorLabelSelector: dataplane.Status.RolloutStatus.Deployment.Selector,
	}
	for _, serviceType := range []consts.ServiceType{
		consts.DataPlaneProxyServiceLabelValue,
		consts.DataPlaneAdminServiceLabelValue,
	} {
		if ok, err := r.waitForLiveServiceSelectorsPropagation(ctx,
			&dataplane,
			serviceType,
			expectedLiveSelector,
		); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed waiting for live %q service to have the expected selector: %w", serviceType, err)
		} else if !ok {
			debug(log, fmt.Sprintf("%q live service do not have the expected selector yet, delegating to DP controller", serviceType), dataplane)
			return r.DataPlaneController.Reconcile(ctx, req)
		}
	}

	{
		// Promotion is effectively done (live services point to the preview).

		// Let's label the preview deployment as live so that it's easily retrievable.
		previewDeploymentSelector := dataplane.Status.RolloutStatus.Deployment.Selector
		if updated, err := r.ensurePreviewDeploymentLabeledLive(ctx, log, &dataplane, previewDeploymentSelector); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to ensure preview deployment becomes live %s/%s: %w", dataplane.Namespace, dataplane.Name, err)
		} else if updated {
			trace(log, "preview deployment labeled as live", dataplane)
		}

		// We can clear the selector in RolloutStatus which will cause next
		// reconciliation to create new preview.
		old := dataplane.DeepCopy()
		dataplane.Status.RolloutStatus.Deployment.Selector = ""
		if err := r.Client.Status().Patch(ctx, &dataplane, client.MergeFrom(old)); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed updating DataPlane's RolloutStatus: %w", err)
		}
	}

	// TODO: Even if we set the condition to true here, it will shortly be set to false in the next reconcile loop.
	// It is so because we trigger another rollout cycle despite no changes in the DataPlane spec.
	// We might consider changing the logic to not trigger a rollout cycle if there are no changes in the spec.
	if err = r.ensureRolledOutCondition(ctx, log, &dataplane, metav1.ConditionTrue, consts.DataPlaneConditionReasonRolloutPromotionDone, ""); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.resetPromoteWhenReadyAnnotation(ctx, &dataplane); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed resetting promote-when-ready annotation: %w", err)
	}

	debug(log, "BlueGreen reconciliation complete for DataPlane resource", dataplane)
	return ctrl.Result{}, nil
}

// resetPromoteWhenReadyAnnotation resets promote-when-ready DataPlane annotation.
// This makes the DataPlane ready for the next rollout cycle and prevents unintentional promotion.
func (r *DataPlaneBlueGreenReconciler) resetPromoteWhenReadyAnnotation(
	ctx context.Context,
	dataplane *operatorv1beta1.DataPlane,
) error {
	oldDp := dataplane.DeepCopy()
	delete(dataplane.Annotations, operatorv1beta1.DataPlanePromoteWhenReadyAnnotationKey)
	if err := r.Client.Patch(ctx, dataplane, client.MergeFrom(oldDp)); err != nil {
		return fmt.Errorf("failed resetting promote-when-ready annotation: %w", err)
	}
	return nil
}

// reduceLiveDeployments reduces the number of live deployments to 1 by deleting the oldest ones.
// It's used to reduce the number of live deployments that are not being used anymore after promotion (the old live
// deployment gets "replaced" by the preview deployment).
func (r *DataPlaneBlueGreenReconciler) reduceLiveDeployments(
	ctx context.Context,
	log logr.Logger,
	dataplane *operatorv1beta1.DataPlane,
) error {
	matchingLabels := client.MatchingLabels{
		"app":                                dataplane.Name,
		consts.DataPlaneDeploymentStateLabel: consts.DataPlaneStateLabelValueLive,
	}
	deployments, err := k8sutils.ListDeploymentsForOwner(
		ctx,
		r.Client,
		dataplane.Namespace,
		dataplane.UID,
		matchingLabels,
	)
	if err != nil {
		return fmt.Errorf("failed listing live deployments: %w", err)
	}

	// If there's only one or no deployments, there's nothing to do.
	if len(deployments) < 2 {
		return nil
	}

	// Sort deployments by creation timestamp, so that we can delete the oldest ones.
	sort.Slice(deployments, func(i, j int) bool {
		return deployments[i].CreationTimestamp.Before(&deployments[j].CreationTimestamp)
	})
	// Delete all but the last deployment.
	for _, deployment := range deployments[:len(deployments)-1] {
		deployment := deployment
		debug(log, "reducing live deployment", dataplane,
			"deployment", fmt.Sprintf("%s/%s", deployment.Namespace, deployment.Name))

		if err := r.Client.Delete(ctx, &deployment); err != nil {
			return fmt.Errorf("failed deleting live deployment %s/%s: %w", deployment.Namespace, deployment.Name, err)
		}
	}
	return nil
}

// ensureRolledOutCondition ensures that DataPlane rollout status contains RolledOut
// Condition with provided status, reason and message.
func (r *DataPlaneBlueGreenReconciler) ensureRolledOutCondition(
	ctx context.Context,
	log logr.Logger,
	dataplane *operatorv1beta1.DataPlane,
	status metav1.ConditionStatus,
	reason k8sutils.ConditionReason,
	message string,
) error {
	c, ok := k8sutils.GetCondition(consts.DataPlaneConditionTypeRolledOut, dataplane.Status.RolloutStatus)
	if ok && c.ObservedGeneration == dataplane.Generation && c.Status == status && c.Reason == string(reason) && c.Message == message {
		// DataPlane rollout status already contains this condition.
		return nil
	}

	oldDataPlane := dataplane.DeepCopy()
	k8sutils.SetCondition(
		k8sutils.NewConditionWithGeneration(consts.DataPlaneConditionTypeRolledOut, status, reason, message, dataplane.Generation),
		dataplane.Status.RolloutStatus,
	)
	_, err := r.patchRolloutStatus(ctx, log, oldDataPlane, dataplane)
	if err != nil {
		return fmt.Errorf("failed patching Rollout Status Conditions for DataPlane %s/%s: %w", dataplane.Namespace, dataplane.Name, err)
	}
	return nil
}

// labelSelectorFromDataPlaneRolloutStatusSelectorDeploymentOpt returns a DeploymentOpt
// function which will set Deployment's selector and spec template labels, based
// on provided DataPlane's Rollout Status selector field.
func labelSelectorFromDataPlaneRolloutStatusSelectorDeploymentOpt(dataplane *operatorv1beta1.DataPlane) func(s *appsv1.Deployment) {
	return func(d *appsv1.Deployment) {
		if dataplane.Status.RolloutStatus != nil &&
			dataplane.Status.RolloutStatus.Deployment != nil &&
			dataplane.Status.RolloutStatus.Deployment.Selector != "" {
			d.Labels[consts.OperatorLabelSelector] = dataplane.Status.RolloutStatus.Deployment.Selector
			d.Spec.Selector.MatchLabels[consts.OperatorLabelSelector] = dataplane.Status.RolloutStatus.Deployment.Selector
			d.Spec.Template.Labels[consts.OperatorLabelSelector] = dataplane.Status.RolloutStatus.Deployment.Selector
		}
	}
}

// labelSelectorFromDataPlaneRolloutStatusSelectorServiceOpt returns a ServiceOpt
// function which will set Service's selector based on provided DataPlane's Rollout
// Status selector field.
func labelSelectorFromDataPlaneRolloutStatusSelectorServiceOpt(dataplane *operatorv1beta1.DataPlane) func(s *corev1.Service) {
	return func(s *corev1.Service) {
		if dataplane.Status.RolloutStatus != nil &&
			dataplane.Status.RolloutStatus.Deployment != nil &&
			dataplane.Status.RolloutStatus.Deployment.Selector != "" {
			s.Spec.Selector[consts.OperatorLabelSelector] = dataplane.Status.RolloutStatus.Deployment.Selector
		}
	}
}

func (r *DataPlaneBlueGreenReconciler) initSelectorInRolloutStatus(ctx context.Context, dataplane *operatorv1beta1.DataPlane) error {
	if dataplane.Status.RolloutStatus != nil && dataplane.Status.RolloutStatus.Deployment != nil && dataplane.Status.RolloutStatus.Deployment.Selector != "" {
		return nil
	}

	oldDataplane := dataplane.DeepCopy()
	if dataplane.Status.RolloutStatus == nil {
		dataplane.Status.RolloutStatus = &operatorv1beta1.DataPlaneRolloutStatus{
			Deployment: &operatorv1beta1.DataPlaneRolloutStatusDeployment{},
		}
	} else if dataplane.Status.RolloutStatus.Deployment == nil {
		dataplane.Status.RolloutStatus.Deployment = &operatorv1beta1.DataPlaneRolloutStatusDeployment{}
	}
	dataplane.Status.RolloutStatus.Deployment.Selector = uuid.New().String()

	if err := r.Client.Status().Patch(ctx, dataplane, client.MergeFrom(oldDataplane)); err != nil {
		return err
	}
	return nil
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

	// If there's nothing to update then bail.
	if len(addresses) == 0 && (dataplaneAdminAPIService == nil || dataplaneAdminAPIService.Name == "") {
		return false, nil
	}

	// If the status is already in place and is as expected then don't update.
	rolloutStatusServiceAdmin := extractRolloutStatusServiceAdmin(dataplane)
	if rolloutStatusServiceAdmin != nil &&
		cmp.Equal(addresses, rolloutStatusServiceAdmin.Addresses, cmpopts.EquateEmpty()) &&
		dataplaneAdminAPIService.Name == rolloutStatusServiceAdmin.Name {
		return false, nil
	}

	old := dataplane.DeepCopy()
	dataplane = initDataPlaneStatusRolloutServicesAdmin(dataplane)

	dataplane.Status.RolloutStatus.Services.AdminAPI.Addresses = addresses
	dataplane.Status.RolloutStatus.Services.AdminAPI.Name = dataplaneAdminAPIService.Name
	return r.patchRolloutStatus(ctx, log, old, dataplane)
}

// patchRolloutStatus Patches the resource status only when there are changes
// between the provided old and updated DataPlanes' rollout statuses.
// It returns a bool flag indicating that the status has been patched and an error.
func (r *DataPlaneBlueGreenReconciler) patchRolloutStatus(ctx context.Context, log logr.Logger, old, updated *operatorv1beta1.DataPlane) (bool, error) {
	if rolloutStatusChanged(old, updated) {
		debug(log, "patching DataPlane status", updated, "status", updated.Status)
		return true, r.Client.Status().Patch(ctx, updated, client.MergeFrom(old))
	}

	return false, nil
}

func rolloutStatusChanged(old, updated *operatorv1beta1.DataPlane) bool {
	return !cmp.Equal(old.Status.RolloutStatus, updated.Status.RolloutStatus)
}

// canProceedWithPromotion verifies whether a DataPlane preview resources can be promoted. It assumes that all the
// preview resources are ready.
func canProceedWithPromotion(dataplane operatorv1beta1.DataPlane) (bool, error) {
	promotionStrategy := dataplane.Spec.Deployment.Rollout.Strategy.BlueGreen.Promotion.Strategy
	switch promotionStrategy {
	case operatorv1beta1.BreakBeforePromotion:
		// If the promotion strategy is BreakBeforePromotion then we need to wait for the user to explicitly
		// mark the DataPlane with the promote-when-ready annotation.
		return dataplane.Annotations[operatorv1beta1.DataPlanePromoteWhenReadyAnnotationKey] ==
			operatorv1beta1.DataPlanePromoteWhenReadyAnnotationTrue, nil
	case operatorv1beta1.AutomaticPromotion:
		// If the promotion strategy is AutomaticPromotion, we can proceed with promotion straight away.
		return true, nil
	default:
		return false, fmt.Errorf("unknown promotion strategy %q", promotionStrategy)
	}
}

// ensurePreviewAdminAPIService ensures the "preview" Admin API Service is available.
func (r *DataPlaneBlueGreenReconciler) ensurePreviewAdminAPIService(
	ctx context.Context,
	dataplane *operatorv1beta1.DataPlane,
) (CreatedUpdatedOrNoop, *corev1.Service, error) {
	additionalServiceLabels := map[string]string{
		consts.DataPlaneServiceStateLabel: consts.DataPlaneStateLabelValuePreview,
	}

	return ensureAdminServiceForDataPlane(
		ctx,
		r.Client,
		dataplane,
		additionalServiceLabels,
		labelSelectorFromDataPlaneRolloutStatusSelectorServiceOpt(dataplane),
	)
}

// ensurePreviewIngressService ensures the "preview" ingress service to access the Kong routes
// in the "preview" version of Kong gateway.
func (r *DataPlaneBlueGreenReconciler) ensurePreviewIngressService(
	ctx context.Context,
	log logr.Logger,
	dataplane *operatorv1beta1.DataPlane,
) (CreatedUpdatedOrNoop, *corev1.Service, error) {
	additionalServiceLabels := map[string]string{
		consts.DataPlaneServiceStateLabel: consts.DataPlaneStateLabelValuePreview,
	}
	return ensureProxyServiceForDataPlane(
		ctx,
		log,
		r.Client,
		dataplane,
		additionalServiceLabels,
		labelSelectorFromDataPlaneRolloutStatusSelectorServiceOpt(dataplane),
	)
}

// ensureDataPlaneRolloutIngressServiceStatus ensures status.rollout.service.ingress
// contains the name and addresses of "preview" ingress service.
func (r *DataPlaneBlueGreenReconciler) ensureDataPlaneRolloutIngressServiceStatus(
	ctx context.Context,
	log logr.Logger,
	dataplane *operatorv1beta1.DataPlane,
	ingressService *corev1.Service,
) (bool, error) {
	addresses, err := addressesFromService(ingressService)
	if err != nil {
		return true, err
	}

	// If there's nothing to update then bail.
	if len(addresses) == 0 && (ingressService == nil || ingressService.Name == "") {
		return false, nil
	}

	// If the status is already in place and is as expected then don't update.
	rolloutStatusServiceIngress := extractRolloutStatusServiceIngress(dataplane)
	if rolloutStatusServiceIngress != nil &&
		cmp.Equal(addresses, rolloutStatusServiceIngress.Addresses, cmpopts.EquateEmpty()) &&
		ingressService.Name == rolloutStatusServiceIngress.Name {
		return false, nil
	}

	// Updating on status.rollout.ingress is needed, we patch the rollout status.
	old := dataplane.DeepCopy()
	dataplane = initDataPlaneStatusRolloutServicesIngress(dataplane)
	dataplane.Status.RolloutStatus.Services.Ingress.Name = ingressService.Name
	dataplane.Status.RolloutStatus.Services.Ingress.Addresses = addresses
	return r.patchRolloutStatus(ctx, log, old, dataplane)
}

// ensurePreviewSelectorOverridesLive ensures that the current preview deployment selector overrides the live one.
// That will make the DataPlane controller modify its live services to point to the preview deployment.
func (r *DataPlaneBlueGreenReconciler) ensurePreviewSelectorOverridesLive(
	ctx context.Context,
	dataplane *operatorv1beta1.DataPlane,
) (updated bool, err error) {
	previewSelector := dataplane.Status.RolloutStatus.Deployment.Selector
	liveSelector := dataplane.Status.Selector
	// If the live selector is already equal to the preview one, there's nothing to do.
	if liveSelector == previewSelector {
		return false, nil
	}

	oldDp := dataplane.DeepCopy()
	dataplane.Status.Selector = previewSelector
	if err := r.Client.Status().Patch(ctx, dataplane, client.MergeFrom(oldDp)); err != nil {
		return false, fmt.Errorf("failed to change live deployment selector to preview: %w", err)
	}
	return true, nil
}

// waitForLiveServiceSelectorsPropagation waits for a live service of a given type to have the expected selector.
// It's used to wait for the Admin and Ingress services to have the selector of the preview deployment during promotion.
func (r *DataPlaneBlueGreenReconciler) waitForLiveServiceSelectorsPropagation(
	ctx context.Context,
	dataplane *operatorv1beta1.DataPlane,
	serviceType consts.ServiceType,
	expectedSelector map[string]string,
) (ok bool, err error) {
	matchingLabels := client.MatchingLabels{
		"app":                                 dataplane.Name,
		consts.DataPlaneServiceTypeLabel:      string(serviceType),
		consts.DataPlaneServiceStateLabel:     consts.DataPlaneStateLabelValueLive,
		consts.GatewayOperatorControlledLabel: consts.DataPlaneManagedLabelValue,
	}

	services, err := k8sutils.ListServicesForOwner(
		ctx,
		r.Client,
		dataplane.Namespace,
		dataplane.UID,
		matchingLabels,
	)
	if err != nil {
		return false, fmt.Errorf("failed listing live Admin API services for DataPlane %s/%s: %w", dataplane.Namespace, dataplane.Name, err)
	}

	svc, ok := lo.Find(services, func(svc corev1.Service) bool {
		return cmp.Equal(svc.Spec.Selector, expectedSelector)
	})
	_ = svc
	return ok, nil
}

// ensurePreviewDeploymentLabeledLive ensures that the preview deployment with a given selector is labeled as live.
// It's used to mark the preview deployment as live during promotion.
func (r *DataPlaneBlueGreenReconciler) ensurePreviewDeploymentLabeledLive(
	ctx context.Context,
	log logr.Logger,
	dataplane *operatorv1beta1.DataPlane,
	selector string,
) (updated bool, err error) {
	matchingLabels := client.MatchingLabels{
		"app":                        dataplane.Name,
		consts.OperatorLabelSelector: selector,
	}
	deployments, err := k8sutils.ListDeploymentsForOwner(
		ctx,
		r.Client,
		dataplane.Namespace,
		dataplane.UID,
		matchingLabels,
	)
	if err != nil {
		return false, fmt.Errorf("failed listing preview deployments: %w", err)
	}
	if len(deployments) == 0 {
		return false, errors.New("no preview deployments found")
	}

	// If there are multiple preview deployments, we will label live only the first one.
	// It shouldn't happen in practice, but we'll log it just in case.
	if len(deployments) > 1 {
		ds := lo.Map(deployments, func(d appsv1.Deployment, _ int) string { return fmt.Sprintf("%s/%s", d.Namespace, d.Name) })
		info(log, "found multiple preview deployments, expected one, will label live only the first one",
			dataplane, "deployments", ds)
	}

	deployment := deployments[0]

	// If the deployment is already labeled as live, we don't need to do anything.
	if deployment.Labels != nil && deployment.Labels[consts.DataPlaneDeploymentStateLabel] == consts.DataPlaneStateLabelValueLive {
		return false, nil
	}

	old := deployment.DeepCopy()
	if deployment.Labels == nil {
		deployment.Labels = map[string]string{}
	}
	deployment.Labels[consts.DataPlaneDeploymentStateLabel] = consts.DataPlaneStateLabelValueLive
	if err := r.Client.Patch(ctx, &deployment, client.MergeFrom(old)); err != nil {
		return false, fmt.Errorf("failed labeling preview deployment %q as live: %w",
			fmt.Sprintf("%s/%s", deployment.Namespace, deployment.Name), err)
	}
	return true, nil
}

// -------------------------------------------------------------------------------
// utility functions to operate pointer fields in rollout status of dataplane
// TODO: find a method to automatically generate the extract* and init* functions
// --------------------------------------------------------------------------------

func extractRolloutStatusServiceAdmin(dataplane *operatorv1beta1.DataPlane) *operatorv1beta1.RolloutStatusService {
	if dataplane.Status.RolloutStatus == nil || dataplane.Status.RolloutStatus.Services == nil {
		return nil
	}
	return dataplane.Status.RolloutStatus.Services.AdminAPI
}

func extractRolloutStatusServiceIngress(dataplane *operatorv1beta1.DataPlane) *operatorv1beta1.RolloutStatusService {
	if dataplane.Status.RolloutStatus == nil || dataplane.Status.RolloutStatus.Services == nil {
		return nil
	}
	return dataplane.Status.RolloutStatus.Services.Ingress
}

func initDataPlaneStatusRollout(dataplane *operatorv1beta1.DataPlane) *operatorv1beta1.DataPlane {
	if dataplane.Status.RolloutStatus == nil {
		dataplane.Status.RolloutStatus = &operatorv1beta1.DataPlaneRolloutStatus{}
	}
	return dataplane
}

func initDataPlaneStatusRolloutServices(dataplane *operatorv1beta1.DataPlane) *operatorv1beta1.DataPlane {
	dataplane = initDataPlaneStatusRollout(dataplane)
	if dataplane.Status.RolloutStatus.Services == nil {
		dataplane.Status.RolloutStatus.Services = &operatorv1beta1.DataPlaneRolloutStatusServices{}
	}
	return dataplane
}

func initDataPlaneStatusRolloutServicesAdmin(dataplane *operatorv1beta1.DataPlane) *operatorv1beta1.DataPlane {
	dataplane = initDataPlaneStatusRolloutServices(dataplane)
	if dataplane.Status.RolloutStatus.Services.AdminAPI == nil {
		dataplane.Status.RolloutStatus.Services.AdminAPI = &operatorv1beta1.RolloutStatusService{}
	}
	return dataplane
}

func initDataPlaneStatusRolloutServicesIngress(dataplane *operatorv1beta1.DataPlane) *operatorv1beta1.DataPlane {
	dataplane = initDataPlaneStatusRolloutServices(dataplane)
	if dataplane.Status.RolloutStatus.Services.Ingress == nil {
		dataplane.Status.RolloutStatus.Services.Ingress = &operatorv1beta1.RolloutStatusService{}
	}
	return dataplane
}
