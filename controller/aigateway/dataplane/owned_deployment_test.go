package dataplane

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	shareddataplane "github.com/kong/kong-operator/v2/controller/pkg/dataplane"
)

// -----------------------------------------------------------------
// helpers
// -----------------------------------------------------------------

// testKonnectAIGateway returns a minimal KonnectAIGateway with the
// given Konnect Configuration/Telemetry endpoints.
func testKonnectAIGateway(cpHost, tpHost string) *konnectv1alpha1.KonnectAIGateway {
	aigwcp := &konnectv1alpha1.KonnectAIGateway{}
	aigwcp.Status.Endpoints = &konnectv1alpha1.KonnectAIGatewayEndpoints{
		Configuration: cpHost,
		Telemetry:     tpHost,
	}
	return aigwcp
}

// findEnv finds an env-var by name in a slice, returning (value, found).
func findEnv(envs []corev1.EnvVar, name string) (string, bool) {
	for _, e := range envs {
		if e.Name == name {
			return e.Value, true
		}
	}
	return "", false
}

// mustEnv asserts an env-var exists and returns its value (first match).
func mustEnv(t *testing.T, envs []corev1.EnvVar, name string) string {
	t.Helper()
	v, ok := findEnv(envs, name)
	require.True(t, ok, "env var %q not found", name)
	return v
}

// -----------------------------------------------------------------
// buildAIGatewayEnvVars
// -----------------------------------------------------------------

func Test_buildAIGatewayEnvVars(t *testing.T) {
	const (
		cpHost = "abc-cp.us.konghq.com"
		tpHost = "abc-tp.us.konghq.com"
	)

	// konnectControlPlane returns a ResolvedControlPlane resolved to a
	// Konnect-backed KonnectAIGateway, onPremControlPlane one resolved to an
	// on-prem OnPremAIGateway.
	konnectControlPlane := func(aigwcp *konnectv1alpha1.KonnectAIGateway) shareddataplane.ResolvedControlPlane {
		return shareddataplane.ResolvedControlPlane{
			Kind:      "KonnectAIGateway",
			IsKonnect: true,
			Object:    aigwcp,
		}
	}
	onPremControlPlane := shareddataplane.ResolvedControlPlane{
		Kind:   "OnPremAIGateway",
		Object: &aigatewayv1alpha1.OnPremAIGateway{},
	}

	tests := []struct {
		name            string
		cp              shareddataplane.ResolvedControlPlane
		certSecretName  string
		adminCertSecret string
		wantErr         bool
		checkEnvs       func(t *testing.T, envs []corev1.EnvVar)
	}{
		{
			name:           "no endpoints in status returns error",
			cp:             konnectControlPlane(&konnectv1alpha1.KonnectAIGateway{}),
			certSecretName: "my-cert",
			wantErr:        true,
		},
		{
			name:           "nil aigwcp (no ControlPlaneRef): Konnect endpoint env vars omitted, no error",
			certSecretName: "my-cert",
			checkEnvs: func(t *testing.T, envs []corev1.EnvVar) {
				for _, name := range []string{
					EnvKongClusterControlPlane,
					EnvKongClusterServerName,
					EnvKongClusterTelemetryEndpoint,
					EnvKongClusterTelemetryServerName,
				} {
					for _, e := range envs {
						assert.NotEqual(t, name, e.Name, "env var %q must not be set when aigwcp is nil", name)
					}
				}
				assert.Equal(t, KonnectCertMountPath+"tls.crt", mustEnv(t, envs, EnvClientCertPath))
				assert.Equal(t, KonnectCertMountPath+"tls.key", mustEnv(t, envs, EnvKonnectClientCertKey))
				assert.Equal(t, "data_plane", mustEnv(t, envs, "KONG_ROLE"))
			},
		},
		{
			name:           "no cert Secret at all: cert-path env vars omitted too",
			certSecretName: "",
			checkEnvs: func(t *testing.T, envs []corev1.EnvVar) {
				for _, name := range []string{EnvClientCertPath, EnvKonnectClientCertKey} {
					for _, e := range envs {
						assert.NotEqual(t, name, e.Name, "env var %q must not be set when there's no cert Secret", name)
					}
				}
				assert.Equal(t, "data_plane", mustEnv(t, envs, "KONG_ROLE"))
			},
		},
		{
			name:           "env vars set correctly from endpoints",
			cp:             konnectControlPlane(testKonnectAIGateway(cpHost, tpHost)),
			certSecretName: "my-cert",
			checkEnvs: func(t *testing.T, envs []corev1.EnvVar) {
				assert.Equal(t, cpHost+":443", mustEnv(t, envs, EnvKongClusterControlPlane))
				assert.Equal(t, cpHost, mustEnv(t, envs, EnvKongClusterServerName))
				assert.Equal(t, tpHost+":443", mustEnv(t, envs, EnvKongClusterTelemetryEndpoint))
				assert.Equal(t, tpHost, mustEnv(t, envs, EnvKongClusterTelemetryServerName))
				assert.Equal(t, KonnectCertMountPath+"tls.crt", mustEnv(t, envs, EnvClientCertPath))
				assert.Equal(t, KonnectCertMountPath+"tls.key", mustEnv(t, envs, EnvKonnectClientCertKey))
			},
		},
		{
			name:           "required hardcoded env vars are present",
			cp:             konnectControlPlane(testKonnectAIGateway(cpHost, tpHost)),
			certSecretName: "my-cert",
			checkEnvs: func(t *testing.T, envs []corev1.EnvVar) {
				assert.Equal(t, "data_plane", mustEnv(t, envs, "KONG_ROLE"))
				assert.Equal(t, "off", mustEnv(t, envs, "KONG_DATABASE"))
				assert.Equal(t, "pki", mustEnv(t, envs, "KONG_CLUSTER_MTLS"))
				assert.Equal(t, "on", mustEnv(t, envs, EnvKongKonnectMode))
			},
		},
		{
			name:            "on-prem control plane: Konnect env vars omitted, admin listener and admin cert env vars set",
			cp:              onPremControlPlane,
			adminCertSecret: "my-admin-cert",
			checkEnvs: func(t *testing.T, envs []corev1.EnvVar) {
				for _, name := range []string{
					EnvKongClusterControlPlane,
					EnvKongClusterServerName,
					EnvKongClusterTelemetryEndpoint,
					EnvKongClusterTelemetryServerName,
					EnvClientCertPath,
					EnvKonnectClientCertKey,
					"KONG_ROLE",
					"KONG_CLUSTER_MTLS",
				} {
					for _, e := range envs {
						assert.NotEqual(t, name, e.Name, "env var %q must not be set for an on-prem control plane", name)
					}
				}
				assert.Equal(t, "off", mustEnv(t, envs, EnvKongKonnectMode))
				assert.Equal(t, "off", mustEnv(t, envs, "KONG_DATABASE"))
				assert.Equal(t, fmt.Sprintf("0.0.0.0:%d ssl", DefaultAdminPort), mustEnv(t, envs, EnvKongAdminListen))
				assert.Equal(t, AdminCertMountPath+"tls.crt", mustEnv(t, envs, EnvKongAdminSSLCert))
				assert.Equal(t, AdminCertMountPath+"tls.key", mustEnv(t, envs, EnvKongAdminSSLCertKey))
				assert.Equal(t, AdminCertMountPath+"ca.crt", mustEnv(t, envs, EnvKongNginxAdminSSLClientCertificate))
				assert.Equal(t, "on", mustEnv(t, envs, EnvKongNginxAdminSSLVerifyClient))
			},
		},
		{
			name: "on-prem control plane with no admin cert Secret: listener and admin cert env vars omitted",
			cp:   onPremControlPlane,
			checkEnvs: func(t *testing.T, envs []corev1.EnvVar) {
				for _, name := range []string{
					EnvKongAdminListen,
					EnvKongAdminSSLCert,
					EnvKongAdminSSLCertKey,
					EnvKongNginxAdminSSLClientCertificate,
					EnvKongNginxAdminSSLVerifyClient,
				} {
					for _, e := range envs {
						assert.NotEqual(t, name, e.Name, "env var %q must not be set when there's no admin cert Secret", name)
					}
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			envs, err := buildAIGatewayEnvVars(tc.cp, tc.certSecretName, tc.adminCertSecret)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			// env var names must be unique regardless of what checkEnvs verifies.
			seen := map[string]int{}
			for _, e := range envs {
				seen[e.Name]++
			}
			for name, count := range seen {
				assert.Equal(t, 1, count, "env var %q duplicated", name)
			}
			if tc.checkEnvs != nil {
				tc.checkEnvs(t, envs)
			}
		})
	}
}
