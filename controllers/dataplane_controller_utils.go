package controllers

import (
	"encoding/json"
	"fmt"
	"os"

	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1beta1 "github.com/kong/gateway-operator/apis/v1beta1"
	"github.com/kong/gateway-operator/internal/consts"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
	"github.com/kong/gateway-operator/internal/versions"
)

// -----------------------------------------------------------------------------
// DataPlane - Private Functions - Generators
// -----------------------------------------------------------------------------

func generateDataPlaneImage(dataplane *operatorv1beta1.DataPlane, validators ...versions.VersionValidationOption) (string, error) {
	if dataplane.Spec.DataPlaneOptions.Deployment.PodTemplateSpec == nil {
		return consts.DefaultDataPlaneImage, nil // TODO: https://github.com/Kong/gateway-operator/issues/20
	}

	container := k8sutils.GetPodContainerByName(&dataplane.Spec.DataPlaneOptions.Deployment.PodTemplateSpec.Spec, consts.DataPlaneProxyContainerName)
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
		// https://github.com/Kong/gateway-operator/issues/261
		return relatedKongImage, nil
	}

	return consts.DefaultDataPlaneImage, nil // TODO: https://github.com/Kong/gateway-operator/issues/20
}

// -----------------------------------------------------------------------------
// DataPlane - Private Functions - Kubernetes Object Labels and Annotations
// -----------------------------------------------------------------------------

func addLabelForDataplane(obj client.Object) {
	labels := obj.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[consts.GatewayOperatorControlledLabel] = consts.DataPlaneManagedLabelValue
	obj.SetLabels(labels)
}

func addAnnotationsForDataplaneIngressService(obj client.Object, dataplane operatorv1beta1.DataPlane) {
	specAnnotations := extractDataPlaneIngressServiceAnnotations(&dataplane)
	if specAnnotations == nil {
		return
	}
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	for k, v := range specAnnotations {
		annotations[k] = v
	}
	encodedSpecAnnotations, err := json.Marshal(specAnnotations)
	if err == nil {
		annotations[consts.AnnotationLastAppliedAnnotations] = string(encodedSpecAnnotations)
	}
	obj.SetAnnotations(annotations)
}

func extractDataPlaneIngressServiceAnnotations(dataplane *operatorv1beta1.DataPlane) map[string]string {
	if dataplane.Spec.DataPlaneOptions.Network.Services == nil ||
		dataplane.Spec.DataPlaneOptions.Network.Services.Ingress == nil ||
		dataplane.Spec.DataPlaneOptions.Network.Services.Ingress.Annotations == nil {
		return nil
	}

	anns := dataplane.Spec.DataPlaneOptions.Network.Services.Ingress.Annotations
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

// -----------------------------------------------------------------------------
// DataPlane - Private Functions - Equality Checks
// -----------------------------------------------------------------------------

func dataplaneSpecDeepEqual(spec1, spec2 *operatorv1beta1.DataPlaneOptions) bool {
	// TODO: Doesn't take .Rollout field into account.
	if !deploymentOptionsDeepEqual(&spec1.Deployment.DeploymentOptions, &spec2.Deployment.DeploymentOptions) ||
		!servicesOptionsDeepEqual(&spec1.Network, &spec2.Network) {
		return false
	}

	return true
}
