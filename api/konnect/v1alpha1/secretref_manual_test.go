package v1alpha1

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// testAIGatewayPolicyConfig mirrors the shape of AIGatewayPolicy.config's own
// OAS example (anonymize/stop_on_error), used to verify that both the JSON
// and YAML secret-data formats resolve to equivalent structured content.
type testAIGatewayPolicyConfig struct {
	Anonymize   []string `json:"anonymize"`
	StopOnError bool     `json:"stop_on_error"`
}

func TestAIGatewayPolicyConfigDataSource_valueFromSecretRef(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	ctx := t.Context()

	tests := []struct {
		name       string
		namespace  string
		secretRef  *SensitiveDataSecretRef
		clientObjs []client.Object
		wantErr    string
		wantConfig *testAIGatewayPolicyConfig
	}{
		{
			name:      "nil SecretRef returns error",
			namespace: "default",
			secretRef: nil,
			wantErr:   "secretRef is nil",
		},
		{
			name:      "failed to get the secret",
			namespace: "default",
			secretRef: &SensitiveDataSecretRef{
				Name: "missing-secret",
				Key:  "config",
			},
			clientObjs: []client.Object{},
			wantErr:    "failed to fetch Secret",
		},
		{
			name:      "secret has no required key",
			namespace: "default",
			secretRef: &SensitiveDataSecretRef{
				Name: "policy-config",
				Key:  "config",
			},
			clientObjs: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "policy-config",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"other-key": []byte(`{"foo":"bar"}`),
					},
				},
			},
			wantErr: "is missing key",
		},
		{
			name:      "JSON format support",
			namespace: "default",
			secretRef: &SensitiveDataSecretRef{
				Name: "policy-config",
				Key:  "config",
			},
			clientObjs: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "policy-config",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"config": []byte(`{"anonymize":["phone","creditcard"],"stop_on_error":true}`),
					},
				},
			},
			wantConfig: &testAIGatewayPolicyConfig{
				Anonymize:   []string{"phone", "creditcard"},
				StopOnError: true,
			},
		},
		{
			name:      "YAML format support",
			namespace: "default",
			secretRef: &SensitiveDataSecretRef{
				Name: "policy-config",
				Key:  "config",
			},
			clientObjs: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "policy-config",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"config": []byte("anonymize:\n  - phone\n  - creditcard\nstop_on_error: true\n"),
					},
				},
			},
			wantConfig: &testAIGatewayPolicyConfig{
				Anonymize:   []string{"phone", "creditcard"},
				StopOnError: true,
			},
		},
		{
			name:      "malformed secret data (neither JSON nor YAML)",
			namespace: "default",
			secretRef: &SensitiveDataSecretRef{
				Name: "policy-config",
				Key:  "config",
			},
			clientObjs: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "policy-config",
						Namespace: "default",
					},
					Data: map[string][]byte{
						// A tab used for indentation is invalid YAML (and isn't
						// valid JSON either), so both parse attempts must fail.
						"config": []byte("key: value\n\tinvalid: true\n"),
					},
				},
			},
			wantErr: "failed to convert YAML to JSON",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.clientObjs...).Build()

			src := &AIGatewayPolicyConfigDataSource{
				Type:      SensitiveDataSourceTypeSecretRef,
				SecretRef: tt.secretRef,
			}

			result, err := src.valueFromSecretRef(ctx, cl, tt.namespace)

			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
				return
			}

			require.NoError(t, err)
			var got testAIGatewayPolicyConfig
			require.NoError(t, json.Unmarshal(result.Raw, &got))
			require.Equal(t, *tt.wantConfig, got)
		})
	}
}
