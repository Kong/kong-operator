package dataplane

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	unstructured "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/managedfields"
	"k8s.io/client-go/tools/events"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

// -----------------------------------------------------------------
// helpers
// -----------------------------------------------------------------

// testKeg returns a minimal KonnectEventControlPlane with the given server URL and ID.
func testKeg(serverURL, id string) *konnectv1alpha1.KonnectEventControlPlane {
	keg := &konnectv1alpha1.KonnectEventControlPlane{}
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
// resolveImage
// -----------------------------------------------------------------

func Test_resolveImage(t *testing.T) {
	const defaultImg = "kong/keg:default"

	specEgdp := func(image string) *eventgatewayv1alpha1.KegDataPlane {
		return &eventgatewayv1alpha1.KegDataPlane{
			Spec: eventgatewayv1alpha1.KegDataPlaneSpec{
				Deployment: &eventgatewayv1alpha1.DeploymentOptions{
					PodTemplateSpec: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: consts.KEGContainerName, Image: image},
							},
						},
					},
				},
			},
		}
	}

	tests := []struct {
		name      string
		egdp      *eventgatewayv1alpha1.KegDataPlane
		envImage  string
		wantImage string
	}{
		{
			name:      "spec image used when set",
			egdp:      specEgdp("custom/keg:latest"),
			wantImage: "custom/keg:latest",
		},
		{
			name:      "spec image takes priority over env var",
			egdp:      specEgdp("spec/keg:spec"),
			envImage:  "env/keg:env",
			wantImage: "spec/keg:spec",
		},
		{
			name:      "env var used when spec image absent",
			egdp:      &eventgatewayv1alpha1.KegDataPlane{},
			envImage:  "env/keg:env",
			wantImage: "env/keg:env",
		},
		{
			name:      "falls back to default image",
			egdp:      &eventgatewayv1alpha1.KegDataPlane{},
			wantImage: defaultImg,
		},
		{
			name:      "default when spec deployment is nil",
			egdp:      &eventgatewayv1alpha1.KegDataPlane{Spec: eventgatewayv1alpha1.KegDataPlaneSpec{Deployment: nil}},
			wantImage: defaultImg,
		},
		{
			name: "default when PodTemplateSpec is nil",
			egdp: &eventgatewayv1alpha1.KegDataPlane{
				Spec: eventgatewayv1alpha1.KegDataPlaneSpec{
					Deployment: &eventgatewayv1alpha1.DeploymentOptions{PodTemplateSpec: nil},
				},
			},
			wantImage: defaultImg,
		},
		{
			name:      "default when keg container image is empty",
			egdp:      specEgdp(""),
			wantImage: defaultImg,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.envImage != "" {
				t.Setenv(consts.RelatedImageKEGEnvVar, tc.envImage)
			}
			assert.Equal(t, tc.wantImage, resolveImage(tc.egdp, defaultImg))
		})
	}
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
		keg     *konnectv1alpha1.KonnectEventControlPlane
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
							InsecureSkipVerify: ptr.To(eventgatewayv1alpha1.TLSVerificationStateEnabled),
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
							InsecureSkipVerify: ptr.To(eventgatewayv1alpha1.TLSVerificationStateDisabled),
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
						EnableDebugEndpoints: ptr.To(eventgatewayv1alpha1.DebugEndpointsStateEnabled),
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
						EnableDebugEndpoints: ptr.To(eventgatewayv1alpha1.DebugEndpointsStateDisabled),
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

// -----------------------------------------------------------------
// buildDeployment
// -----------------------------------------------------------------

func Test_buildDeployment(t *testing.T) {
	tc := managedfields.NewDeducedTypeConverter()

	validKeg := &konnectv1alpha1.KonnectEventControlPlane{}
	validKeg.Status.KonnectEntityStatus = konnectv1alpha2.KonnectEntityStatus{
		ServerURL: "https://us.api.konghq.com",
		ID:        "gw-id",
	}

	invalidKeg := &konnectv1alpha1.KonnectEventControlPlane{}
	invalidKeg.Status.ServerURL = "invalid-region.example.com"

	tests := []struct {
		name           string
		egdp           *eventgatewayv1alpha1.KegDataPlane
		keg            *konnectv1alpha1.KonnectEventControlPlane
		image          string
		certSecretName string
		wantErr        bool
		check          func(t *testing.T, u *unstructured.Unstructured)
	}{
		{
			name:           "spec.strategy absent (no overlay)",
			egdp:           &eventgatewayv1alpha1.KegDataPlane{},
			keg:            validKeg,
			image:          "kong/keg:test",
			certSecretName: "my-secret",
			check: func(t *testing.T, u *unstructured.Unstructured) {
				_, hasStrategy, err := unstructured.NestedFieldNoCopy(u.Object, "spec", "strategy")
				require.NoError(t, err)
				assert.False(t, hasStrategy, "spec.strategy must be absent to avoid SSA noise")
			},
		},
		{
			name:           "apiVersion and kind set correctly",
			egdp:           &eventgatewayv1alpha1.KegDataPlane{},
			keg:            validKeg,
			image:          "kong/keg:test",
			certSecretName: "cert-secret",
			check: func(t *testing.T, u *unstructured.Unstructured) {
				assert.Equal(t, "apps/v1", u.GetAPIVersion())
				assert.Equal(t, "Deployment", u.GetKind())
			},
		},
		{
			name:           "replicas absent when deployment spec not set",
			egdp:           &eventgatewayv1alpha1.KegDataPlane{},
			keg:            validKeg,
			image:          "kong/keg:test",
			certSecretName: "secret",
			check: func(t *testing.T, u *unstructured.Unstructured) {
				_, hasReplicas, _ := unstructured.NestedFieldNoCopy(u.Object, "spec", "replicas")
				assert.False(t, hasReplicas)
			},
		},
		{
			name: "replicas set from DeploymentOptions",
			egdp: &eventgatewayv1alpha1.KegDataPlane{
				Spec: eventgatewayv1alpha1.KegDataPlaneSpec{
					Deployment: &eventgatewayv1alpha1.DeploymentOptions{Replicas: new(int32(3))},
				},
			},
			keg:            validKeg,
			image:          "kong/keg:test",
			certSecretName: "secret",
			check: func(t *testing.T, u *unstructured.Unstructured) {
				replicas, _, _ := unstructured.NestedInt64(u.Object, "spec", "replicas")
				assert.Equal(t, int64(3), replicas)
			},
		},
		{
			name: "spec.strategy absent with PodTemplateSpec overlay",
			egdp: &eventgatewayv1alpha1.KegDataPlane{
				Spec: eventgatewayv1alpha1.KegDataPlaneSpec{
					Deployment: &eventgatewayv1alpha1.DeploymentOptions{
						PodTemplateSpec: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{Name: consts.KEGContainerName, Image: "custom/keg:overlay"},
								},
							},
						},
					},
				},
			},
			keg:            validKeg,
			image:          "custom/keg:overlay",
			certSecretName: "secret",
			check: func(t *testing.T, u *unstructured.Unstructured) {
				_, hasStrategy, err := unstructured.NestedFieldNoCopy(u.Object, "spec", "strategy")
				require.NoError(t, err)
				assert.False(t, hasStrategy, "spec.strategy must be absent even with PodTemplateSpec overlay")
			},
		},
		{
			name:           "invalid server URL returns error",
			egdp:           &eventgatewayv1alpha1.KegDataPlane{},
			keg:            invalidKeg,
			image:          "kong/keg:test",
			certSecretName: "secret",
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := buildDeployment(tc, tt.egdp, tt.keg, tt.image, tt.certSecretName)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, u)
			if tt.check != nil {
				tt.check(t, u)
			}
		})
	}
}

// -----------------------------------------------------------------
// ensureDeployment
// -----------------------------------------------------------------

func Test_ensureDeployment(t *testing.T) {
	const (
		ns     = "test-ns"
		dpName = "my-dp"
	)

	tc := managedfields.NewDeducedTypeConverter()
	scheme := managerscheme.Get()

	validKeg := &konnectv1alpha1.KonnectEventControlPlane{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "my-keg"},
	}
	validKeg.Status.KonnectEntityStatus = konnectv1alpha2.KonnectEntityStatus{
		ServerURL: "https://us.api.konghq.com",
		ID:        "gw-id",
	}

	egdp := &eventgatewayv1alpha1.KegDataPlane{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: dpName},
	}

	tests := []struct {
		name string
		// buildClient is called with a fresh fake client; return the client to use.
		buildClient func(base client.WithWatch) client.Client
		// prepareRecorder optionally drains events before the assertion (e.g. after a pre-run).
		prepareRecorder func(r *Reconciler, rec *events.FakeRecorder)
		wantErr         bool
		// substring expected in the first event; "" = no event expected
		wantEvent string
	}{
		{
			name:        "first call creates deployment and records DeploymentCreated event",
			buildClient: func(base client.WithWatch) client.Client { return base },
			wantErr:     false,
			wantEvent:   "DeploymentCreated",
		},
		{
			name:        "second call after content change records DeploymentUpdated event",
			buildClient: func(base client.WithWatch) client.Client { return base },
			// Run once first so the object exists, then drain the creation event.
			prepareRecorder: func(r *Reconciler, rec *events.FakeRecorder) {
				_ = r.ensureDeployment(context.Background(), logr.Discard(), egdp, validKeg, "cert-secret")
				<-rec.Events
			},
			wantErr:   false,
			wantEvent: "DeploymentUpdated",
		},
		{
			name: "Apply error is propagated and DeploymentFailed event is recorded",
			buildClient: func(base client.WithWatch) client.Client {
				return interceptor.NewClient(base, interceptor.Funcs{
					Apply: func(ctx context.Context, c client.WithWatch, obj runtime.ApplyConfiguration, opts ...client.ApplyOption) error {
						return assert.AnError
					},
				})
			},
			wantErr:   true,
			wantEvent: "DeploymentFailed",
		},
	}

	for _, tc2 := range tests {
		t.Run(tc2.name, func(t *testing.T) {
			recorder := events.NewFakeRecorder(10)
			base := fake.NewClientBuilder().WithScheme(scheme).Build()
			r := &Reconciler{
				Client:        tc2.buildClient(base),
				typeConverter: tc,
				eventRecorder: recorder,
			}

			if tc2.prepareRecorder != nil {
				tc2.prepareRecorder(r, recorder)
			}

			err := r.ensureDeployment(context.Background(), logr.Discard(), egdp, validKeg, "cert-secret")

			if tc2.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			if tc2.wantEvent != "" {
				select {
				case event := <-recorder.Events:
					assert.Contains(t, event, tc2.wantEvent)
				default:
					t.Errorf("expected event containing %q but channel was empty", tc2.wantEvent)
				}
			} else {
				assert.Empty(t, recorder.Events, "expected no events but got %d", len(recorder.Events))
			}
		})
	}
}
