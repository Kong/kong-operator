package dataplane

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
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

	tests := []struct {
		name      string
		aigwcp    *konnectv1alpha1.KonnectAIGateway
		wantErr   bool
		checkEnvs func(t *testing.T, envs []corev1.EnvVar)
	}{
		{
			name:    "no endpoints in status returns error",
			aigwcp:  &konnectv1alpha1.KonnectAIGateway{},
			wantErr: true,
		},
		{
			name:   "nil aigwcp (no ControlPlaneRef): Konnect endpoint env vars omitted, no error",
			aigwcp: nil,
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
			name:   "env vars set correctly from endpoints",
			aigwcp: testKonnectAIGateway(cpHost, tpHost),
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
			name:   "required hardcoded env vars are present",
			aigwcp: testKonnectAIGateway(cpHost, tpHost),
			checkEnvs: func(t *testing.T, envs []corev1.EnvVar) {
				assert.Equal(t, "data_plane", mustEnv(t, envs, "KONG_ROLE"))
				assert.Equal(t, "off", mustEnv(t, envs, "KONG_DATABASE"))
				assert.Equal(t, "pki", mustEnv(t, envs, "KONG_CLUSTER_MTLS"))
				assert.Equal(t, "on", mustEnv(t, envs, "KONG_KONNECT_MODE"))
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			envs, err := buildAIGatewayEnvVars(tc.aigwcp)
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
