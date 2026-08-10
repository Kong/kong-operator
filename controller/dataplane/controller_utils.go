package dataplane

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kcfgdataplane "github.com/kong/kong-operator/v2/api/gateway-operator/dataplane"
	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/controller/pkg/reservedkeys"
	"github.com/kong/kong-operator/v2/internal/versions"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// -----------------------------------------------------------------------------
// DataPlane - Private Functions - Generators
// -----------------------------------------------------------------------------

func generateDataPlaneImage(dataplane *operatorv1beta1.DataPlane, defaultImage string, validators ...versions.VersionValidationOption) (string, error) {
	if dataplane.Spec.Deployment.PodTemplateSpec == nil {
		return defaultImage, nil // TODO: https://github.com/kong/kong-operator-archive/issues/20
	}

	container := k8sutils.GetPodContainerByName(&dataplane.Spec.Deployment.PodTemplateSpec.Spec, consts.DataPlaneProxyContainerName)
	if container != nil && container.Image != "" {
		for _, v := range validators {
			supported, err := v(container.Image)
			if err != nil {
				return "", err
			}
			if !supported {
				return "", fmt.Errorf("unsupported DataPlane image %s", container.Image)
			}
		}
		return container.Image, nil
	}

	if relatedKongImage := os.Getenv("RELATED_IMAGE_KONG"); relatedKongImage != "" {
		// RELATED_IMAGE_KONG is set by the operator-sdk when building the operator bundle.
		// https://github.com/kong/kong-operator-archive/issues/261
		return relatedKongImage, nil
	}

	return defaultImage, nil // TODO: https://github.com/kong/kong-operator-archive/issues/20
}

// -----------------------------------------------------------------------------
// DataPlane - Private Functions - Kubernetes Object Labels and Annotations
// -----------------------------------------------------------------------------

func addAnnotationsForDataPlaneIngressService(svc *corev1.Service, dataplane operatorv1beta1.DataPlane) {
	specAnnotations := extractDataPlaneIngressServiceAnnotations(&dataplane)
	if specAnnotations == nil {
		return
	}
	annotations := svc.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	maps.Copy(annotations, specAnnotations)
	encodedSpecAnnotations, err := json.Marshal(specAnnotations)
	if err == nil {
		annotations[consts.AnnotationLastAppliedAnnotations] = string(encodedSpecAnnotations)
	}
	svc.SetAnnotations(annotations)
}

func extractDataPlaneIngressServiceAnnotations(dataplane *operatorv1beta1.DataPlane) map[string]string {
	if dataplane.Spec.Network.Services == nil ||
		dataplane.Spec.Network.Services.Ingress == nil ||
		dataplane.Spec.Network.Services.Ingress.Annotations == nil {
		return nil
	}

	anns := dataplane.Spec.Network.Services.Ingress.Annotations
	return anns
}

func addLabelsForDataPlaneIngressService(svc *corev1.Service, dataplane operatorv1beta1.DataPlane) {
	specLabels := extractDataPlaneIngressServiceLabels(&dataplane)
	if specLabels == nil {
		return
	}
	lbls := svc.GetLabels()
	if lbls == nil {
		lbls = make(map[string]string)
	}
	maps.Copy(lbls, specLabels)
	svc.SetLabels(lbls)
}

func extractDataPlaneIngressServiceLabels(dataplane *operatorv1beta1.DataPlane) map[string]string {
	if dataplane.Spec.Network.Services == nil ||
		dataplane.Spec.Network.Services.Ingress == nil ||
		dataplane.Spec.Network.Services.Ingress.Labels == nil {
		return nil
	}
	result := make(map[string]string, len(dataplane.Spec.Network.Services.Ingress.Labels))
	for k, v := range dataplane.Spec.Network.Services.Ingress.Labels {
		result[string(k)] = string(v)
	}
	return result
}

// extractOutdatedDataPlaneIngressServiceAnnotations returns the last applied annotations
// of ingress service from `DataPlane` spec but disappeared in current `DataPlane` spec.
func extractOutdatedDataPlaneIngressServiceAnnotations(
	dataplane *operatorv1beta1.DataPlane, existingAnnotations map[string]string,
) (map[string]string, error) {
	if existingAnnotations == nil {
		return nil, nil
	}
	lastAppliedAnnotationsEncoded, ok := existingAnnotations[consts.AnnotationLastAppliedAnnotations]
	if !ok {
		return nil, nil
	}
	outdatedAnnotations := map[string]string{}
	err := json.Unmarshal([]byte(lastAppliedAnnotationsEncoded), &outdatedAnnotations)
	if err != nil {
		return nil, fmt.Errorf("failed to decode last applied annotations: %w", err)
	}
	// If an annotation is present in last applied annotations but not in current spec of annotations,
	// the annotation is outdated and should be removed.
	// So we remove the annotations present in current spec in last applied annotations,
	// the remaining annotations are outdated and should be removed.
	currentSpecifiedAnnotations := extractDataPlaneIngressServiceAnnotations(dataplane)
	for k := range currentSpecifiedAnnotations {
		delete(outdatedAnnotations, k)
	}
	return outdatedAnnotations, nil
}

// dataPlaneDeploymentReservedKeys reports whether a label/annotation key is reserved
// for internal operator or Kubernetes use and must be dropped from any
// spec.deployment.labels/annotations provided by the user.
var dataPlaneDeploymentReservedKeys = reservedkeys.NewChecker("app", "deployment.kubernetes.io/revision")

func addAnnotationsForDataPlaneDeployment(logger logr.Logger, deployment *appsv1.Deployment, dataplane operatorv1beta1.DataPlane) {
	specAnnotations := extractDataPlaneDeploymentAnnotations(&dataplane)
	if specAnnotations == nil {
		return
	}

	specAnnotations = reservedkeys.Filter(logger, reservedkeys.MetadataTypeAnnotation, specAnnotations, dataPlaneDeploymentReservedKeys)
	deployment.Annotations = reservedkeys.MergeAnnotationsTracked(deployment.Annotations, specAnnotations)
}

func extractDataPlaneDeploymentAnnotations(dataplane *operatorv1beta1.DataPlane) map[string]string {
	return dataplane.Spec.Deployment.Annotations
}

func addLabelsForDataPlaneDeployment(logger logr.Logger, deployment *appsv1.Deployment, dataplane operatorv1beta1.DataPlane) {
	specLabels := dataplane.Spec.Deployment.Labels
	if specLabels == nil {
		return
	}
	specLabels = reservedkeys.Filter(logger, reservedkeys.MetadataTypeLabel, specLabels, dataPlaneDeploymentReservedKeys)
	deployment.Labels = reservedkeys.Merge(deployment.Labels, specLabels)
}

// extractOutdatedDataPlaneDeploymentAnnotations returns the last applied annotations
// of the DataPlane Deployment from `DataPlane` spec but disappeared in current `DataPlane` spec.
func extractOutdatedDataPlaneDeploymentAnnotations(
	dataplane *operatorv1beta1.DataPlane, existingAnnotations map[string]string,
) (map[string]string, error) {
	outdatedAnnotations, err := reservedkeys.ExtractOutdated(extractDataPlaneDeploymentAnnotations(dataplane), existingAnnotations)
	if err != nil {
		return nil, err
	}
	return outdatedAnnotations, nil
}

// ensureDataPlaneReadyStatus ensures that the provided DataPlane gets an up to
// date Ready status condition.
// It sets the condition based on the readiness of DataPlane's Deployment and
// its ingress Service receiving an address.
func ensureDataPlaneReadyStatus(
	ctx context.Context,
	cl client.Client,
	logger logr.Logger,
	dataplane *operatorv1beta1.DataPlane,
	generation int64,
) (ctrl.Result, error) {
	// retrieve a fresh copy of the dataplane to reduce the number of times we have to error on update
	// due to new changes when the `DataPlane` resource is very active.
	if err := cl.Get(ctx, client.ObjectKeyFromObject(dataplane), dataplane); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed getting DataPlane %s/%s: %w", dataplane.Namespace, dataplane.Name, err)
	}

	deployments, err := listDataPlaneLiveDeployments(ctx, cl, dataplane)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed listing deployments for DataPlane %s/%s: %w", dataplane.Namespace, dataplane.Name, err)
	}

	switch len(deployments) {
	case 0:
		log.Debug(logger, "Deployment for DataPlane not present yet")

		// Set Ready to false for dataplane as the underlying deployment is not ready.
		k8sutils.SetCondition(
			k8sutils.NewConditionWithGeneration(
				kcfgdataplane.ReadyType,
				metav1.ConditionFalse,
				kcfgdataplane.WaitingToBecomeReadyReason,
				kcfgdataplane.WaitingToBecomeReadyMessage,
				generation,
			),
			dataplane,
		)
		setDeploymentRolledOutCondition(dataplane, nil, generation)
		ensureDataPlaneReadinessStatus(dataplane, appsv1.DeploymentStatus{
			Replicas:      0,
			ReadyReplicas: 0,
		})
		res, err := patchDataPlaneStatus(ctx, cl, logger, dataplane)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed patching status (Deployment not present) for DataPlane %s/%s: %w", dataplane.Namespace, dataplane.Name, err)
		}
		if res {
			return ctrl.Result{}, nil
		}

	case 1: // Expect just 1.

	default: // More than 1.
		log.Info(logger, "expected only 1 Deployment for DataPlane")
		return ctrl.Result{Requeue: true}, nil
	}

	deployment := deployments[0]
	if _, ready := isDeploymentReady(&deployment); !ready {
		log.Debug(logger, "Deployment for DataPlane not ready yet")

		// Set Ready to false for dataplane as the underlying deployment is not ready.
		k8sutils.SetCondition(
			k8sutils.NewConditionWithGeneration(
				kcfgdataplane.ReadyType,
				metav1.ConditionFalse,
				kcfgdataplane.WaitingToBecomeReadyReason,
				fmt.Sprintf("%s: Deployment %s is not ready yet", kcfgdataplane.WaitingToBecomeReadyMessage, deployment.Name),
				generation,
			),
			dataplane,
		)
		setDeploymentRolledOutCondition(dataplane, &deployment, generation)
		ensureDataPlaneReadinessStatus(dataplane, deployment.Status)
		if _, err := patchDataPlaneStatus(ctx, cl, logger, dataplane); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed patching status (Deployment not ready) for DataPlane %s/%s: %w", dataplane.Namespace, dataplane.Name, err)
		}
		return ctrl.Result{}, nil
	}

	services, err := listDataPlaneLiveServices(ctx, cl, dataplane)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed listing ingress services for DataPlane %s/%s: %w", dataplane.Namespace, dataplane.Name, err)
	}

	switch len(services) {
	case 0:
		log.Debug(logger, "Ingress Service for DataPlane not present")

		// Set Ready to false for dataplane as the Service is not ready yet.
		k8sutils.SetCondition(
			k8sutils.NewConditionWithGeneration(
				kcfgdataplane.ReadyType,
				metav1.ConditionFalse,
				kcfgdataplane.WaitingToBecomeReadyReason,
				kcfgdataplane.WaitingToBecomeReadyMessage,
				generation,
			),
			dataplane,
		)
		setDeploymentRolledOutCondition(dataplane, &deployment, generation)
		ensureDataPlaneReadinessStatus(dataplane, deployment.Status)
		_, err := patchDataPlaneStatus(ctx, cl, logger, dataplane)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed patching status (ingress Service not present) for DataPlane %s/%s: %w", dataplane.Namespace, dataplane.Name, err)
		}
		return ctrl.Result{}, nil

	case 1: // Expect just 1.

	default: // More than 1.
		log.Info(logger, "expected only 1 ingress Service for DataPlane")
		return ctrl.Result{Requeue: true}, nil
	}

	ingressService := services[0]
	if !dataPlaneIngressServiceIsReady(&ingressService) {
		log.Debug(logger, "Ingress Service for DataPlane not ready yet")

		// Set Ready to false for dataplane as the Service is not ready yet.
		k8sutils.SetCondition(
			k8sutils.NewConditionWithGeneration(
				kcfgdataplane.ReadyType,
				metav1.ConditionFalse,
				kcfgdataplane.WaitingToBecomeReadyReason,
				fmt.Sprintf("%s: ingress Service %s is not ready yet", kcfgdataplane.WaitingToBecomeReadyMessage, ingressService.Name),
				generation,
			),
			dataplane,
		)
		setDeploymentRolledOutCondition(dataplane, &deployment, generation)
		ensureDataPlaneReadinessStatus(dataplane, deployment.Status)
		_, err := patchDataPlaneStatus(ctx, cl, logger, dataplane)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed patching status (ingress Service not ready) for DataPlane %s/%s: %w", dataplane.Namespace, dataplane.Name, err)
		}
		return ctrl.Result{}, nil
	}

	k8sutils.SetReadyWithGeneration(dataplane, generation)
	setDeploymentRolledOutCondition(dataplane, &deployment, generation)
	ensureDataPlaneReadinessStatus(dataplane, deployment.Status)

	if _, err := patchDataPlaneStatus(ctx, cl, logger, dataplane); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed patching status for DataPlane %s/%s: %w", dataplane.Namespace, dataplane.Name, err)
	}

	return ctrl.Result{}, nil
}

func listDataPlaneLiveDeployments(
	ctx context.Context,
	cl client.Client,
	dataplane *operatorv1beta1.DataPlane,
) ([]appsv1.Deployment, error) {
	return k8sutils.ListDeploymentsForOwner(ctx,
		cl,
		dataplane.Namespace,
		dataplane.UID,
		client.MatchingLabels{
			"app":                                dataplane.Name,
			consts.DataPlaneDeploymentStateLabel: consts.DataPlaneStateLabelValueLive,
		},
	)
}

func listDataPlaneLiveServices(
	ctx context.Context,
	cl client.Client,
	dataplane *operatorv1beta1.DataPlane,
) ([]corev1.Service, error) {
	return k8sutils.ListServicesForOwner(ctx,
		cl,
		dataplane.Namespace,
		dataplane.UID,
		client.MatchingLabels{
			"app":                             dataplane.Name,
			consts.DataPlaneServiceStateLabel: consts.DataPlaneStateLabelValueLive,
			consts.DataPlaneServiceTypeLabel:  string(consts.DataPlaneIngressServiceLabelValue),
		},
	)
}

// isDeploymentReady reports whether the DataPlane's Deployment is ready.
// It does not indicate whether the rollout has completed, that is a DataPlane can indicate
// that it's ready (e.g. all replicas are available) but not fully rolled out
// (e.g. new spec has not completely rolled out).
// This will return ConditionTrue if the Deployment has .Status.AvailableReplicas equal
// at least to the number of replicas specified in .Spec.Replicas, and .Status.Replicas is not 0.
// This way, the DataPlane Ready status condition does not flap when a rolling update
// is performed.
// It will still return ConditionFalse if Kubernetes gave up on the rollout
// (Progressing=False/ProgressDeadlineExceeded), so a stalled rollout (e.g. a broken
// readiness probe on the new ReplicaSet) is eventually reported as not ready even
// though the old replicas are still available.
func isDeploymentReady(deployment *appsv1.Deployment) (metav1.ConditionStatus, bool) {
	// We check if the Deployment is not Ready.
	// This is the case when status has replicas set to 0 or status.availableReplicas
	// in status is less than status.replicas.
	if deployment.Status.Replicas == 0 {
		return metav1.ConditionFalse, false
	}

	if specReplicas := deployment.Spec.Replicas; specReplicas != nil &&
		deployment.Status.AvailableReplicas < *specReplicas {
		return metav1.ConditionFalse, false
	}

	if c := getDeploymentCondition(deployment, appsv1.DeploymentProgressing); c != nil &&
		c.Status == corev1.ConditionFalse && c.Reason == string(kcfgdataplane.DeploymentRolloutStalledReason) {
		return metav1.ConditionFalse, false
	}

	return metav1.ConditionTrue, true
}

// getDeploymentCondition returns the Deployment condition of the given type, or nil if absent.
func getDeploymentCondition(deployment *appsv1.Deployment, condType appsv1.DeploymentConditionType) *appsv1.DeploymentCondition {
	for i := range deployment.Status.Conditions {
		if deployment.Status.Conditions[i].Type == condType {
			return &deployment.Status.Conditions[i]
		}
	}
	return nil
}

// isDeploymentRolledOut reports whether the Deployment has fully rolled out its current spec,
// i.e. all replicas have been updated to the latest ReplicaSet and are available.
// This mirrors the check `kubectl rollout status` performs.
func isDeploymentRolledOut(deployment *appsv1.Deployment) bool {
	if deployment.Status.ObservedGeneration < deployment.Generation {
		return false
	}
	if specReplicas := deployment.Spec.Replicas; specReplicas != nil &&
		deployment.Status.UpdatedReplicas < *specReplicas {
		return false
	}
	return deployment.Status.Replicas == deployment.Status.UpdatedReplicas &&
		deployment.Status.AvailableReplicas == deployment.Status.UpdatedReplicas
}

// setDeploymentRolledOutCondition records whether the DataPlane's live Deployment
// has fully rolled out the given generation of the DataPlane spec.
// deployment is nil when no live Deployment exists yet.
// This is deliberately kept separate from the Ready condition: Ready reports
// whether the DataPlane is currently serving traffic (which stays true across a
// rolling update as long as old replicas are still available), while this
// condition reports whether the *current* generation's spec is what is actually
// serving. It is recomputed from the Deployment on every reconcile, so unlike a
// value inherited from a previous reconcile, it can never go stale.
func setDeploymentRolledOutCondition(
	dataplane *operatorv1beta1.DataPlane,
	deployment *appsv1.Deployment,
	generation int64,
) {
	status, reason, message := metav1.ConditionFalse,
		kcfgdataplane.DeploymentRolloutProgressingReason,
		"Waiting for the Deployment to roll out"

	switch {
	case deployment == nil:
		message = "Deployment not present yet"
	case isDeploymentRolledOut(deployment):
		status, reason, message = metav1.ConditionTrue,
			kcfgdataplane.DeploymentRolloutCompleteReason,
			"All replicas run the current generation"
	default:
		if c := getDeploymentCondition(deployment, appsv1.DeploymentProgressing); c != nil &&
			c.Status == corev1.ConditionFalse && c.Reason == string(kcfgdataplane.DeploymentRolloutStalledReason) {
			reason, message = kcfgdataplane.DeploymentRolloutStalledReason, c.Message
		}
	}

	k8sutils.SetCondition(
		k8sutils.NewConditionWithGeneration(kcfgdataplane.DeploymentRolledOutType, status, reason, message, generation),
		dataplane,
	)
}
