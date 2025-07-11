package config

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	konnectv1alpha2 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha2"
)

func TestBuildKonnectAddress(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		expected string
	}{
		{
			name:     "standard endpoint",
			endpoint: "https://7b46471d3b.us.tp0.konghq.tech:443",
			expected: "https://us.kic.api.konghq.tech",
		},
		{
			name:     "different region",
			endpoint: "https://abcd1234.eu.tp0.konghq.tech:443",
			expected: "https://eu.kic.api.konghq.tech",
		},
		{
			name:     "longer hostname",
			endpoint: "https://abcd1234.us.tp0.konghq.foo.bar.tech:443",
			expected: "https://us.kic.api.konghq.foo.bar.tech",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildKonnectAddress(tt.endpoint)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestKICInKonnectDefaults(t *testing.T) {
	tests := []struct {
		name                   string
		konnectExtensionStatus konnectv1alpha2.KonnectExtensionStatus
		expected               []corev1.EnvVar
		expectError            bool
	}{
		{
			name: "K8s Ingress Controller",
			konnectExtensionStatus: konnectv1alpha2.KonnectExtensionStatus{
				Konnect: &konnectv1alpha2.KonnectExtensionControlPlaneStatus{
					ClusterType: konnectv1alpha2.ClusterTypeK8sIngressController,
					Endpoints: konnectv1alpha2.KonnectEndpoints{
						ControlPlaneEndpoint: "https://7b46471d3b.us.tp0.konghq.tech:443",
					},
					ControlPlaneID: "abcdedf",
				},
				DataPlaneClientAuth: &konnectv1alpha2.DataPlaneClientAuthStatus{
					CertificateSecretRef: &konnectv1alpha2.SecretRef{
						Name: "test-secret",
					},
				},
			},
			expected: []corev1.EnvVar{
				{
					Name:  "CONTROLLER_KONNECT_ADDRESS",
					Value: "https://us.kic.api.konghq.tech",
				},
				{
					Name:  "CONTROLLER_KONNECT_CONTROL_PLANE_ID",
					Value: "abcdedf",
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
								Name: "test-secret",
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
								Name: "test-secret",
							},
						},
					},
				},
			},
			expectError: false,
		},
		{
			name: "Unsupported Cluster Type",
			konnectExtensionStatus: konnectv1alpha2.KonnectExtensionStatus{
				Konnect: &konnectv1alpha2.KonnectExtensionControlPlaneStatus{
					ClusterType: konnectv1alpha2.ClusterTypeControlPlane,
				},
			},
			expected:    nil,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := KICInKonnectDefaults(tt.konnectExtensionStatus)
			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expected, result)
			}
		})
	}
}
