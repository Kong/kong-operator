package controllers

import (
	"fmt"
	"os"

	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	"github.com/kong/gateway-operator/internal/consts"
	"github.com/kong/gateway-operator/internal/versions"
)

// -----------------------------------------------------------------------------
// DataPlane - Private Functions - Generators
// -----------------------------------------------------------------------------

func generateDataPlaneImage(dataplane *operatorv1alpha1.DataPlane, validators ...versions.VersionValidationOption) (string, error) {
	if dataplane.Spec.Deployment.ContainerImage != nil {
		dataplaneImage := *dataplane.Spec.Deployment.ContainerImage
		if dataplane.Spec.Deployment.Version != nil {
			dataplaneImage = fmt.Sprintf("%s:%s", dataplaneImage, *dataplane.Spec.Deployment.Version)
		}
		for _, v := range validators {
			if !v(dataplaneImage) {
				return "", fmt.Errorf("unsupported DataPlane image %s", dataplaneImage)
			}
		}
		return dataplaneImage, nil
	}

	if relatedKongImage := os.Getenv("RELATED_IMAGE_KONG"); relatedKongImage != "" {
		// RELATED_IMAGE_KONG is set by the operator-sdk when building the operator bundle.
		// https://github.com/Kong/gateway-operator/issues/261
		return relatedKongImage, nil
	}

	return consts.DefaultDataPlaneImage, nil // TODO: https://github.com/Kong/gateway-operator/issues/20

}

// -----------------------------------------------------------------------------
// DataPlane - Private Functions - Kubernetes Object Labels
// -----------------------------------------------------------------------------

func addLabelForDataplane(obj client.Object) {
	labels := obj.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[consts.GatewayOperatorControlledLabel] = consts.DataPlaneManagedLabelValue
	obj.SetLabels(labels)
}

// -----------------------------------------------------------------------------
// DataPlane - Private Functions - Equality Checks
// -----------------------------------------------------------------------------

func dataplaneSpecDeepEqual(spec1, spec2 *operatorv1alpha1.DataPlaneOptions) bool {
	return deploymentOptionsDeepEqual(&spec1.Deployment, &spec2.Deployment)
}
