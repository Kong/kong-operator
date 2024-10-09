package dataplane

import (
	"context"
	"errors"
	"strings"

	"github.com/samber/lo"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/api/v1alpha1"
	"github.com/kong/gateway-operator/api/v1beta1"
	dputils "github.com/kong/gateway-operator/internal/utils/dataplane"
	"github.com/kong/gateway-operator/pkg/consts"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"
	k8sresources "github.com/kong/gateway-operator/pkg/utils/kubernetes/resources"
)

// applyDataPlaneKonnectExtension gets the DataPlane as argument, and in case it references a KonnectExtension, it
// fetches the referenced extension and applies the necessary changes to the DataPlane spec.
func applyDataPlaneKonnectExtension(ctx context.Context, cl client.Client, dataplane *v1beta1.DataPlane) error {
	for _, extensionRef := range dataplane.Spec.Extensions {
		if extensionRef.Group != v1alpha1.SchemeGroupVersion.Group || extensionRef.Kind != "DataPlaneKonnectExtension" {
			continue
		}
		namespace := dataplane.Namespace
		if extensionRef.Namespace != nil && *extensionRef.Namespace != namespace {
			return errors.New("cross-namespace reference is not currently supported for Konnect extensions")
		}

		konnectExt := v1alpha1.DataPlaneKonnectExtension{}
		if err := cl.Get(ctx, client.ObjectKey{
			Namespace: namespace,
			Name:      extensionRef.Name,
		}, &konnectExt); err != nil {
			return err
		}

		if dataplane.Spec.Deployment.PodTemplateSpec == nil {
			dataplane.Spec.Deployment.PodTemplateSpec = &corev1.PodTemplateSpec{}
		}

		d := k8sresources.Deployment(appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: *dataplane.Spec.Deployment.PodTemplateSpec,
			},
		})
		if container := k8sutils.GetPodContainerByName(&d.Spec.Template.Spec, consts.DataPlaneProxyContainerName); container == nil {
			d.Spec.Template.Spec.Containers = append(d.Spec.Template.Spec.Containers, corev1.Container{
				Name: consts.DataPlaneProxyContainerName,
			})
		}

		d.WithVolume(kongInKonnectClusterCertificateVolume())
		d.WithVolumeMount(kongInKonnectClusterCertificateVolumeMount(), consts.DataPlaneProxyContainerName)
		d.WithVolume(kongInKonnectClusterCertVolume(konnectExt.Spec.AuthConfiguration.ClusterCertificateSecretRef.Name))
		d.WithVolumeMount(kongInKonnectClusterVolumeMount(), consts.DataPlaneProxyContainerName)

		envSet := customizeKongInKonnectDefaults(
			dputils.KongInKonnectDefaults,
			*konnectExt.Spec.ControlPlaneRef.KonnectID,
			konnectExt.Spec.ControlPlaneRegion,
			konnectExt.Spec.ServerHostname)

		dputils.FillDataPlaneProxyContainerEnvs(nil, &d.Spec.Template, envSet)
		dataplane.Spec.Deployment.PodTemplateSpec = &d.Spec.Template
	}
	return nil
}

// customizeKongInKonnectDefaults replaces placeholders in the KongInKonnect env list with the actual values.
func customizeKongInKonnectDefaults(envSet map[string]string,
	controlPlane,
	region,
	server string,
) map[string]string {
	newEnvSet := make(map[string]string, len(envSet))
	for k, v := range envSet {
		v = strings.ReplaceAll(v, "<CP-ID>", controlPlane)
		v = strings.ReplaceAll(v, "<REGION>", region)
		v = strings.ReplaceAll(v, "<SERVER>", server)
		newEnvSet[k] = v
	}
	return newEnvSet
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
