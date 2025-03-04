package config

import (
	"fmt"
	"strings"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

var kicInKonnectDefaults = func(secretName string) []corev1.EnvVar {
	return []corev1.EnvVar{
		{
			Name:  "CONTROLLER_KONNECT_ADDRESS",
			Value: "<KONNECT-ADDRESS>",
		},
		{
			Name:  "CONTROLLER_KONNECT_CONTROL_PLANE_ID",
			Value: "<KONNECT-CONTROL-PLANE-ID>",
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
						Name: secretName,
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
						Name: secretName,
					},
				},
			},
		},
	}
}

// KongInKonnectDefaults returns the slice of Konnect-related env vars properly configured.
func KICInKonnectDefaults(konnectExtensionStatus konnectv1alpha1.KonnectExtensionStatus) ([]corev1.EnvVar, error) {
	var template []corev1.EnvVar
	switch konnectExtensionStatus.Konnect.ClusterType {
	case konnectv1alpha1.ClusterTypeK8sIngressController:
		template = kicInKonnectDefaults(konnectExtensionStatus.DataPlaneClientAuth.CertificateSecretRef.Name)
	case konnectv1alpha1.ClusterTypeControlPlane:
		return nil, fmt.Errorf("unsupported Konnect cluster type: %s", konnectExtensionStatus.Konnect.ClusterType)
	default:
		// default never happens as the validation is at the CRD level
		panic(fmt.Sprintf("unsupported Konnect cluster type: %s", konnectExtensionStatus.Konnect.ClusterType))
	}

	newEnvSet := make([]corev1.EnvVar, len(template))
	for i, env := range template {
		newValue := strings.ReplaceAll(env.Value, "<KONNECT-ADDRESS>", buildKonnectAddress(konnectExtensionStatus.Konnect.Endpoints.ControlPlaneEndpoint))
		newValue = strings.ReplaceAll(newValue, "<KONNECT-CONTROL-PLANE-ID>", konnectExtensionStatus.Konnect.ControlPlaneID)
		env.Value = newValue
		newEnvSet[i] = env
	}

	return newEnvSet, nil
}

func buildKonnectAddress(endpoint string) string {
	// input: "https://7b46471d3b.us.tp0.konghq.tech:443"
	// output: "https://us.kic.api.konghq.tech"
	portlessEndpoint := strings.TrimSuffix(endpoint, ":443")
	addressSlice := strings.Split(portlessEndpoint, ".")
	addressSlice = lo.Filter(addressSlice, func(_ string, i int) bool {
		// remove 7b46471d3b and tp0
		if i == 0 || i == 2 {
			return false
		}
		return true
	})
	return fmt.Sprintf("https://%s.kic.api.%s", addressSlice[0], strings.Join(addressSlice[1:], "."))
}
