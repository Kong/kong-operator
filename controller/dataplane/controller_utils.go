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

	kcfgdataplane "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/dataplane"
	operatorv1beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1beta1"

	"github.com/kong/kong-operator/v2/controller/pkg/log"
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

func addAnnotationsForDataPlaneIngressService(obj client.Object, dataplane operatorv1beta1.DataPlane) {
	specAnnotations := extractDataPlaneIngressServiceAnnotations(&dataplane)
	if specAnnotations == nil {
		return
	}
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	maps.Copy(annotations, specAnnotations)
	encodedSpecAnnotations, err := json.Marshal(specAnnotations)
	if err == nil {
		annotations[consts.AnnotationLastAppliedAnnotations] = string(encodedSpecAnnotations)
	}
	obj.SetAnnotations(annotations)
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
	if _, ready := isDeploymentReady(deployment.Status); !ready {
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
		ensureDataPlaneReadinessStatus(dataplane, deployment.Status)
		_, err := patchDataPlaneStatus(ctx, cl, logger, dataplane)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed patching status (ingress Service not ready) for DataPlane %s/%s: %w", dataplane.Namespace, dataplane.Name, err)
		}
		return ctrl.Result{}, nil
	}

	k8sutils.SetReadyWithGeneration(dataplane, generation)
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

// isDeploymentReady if the DataPlane's Deployment is ready.
// It does not indicate if the rollout has completed, that is a DataPlane can indicate
// that it's ready (e.g. all replicas are available) but not fully rolled out
// (e.g. new spec has not completely rolled out).
func isDeploymentReady(deploymentStatus appsv1.DeploymentStatus) (metav1.ConditionStatus, bool) {
	// We check if the Deployment is not Ready.
	// This is the case when status has replicas set to 0 or status.availableReplicas
	// in status is less than status.replicas.
	if deploymentStatus.Replicas == 0 ||
		deploymentStatus.AvailableReplicas < deploymentStatus.Replicas {
		return metav1.ConditionFalse, false
	}

	return metav1.ConditionTrue, true
}
