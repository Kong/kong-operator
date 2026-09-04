package dataplane

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
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
// resolveImage
// -----------------------------------------------------------------

func Test_ResolveImage(t *testing.T) {
	const defaultImg = "kong/aigw:default"

	specAigwdp := func(image string) *aigatewayv1alpha1.AIGatewayDataPlane {
		return &aigatewayv1alpha1.AIGatewayDataPlane{
			Spec: aigatewayv1alpha1.AIGatewayDataPlaneSpec{
				Deployment: &aigatewayv1alpha1.DeploymentOptions{
					PodTemplateSpec: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: consts.AIGatewayDataPlaneContainerName, Image: image},
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
				t.Setenv(consts.RelatedImageAIGatewayDataPlaneEnvVar, tc.envImage)
			}
			assert.Equal(t, tc.wantImage, ResolveImage(tc.aigwdp, deploymentConfigWithDefaultImage(defaultImg)))
		})
	}
}

// infoCountSink is a minimal logr.LogSink that counts Info() calls.

// -----------------------------------------------------------------
// addAnnotationsForAIGatewayDataPlaneDeployment / addLabelsForAIGatewayDataPlaneDeployment
// -----------------------------------------------------------------

func Test_addAnnotationsForDataPlaneDeployment(t *testing.T) {
	testCases := []struct {
		name                string
		existingAnnotations map[string]string
		specAnnotations     map[string]string
		expectedAnnotations map[string]string
		expectedInfoCount   int
	}{
		{
			name:                "no-op when AIGatewayDataPlane has no deployment annotations",
			existingAnnotations: map[string]string{"existing": "val"},
			expectedAnnotations: map[string]string{"existing": "val"},
		},
		{
			name:                "new keys merged, conflicting keys overridden",
			existingAnnotations: map[string]string{"existing": "val", "conflict": "old"},
			specAnnotations:     map[string]string{"new": "val", "conflict": "new"},
			expectedAnnotations: map[string]string{"existing": "val", "new": "val", "conflict": "new"},
		},
		{
			name:                "nil existing annotations initialized correctly",
			specAnnotations:     map[string]string{"k": "v"},
			expectedAnnotations: map[string]string{"k": "v"},
		},
		{
			name: "reserved keys are dropped and a warning is logged",
			specAnnotations: map[string]string{
				"safe":                           "val",
				consts.OperatorLabelPrefix + "x": "val",
			},
			expectedAnnotations: map[string]string{"safe": "val"},
			expectedInfoCount:   1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			aigwdp := &aigatewayv1alpha1.AIGatewayDataPlane{
				Spec: aigatewayv1alpha1.AIGatewayDataPlaneSpec{
					Deployment: &aigatewayv1alpha1.DeploymentOptions{Annotations: tc.specAnnotations},
				},
			}
			deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Annotations: tc.existingAnnotations}}
			var infoCount int
			addAnnotationsForDataPlaneDeployment(logr.New(infoCountSink{count: &infoCount}), deployment, testConfig.Deployment.DeploymentAnnotations(aigwdp))
			require.Equal(t, tc.expectedAnnotations, deployment.Annotations)
			assert.Equal(t, tc.expectedInfoCount, infoCount)
		})
	}
}

func Test_addLabelsForDataPlaneDeployment(t *testing.T) {
	testCases := []struct {
		name              string
		existingLabels    map[string]string
		specLabels        map[string]string
		expectedLabels    map[string]string
		expectedInfoCount int
	}{
		{
			name:           "no-op when AIGatewayDataPlane has no deployment labels",
			existingLabels: map[string]string{"existing": "val"},
			expectedLabels: map[string]string{"existing": "val"},
		},
		{
			name:           "new keys merged, conflicting keys overridden",
			existingLabels: map[string]string{"existing": "val", "conflict": "old"},
			specLabels:     map[string]string{"new": "val", "conflict": "new"},
			expectedLabels: map[string]string{"existing": "val", "new": "val", "conflict": "new"},
		},
		{
			name:           "nil existing labels initialized correctly",
			specLabels:     map[string]string{"k": "v"},
			expectedLabels: map[string]string{"k": "v"},
		},
		{
			name:              "reserved keys are dropped and a warning is logged",
			specLabels:        map[string]string{"safe": "val", "app.kubernetes.io/name": "should-not-override-base-label"},
			expectedLabels:    map[string]string{"safe": "val"},
			expectedInfoCount: 1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			aigwdp := &aigatewayv1alpha1.AIGatewayDataPlane{
				Spec: aigatewayv1alpha1.AIGatewayDataPlaneSpec{
					Deployment: &aigatewayv1alpha1.DeploymentOptions{Labels: tc.specLabels},
				},
			}
			deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Labels: tc.existingLabels}}
			var infoCount int
			addLabelsForDataPlaneDeployment(logr.New(infoCountSink{count: &infoCount}), deployment, testConfig.Deployment.DeploymentLabels(aigwdp))
			require.Equal(t, tc.expectedLabels, deployment.Labels)
			assert.Equal(t, tc.expectedInfoCount, infoCount)
		})
	}
}

// -----------------------------------------------------------------
// generateBaseDeployment
// -----------------------------------------------------------------

func Test_GenerateBaseDeployment_hardening(t *testing.T) {
	aigwdp := &aigatewayv1alpha1.AIGatewayDataPlane{
		ObjectMeta: metav1.ObjectMeta{Name: "my-aigw", Namespace: "test-ns"},
	}
	aigwcp := testKonnectAIGateway()

	d, err := GenerateBaseDeployment(logr.Discard(), aigwdp, aigwcp, "kong/aigw:test", "cert-secret", testConfig)
	require.NoError(t, err)
	require.Len(t, d.Spec.Template.Spec.Containers, 1)
	container := d.Spec.Template.Spec.Containers[0]

	require.Equal(t, &corev1.SecurityContext{
		AllowPrivilegeEscalation: new(false),
		ReadOnlyRootFilesystem:   new(true),
		RunAsNonRoot:             new(true),
		RunAsUser:                new(int64(65532)),
		RunAsGroup:               new(int64(65532)),
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
			Add:  []corev1.Capability{"NET_BIND_SERVICE"},
		},
	}, container.SecurityContext)

	assert.Equal(t, []corev1.VolumeMount{
		{Name: KonnectCertVolumeName, MountPath: KonnectCertMountPath, ReadOnly: true},
		{Name: "tmp", MountPath: "/tmp"},
		{Name: "var-kong", MountPath: "/var/kong"},
	}, container.VolumeMounts)

	assert.Equal(t, "/var/kong", mustEnv(t, container.Env, "KONG_PREFIX"))

	volumeNames := make([]string, 0, len(d.Spec.Template.Spec.Volumes))
	for _, v := range d.Spec.Template.Spec.Volumes {
		volumeNames = append(volumeNames, v.Name)
	}
	assert.ElementsMatch(t, []string{"tmp", "var-kong", KonnectCertVolumeName}, volumeNames)
}

// Test_GenerateBaseDeployment_LabelsAndAnnotations verifies that
// spec.deployment.labels/annotations are propagated onto the generated
// Deployment's own metadata, that reserved keys are dropped, and that the
// Pod template's labels (which share the base labels map) are unaffected.
func Test_GenerateBaseDeployment_LabelsAndAnnotations(t *testing.T) {
	aigwdp := &aigatewayv1alpha1.AIGatewayDataPlane{
		ObjectMeta: metav1.ObjectMeta{Name: "my-aigw", Namespace: "test-ns"},
		Spec: aigatewayv1alpha1.AIGatewayDataPlaneSpec{
			Deployment: &aigatewayv1alpha1.DeploymentOptions{
				Annotations: map[string]string{
					"deployment-annotation":          "value",
					consts.OperatorLabelPrefix + "x": "should-be-dropped",
				},
				Labels: map[string]string{
					"deployment-label":       "value",
					"app.kubernetes.io/name": "should-be-dropped",
				},
			},
		},
	}
	aigwcp := testKonnectAIGateway()

	d, err := GenerateBaseDeployment(logr.Discard(), aigwdp, aigwcp, "kong/aigw:test", "cert-secret", testConfig)
	require.NoError(t, err)

	assert.Equal(t, "value", d.Labels["deployment-label"])
	assert.Equal(t, "value", d.Annotations["deployment-annotation"])
	assert.NotContains(t, d.Annotations, consts.OperatorLabelPrefix+"x")

	// The base "app.kubernetes.io/name" label set by the generator must survive the merge.
	assert.Equal(t, consts.AIGatewayDataPlaneContainerName, d.Labels["app.kubernetes.io/name"])

	// The Pod template shares the base labels map with the Deployment; the
	// custom Deployment-level label must not leak into it.
	assert.NotContains(t, d.Spec.Template.Labels, "deployment-label")
}

// -----------------------------------------------------------------
// buildDeployment
// -----------------------------------------------------------------

func Test_BuildDeployment(t *testing.T) {
	tc := managedfields.NewDeducedTypeConverter()

	validCP := testKonnectAIGateway()
	invalidCP := &konnectv1alpha1.KonnectAIGateway{}

	tests := []struct {
		name           string
		aigwdp         *aigatewayv1alpha1.AIGatewayDataPlane
		aigwcp         *konnectv1alpha1.KonnectAIGateway
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
									{Name: consts.AIGatewayDataPlaneContainerName, Image: "custom/aigw:overlay"},
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
			name:           "KonnectAIGateway with no endpoints returns error",
			aigwdp:         &aigatewayv1alpha1.AIGatewayDataPlane{},
			aigwcp:         invalidCP,
			image:          "kong/aigw:test",
			certSecretName: "secret",
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := BuildDeployment(logr.Discard(), tc, tt.aigwdp, tt.aigwcp, tt.image, tt.certSecretName, testConfig)
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

	validCP := testKonnectAIGateway()

	aigwdp := &aigatewayv1alpha1.AIGatewayDataPlane{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: dpName},
	}

	tests := []struct {
		name string
		// buildClient is called with a fresh fake client; return the client to use.
		buildClient func(base client.WithWatch) client.Client
		// prepareRecorder optionally drains events before the assertion (e.g. after a pre-run).
		prepareRecorder func(r *testReconciler, rec *events.FakeRecorder)
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
			prepareRecorder: func(r *testReconciler, rec *events.FakeRecorder) {
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
			r := &testReconciler{
				Config:        testConfig,
				Client:        tc2.buildClient(base),
				TypeConverter: tc,
				EventRecorder: recorder,
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
