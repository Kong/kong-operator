package index

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
)

func TestEventGatewayListenerPolicyOnEventGatewayTLSTrustBundleRef(t *testing.T) {
	var extract client.IndexerFunc
	for _, opt := range OptionsForEventGatewayListenerPolicy() {
		if opt.Field == IndexFieldEventGatewayListenerPolicyOnEventGatewayTLSTrustBundleRef {
			extract = opt.ExtractValueFn
		}
	}
	require.NotNil(t, extract)

	policy := func(bundles ...configurationv1alpha1.TLSTrustBundleReference) *configurationv1alpha1.EventGatewayListenerPolicy {
		return &configurationv1alpha1.EventGatewayListenerPolicy{
			Name: "policy", Namespace: "ns",
			Spec: configurationv1alpha1.EventGatewayListenerPolicySpec{
				APISpec: configurationv1alpha1.EventGatewayListenerPolicyAPISpec{
					EventGatewayListenerPolicyConfig: &configurationv1alpha1.EventGatewayListenerPolicyConfig{
						Type: configurationv1alpha1.EventGatewayListenerPolicyConfigTypeEventGatewayTLSListen,
						EventGatewayTLSListen: &configurationv1alpha1.EventGatewayTLSListenerPolicy{
							Config: configurationv1alpha1.EventGatewayTLSListenerPolicyConfig{
								ClientAuthentication: configurationv1alpha1.EventGatewayTLSListenerPolicyConfigClientAuthentication{
									Mode:            "required",
									TLSTrustBundles: bundles,
								},
							},
						},
					},
				},
			},
		}
	}

	tests := []struct {
		name     string
		input    client.Object
		expected []string
	}{
		{
			name:     "not an EventGatewayListenerPolicy",
			input:    &configurationv1alpha1.EventGatewayTLSTrustBundle{},
			expected: nil,
		},
		{
			name:     "no TLS listener policy config",
			input:    &configurationv1alpha1.EventGatewayListenerPolicy{},
			expected: nil,
		},
		{
			name: "only Konnect IDs and names are not indexed",
			input: policy(
				configurationv1alpha1.TLSTrustBundleReference{ID: new("trust-bundle-id")},
				configurationv1alpha1.TLSTrustBundleReference{Name: new(configurationv1alpha1.TLSTrustBundleName("trust-bundle-name"))},
			),
			expected: nil,
		},
		{
			name: "namespacedRefs are indexed with the policy namespace by default",
			input: policy(
				configurationv1alpha1.TLSTrustBundleReference{ID: new("trust-bundle-id")},
				configurationv1alpha1.TLSTrustBundleReference{NamespacedRef: &configurationv1alpha1.EventGatewayTLSTrustBundleRef{Name: "bundle-a"}},
				configurationv1alpha1.TLSTrustBundleReference{NamespacedRef: &configurationv1alpha1.EventGatewayTLSTrustBundleRef{Name: "bundle-b", Namespace: "other"}},
			),
			expected: []string{"ns/bundle-a", "other/bundle-b"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, extract(tt.input))
		})
	}
}
