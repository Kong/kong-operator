package dataplane

import (
	"context"
	"errors"
	"fmt"

	"github.com/samber/lo"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1alpha1 "github.com/kong/gateway-operator/api/v1alpha1"
	"github.com/kong/gateway-operator/api/v1beta1"
	konnectextensions "github.com/kong/gateway-operator/internal/extensions/konnect"
	dputils "github.com/kong/gateway-operator/internal/utils/dataplane"
	"github.com/kong/gateway-operator/pkg/consts"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"
	k8sresources "github.com/kong/gateway-operator/pkg/utils/kubernetes/resources"
)

// applyKonnectExtension gets the DataPlane as argument, and in case it references a KonnectExtension, it
// fetches the referenced extension and applies the necessary changes to the DataPlane spec.
func applyKonnectExtension(ctx context.Context, cl client.Client, dataplane *v1beta1.DataPlane) error {
	for _, extensionRef := range dataplane.Spec.Extensions {
		if extensionRef.Group != operatorv1alpha1.SchemeGroupVersion.Group || extensionRef.Kind != operatorv1alpha1.KonnectExtensionKind {
			continue
		}
		namespace := dataplane.Namespace
		if extensionRef.Namespace != nil && *extensionRef.Namespace != namespace {
			return errors.Join(konnectextensions.ErrCrossNamespaceReference, fmt.Errorf("the cross-namespace reference to the extension %s/%s is not permitted", *extensionRef.Namespace, extensionRef.Name))
		}

		konnectExt := operatorv1alpha1.KonnectExtension{}
		if err := cl.Get(ctx, client.ObjectKey{
			Namespace: namespace,
			Name:      extensionRef.Name,
		}, &konnectExt); err != nil {
			if k8serrors.IsNotFound(err) {
				return errors.Join(konnectextensions.ErrKonnectExtensionNotFound, fmt.Errorf("the extension %s/%s referenced by the DataPlane is not found", namespace, extensionRef.Name))
			} else {
				return err
			}
		}

		secret := corev1.Secret{}
		if err := cl.Get(ctx, client.ObjectKey{
			Namespace: namespace,
			Name:      konnectExt.Spec.AuthConfiguration.ClusterCertificateSecretRef.Name,
		}, &secret); err != nil {
			if k8serrors.IsNotFound(err) {
				return errors.Join(konnectextensions.ErrClusterCertificateNotFound, fmt.Errorf("the cluster certificate secret %s/%s referenced by the extension %s/%s is not found", namespace, konnectExt.Spec.AuthConfiguration.ClusterCertificateSecretRef.Name, namespace, extensionRef.Name))
			} else {
				return err
			}
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

		// KonnectID is the only supported type for now, and its presence is guaranteed by a proper CEL rule.
		envSet := dputils.KongInKonnectDefaults(dputils.KongInKonnectParams{
			ControlPlane: *konnectExt.Spec.ControlPlaneRef.KonnectID,
			Region:       konnectExt.Spec.ControlPlaneRegion,
			Server:       konnectExt.Spec.ServerHostname,
		})

		dputils.FillDataPlaneProxyContainerEnvs(nil, &d.Spec.Template, envSet)
		dataplane.Spec.Deployment.PodTemplateSpec = &d.Spec.Template
	}
	return nil
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
