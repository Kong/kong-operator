package konnect

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/internal/utils/config"
	"github.com/kong/gateway-operator/pkg/consts"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

// ApplyDataPlaneKonnectExtension gets the DataPlane as argument, and in case it references a KonnectExtension, it
// fetches the referenced extension and applies the necessary changes to the DataPlane spec.
func ApplyControlPlaneKonnectExtension(ctx context.Context, cl client.Client, controlPlane *operatorv1beta1.ControlPlane) (bool, error) {
	var konnectExtension *konnectv1alpha1.KonnectExtension
	for _, extensionRef := range controlPlane.Spec.Extensions {
		extension, err := getExtension(ctx, cl, controlPlane.Namespace, extensionRef)
		if err != nil {
			return false, err
		}
		if extension != nil {
			konnectExtension = extension
			break
		}
	}
	if konnectExtension == nil {
		return false, nil
	}

	envSet, err := config.KICInKonnectDefaults(konnectExtension.Status)
	if err != nil {
		return true, err
	}
	config.FillContainerEnvs(nil, controlPlane.Spec.Deployment.PodTemplateSpec, consts.ControlPlaneControllerContainerName, envSet)

	return true, nil
}
