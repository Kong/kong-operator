package konnect

import (
	"context"
	"errors"
	"strings"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

func getKonnectAPIAuthRefNN(ctx context.Context, cl client.Client, ext *konnectv1alpha1.KonnectExtension) (types.NamespacedName, error) {
	if ext.Spec.KonnectConfiguration != nil {
		// TODO: handle cross namespace refs when supported.
		return types.NamespacedName{
			Namespace: ext.Namespace,
			Name:      ext.Spec.KonnectConfiguration.APIAuthConfigurationRef.Name,
		}, nil
	}

	// In case the KonnectConfiguration is not set, we fetch the KonnectGatewayControlPlane
	// and get the KonnectConfiguration from `spec.konnectControlPlane.controlPlane.konnectNamespacedRef`.
	// KonnectGatewayControlPlane reference and KonnectConfiguration
	// are mutually exclusive in the KonnectExtension API.
	cpRef := ext.Spec.KonnectControlPlane.ControlPlaneRef.KonnectNamespacedRef
	kgcp := &konnectv1alpha1.KonnectGatewayControlPlane{}
	err := cl.Get(ctx, client.ObjectKey{
		// TODO: handle cross namespace refs to KonnectGatewayControlPlane when referencing CP from another namespace is supported.
		Namespace: ext.Namespace,
		Name:      cpRef.Name,
	}, kgcp)
	if err != nil {
		return types.NamespacedName{}, err
	}
	return types.NamespacedName{
		Namespace: kgcp.Namespace,
		Name:      kgcp.Spec.KonnectConfiguration.APIAuthConfigurationRef.Name,
	}, nil
}

func getCertificateSecret(ctx context.Context, cl client.Client, ext konnectv1alpha1.KonnectExtension) (*corev1.Secret, error) {
	var certificateSecret corev1.Secret
	switch *ext.Spec.DataPlaneClientAuth.CertificateSecret.Provisioning {
	case konnectv1alpha1.ManualSecretProvisioning:
		// No need to check CertificateSecretRef is nil, as it is enforced at the CRD level.
		if err := cl.Get(ctx, types.NamespacedName{
			Namespace: ext.Namespace,
			Name:      ext.Spec.DataPlaneClientAuth.CertificateSecret.CertificateSecretRef.Name,
		}, &certificateSecret); err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("automatic secret provisioning not supported yet")
	}
	return &certificateSecret, nil
}

func konnectClusterTypeToCRDClusterType(clusterType sdkkonnectcomp.ControlPlaneClusterType) konnectv1alpha1.KonnectExtensionClusterType {
	switch clusterType {
	case sdkkonnectcomp.ControlPlaneClusterTypeClusterTypeControlPlane:
		return konnectv1alpha1.ClusterTypeControlPlane
	case sdkkonnectcomp.ControlPlaneClusterTypeClusterTypeK8SIngressController:
		return konnectv1alpha1.ClusterTypeK8sIngressController
	default:
		// default never happens as the validation is at the CRD level
		return ""
	}
}

func sanitizeCert(cert string) string {
	newCert := strings.TrimSuffix(cert, "\n")
	newCert = strings.ReplaceAll(newCert, "\r", "")
	return newCert
}
