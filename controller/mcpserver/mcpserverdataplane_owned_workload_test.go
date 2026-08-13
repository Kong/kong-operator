package mcpserver

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/managedfields"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	mcpv1alpha1 "github.com/kong/kong-operator/v2/api/mcp/v1alpha1"
	konnectcontroller "github.com/kong/kong-operator/v2/controller/konnect"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

const (
	testMCPServerNamespace = "test-ns"
	testMCPServerName      = "my-mcp-server"
)

func minimalMCPServerDataPlane() *mcpv1alpha1.MCPServerDataPlane {
	return &mcpv1alpha1.MCPServerDataPlane{
		ObjectMeta: metav1.ObjectMeta{Namespace: testMCPServerNamespace, Name: testMCPServerName},
	}
}

func minimalAPIAuth() *konnectv1alpha1.KonnectAPIAuthConfiguration {
	return &konnectv1alpha1.KonnectAPIAuthConfiguration{
		ObjectMeta: metav1.ObjectMeta{Namespace: testMCPServerNamespace, Name: "api-auth"},
		Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
			Type:      konnectv1alpha1.KonnectAPIAuthTypeToken,
			Token:     "test-token",
			ServerURL: "https://us.api.konghq.com",
		},
	}
}

func mcpServerMetadataWithContainers() mcpServerMetadata {
	return mcpServerMetadata{
		ContainerImage:     "mcp-image:latest",
		InitContainerImage: "init-image:latest",
		Version:            "v1",
		ControlPlaneID:     "cp-id",
		MCPServerID:        "mcp-server-id",
	}
}

// tokenSecret returns a bare Secret with the name/namespace ensureTokenSecret
// would generate. Callers that only need patEnvVarFromAuth/generateDeployment
// to resolve a Secret name don't need its content, so this doesn't replicate
// ensureTokenSecret's body (see Test_ensureTokenSecret for that).
func tokenSecret(mcpDataPlane *mcpv1alpha1.MCPServerDataPlane) *corev1.Secret {
	nn := generateWorkloadNN(mcpDataPlane)
	return &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: nn.Name, Namespace: nn.Namespace}}
}

func Test_ensureDeployment(t *testing.T) {
	scheme := managerscheme.Get()
	tc := managedfields.NewDeducedTypeConverter()
	mcpDataPlane := minimalMCPServerDataPlane()
	apiAuth := minimalAPIAuth()

	tests := []struct {
		name            string
		metadata        mcpServerMetadata
		buildClient     func(base client.WithWatch) client.Client
		prepareRecorder func(t *testing.T, r *MCPServerDataPlaneReconciler, rec *events.FakeRecorder)
		wantErr         bool
		wantEvent       string
	}{
		{
			name:        "missing init container info returns error",
			metadata:    mcpServerMetadata{ContainerImage: "mcp-image:latest"},
			buildClient: func(base client.WithWatch) client.Client { return base },
			wantErr:     true,
		},
		{
			name:        "missing container info returns error",
			metadata:    mcpServerMetadata{InitContainerImage: "init-image:latest"},
			buildClient: func(base client.WithWatch) client.Client { return base },
			wantErr:     true,
		},
		{
			name:        "first call creates deployment and records DeploymentCreated event",
			metadata:    mcpServerMetadataWithContainers(),
			buildClient: func(base client.WithWatch) client.Client { return base },
			wantEvent:   "DeploymentCreated",
		},
		{
			name:        "second call after content change records DeploymentUpdated event",
			metadata:    mcpServerMetadataWithContainers(),
			buildClient: func(base client.WithWatch) client.Client { return base },
			prepareRecorder: func(t *testing.T, r *MCPServerDataPlaneReconciler, rec *events.FakeRecorder) {
				ts := tokenSecret(mcpDataPlane)
				_, _ = r.ensureDeployment(t.Context(), logr.Discard(), mcpDataPlane, mcpServerMetadataWithContainers(), apiAuth.Spec.ServerURL, ts)
				<-rec.Events
			},
			wantEvent: "DeploymentUpdated",
		},
		{
			name:     "apply error is propagated and DeploymentFailed event is recorded",
			metadata: mcpServerMetadataWithContainers(),
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

	for _, testcase := range tests {
		t.Run(testcase.name, func(t *testing.T) {
			recorder := events.NewFakeRecorder(10)
			base := fake.NewClientBuilder().WithScheme(scheme).Build()
			r := &MCPServerDataPlaneReconciler{
				Client:        testcase.buildClient(base),
				TypeConverter: tc,
				eventRecorder: recorder,
			}

			if testcase.prepareRecorder != nil {
				testcase.prepareRecorder(t, r, recorder)
			}

			tokenSecret := tokenSecret(mcpDataPlane)
			deploy, err := r.ensureDeployment(t.Context(), logr.Discard(), mcpDataPlane, testcase.metadata, apiAuth.Spec.ServerURL, tokenSecret)

			if testcase.wantErr {
				require.Error(t, err)
				assert.Nil(t, deploy)
			} else {
				require.NoError(t, err)
				require.NotNil(t, deploy)
			}

			if testcase.wantEvent != "" {
				select {
				case event := <-recorder.Events:
					assert.Contains(t, event, testcase.wantEvent)
				default:
					t.Errorf("expected event containing %q but channel was empty", testcase.wantEvent)
				}
			} else {
				assert.Empty(t, recorder.Events, "expected no events but got %d", len(recorder.Events))
			}
		})
	}
}

func Test_ensureTokenSecret(t *testing.T) {
	scheme := managerscheme.Get()
	tc := managedfields.NewDeducedTypeConverter()
	mcpDataPlane := minimalMCPServerDataPlane()

	tests := []struct {
		name                string
		secretLabelSelector string
		apiAuth             *konnectv1alpha1.KonnectAPIAuthConfiguration
		buildClient         func(base client.WithWatch) client.Client
		seed                []client.Object
		wantErr             bool
		wantEvent           string
		check               func(t *testing.T, secret *corev1.Secret, c client.Client)
	}{
		{
			name:    "token type creates a Secret with Data (not StringData) and no empty label",
			apiAuth: minimalAPIAuth(),
			check: func(t *testing.T, secret *corev1.Secret, c client.Client) {
				assert.Equal(t, []byte("test-token"), secret.Data[konnectcontroller.SecretTokenKey])
				assert.Nil(t, secret.StringData)
				assert.NotContains(t, secret.Labels, "")
				assert.Equal(t, consts.MCPServerManagedByLabelValue, secret.Labels[consts.GatewayOperatorManagedByLabel])
				require.Len(t, secret.OwnerReferences, 1)
				assert.Equal(t, mcpDataPlane.Name, secret.OwnerReferences[0].Name)
			},
			wantEvent: "TokenSecretCreated",
		},
		{
			name:                "token type with a SecretLabelSelector sets the label",
			secretLabelSelector: "konghq.com/some-selector",
			apiAuth:             minimalAPIAuth(),
			check: func(t *testing.T, secret *corev1.Secret, c client.Client) {
				assert.Equal(t, "true", secret.Labels["konghq.com/some-selector"])
			},
			wantEvent: "TokenSecretCreated",
		},
		{
			name: "secretRef type in the same namespace returns the referenced name and deletes a stale generated Secret",
			apiAuth: &konnectv1alpha1.KonnectAPIAuthConfiguration{
				ObjectMeta: metav1.ObjectMeta{Namespace: testMCPServerNamespace, Name: "api-auth"},
				Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
					Type:      konnectv1alpha1.KonnectAPIAuthTypeSecretRef,
					SecretRef: &corev1.SecretReference{Name: "user-provided-secret"},
				},
			},
			seed: []client.Object{tokenSecret(mcpDataPlane)},
			check: func(t *testing.T, secret *corev1.Secret, c client.Client) {
				assert.Equal(t, "user-provided-secret", secret.Name)
				assert.Equal(t, testMCPServerNamespace, secret.Namespace)

				stale := tokenSecret(mcpDataPlane)
				err := c.Get(t.Context(), client.ObjectKeyFromObject(stale), stale)
				assert.True(t, apierrors.IsNotFound(err), "stale generated Token Secret should have been deleted, got err: %v", err)
			},
		},
		{
			name: "secretRef type in a different namespace is rejected",
			apiAuth: &konnectv1alpha1.KonnectAPIAuthConfiguration{
				ObjectMeta: metav1.ObjectMeta{Namespace: testMCPServerNamespace, Name: "api-auth"},
				Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
					Type:      konnectv1alpha1.KonnectAPIAuthTypeSecretRef,
					SecretRef: &corev1.SecretReference{Name: "other-ns-secret", Namespace: "other-ns"},
				},
			},
			wantErr: true,
		},
		{
			name: "unsupported auth type returns an error",
			apiAuth: &konnectv1alpha1.KonnectAPIAuthConfiguration{
				ObjectMeta: metav1.ObjectMeta{Namespace: testMCPServerNamespace, Name: "api-auth"},
				Spec:       konnectv1alpha1.KonnectAPIAuthConfigurationSpec{Type: "bogus"},
			},
			wantErr: true,
		},
		{
			name:    "apply error is propagated and TokenSecretFailed event is recorded",
			apiAuth: minimalAPIAuth(),
			buildClient: func(base client.WithWatch) client.Client {
				return interceptor.NewClient(base, interceptor.Funcs{
					Apply: func(ctx context.Context, c client.WithWatch, obj runtime.ApplyConfiguration, opts ...client.ApplyOption) error {
						return assert.AnError
					},
				})
			},
			wantErr:   true,
			wantEvent: "TokenSecretFailed",
		},
	}

	for _, testcase := range tests {
		t.Run(testcase.name, func(t *testing.T) {
			recorder := events.NewFakeRecorder(10)
			builder := fake.NewClientBuilder().WithScheme(scheme)
			if len(testcase.seed) > 0 {
				builder = builder.WithObjects(testcase.seed...)
			}
			base := builder.Build()
			buildClient := testcase.buildClient
			if buildClient == nil {
				buildClient = func(base client.WithWatch) client.Client { return base }
			}
			r := &MCPServerDataPlaneReconciler{
				Client:              buildClient(base),
				TypeConverter:       tc,
				eventRecorder:       recorder,
				SecretLabelSelector: testcase.secretLabelSelector,
			}

			secret, err := r.ensureTokenSecret(t.Context(), logr.Discard(), mcpDataPlane, testcase.apiAuth)

			if testcase.wantErr {
				require.Error(t, err)
				assert.Nil(t, secret)
			} else {
				require.NoError(t, err)
				require.NotNil(t, secret)
			}

			if testcase.check != nil {
				testcase.check(t, secret, r.Client)
			}

			if testcase.wantEvent != "" {
				select {
				case event := <-recorder.Events:
					assert.Contains(t, event, testcase.wantEvent)
				default:
					t.Errorf("expected event containing %q but channel was empty", testcase.wantEvent)
				}
			} else {
				assert.Empty(t, recorder.Events, "expected no events but got %d", len(recorder.Events))
			}
		})
	}
}

func Test_ensureService(t *testing.T) {
	scheme := managerscheme.Get()
	tc := managedfields.NewDeducedTypeConverter()
	mcpDataPlane := minimalMCPServerDataPlane()

	tests := []struct {
		name            string
		buildClient     func(base client.WithWatch) client.Client
		prepareRecorder func(t *testing.T, r *MCPServerDataPlaneReconciler, rec *events.FakeRecorder)
		wantErr         bool
		wantEvent       string
	}{
		{
			name:        "first call creates service and records ServiceCreated event",
			buildClient: func(base client.WithWatch) client.Client { return base },
			wantEvent:   "ServiceCreated",
		},
		{
			name:        "second call after content change records ServiceUpdated event",
			buildClient: func(base client.WithWatch) client.Client { return base },
			prepareRecorder: func(t *testing.T, r *MCPServerDataPlaneReconciler, rec *events.FakeRecorder) {
				_ = r.ensureService(t.Context(), logr.Discard(), mcpDataPlane)
				<-rec.Events
			},
			wantEvent: "ServiceUpdated",
		},
		{
			name: "apply error is propagated and ServiceFailed event is recorded",
			buildClient: func(base client.WithWatch) client.Client {
				return interceptor.NewClient(base, interceptor.Funcs{
					Apply: func(ctx context.Context, c client.WithWatch, obj runtime.ApplyConfiguration, opts ...client.ApplyOption) error {
						return assert.AnError
					},
				})
			},
			wantErr:   true,
			wantEvent: "ServiceFailed",
		},
	}

	for _, testcase := range tests {
		t.Run(testcase.name, func(t *testing.T) {
			recorder := events.NewFakeRecorder(10)
			base := fake.NewClientBuilder().WithScheme(scheme).Build()
			r := &MCPServerDataPlaneReconciler{
				Client:        testcase.buildClient(base),
				TypeConverter: tc,
				eventRecorder: recorder,
			}

			if testcase.prepareRecorder != nil {
				testcase.prepareRecorder(t, r, recorder)
			}

			err := r.ensureService(t.Context(), logr.Discard(), mcpDataPlane)

			if testcase.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			if testcase.wantEvent != "" {
				select {
				case event := <-recorder.Events:
					assert.Contains(t, event, testcase.wantEvent)
				default:
					t.Errorf("expected event containing %q but channel was empty", testcase.wantEvent)
				}
			} else {
				assert.Empty(t, recorder.Events, "expected no events but got %d", len(recorder.Events))
			}
		})
	}
}

func Test_generateDeployment(t *testing.T) {
	mcpDataPlane := minimalMCPServerDataPlane()
	apiAuth := minimalAPIAuth()
	metadata := mcpServerMetadataWithContainers()

	tokenSecret := tokenSecret(mcpDataPlane)
	deploy := generateDeployment(logr.Discard(), mcpDataPlane, metadata, tokenSecret, apiAuth.Spec.ServerURL)

	nn := generateWorkloadNN(mcpDataPlane)
	assert.Equal(t, nn.Name, deploy.Name)
	assert.Equal(t, nn.Namespace, deploy.Namespace)
	require.Len(t, deploy.Spec.Template.Spec.InitContainers, 1)
	assert.Equal(t, "init-image:latest", deploy.Spec.Template.Spec.InitContainers[0].Image)
	assert.Contains(t, deploy.Spec.Template.Spec.InitContainers[0].Args, "-cp-id")
	assert.Contains(t, deploy.Spec.Template.Spec.InitContainers[0].Args, metadata.ControlPlaneID)
	assert.Contains(t, deploy.Spec.Template.Spec.InitContainers[0].Args, "-mcp-server-id")
	assert.Contains(t, deploy.Spec.Template.Spec.InitContainers[0].Args, metadata.MCPServerID)
	require.Len(t, deploy.Spec.Template.Spec.Containers, 1)
	assert.Equal(t, "mcp-image:latest", deploy.Spec.Template.Spec.Containers[0].Image)
	require.Len(t, deploy.OwnerReferences, 1)
	assert.Equal(t, mcpDataPlane.Name, deploy.OwnerReferences[0].Name)
}

func Test_generateDeployment_LabelsAndAnnotations(t *testing.T) {
	mcpDataPlane := minimalMCPServerDataPlane()
	mcpDataPlane.Spec.Deployment = &mcpv1alpha1.DeploymentOptions{
		Labels: map[string]string{
			"team":                               "platform",
			consts.GatewayOperatorManagedByLabel: "should-not-override-base-label",
		},
		Annotations: map[string]string{
			"team-contact":                "platform@konghq.com",
			mcpServerVersionAnnotationKey: "should-not-override-version-annotation",
		},
	}
	apiAuth := minimalAPIAuth()
	metadata := mcpServerMetadataWithContainers()

	tokenSecret := tokenSecret(mcpDataPlane)
	deploy := generateDeployment(logr.Discard(), mcpDataPlane, metadata, tokenSecret, apiAuth.Spec.ServerURL)

	assert.Equal(t, "platform", deploy.Labels["team"])
	assert.Equal(t, consts.MCPServerManagedByLabelValue, deploy.Labels[consts.GatewayOperatorManagedByLabel],
		"reserved label key must remain operator-managed")
	assert.Equal(t, "platform@konghq.com", deploy.Annotations["team-contact"])
	assert.Equal(t, metadata.Version, deploy.Annotations[mcpServerVersionAnnotationKey],
		"reserved annotation key must remain operator-managed")

	// The Pod template must be unaffected by Deployment-level labels/annotations.
	assert.NotContains(t, deploy.Spec.Template.Labels, "team")
	assert.NotContains(t, deploy.Spec.Template.Annotations, "team-contact")
}

// infoCountSink is a minimal logr.LogSink that counts Info() calls.
type infoCountSink struct{ count *int }

func (s infoCountSink) Init(logr.RuntimeInfo)             {}
func (s infoCountSink) Enabled(int) bool                  { return true }
func (s infoCountSink) Info(_ int, _ string, _ ...any)    { *s.count++ }
func (s infoCountSink) Error(_ error, _ string, _ ...any) {}
func (s infoCountSink) WithValues(_ ...any) logr.LogSink  { return s }
func (s infoCountSink) WithName(_ string) logr.LogSink    { return s }

func Test_addAnnotationsForMCPServerDataPlaneDeployment(t *testing.T) {
	testCases := []struct {
		name                string
		existingAnnotations map[string]string
		specAnnotations     map[string]string
		expectedAnnotations map[string]string
		expectedInfoCount   int
	}{
		{
			name:                "no-op when MCPServerDataPlane has no deployment annotations",
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
				mcpServerVersionAnnotationKey:    "should-not-be-settable-by-user",
			},
			expectedAnnotations: map[string]string{"safe": "val"},
			expectedInfoCount:   2,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mcpDataPlane := &mcpv1alpha1.MCPServerDataPlane{
				Spec: mcpv1alpha1.MCPServerDataPlaneSpec{
					Deployment: &mcpv1alpha1.DeploymentOptions{Annotations: tc.specAnnotations},
				},
			}
			deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Annotations: tc.existingAnnotations}}
			var infoCount int
			addAnnotationsForMCPServerDataPlaneDeployment(logr.New(infoCountSink{count: &infoCount}), deployment, mcpDataPlane)
			require.Equal(t, tc.expectedAnnotations, deployment.Annotations)
			assert.Equal(t, tc.expectedInfoCount, infoCount)
		})
	}
}

func Test_addLabelsForMCPServerDataPlaneDeployment(t *testing.T) {
	testCases := []struct {
		name              string
		existingLabels    map[string]string
		specLabels        map[string]string
		expectedLabels    map[string]string
		expectedInfoCount int
	}{
		{
			name:           "no-op when MCPServerDataPlane has no deployment labels",
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
			specLabels:        map[string]string{"safe": "val", "app": "should-not-override-selector-label"},
			expectedLabels:    map[string]string{"safe": "val"},
			expectedInfoCount: 1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mcpDataPlane := &mcpv1alpha1.MCPServerDataPlane{
				Spec: mcpv1alpha1.MCPServerDataPlaneSpec{
					Deployment: &mcpv1alpha1.DeploymentOptions{Labels: tc.specLabels},
				},
			}
			deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Labels: tc.existingLabels}}
			var infoCount int
			addLabelsForMCPServerDataPlaneDeployment(logr.New(infoCountSink{count: &infoCount}), deployment, mcpDataPlane)
			require.Equal(t, tc.expectedLabels, deployment.Labels)
			assert.Equal(t, tc.expectedInfoCount, infoCount)
		})
	}
}

func Test_generateDeployment_Replicas(t *testing.T) {
	apiAuth := minimalAPIAuth()
	metadata := mcpServerMetadataWithContainers()

	tests := []struct {
		name       string
		deployment *mcpv1alpha1.DeploymentOptions
		want       int32
	}{
		{
			name:       "no deployment options defaults to 1 replica",
			deployment: nil,
			want:       1,
		},
		{
			name: "explicit replicas",
			deployment: &mcpv1alpha1.DeploymentOptions{
				Replicas: new(int32(3)),
			},
			want: 3,
		},
		{
			name:       "nil replicas defaults to 1",
			deployment: &mcpv1alpha1.DeploymentOptions{},
			want:       1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mcpDataPlane := minimalMCPServerDataPlane()
			mcpDataPlane.Spec.Deployment = tc.deployment

			tokenSecret := tokenSecret(mcpDataPlane)
			deploy := generateDeployment(logr.Discard(), mcpDataPlane, metadata, tokenSecret, apiAuth.Spec.ServerURL)

			require.NotNil(t, deploy.Spec.Replicas)
			assert.Equal(t, tc.want, *deploy.Spec.Replicas)
		})
	}
}

func Test_generateService(t *testing.T) {
	mcpDataPlane := minimalMCPServerDataPlane()
	svc := generateService(mcpDataPlane)

	nn := generateWorkloadNN(mcpDataPlane)
	assert.Equal(t, nn.Name, svc.Name)
	assert.Equal(t, nn.Namespace, svc.Namespace)
	require.Len(t, svc.Spec.Ports, 1)
	require.Len(t, svc.OwnerReferences, 1)
	assert.Equal(t, mcpDataPlane.Name, svc.OwnerReferences[0].Name)
}

func Test_patEnvVarFromAuth(t *testing.T) {
	secret := tokenSecret(minimalMCPServerDataPlane())
	env := patEnvVarFromAuth(secret)
	assert.Equal(t, "PAT", env.Name)
	require.NotNil(t, env.ValueFrom)
	require.NotNil(t, env.ValueFrom.SecretKeyRef)
	assert.Equal(t, secret.Name, env.ValueFrom.SecretKeyRef.Name)
	assert.Equal(t, konnectcontroller.SecretTokenKey, env.ValueFrom.SecretKeyRef.Key)
}
