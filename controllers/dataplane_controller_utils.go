package controllers

import (
	"fmt"
	"os"

	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	"github.com/kong/gateway-operator/internal/consts"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
	"github.com/kong/gateway-operator/internal/versions"
)

// -----------------------------------------------------------------------------
// DataPlane - Private Functions - Generators
// -----------------------------------------------------------------------------

func generateDataPlaneImage(dataplane *operatorv1alpha1.DataPlane, validators ...versions.VersionValidationOption) (string, error) {
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

func addAnnotationsForDataplaneProxyService(obj client.Object, dataplane operatorv1alpha1.DataPlane) {
	if dataplane.Spec.Network.Services == nil || dataplane.Spec.Network.Services.Ingress.Annotations == nil {
		return
	}
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	for k, v := range dataplane.Spec.Network.Services.Ingress.Annotations {
		annotations[k] = v
	}
	obj.SetAnnotations(annotations)
}

// -----------------------------------------------------------------------------
// DataPlane - Private Functions - Equality Checks
// -----------------------------------------------------------------------------

func dataplaneSpecDeepEqual(spec1, spec2 *operatorv1alpha1.DataPlaneOptions) bool {
	// TODO: Doesn't take .Rollout field into account.
	if !deploymentOptionsDeepEqual(&spec1.Deployment.DeploymentOptions, &spec2.Deployment.DeploymentOptions) ||
		!servicesOptionsDeepEqual(&spec1.Network, &spec2.Network) {
		return false
	}

	return true
}
