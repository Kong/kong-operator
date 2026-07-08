package mcpserver

import (
	"context"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/managedfields"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
)

const (
	testMCPServerNamespace = "test-ns"
	testMCPServerName      = "my-mcp-server"
)

func minimalMCPServer() *konnectv1alpha1.MCPServer {
	return &konnectv1alpha1.MCPServer{
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

func remoteMCPServerWithContainers() *sdkkonnectcomp.MCPServerCPInfo {
	return &sdkkonnectcomp.MCPServerCPInfo{
		ID:            "remote-id",
		Version:       "v1",
		InitContainer: &sdkkonnectcomp.ContainerSpec{Image: new("init-image:latest")},
		Container:     &sdkkonnectcomp.ContainerSpec{Image: new("mcp-image:latest")},
	}
}

func Test_ensureDeployment(t *testing.T) {
	scheme := managerscheme.Get()
	tc := managedfields.NewDeducedTypeConverter()
	mcpServer := minimalMCPServer()
	apiAuth := minimalAPIAuth()

	tests := []struct {
		name            string
		remoteMCPServer *sdkkonnectcomp.MCPServerCPInfo
		buildClient     func(base client.WithWatch) client.Client
		prepareRecorder func(t *testing.T, r *MCPServerReconciler, rec *events.FakeRecorder)
		wantErr         bool
		wantEvent       string
	}{
		{
			name:            "missing init container info returns error",
			remoteMCPServer: &sdkkonnectcomp.MCPServerCPInfo{Container: &sdkkonnectcomp.ContainerSpec{}},
			buildClient:     func(base client.WithWatch) client.Client { return base },
			wantErr:         true,
		},
		{
			name:            "missing container info returns error",
			remoteMCPServer: &sdkkonnectcomp.MCPServerCPInfo{InitContainer: &sdkkonnectcomp.ContainerSpec{}},
			buildClient:     func(base client.WithWatch) client.Client { return base },
			wantErr:         true,
		},
		{
			name:            "first call creates deployment and records DeploymentCreated event",
			remoteMCPServer: remoteMCPServerWithContainers(),
			buildClient:     func(base client.WithWatch) client.Client { return base },
			wantEvent:       "DeploymentCreated",
		},
		{
			name:            "second call after content change records DeploymentUpdated event",
			remoteMCPServer: remoteMCPServerWithContainers(),
			buildClient:     func(base client.WithWatch) client.Client { return base },
			prepareRecorder: func(t *testing.T, r *MCPServerReconciler, rec *events.FakeRecorder) {
				_, _ = r.ensureDeployment(t.Context(), logr.Discard(), mcpServer, remoteMCPServerWithContainers(), apiAuth)
				<-rec.Events
			},
			wantEvent: "DeploymentUpdated",
		},
		{
			name:            "apply error is propagated and DeploymentFailed event is recorded",
			remoteMCPServer: remoteMCPServerWithContainers(),
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
			r := &MCPServerReconciler{
				Client:        testcase.buildClient(base),
				TypeConverter: tc,
				eventRecorder: recorder,
			}

			if testcase.prepareRecorder != nil {
				testcase.prepareRecorder(t, r, recorder)
			}

			deploy, err := r.ensureDeployment(t.Context(), logr.Discard(), mcpServer, testcase.remoteMCPServer, apiAuth)

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

func Test_ensureService(t *testing.T) {
	scheme := managerscheme.Get()
	tc := managedfields.NewDeducedTypeConverter()
	mcpServer := minimalMCPServer()

	tests := []struct {
		name            string
		buildClient     func(base client.WithWatch) client.Client
		prepareRecorder func(t *testing.T, r *MCPServerReconciler, rec *events.FakeRecorder)
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
			prepareRecorder: func(t *testing.T, r *MCPServerReconciler, rec *events.FakeRecorder) {
				_ = r.ensureService(t.Context(), logr.Discard(), mcpServer)
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
			r := &MCPServerReconciler{
				Client:        testcase.buildClient(base),
				TypeConverter: tc,
				eventRecorder: recorder,
			}

			if testcase.prepareRecorder != nil {
				testcase.prepareRecorder(t, r, recorder)
			}

			err := r.ensureService(t.Context(), logr.Discard(), mcpServer)

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
	mcpServer := minimalMCPServer()
	apiAuth := minimalAPIAuth()
	remote := remoteMCPServerWithContainers()

	deploy := generateDeployment(mcpServer, *remote, apiAuth)

	nn := generateWorkloadNN(mcpServer)
	assert.Equal(t, nn.Name, deploy.Name)
	assert.Equal(t, nn.Namespace, deploy.Namespace)
	require.Len(t, deploy.Spec.Template.Spec.InitContainers, 1)
	assert.Equal(t, "init-image:latest", deploy.Spec.Template.Spec.InitContainers[0].Image)
	require.Len(t, deploy.Spec.Template.Spec.Containers, 1)
	assert.Equal(t, "mcp-image:latest", deploy.Spec.Template.Spec.Containers[0].Image)
	require.Len(t, deploy.OwnerReferences, 1)
	assert.Equal(t, mcpServer.Name, deploy.OwnerReferences[0].Name)
}

func Test_generateService(t *testing.T) {
	mcpServer := minimalMCPServer()
	svc := generateService(mcpServer)

	nn := generateWorkloadNN(mcpServer)
	assert.Equal(t, nn.Name, svc.Name)
	assert.Equal(t, nn.Namespace, svc.Namespace)
	require.Len(t, svc.Spec.Ports, 1)
	require.Len(t, svc.OwnerReferences, 1)
	assert.Equal(t, mcpServer.Name, svc.OwnerReferences[0].Name)
}

func Test_patEnvVarFromAuth(t *testing.T) {
	t.Run("token type inlines the token value", func(t *testing.T) {
		apiAuth := minimalAPIAuth()
		env := patEnvVarFromAuth(apiAuth)
		assert.Equal(t, "PAT", env.Name)
		assert.Equal(t, "test-token", env.Value)
		assert.Nil(t, env.ValueFrom)
	})

	t.Run("secretRef type sources the value from a Secret", func(t *testing.T) {
		apiAuth := &konnectv1alpha1.KonnectAPIAuthConfiguration{
			Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
				Type:      konnectv1alpha1.KonnectAPIAuthTypeSecretRef,
				SecretRef: &corev1.SecretReference{Name: "konnect-token"},
			},
		}
		env := patEnvVarFromAuth(apiAuth)
		assert.Equal(t, "PAT", env.Name)
		require.NotNil(t, env.ValueFrom)
		require.NotNil(t, env.ValueFrom.SecretKeyRef)
		assert.Equal(t, "konnect-token", env.ValueFrom.SecretKeyRef.Name)
		assert.Equal(t, "token", env.ValueFrom.SecretKeyRef.Key)
	})
}
