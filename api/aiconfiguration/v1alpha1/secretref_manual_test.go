package v1alpha1

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/crossnamespace"
)

// secretReferenceGrant returns a KongReferenceGrant in secretNamespace that allows fromKind
// entities in fromNamespace to reference the Secret secretName. An empty secretName allows
// every Secret in the namespace.
func secretReferenceGrant(secretNamespace, fromNamespace, fromKind, secretName string) *configurationv1alpha1.KongReferenceGrant {
	to := configurationv1alpha1.ReferenceGrantTo{Group: "core", Kind: "Secret"}
	if secretName != "" {
		to.Name = new(configurationv1alpha1.ObjectName(secretName))
	}
	return &configurationv1alpha1.KongReferenceGrant{
		Name: "allow-" + fromNamespace + "-" + fromKind, Namespace: secretNamespace,
		Spec: configurationv1alpha1.KongReferenceGrantSpec{
			From: []configurationv1alpha1.ReferenceGrantFrom{{
				Group:     configurationv1alpha1.Group(GroupVersion.Group),
				Kind:      configurationv1alpha1.Kind(fromKind),
				Namespace: configurationv1alpha1.Namespace(fromNamespace),
			}},
			To: []configurationv1alpha1.ReferenceGrantTo{to},
		},
	}
}

func TestCheckCrossNamespaceSecretRefsGranted(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, AddToScheme(scheme))
	require.NoError(t, configurationv1alpha1.AddToScheme(scheme))

	policyWithSecretRef := func(namespace *string) *AIGatewayPolicy {
		return &AIGatewayPolicy{
			Name: "policy", Namespace: "default",
			Spec: AIGatewayPolicySpec{
				APISpec: AIGatewayPolicyAPISpec{
					Config: AIGatewayPolicyConfigDataSource{
						Type:      SensitiveDataSourceTypeSecretRef,
						SecretRef: &SensitiveDataSecretRef{Name: "creds", Key: "config", Namespace: namespace},
					},
				},
			},
		}
	}

	tests := []struct {
		name           string
		obj            *AIGatewayPolicy
		grants         []client.Object
		wantNotGranted bool
	}{
		{
			name: "secretRef without namespace needs no grant",
			obj:  policyWithSecretRef(nil),
		},
		{
			name: "secretRef to the entity's namespace needs no grant",
			obj:  policyWithSecretRef(new("default")),
		},
		{
			name:           "cross-namespace secretRef without grant is denied",
			obj:            policyWithSecretRef(new("secrets")),
			wantNotGranted: true,
		},
		{
			name:   "cross-namespace secretRef allowed by a grant for the Secret",
			obj:    policyWithSecretRef(new("secrets")),
			grants: []client.Object{secretReferenceGrant("secrets", "default", "AIGatewayPolicy", "creds")},
		},
		{
			name:   "cross-namespace secretRef allowed by a grant for every Secret",
			obj:    policyWithSecretRef(new("secrets")),
			grants: []client.Object{secretReferenceGrant("secrets", "default", "AIGatewayPolicy", "")},
		},
		{
			name:           "grant for another kind is denied",
			obj:            policyWithSecretRef(new("secrets")),
			grants:         []client.Object{secretReferenceGrant("secrets", "default", "AIGatewayVault", "creds")},
			wantNotGranted: true,
		},
		{
			name:           "grant for another source namespace is denied",
			obj:            policyWithSecretRef(new("secrets")),
			grants:         []client.Object{secretReferenceGrant("secrets", "other", "AIGatewayPolicy", "creds")},
			wantNotGranted: true,
		},
		{
			name:           "grant for another Secret is denied",
			obj:            policyWithSecretRef(new("secrets")),
			grants:         []client.Object{secretReferenceGrant("secrets", "default", "AIGatewayPolicy", "other")},
			wantNotGranted: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.grants...).Build()
			err := CheckCrossNamespaceSecretRefsGranted(t.Context(), cl, tt.obj)
			if tt.wantNotGranted {
				require.True(t, crossnamespace.IsReferenceNotGranted(err), "expected not granted error, got %v", err)
				return
			}
			require.NoError(t, err)
		})
	}
}

// TestNoteSecretLabelRequirement pins the not-found gate: only not-found errors get the
// Secret label selector hint (the operator's caches hold only labeled Secrets), other
// errors pass through unchanged.
func TestNoteSecretLabelRequirement(t *testing.T) {
	notFound := apierrors.NewNotFound(corev1.Resource("secrets"), "client-secret")
	err := noteSecretLabelRequirement(fmt.Errorf("failed to fetch Secret default/client-secret: %w", notFound))
	require.ErrorContains(t, err, "konghq.com/secret=true")

	other := fmt.Errorf("secret default/client-secret is missing key %q", "client-secret")
	require.Equal(t, other.Error(), noteSecretLabelRequirement(other).Error())
}

func TestAIGatewayPolicyConfigDataSource_valueFromSecretRef(t *testing.T) {

	// testAIGatewayPolicyConfig mirrors the shape of AIGatewayPolicy.config's own
	// OAS example (anonymize/stop_on_error), used to verify that both the JSON
	// and YAML secret-data formats resolve to equivalent structured content.
	type testAIGatewayPolicyConfig struct {
		Anonymize   []string `json:"anonymize"`
		StopOnError bool     `json:"stop_on_error"`
	}

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
					Name:      "policy-config",
					Namespace: "default",
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
					Name:      "policy-config",
					Namespace: "default",
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
					Name:      "policy-config",
					Namespace: "default",
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
					Name:      "policy-config",
					Namespace: "default",
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
