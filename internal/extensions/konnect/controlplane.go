package konnect

import (
	"context"
	"errors"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1alpha1 "github.com/kong/gateway-operator/api/v1alpha1"
	operatorv1beta1 "github.com/kong/gateway-operator/api/v1beta1"
	"github.com/kong/gateway-operator/pkg/consts"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"
	k8sresources "github.com/kong/gateway-operator/pkg/utils/kubernetes/resources"
)

func ApplyControlPlaneKonnectExtension(ctx context.Context, cl client.Client, controlplane *operatorv1beta1.ControlPlane) error {
	for _, extensionRef := range controlplane.Spec.Extensions {
		if extensionRef.Group != operatorv1alpha1.SchemeGroupVersion.Group || extensionRef.Kind != operatorv1alpha1.KonnectExtensionKind {
			continue
		}
		namespace := controlplane.Namespace
		if extensionRef.Namespace != nil && *extensionRef.Namespace != namespace {
			return errors.Join(ErrCrossNamespaceReference, fmt.Errorf("the cross-namespace reference to the extension %s/%s is not permitted", *extensionRef.Namespace, extensionRef.Name))
		}

		konnectExt := operatorv1alpha1.KonnectExtension{}
		if err := cl.Get(ctx, client.ObjectKey{
			Namespace: namespace,
			Name:      extensionRef.Name,
		}, &konnectExt); err != nil {
			if k8serrors.IsNotFound(err) {
				return errors.Join(ErrKonnectExtensionNotFound, fmt.Errorf("the extension %s/%s referenced by the DataPlane is not found", namespace, extensionRef.Name))
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
				return errors.Join(ErrClusterCertificateNotFound, fmt.Errorf("the cluster certificate secret %s/%s referenced by the extension %s/%s is not found", namespace, konnectExt.Spec.AuthConfiguration.ClusterCertificateSecretRef.Name, namespace, extensionRef.Name))
			} else {
				return err
			}
		}

		if controlplane.Spec.Deployment.PodTemplateSpec == nil {
			controlplane.Spec.Deployment.PodTemplateSpec = &corev1.PodTemplateSpec{}
		}

		d := k8sresources.Deployment(appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: *controlplane.Spec.Deployment.PodTemplateSpec,
			},
		})
		if container := k8sutils.GetPodContainerByName(&d.Spec.Template.Spec, consts.ControlPlaneControllerContainerName); container == nil {
			d.Spec.Template.Spec.Containers = append(d.Spec.Template.Spec.Containers, corev1.Container{
				Name: consts.DataPlaneProxyContainerName,
			})
		}
	}
	return nil
}
