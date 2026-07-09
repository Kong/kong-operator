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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

// -----------------------------------------------------------------
// helpers
// -----------------------------------------------------------------

// testAIGatewayControlPlane returns a minimal AIGatewayControlPlane with the
// given Konnect Configuration/Telemetry endpoints.
func testAIGatewayControlPlane(cpHost, tpHost string) *konnectv1alpha1.AIGatewayControlPlane {
	aigwcp := &konnectv1alpha1.AIGatewayControlPlane{}
	aigwcp.Status.Endpoints = &konnectv1alpha1.AIGatewayControlPlaneEndpoints{
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
// resolveImage
// -----------------------------------------------------------------

func Test_resolveImage(t *testing.T) {
	const defaultImg = "kong/aigw:default"

	specAigwdp := func(image string) *aigatewayv1alpha1.AIGatewayDataPlane {
		return &aigatewayv1alpha1.AIGatewayDataPlane{
			Spec: aigatewayv1alpha1.AIGatewayDataPlaneSpec{
				Deployment: &aigatewayv1alpha1.DeploymentOptions{
					PodTemplateSpec: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: consts.AIGatewayContainerName, Image: image},
							},
						},
					},
				},
			},
		}
	}

	tests := []struct {
		name      string
		aigwdp    *aigatewayv1alpha1.AIGatewayDataPlane
		envImage  string
		wantImage string
	}{
		{
			name:      "spec image used when set",
			aigwdp:    specAigwdp("custom/aigw:latest"),
			wantImage: "custom/aigw:latest",
		},
		{
			name:      "spec image takes priority over env var",
			aigwdp:    specAigwdp("spec/aigw:spec"),
			envImage:  "env/aigw:env",
			wantImage: "spec/aigw:spec",
		},
		{
			name:      "env var used when spec image absent",
			aigwdp:    &aigatewayv1alpha1.AIGatewayDataPlane{},
			envImage:  "env/aigw:env",
			wantImage: "env/aigw:env",
		},
		{
			name:      "falls back to default image",
			aigwdp:    &aigatewayv1alpha1.AIGatewayDataPlane{},
			wantImage: defaultImg,
		},
		{
			name:      "default when spec deployment is nil",
			aigwdp:    &aigatewayv1alpha1.AIGatewayDataPlane{Spec: aigatewayv1alpha1.AIGatewayDataPlaneSpec{Deployment: nil}},
			wantImage: defaultImg,
		},
		{
			name: "default when PodTemplateSpec is nil",
			aigwdp: &aigatewayv1alpha1.AIGatewayDataPlane{
				Spec: aigatewayv1alpha1.AIGatewayDataPlaneSpec{
					Deployment: &aigatewayv1alpha1.DeploymentOptions{PodTemplateSpec: nil},
				},
			},
			wantImage: defaultImg,
		},
		{
			name:      "default when aigw container image is empty",
			aigwdp:    specAigwdp(""),
			wantImage: defaultImg,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.envImage != "" {
				t.Setenv(consts.RelatedImageAIGatewayEnvVar, tc.envImage)
			}
			assert.Equal(t, tc.wantImage, resolveImage(tc.aigwdp, defaultImg))
		})
	}
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
		aigwcp    *konnectv1alpha1.AIGatewayControlPlane
		wantErr   bool
		checkEnvs func(t *testing.T, envs []corev1.EnvVar)
	}{
		{
			name:    "no endpoints in status returns error",
			aigwcp:  &konnectv1alpha1.AIGatewayControlPlane{},
			wantErr: true,
		},
		{
			name:   "env vars set correctly from endpoints",
			aigwcp: testAIGatewayControlPlane(cpHost, tpHost),
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
			aigwcp: testAIGatewayControlPlane(cpHost, tpHost),
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

// -----------------------------------------------------------------
// buildDeployment
// -----------------------------------------------------------------

func Test_buildDeployment(t *testing.T) {
	tc := managedfields.NewDeducedTypeConverter()

	validCP := testAIGatewayControlPlane("cp.example.com", "tp.example.com")
	invalidCP := &konnectv1alpha1.AIGatewayControlPlane{}

	tests := []struct {
		name           string
		aigwdp         *aigatewayv1alpha1.AIGatewayDataPlane
		aigwcp         *konnectv1alpha1.AIGatewayControlPlane
		image          string
		certSecretName string
		wantErr        bool
		check          func(t *testing.T, u *unstructured.Unstructured)
	}{
		{
			name:           "spec.strategy absent (no overlay)",
			aigwdp:         &aigatewayv1alpha1.AIGatewayDataPlane{},
			aigwcp:         validCP,
			image:          "kong/aigw:test",
			certSecretName: "my-secret",
			check: func(t *testing.T, u *unstructured.Unstructured) {
				_, hasStrategy, err := unstructured.NestedFieldNoCopy(u.Object, "spec", "strategy")
				require.NoError(t, err)
				assert.False(t, hasStrategy, "spec.strategy must be absent to avoid SSA noise")
			},
		},
		{
			name:           "apiVersion and kind set correctly",
			aigwdp:         &aigatewayv1alpha1.AIGatewayDataPlane{},
			aigwcp:         validCP,
			image:          "kong/aigw:test",
			certSecretName: "cert-secret",
			check: func(t *testing.T, u *unstructured.Unstructured) {
				assert.Equal(t, "apps/v1", u.GetAPIVersion())
				assert.Equal(t, "Deployment", u.GetKind())
			},
		},
		{
			name:           "replicas absent when deployment spec not set",
			aigwdp:         &aigatewayv1alpha1.AIGatewayDataPlane{},
			aigwcp:         validCP,
			image:          "kong/aigw:test",
			certSecretName: "secret",
			check: func(t *testing.T, u *unstructured.Unstructured) {
				_, hasReplicas, _ := unstructured.NestedFieldNoCopy(u.Object, "spec", "replicas")
				assert.False(t, hasReplicas)
			},
		},
		{
			name: "replicas set from DeploymentOptions",
			aigwdp: &aigatewayv1alpha1.AIGatewayDataPlane{
				Spec: aigatewayv1alpha1.AIGatewayDataPlaneSpec{
					Deployment: &aigatewayv1alpha1.DeploymentOptions{Replicas: new(int32(3))},
				},
			},
			aigwcp:         validCP,
			image:          "kong/aigw:test",
			certSecretName: "secret",
			check: func(t *testing.T, u *unstructured.Unstructured) {
				replicas, _, _ := unstructured.NestedInt64(u.Object, "spec", "replicas")
				assert.Equal(t, int64(3), replicas)
			},
		},
		{
			name: "spec.strategy absent with PodTemplateSpec overlay",
			aigwdp: &aigatewayv1alpha1.AIGatewayDataPlane{
				Spec: aigatewayv1alpha1.AIGatewayDataPlaneSpec{
					Deployment: &aigatewayv1alpha1.DeploymentOptions{
						PodTemplateSpec: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{Name: consts.AIGatewayContainerName, Image: "custom/aigw:overlay"},
								},
							},
						},
					},
				},
			},
			aigwcp:         validCP,
			image:          "custom/aigw:overlay",
			certSecretName: "secret",
			check: func(t *testing.T, u *unstructured.Unstructured) {
				_, hasStrategy, err := unstructured.NestedFieldNoCopy(u.Object, "spec", "strategy")
				require.NoError(t, err)
				assert.False(t, hasStrategy, "spec.strategy must be absent even with PodTemplateSpec overlay")
			},
		},
		{
			name:           "AIGatewayControlPlane with no endpoints returns error",
			aigwdp:         &aigatewayv1alpha1.AIGatewayDataPlane{},
			aigwcp:         invalidCP,
			image:          "kong/aigw:test",
			certSecretName: "secret",
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := buildDeployment(tc, tt.aigwdp, tt.aigwcp, tt.image, tt.certSecretName)
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

	validCP := testAIGatewayControlPlane("cp.example.com", "tp.example.com")

	aigwdp := &aigatewayv1alpha1.AIGatewayDataPlane{
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
				_ = r.ensureDeployment(context.Background(), logr.Discard(), aigwdp, validCP, "cert-secret")
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
				TypeConverter: tc,
				eventRecorder: recorder,
			}

			if tc2.prepareRecorder != nil {
				tc2.prepareRecorder(r, recorder)
			}

			err := r.ensureDeployment(context.Background(), logr.Discard(), aigwdp, validCP, "cert-secret")

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
