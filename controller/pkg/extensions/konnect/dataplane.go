package konnect

import (
	"context"

	"github.com/samber/lo"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1beta1"
	konnectv1alpha2 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha2"

	"github.com/kong/kong-operator/internal/utils/config"
	"github.com/kong/kong-operator/pkg/consts"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
	k8sresources "github.com/kong/kong-operator/pkg/utils/kubernetes/resources"
)

// ApplyDataPlaneKonnectExtension gets the DataPlane as argument, and in case it references a KonnectExtension, it
// fetches the referenced extension and applies the necessary changes to the DataPlane spec.
func ApplyDataPlaneKonnectExtension(ctx context.Context, cl client.Client, dataPlane *operatorv1beta1.DataPlane) (bool, error) {
	var konnectExtension *konnectv1alpha2.KonnectExtension
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

	d.WithVolume(kongInKonnectClusterCertificateVolume())
	d.WithVolumeMount(kongInKonnectClusterCertificateVolumeMount(), consts.DataPlaneProxyContainerName)
	d.WithVolume(kongInKonnectClusterCertVolume(konnectExtension.Status.DataPlaneClientAuth.CertificateSecretRef.Name))
	d.WithVolumeMount(kongInKonnectClusterVolumeMount(), consts.DataPlaneProxyContainerName)

	// KonnectID is the only supported type for now, and its presence is guaranteed by a proper CEL rule.
	var dataplaneLabels map[string]konnectv1alpha2.DataPlaneLabelValue
	if konnectExtension.Spec.Konnect.DataPlane != nil {
		dataplaneLabels = konnectExtension.Spec.Konnect.DataPlane.Labels
	}
	envSet := config.KongInKonnectDefaults(dataplaneLabels, konnectExtension.Status)

	config.FillContainerEnvs(nil, &d.Spec.Template, consts.DataPlaneProxyContainerName, config.EnvVarMapToSlice(envSet))
	dataPlane.Spec.Deployment.PodTemplateSpec = &d.Spec.Template

	return true, nil
}

func kongInKonnectClusterCertVolume(secretName string) corev1.Volume {
	return corev1.Volume{
		Name: consts.KongClusterCertVolume,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName:  secretName,
				DefaultMode: lo.ToPtr(int32(420)),
			},
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
