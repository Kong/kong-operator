package controlplane

import (
	"fmt"
	"os"
	"reflect"

	"github.com/kong/gateway-operator/internal/versions"
	"github.com/kong/gateway-operator/pkg/consts"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"
	k8scompare "github.com/kong/gateway-operator/pkg/utils/kubernetes/compare"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
)

// DefaultsArgs contains the parameters to pass to setControlPlaneDefaults
type DefaultsArgs struct {
	Namespace                   string
	ControlPlaneName            string
	DataPlaneIngressServiceName string
	DataPlaneAdminServiceName   string
	OwnedByGateway              string
}

// -----------------------------------------------------------------------------
// ControlPlane - Public Functions
// -----------------------------------------------------------------------------

// GenerateImage returns the image to use for the control plane.
func GenerateImage(opts *operatorv1beta1.ControlPlaneOptions, validators ...versions.VersionValidationOption) (string, error) {
	container := k8sutils.GetPodContainerByName(&opts.Deployment.PodTemplateSpec.Spec, consts.ControlPlaneControllerContainerName)
	if container == nil {
		// This is just a safeguard against running the operator without an admission webhook
		// (which would prevent admission of a ControlPlane without an image specified)
		// to prevent panics.
		return "", fmt.Errorf("unsupported ControlPlane without image")
	}
	if container.Image != "" {
		for _, v := range validators {
			supported, err := v(container.Image)
			if err != nil {
				return "", err
			}
			if !supported {
				return "", fmt.Errorf("unsupported ControlPlane image %s", container.Image)
			}
		}
		return container.Image, nil
	}

	if relatedKongControllerImage := os.Getenv("RELATED_IMAGE_KONG_CONTROLLER"); relatedKongControllerImage != "" {
		// RELATED_IMAGE_KONG_CONTROLLER is set by the operator-sdk when building the operator bundle.
		// https://github.com/Kong/gateway-operator-archive/issues/261
		return relatedKongControllerImage, nil
	}

	return consts.DefaultControlPlaneImage, nil // TODO: https://github.com/Kong/gateway-operator-archive/issues/20
}

// -----------------------------------------------------------------------------
// ControlPlane - Private Functions - Equality Checks
// -----------------------------------------------------------------------------

// SpecDeepEqual returns true if the two ControlPlaneOptions are equal.
func SpecDeepEqual(spec1, spec2 *operatorv1beta1.ControlPlaneOptions, envVarsToIgnore ...string) bool {
	if !k8scompare.ControlPlaneDeploymentOptionsDeepEqual(&spec1.Deployment, &spec2.Deployment, envVarsToIgnore...) ||
		!reflect.DeepEqual(spec1.DataPlane, spec2.DataPlane) {
		return false
	}

	if !reflect.DeepEqual(spec1.Extensions, spec2.Extensions) {
		return false
	}

	if !reflect.DeepEqual(spec1.WatchNamespaces, spec2.WatchNamespaces) {
		return false
	}

	return true
}
