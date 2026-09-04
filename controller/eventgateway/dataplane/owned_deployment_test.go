package dataplane

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
)

// -----------------------------------------------------------------
// helpers
// -----------------------------------------------------------------

// testKeg returns a minimal KonnectEventGateway with the given server URL and ID.
func testKeg(serverURL, id string) *konnectv1alpha1.KonnectEventGateway {
	keg := &konnectv1alpha1.KonnectEventGateway{}
	keg.Status.ServerURL = serverURL
	keg.Status.ID = id
	return keg
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
// buildKEGEnvVars
// -----------------------------------------------------------------

func Test_buildKEGEnvVars(t *testing.T) {
	const (
		serverURL = "https://us.api.konghq.com"
		clusterID = "cluster-abc"
	)

	validKeg := testKeg(serverURL, clusterID)

	tests := []struct {
		name    string
		egdp    *eventgatewayv1alpha1.KegDataPlane
		keg     *konnectv1alpha1.KonnectEventGateway
		wantErr bool
		// checkEnvs is called with the resulting env slice when wantErr is false.
		checkEnvs func(t *testing.T, envs []corev1.EnvVar)
	}{
		{
			name: "base env vars set correctly",
			egdp: &eventgatewayv1alpha1.KegDataPlane{},
			keg:  validKeg,
			checkEnvs: func(t *testing.T, envs []corev1.EnvVar) {
				assert.Equal(t, "us", mustEnv(t, envs, EnvKonnectRegion))
				assert.Equal(t, clusterID, mustEnv(t, envs, EnvKonnectGatewayClusterID))
				assert.Equal(t, KonnectCertMountPath+"tls.crt", mustEnv(t, envs, EnvKonnectClientCertPath))
				assert.Equal(t, KonnectCertMountPath+"tls.key", mustEnv(t, envs, EnvKonnectClientKeyPath))
				assert.Equal(t, "konghq.com", mustEnv(t, envs, EnvKonnectDomain))
				assert.Equal(t, "0.0.0.0:8080", mustEnv(t, envs, EnvRuntimeHealthAddr))
			},
		},
		{
			name:    "invalid server URL returns error",
			egdp:    &eventgatewayv1alpha1.KegDataPlane{},
			keg:     testKeg("not-a-valid-region.something", "id"),
			wantErr: true,
		},
		{
			name: "nil config: no optional vars present",
			egdp: &eventgatewayv1alpha1.KegDataPlane{},
			keg:  validKeg,
			checkEnvs: func(t *testing.T, envs []corev1.EnvVar) {
				_, ok := findEnv(envs, EnvKonnectAPIRequestTimeout)
				assert.False(t, ok, "unexpected APIRequestTimeout env var")
			},
		},
		{
			name: "konnect domain override",
			egdp: &eventgatewayv1alpha1.KegDataPlane{
				Spec: eventgatewayv1alpha1.KegDataPlaneSpec{
					Config: &eventgatewayv1alpha1.KegDataPlaneConfiguration{
						Konnect: &eventgatewayv1alpha1.KonnectConfig{Domain: new("custom.example.com")},
					},
				},
			},
			keg: validKeg,
			checkEnvs: func(t *testing.T, envs []corev1.EnvVar) {
				assert.Equal(t, "custom.example.com", mustEnv(t, envs, EnvKonnectDomain))
			},
		},
		{
			name: "konnect API request timeout",
			egdp: &eventgatewayv1alpha1.KegDataPlane{
				Spec: eventgatewayv1alpha1.KegDataPlaneSpec{
					Config: &eventgatewayv1alpha1.KegDataPlaneConfiguration{
						Konnect: &eventgatewayv1alpha1.KonnectConfig{APIRequestTimeoutSeconds: new(int32(30))},
					},
				},
			},
			keg: validKeg,
			checkEnvs: func(t *testing.T, envs []corev1.EnvVar) {
				assert.Equal(t, "30s", mustEnv(t, envs, EnvKonnectAPIRequestTimeout))
			},
		},
		{
			name: "konnect insecure skip verify enabled",
			egdp: &eventgatewayv1alpha1.KegDataPlane{
				Spec: eventgatewayv1alpha1.KegDataPlaneSpec{
					Config: &eventgatewayv1alpha1.KegDataPlaneConfiguration{
						Konnect: &eventgatewayv1alpha1.KonnectConfig{
							InsecureSkipVerify: new(eventgatewayv1alpha1.TLSVerificationStateEnabled),
						},
					},
				},
			},
			keg: validKeg,
			checkEnvs: func(t *testing.T, envs []corev1.EnvVar) {
				assert.Equal(t, "true", mustEnv(t, envs, EnvKonnectInsecureSkipVerify))
			},
		},
		{
			name: "konnect insecure skip verify disabled",
			egdp: &eventgatewayv1alpha1.KegDataPlane{
				Spec: eventgatewayv1alpha1.KegDataPlaneSpec{
					Config: &eventgatewayv1alpha1.KegDataPlaneConfiguration{
						Konnect: &eventgatewayv1alpha1.KonnectConfig{
							InsecureSkipVerify: new(eventgatewayv1alpha1.TLSVerificationStateDisabled),
						},
					},
				},
			},
			keg: validKeg,
			checkEnvs: func(t *testing.T, envs []corev1.EnvVar) {
				assert.Equal(t, "false", mustEnv(t, envs, EnvKonnectInsecureSkipVerify))
			},
		},
		{
			name: "config poll interval",
			egdp: &eventgatewayv1alpha1.KegDataPlane{
				Spec: eventgatewayv1alpha1.KegDataPlaneSpec{
					Config: &eventgatewayv1alpha1.KegDataPlaneConfiguration{
						ConfigPollIntervalSeconds: new(int32(60)),
					},
				},
			},
			keg: validKeg,
			checkEnvs: func(t *testing.T, envs []corev1.EnvVar) {
				assert.Equal(t, "60s", mustEnv(t, envs, EnvConfigPollInterval))
			},
		},
		{
			name: "debug endpoints enabled",
			egdp: &eventgatewayv1alpha1.KegDataPlane{
				Spec: eventgatewayv1alpha1.KegDataPlaneSpec{
					Config: &eventgatewayv1alpha1.KegDataPlaneConfiguration{
						EnableDebugEndpoints: new(eventgatewayv1alpha1.DebugEndpointsStateEnabled),
					},
				},
			},
			keg: validKeg,
			checkEnvs: func(t *testing.T, envs []corev1.EnvVar) {
				assert.Equal(t, "true", mustEnv(t, envs, EnvEnableDebugEndpoints))
			},
		},
		{
			name: "debug endpoints disabled",
			egdp: &eventgatewayv1alpha1.KegDataPlane{
				Spec: eventgatewayv1alpha1.KegDataPlaneSpec{
					Config: &eventgatewayv1alpha1.KegDataPlaneConfiguration{
						EnableDebugEndpoints: new(eventgatewayv1alpha1.DebugEndpointsStateDisabled),
					},
				},
			},
			keg: validKeg,
			checkEnvs: func(t *testing.T, envs []corev1.EnvVar) {
				assert.Equal(t, "false", mustEnv(t, envs, EnvEnableDebugEndpoints))
			},
		},
		{
			name: "observability fields",
			egdp: &eventgatewayv1alpha1.KegDataPlane{
				Spec: eventgatewayv1alpha1.KegDataPlaneSpec{
					Config: &eventgatewayv1alpha1.KegDataPlaneConfiguration{
						Observability: &eventgatewayv1alpha1.ObservabilityConfig{
							LogFlags:                           new("debug"),
							LogFormat:                          new("json"),
							MetricsRollupAllowMap:              new("my-map"),
							PolicyErrorsInfoLogIntervalSeconds: new(int32(10)),
						},
					},
				},
			},
			keg: validKeg,
			checkEnvs: func(t *testing.T, envs []corev1.EnvVar) {
				assert.Equal(t, "debug", mustEnv(t, envs, EnvObsLogFlags))
				assert.Equal(t, "json", mustEnv(t, envs, EnvObsLogFormat))
				assert.Equal(t, "my-map", mustEnv(t, envs, EnvObsMetricsRollupAllowMap))
				assert.Equal(t, "10s", mustEnv(t, envs, EnvObsPolicyErrorsInfoLogInterval))
			},
		},
		{
			name: "runtime fields",
			egdp: &eventgatewayv1alpha1.KegDataPlane{
				Spec: eventgatewayv1alpha1.KegDataPlaneSpec{
					Config: &eventgatewayv1alpha1.KegDataPlaneConfiguration{
						Runtime: &eventgatewayv1alpha1.RuntimeOptions{
							HealthListenerAddressPort: new("0.0.0.0:9090"),
							DrainDurationSeconds:      new(int32(15)),
							ShutdownTimeoutSeconds:    new(int32(30)),
						},
					},
				},
			},
			keg: validKeg,
			checkEnvs: func(t *testing.T, envs []corev1.EnvVar) {
				assert.Equal(t, "0.0.0.0:9090", mustEnv(t, envs, EnvRuntimeHealthAddr))
				assert.Equal(t, "15s", mustEnv(t, envs, EnvRuntimeDrainDuration))
				assert.Equal(t, "30s", mustEnv(t, envs, EnvRuntimeShutdownTimeout))
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			envs, err := buildKEGEnvVars(tc.egdp, tc.keg)
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
