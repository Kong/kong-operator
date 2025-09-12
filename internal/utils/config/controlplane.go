package config

import (
	"fmt"
	"strings"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

type kicInKonnectParams struct {
	konnectAddress      string
	controlPlaneID      string
	tlsClientSecretName string
}

func kicInKonnectDefaults(params kicInKonnectParams) []corev1.EnvVar {
	return []corev1.EnvVar{
		{
			Name:  "CONTROLLER_KONNECT_ADDRESS",
			Value: params.konnectAddress,
		},
		{
			Name:  "CONTROLLER_KONNECT_CONTROL_PLANE_ID",
			Value: params.controlPlaneID,
		},
		{
			Name:  "CONTROLLER_KONNECT_LICENSING_ENABLED",
			Value: "true",
		},
		{
			Name:  "CONTROLLER_KONNECT_SYNC_ENABLED",
			Value: "true",
		},
		{
			Name:  "CONTROLLER_FEATURE_GATES",
			Value: "FillIDs=true",
		},
		{
			Name: "CONTROLLER_KONNECT_TLS_CLIENT_KEY",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					Key: "tls.key",
					LocalObjectReference: corev1.LocalObjectReference{
						Name: params.tlsClientSecretName,
					},
				},
			},
		},
		{
			Name: "CONTROLLER_KONNECT_TLS_CLIENT_CERT",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					Key: "tls.crt",
					LocalObjectReference: corev1.LocalObjectReference{
						Name: params.tlsClientSecretName,
					},
				},
			},
		},
	}
}

// KongInKonnectDefaults returns the slice of Konnect-related env vars properly configured.
func KICInKonnectDefaults(konnectExtensionStatus konnectv1alpha1.KonnectExtensionStatus) ([]corev1.EnvVar, error) {
	var envVars []corev1.EnvVar
	switch konnectExtensionStatus.Konnect.ClusterType {
	case konnectv1alpha1.ClusterTypeK8sIngressController:
		envVars = kicInKonnectDefaults(
			kicInKonnectParams{
				konnectAddress:      buildKonnectAddress(konnectExtensionStatus.Konnect.Endpoints.ControlPlaneEndpoint),
				controlPlaneID:      konnectExtensionStatus.Konnect.ControlPlaneID,
				tlsClientSecretName: konnectExtensionStatus.DataPlaneClientAuth.CertificateSecretRef.Name,
			},
		)
	case konnectv1alpha1.ClusterTypeControlPlane:
		return nil, fmt.Errorf("unsupported Konnect cluster type: %s", konnectExtensionStatus.Konnect.ClusterType)
	default:
		// default never happens as the validation is at the CRD level
		panic(fmt.Sprintf("unsupported Konnect cluster type: %s", konnectExtensionStatus.Konnect.ClusterType))
	}

	return envVars, nil
}

// buildKonnectAddress builds the Konnect address out of the control plane endpoint.
// input: "https://7b46471d3b.us..konghq.tech:443"
// output: "https://us.kic.api.konghq.tech"
func buildKonnectAddress(endpoint string) string {
	portlessEndpoint := strings.TrimSuffix(endpoint, ":443")
	addressSlice := strings.Split(portlessEndpoint, ".")
	addressSlice = lo.Filter(addressSlice, func(_ string, i int) bool {
		// remove 7b46471d3b and tp
		if i == 0 || i == 2 {
			return false
		}
		return true
	})
	return fmt.Sprintf("https://%s.kic.api.%s", addressSlice[0], strings.Join(addressSlice[1:], "."))
}
