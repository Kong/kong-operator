package konnect

import (
	"context"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	client "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha2"

	extensionserrors "github.com/kong/kong-operator/controller/pkg/extensions/errors"
	managercfg "github.com/kong/kong-operator/ingress-controller/pkg/manager/config"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/modules/manager/scheme"
)

func TestBuildKonnectAddress(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		expected string
	}{
		{
			name:     "standard endpoint",
			endpoint: "https://7b46471d3b.us.tp.konghq.tech:443",
			expected: "https://us.kic.api.konghq.tech",
		},
		{
			name:     "different region",
			endpoint: "https://abcd1234.eu.tp.konghq.tech:443",
			expected: "https://eu.kic.api.konghq.tech",
		},
		{
			name:     "longer hostname",
			endpoint: "https://abcd1234.us.tp.konghq.foo.bar.tech:443",
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

func TestApplyFeatureGatesToControlPlane(t *testing.T) {
	fillIDsFeatureGate := gwtypes.ControlPlaneFeatureGate{
		Name:  managercfg.FillIDsFeature,
		State: gwtypes.FeatureGateStateEnabled,
	}

	otherFeatureGate := gwtypes.ControlPlaneFeatureGate{
		Name:  "OtherFeatureGate",
		State: gwtypes.FeatureGateStateEnabled,
	}

	otherFeatureGate1 := gwtypes.ControlPlaneFeatureGate{
		Name:  "FeatureGate1",
		State: gwtypes.FeatureGateStateEnabled,
	}

	otherFeatureGate2 := gwtypes.ControlPlaneFeatureGate{
		Name:  "FeatureGate2",
		State: gwtypes.FeatureGateStateEnabled,
	}

	disabledFillIDs := gwtypes.ControlPlaneFeatureGate{
		Name:  managercfg.FillIDsFeature,
		State: gwtypes.FeatureGateStateDisabled,
	}

	tests := []struct {
		name                string
		initialFeatureGates []gwtypes.ControlPlaneFeatureGate
		expectedLength      int
		checkAssertions     func(t *testing.T, cp *gwtypes.ControlPlane)
	}{
		{
			name:                "nil feature gates",
			initialFeatureGates: nil,
			expectedLength:      1,
			checkAssertions: func(t *testing.T, cp *gwtypes.ControlPlane) {
				require.Equal(t, fillIDsFeatureGate.Name, cp.Spec.ControlPlaneOptions.FeatureGates[0].Name)
				require.Equal(t, fillIDsFeatureGate.State, cp.Spec.ControlPlaneOptions.FeatureGates[0].State)
			},
		},
		{
			name:                "empty feature gates",
			initialFeatureGates: []gwtypes.ControlPlaneFeatureGate{},
			expectedLength:      1,
			checkAssertions: func(t *testing.T, cp *gwtypes.ControlPlane) {
				require.Equal(t, fillIDsFeatureGate.Name, cp.Spec.ControlPlaneOptions.FeatureGates[0].Name)
				require.Equal(t, fillIDsFeatureGate.State, cp.Spec.ControlPlaneOptions.FeatureGates[0].State)
			},
		},
		{
			name: "feature gate already present and enabled",
			initialFeatureGates: []gwtypes.ControlPlaneFeatureGate{
				{
					Name:  managercfg.FillIDsFeature,
					State: gwtypes.FeatureGateStateEnabled,
				},
			},
			expectedLength: 1,
			checkAssertions: func(t *testing.T, cp *gwtypes.ControlPlane) {
				require.Equal(t, fillIDsFeatureGate.Name, cp.Spec.ControlPlaneOptions.FeatureGates[0].Name)
				require.Equal(t, fillIDsFeatureGate.State, cp.Spec.ControlPlaneOptions.FeatureGates[0].State)
			},
		},
		{
			name: "feature gate already present but disabled",
			initialFeatureGates: []gwtypes.ControlPlaneFeatureGate{
				{
					Name:  managercfg.FillIDsFeature,
					State: gwtypes.FeatureGateStateDisabled,
				},
			},
			expectedLength: 1,
			checkAssertions: func(t *testing.T, cp *gwtypes.ControlPlane) {
				require.Equal(t, fillIDsFeatureGate.Name, cp.Spec.ControlPlaneOptions.FeatureGates[0].Name)
				require.Equal(t, fillIDsFeatureGate.State, cp.Spec.ControlPlaneOptions.FeatureGates[0].State)
			},
		},
		{
			name:                "feature gate not present with other gates",
			initialFeatureGates: []gwtypes.ControlPlaneFeatureGate{otherFeatureGate},
			expectedLength:      2,
			checkAssertions: func(t *testing.T, cp *gwtypes.ControlPlane) {
				require.Equal(t, otherFeatureGate.Name, cp.Spec.ControlPlaneOptions.FeatureGates[0].Name)
				require.Equal(t, otherFeatureGate.State, cp.Spec.ControlPlaneOptions.FeatureGates[0].State)
				require.Equal(t, fillIDsFeatureGate.Name, cp.Spec.ControlPlaneOptions.FeatureGates[1].Name)
				require.Equal(t, fillIDsFeatureGate.State, cp.Spec.ControlPlaneOptions.FeatureGates[1].State)
			},
		},
		{
			name: "feature gate in middle of list",
			initialFeatureGates: []gwtypes.ControlPlaneFeatureGate{
				otherFeatureGate1,
				disabledFillIDs,
				otherFeatureGate2,
			},
			expectedLength: 3,
			checkAssertions: func(t *testing.T, cp *gwtypes.ControlPlane) {
				require.Equal(t, otherFeatureGate1.Name, cp.Spec.ControlPlaneOptions.FeatureGates[0].Name)
				require.Equal(t, otherFeatureGate1.State, cp.Spec.ControlPlaneOptions.FeatureGates[0].State)
				require.Equal(t, fillIDsFeatureGate.Name, cp.Spec.ControlPlaneOptions.FeatureGates[1].Name)
				require.Equal(t, fillIDsFeatureGate.State, cp.Spec.ControlPlaneOptions.FeatureGates[1].State)
				require.Equal(t, otherFeatureGate2.Name, cp.Spec.ControlPlaneOptions.FeatureGates[2].Name)
				require.Equal(t, otherFeatureGate2.State, cp.Spec.ControlPlaneOptions.FeatureGates[2].State)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cp := &gwtypes.ControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cp",
				},
				Spec: gwtypes.ControlPlaneSpec{
					ControlPlaneOptions: gwtypes.ControlPlaneOptions{
						FeatureGates: tt.initialFeatureGates,
					},
				},
			}

			if tt.initialFeatureGates == nil {
				require.Nil(t, cp.Spec.FeatureGates)
			} else if len(tt.initialFeatureGates) == 0 {
				require.Empty(t, cp.Spec.FeatureGates)
			}

			applyFeatureGatesToControlPlane(cp)

			require.NotNil(t, cp.Spec.FeatureGates)
			require.Len(t, cp.Spec.FeatureGates, tt.expectedLength)

			tt.checkAssertions(t, cp)
		})
	}
}

func TestGetTLSClientCertAndKey(t *testing.T) {
	tests := []struct {
		name           string
		secretData     map[string][]byte
		secretExists   bool
		expectedCert   string
		expectedKey    string
		expectedErrMsg string
	}{
		{
			name: "success - valid secret with cert and key",
			secretData: map[string][]byte{
				"tls.crt": []byte("test-certificate"),
				"tls.key": []byte("test-private-key"),
			},
			secretExists:   true,
			expectedCert:   "test-certificate",
			expectedKey:    "test-private-key",
			expectedErrMsg: "",
		},
		{
			name:           "error - secret not found",
			secretExists:   false,
			expectedCert:   "",
			expectedKey:    "",
			expectedErrMsg: "failed to get TLS client secret test-namespace/test-secret",
		},
		{
			name: "error - missing tls.crt",
			secretData: map[string][]byte{
				"tls.key": []byte("test-private-key"),
			},
			secretExists:   true,
			expectedCert:   "",
			expectedKey:    "",
			expectedErrMsg: "TLS certificate not found in secret test-namespace/test-secret",
		},
		{
			name: "error - missing tls.key",
			secretData: map[string][]byte{
				"tls.crt": []byte("test-certificate"),
			},
			secretExists:   true,
			expectedCert:   "",
			expectedKey:    "",
			expectedErrMsg: "TLS key not found in secret test-namespace/test-secret",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := fake.NewClientBuilder()

			if tt.secretExists {
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-secret",
						Namespace: "test-namespace",
					},
					Data: tt.secretData,
				}
				builder = builder.WithObjects(secret)
			}

			cl := builder.Build()

			cert, key, err := getTLSClientCertAndKey(context.Background(), cl, "test-secret", "test-namespace")

			if tt.expectedErrMsg != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedErrMsg)
				assert.Empty(t, cert)
				assert.Empty(t, key)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedCert, cert)
				assert.Equal(t, tt.expectedKey, key)
			}
		})
	}
}

func TestGetKonnectConfig(t *testing.T) {
	tests := []struct {
		name                   string
		konnectExtensionConfig *KonnectExtensionConfig
		expectedConfig         *managercfg.KonnectConfig
	}{
		{
			name:                   "nil KonnectExtensionConfig",
			konnectExtensionConfig: nil,
			expectedConfig:         nil,
		},
		{
			name: "valid KonnectExtensionConfig",
			konnectExtensionConfig: &KonnectExtensionConfig{
				KonnectConfig: &managercfg.KonnectConfig{
					Address: "https://example.com",
				},
			},
			expectedConfig: &managercfg.KonnectConfig{
				Address: "https://example.com",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			processor := &ControlPlaneKonnectExtensionProcessor{
				KonnectExtensionConfig: tt.konnectExtensionConfig,
			}
			result := processor.GetKonnectConfig()
			assert.Equal(t, tt.expectedConfig, result)
		})
	}
}

func TestControlPlaneKonnectExtensionProcessor_Process(t *testing.T) {
	// Create a scheme for the fake client.
	s := scheme.Get()

	// Define test namespace and names.
	const (
		testNamespace  = "test-namespace"
		extensionName  = "test-konnect-extension"
		secretName     = "test-tls-secret"
		controlPlaneID = "test-control-plane-id"
	)

	// Helper function to create a valid KonnectExtension.
	createValidKonnectExtension := func() *konnectv1alpha2.KonnectExtension {
		return &konnectv1alpha2.KonnectExtension{
			ObjectMeta: metav1.ObjectMeta{
				Name:      extensionName,
				Namespace: testNamespace,
			},
			Status: konnectv1alpha2.KonnectExtensionStatus{
				Konnect: &konnectv1alpha2.KonnectExtensionControlPlaneStatus{
					ControlPlaneID: controlPlaneID,
					Endpoints: konnectv1alpha2.KonnectEndpoints{
						ControlPlaneEndpoint: "7b46471d3b.us.tp.konghq.tech:443",
						TelemetryEndpoint:    "7b46471d3b.us.tp.konghq.tech",
					},
					ClusterType: konnectv1alpha2.ClusterTypeK8sIngressController,
				},
				DataPlaneClientAuth: &konnectv1alpha2.DataPlaneClientAuthStatus{
					CertificateSecretRef: &konnectv1alpha2.SecretRef{
						Name: secretName,
					},
				},
				Conditions: []metav1.Condition{
					{
						Type:   konnectv1alpha2.KonnectExtensionReadyConditionType,
						Status: metav1.ConditionTrue,
					},
				},
			},
		}
	}

	// Helper function to create a valid ControlPlane.
	createValidControlPlane := func() *gwtypes.ControlPlane {
		return &gwtypes.ControlPlane{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-control-plane",
				Namespace: testNamespace,
			},
			Spec: gwtypes.ControlPlaneSpec{
				Extensions: []commonv1alpha1.ExtensionRef{
					{
						Group: konnectv1alpha1.SchemeGroupVersion.Group,
						Kind:  konnectv1alpha2.KonnectExtensionKind,
						NamespacedRef: commonv1alpha1.NamespacedRef{
							Name: extensionName,
						},
					},
				},
				ControlPlaneOptions: gwtypes.ControlPlaneOptions{},
			},
		}
	}

	// Helper function to create a valid TLS secret.
	createValidTLSSecret := func() *corev1.Secret {
		return &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: testNamespace,
			},
			Data: map[string][]byte{
				"tls.crt": []byte("test-certificate"),
				"tls.key": []byte("test-private-key"),
			},
		}
	}

	tests := []struct {
		name            string
		object          client.Object
		setupClient     func(t *testing.T) client.Client
		wantProcessed   bool
		wantErr         bool
		wantErrContains string
		checkAssertions func(t *testing.T, processor *ControlPlaneKonnectExtensionProcessor, cp *gwtypes.ControlPlane)
	}{
		{
			name:   "success - valid KonnectExtension",
			object: createValidControlPlane(),
			setupClient: func(t *testing.T) client.Client {
				return fake.NewClientBuilder().
					WithScheme(s).
					WithObjects(
						createValidKonnectExtension(),
						createValidTLSSecret(),
					).
					Build()
			},
			wantProcessed: true,
			checkAssertions: func(t *testing.T, processor *ControlPlaneKonnectExtensionProcessor, cp *gwtypes.ControlPlane) {
				require.NotNil(t, processor.KonnectExtensionConfig)
				require.NotNil(t, processor.KonnectExtensionConfig.KonnectConfig)

				// Check that KonnectConfig was set correctly.
				kConfig := processor.KonnectExtensionConfig.KonnectConfig
				assert.Equal(t, "https://us.kic.api.konghq.tech", kConfig.Address)
				assert.Equal(t, controlPlaneID, kConfig.ControlPlaneID)
				assert.Equal(t, "test-certificate", kConfig.TLSClient.Cert)
				assert.Equal(t, "test-private-key", kConfig.TLSClient.Key)
				assert.True(t, kConfig.LicenseSynchronizationEnabled)
				assert.True(t, kConfig.ConfigSynchronizationEnabled)

				// Check FillIDs feature gate was applied.
				require.NotEmpty(t, cp.Spec.FeatureGates)
				found := false
				for _, fg := range cp.Spec.FeatureGates {
					if fg.Name == managercfg.FillIDsFeature {
						found = true
						assert.Equal(t, gwtypes.FeatureGateStateEnabled, fg.State)
					}
				}
				assert.True(t, found, "FillIDs feature gate should be present")
			},
		},
		{
			name:   "error - object is not a ControlPlane",
			object: &corev1.Pod{},
			setupClient: func(t *testing.T) client.Client {
				return fake.NewClientBuilder().
					WithScheme(s).
					Build()
			},
			wantProcessed:   false,
			wantErr:         true,
			wantErrContains: "object is not a ControlPlane",
		},
		{
			name:   "error - KonnectExtension not found",
			object: createValidControlPlane(),
			setupClient: func(t *testing.T) client.Client {
				return fake.NewClientBuilder().
					WithScheme(s).
					Build()
			},
			wantProcessed:   false,
			wantErr:         true,
			wantErrContains: extensionserrors.ErrKonnectExtensionNotFound.Error(),
		},
		{
			name:   "error - KonnectExtension not ready",
			object: createValidControlPlane(),
			setupClient: func(t *testing.T) client.Client {
				ext := createValidKonnectExtension()
				ext.Status.Conditions = []metav1.Condition{
					{
						Type:   konnectv1alpha2.KonnectExtensionReadyConditionType,
						Status: metav1.ConditionFalse,
					},
				}
				return fake.NewClientBuilder().
					WithScheme(s).
					WithObjects(ext).
					Build()
			},
			wantProcessed:   false,
			wantErr:         true,
			wantErrContains: extensionserrors.ErrKonnectExtensionNotReady.Error(),
		},
		{
			name: "error - cross-namespace reference",
			object: func() *gwtypes.ControlPlane {
				cp := createValidControlPlane()
				cp.Spec.Extensions[0].Namespace = lo.ToPtr("different-namespace")
				return cp
			}(),
			setupClient: func(t *testing.T) client.Client {
				return fake.NewClientBuilder().
					WithScheme(s).
					WithObjects(createValidKonnectExtension()).
					Build()
			},
			wantProcessed:   false,
			wantErr:         true,
			wantErrContains: extensionserrors.ErrCrossNamespaceReference.Error(),
		},
		{
			name:   "error - TLS secret not found",
			object: createValidControlPlane(),
			setupClient: func(t *testing.T) client.Client {
				return fake.NewClientBuilder().
					WithScheme(s).
					WithObjects(createValidKonnectExtension()).
					Build()
			},
			wantProcessed:   false,
			wantErr:         true,
			wantErrContains: "failed to get TLS client secret",
		},
		{
			name:   "error - TLS certificate missing in secret",
			object: createValidControlPlane(),
			setupClient: func(t *testing.T) client.Client {
				secret := createValidTLSSecret()
				delete(secret.Data, "tls.crt")

				return fake.NewClientBuilder().
					WithScheme(s).
					WithObjects(
						createValidKonnectExtension(),
						secret,
					).
					Build()
			},
			wantProcessed:   false,
			wantErr:         true,
			wantErrContains: "TLS certificate not found in secret",
		},
		{
			name:   "error - TLS key missing in secret",
			object: createValidControlPlane(),
			setupClient: func(t *testing.T) client.Client {
				secret := createValidTLSSecret()
				delete(secret.Data, "tls.key")

				return fake.NewClientBuilder().
					WithScheme(s).
					WithObjects(
						createValidKonnectExtension(),
						secret,
					).
					Build()
			},
			wantProcessed:   false,
			wantErr:         true,
			wantErrContains: "TLS key not found in secret",
		},
		{
			name:   "error - unsupported Konnect cluster type",
			object: createValidControlPlane(),
			setupClient: func(t *testing.T) client.Client {
				ext := createValidKonnectExtension()
				ext.Status.Konnect.ClusterType = konnectv1alpha2.ClusterTypeControlPlane

				return fake.NewClientBuilder().
					WithScheme(s).
					WithObjects(
						ext,
						createValidTLSSecret(),
					).
					Build()
			},
			wantProcessed:   false,
			wantErr:         true,
			wantErrContains: "unsupported Konnect cluster type",
		},
		{
			name: "no KonnectExtension reference",
			object: func() *gwtypes.ControlPlane {
				cp := createValidControlPlane()
				cp.Spec.Extensions = []commonv1alpha1.ExtensionRef{}
				return cp
			}(),
			setupClient: func(t *testing.T) client.Client {
				return fake.NewClientBuilder().
					WithScheme(s).
					Build()
			},
			wantProcessed: false,
			wantErr:       false,
		},
		{
			name: "different extension type",
			object: func() *gwtypes.ControlPlane {
				cp := createValidControlPlane()
				cp.Spec.Extensions[0].Kind = "OtherExtensionKind"
				return cp
			}(),
			setupClient: func(t *testing.T) client.Client {
				return fake.NewClientBuilder().
					WithScheme(s).
					Build()
			},
			wantProcessed: false,
			wantErr:       false,
		},
		{
			name: "feature gate handling - no initial feature gates",
			object: func() *gwtypes.ControlPlane {
				cp := createValidControlPlane()
				cp.Spec.FeatureGates = nil
				return cp
			}(),
			setupClient: func(t *testing.T) client.Client {
				return fake.NewClientBuilder().
					WithScheme(s).
					WithObjects(
						createValidKonnectExtension(),
						createValidTLSSecret(),
					).
					Build()
			},
			wantProcessed: true,
			checkAssertions: func(t *testing.T, processor *ControlPlaneKonnectExtensionProcessor, cp *gwtypes.ControlPlane) {
				require.NotNil(t, cp.Spec.FeatureGates)
				require.Len(t, cp.Spec.FeatureGates, 1)
				assert.Equal(t, managercfg.FillIDsFeature, cp.Spec.FeatureGates[0].Name)
				assert.Equal(t, gwtypes.FeatureGateStateEnabled, cp.Spec.FeatureGates[0].State)
			},
		},
		{
			name: "feature gate handling - existing feature gates",
			object: func() *gwtypes.ControlPlane {
				cp := createValidControlPlane()
				cp.Spec.FeatureGates = []gwtypes.ControlPlaneFeatureGate{
					{
						Name:  "ExistingFeatureGate",
						State: gwtypes.FeatureGateStateEnabled,
					},
				}
				return cp
			}(),
			setupClient: func(t *testing.T) client.Client {
				return fake.NewClientBuilder().
					WithScheme(s).
					WithObjects(
						createValidKonnectExtension(),
						createValidTLSSecret(),
					).
					Build()
			},
			wantProcessed: true,
			checkAssertions: func(t *testing.T, processor *ControlPlaneKonnectExtensionProcessor, cp *gwtypes.ControlPlane) {
				require.NotNil(t, cp.Spec.FeatureGates)
				require.Len(t, cp.Spec.FeatureGates, 2)

				// Find and check FillIDs feature gate.
				found := false
				for _, fg := range cp.Spec.FeatureGates {
					if fg.Name == managercfg.FillIDsFeature {
						found = true
						assert.Equal(t, gwtypes.FeatureGateStateEnabled, fg.State)
					}
				}
				assert.True(t, found, "FillIDs feature gate should be present")

				// Ensure the existing feature gate is preserved.
				found = false
				for _, fg := range cp.Spec.FeatureGates {
					if fg.Name == "ExistingFeatureGate" {
						found = true
						assert.Equal(t, gwtypes.FeatureGateStateEnabled, fg.State)
					}
				}
				assert.True(t, found, "Existing feature gate should be preserved")
			},
		},
		{
			name: "feature gate handling - existing FillIDs feature gate (disabled)",
			object: func() *gwtypes.ControlPlane {
				cp := createValidControlPlane()
				cp.Spec.FeatureGates = []gwtypes.ControlPlaneFeatureGate{
					{
						Name:  managercfg.FillIDsFeature,
						State: gwtypes.FeatureGateStateDisabled,
					},
				}
				return cp
			}(),
			setupClient: func(t *testing.T) client.Client {
				return fake.NewClientBuilder().
					WithScheme(s).
					WithObjects(
						createValidKonnectExtension(),
						createValidTLSSecret(),
					).
					Build()
			},
			wantProcessed: true,
			checkAssertions: func(t *testing.T, processor *ControlPlaneKonnectExtensionProcessor, cp *gwtypes.ControlPlane) {
				require.NotNil(t, cp.Spec.FeatureGates)
				require.Len(t, cp.Spec.FeatureGates, 1)
				assert.Equal(t, managercfg.FillIDsFeature, cp.Spec.FeatureGates[0].Name)
				assert.Equal(t, gwtypes.FeatureGateStateEnabled, cp.Spec.FeatureGates[0].State,
					"FillIDs feature gate should be enabled even if it was previously disabled")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			client := tt.setupClient(t)
			processor := &ControlPlaneKonnectExtensionProcessor{}

			processed, err := processor.Process(ctx, client, tt.object)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.wantErrContains != "" {
					assert.Contains(t, err.Error(), tt.wantErrContains)
				}
			} else {
				assert.NoError(t, err)
			}

			assert.Equal(t, tt.wantProcessed, processed)

			if tt.checkAssertions != nil && !tt.wantErr {
				cp, ok := tt.object.(*gwtypes.ControlPlane)
				require.True(t, ok, "object should be a ControlPlane")
				tt.checkAssertions(t, processor, cp)
			}
		})
	}
}
