package konnect

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	extensionserrors "github.com/kong/kong-operator/v2/controller/pkg/extensions/errors"
	"github.com/kong/kong-operator/v2/controller/pkg/extensions/processor"
	"github.com/kong/kong-operator/v2/internal/utils/config"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	k8sresources "github.com/kong/kong-operator/v2/pkg/utils/kubernetes/resources"
)

// DataPlaneKonnectExtensionProcessor processes Konnect extensions for DataPlane resources.
type DataPlaneKonnectExtensionProcessor struct {
	// adminAPIDisabled is whether the last successful Process determined the admin API to be disabled.
	adminAPIDisabled bool
}

// Compile-time check to ensure DataPlaneKonnectExtensionProcessor implements the processor.Processor interface.
var _ processor.Processor = (*DataPlaneKonnectExtensionProcessor)(nil)

// Process gets the DataPlane as argument, and in case it references a KonnectExtension, it
// fetches the referenced extension and applies the necessary changes to the DataPlane spec.
func (p *DataPlaneKonnectExtensionProcessor) Process(ctx context.Context, cl client.Client, object client.Object) (bool, error) {
	var konnectExtension *konnectv1alpha2.KonnectExtension
	p.adminAPIDisabled = false

	// First thing we do is check if the object is a DataPlane.
	dataPlane, ok := object.(*operatorv1beta1.DataPlane)
	if !ok {
		return false, fmt.Errorf("object is not a DataPlane: %T", object)
	}

	for _, extensionRef := range dataPlane.Spec.Extensions {
		extension, err := getExtension(ctx, cl, dataPlane.Namespace, extensionRef)
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

	if dataPlane.Spec.Deployment.PodTemplateSpec == nil {
		dataPlane.Spec.Deployment.PodTemplateSpec = &corev1.PodTemplateSpec{}
	}

	d := k8sresources.Deployment(appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{
			Template: *dataPlane.Spec.Deployment.PodTemplateSpec,
		},
	})
	if container := k8sutils.GetPodContainerByName(&d.Spec.Template.Spec, consts.DataPlaneProxyContainerName); container == nil {
		d.Spec.Template.Spec.Containers = append(d.Spec.Template.Spec.Containers, corev1.Container{
			Name: consts.DataPlaneProxyContainerName,
		})
	}

	// Evaluated before the pod template is mutated below.
	adminAPIDisabled := AdminAPIDisabled(dataPlane, konnectExtension.Status.Konnect.ClusterType)
	// Without the admin API, the admin API cluster certificate isn't needed.
	if !adminAPIDisabled {
		d.WithVolume(kongInKonnectClusterCertificateVolume())
		d.WithVolumeMount(kongInKonnectClusterCertificateVolumeMount(), consts.DataPlaneProxyContainerName)
	}
	d.WithVolume(kongInKonnectClusterCertVolume(konnectExtension.Status.DataPlaneClientAuth.CertificateSecretRef.Name))
	d.WithVolumeMount(kongInKonnectClusterVolumeMount(), consts.DataPlaneProxyContainerName)

	// KonnectID is the only supported type for now, and its presence is guaranteed by a proper CEL rule.
	var dataplaneLabels map[string]konnectv1alpha2.DataPlaneLabelValue
	if konnectExtension.Spec.Konnect.DataPlane != nil {
		dataplaneLabels = konnectExtension.Spec.Konnect.DataPlane.Labels
	}
	envSet, err := config.KongInKonnectDefaults(dataplaneLabels, konnectExtension.Status)
	if err != nil {
		return false, fmt.Errorf("%w: %w", extensionserrors.ErrInvalidEndpoint, err)
	}

	config.FillContainerEnvs(nil, &d.Spec.Template, consts.DataPlaneProxyContainerName, config.EnvVarMapToSlice(envSet))
	dataPlane.Spec.Deployment.PodTemplateSpec = &d.Spec.Template
	p.adminAPIDisabled = adminAPIDisabled

	return true, nil
}

// AdminAPIDisabled returns true when the DataPlane is attached to a Konnect hybrid control plane
// and the user didn't opt out by setting KONG_ADMIN_LISTEN.
func (p *DataPlaneKonnectExtensionProcessor) AdminAPIDisabled() bool {
	return p.adminAPIDisabled
}

func kongInKonnectClusterCertVolume(secretName string) corev1.Volume {
	return corev1.Volume{
		Name: consts.KongClusterCertVolume,
		Secret: &corev1.SecretVolumeSource{
			SecretName:  secretName,
			DefaultMode: new(int32(420)),
		},
	}
}

func kongInKonnectClusterVolumeMount() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      consts.KongClusterCertVolume,
		MountPath: consts.KongClusterCertVolumeMountPath,
	}
}

func kongInKonnectClusterCertificateVolume() corev1.Volume {
	return corev1.Volume{
		Name: consts.ClusterCertificateVolume,
	}
}

func kongInKonnectClusterCertificateVolumeMount() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      consts.ClusterCertificateVolume,
		MountPath: consts.ClusterCertificateVolumeMountPath,
		ReadOnly:  true,
	}
}
