package v1alpha1

import (
	"bytes"
	"testing"

	"github.com/Kong/ai-deck-converter/aigw"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestAIGatewayConsumerGroup_ToAIGWConsumerGroup covers the policy reference resolution —
// deliberately without any SetKonnectID on the referenced policy, pinning that the on-prem
// translation uses the name-only resolver, unlike the generated Konnect resolver (contrast
// zz_generated_aigatewayconsumergroup_sdkops.go's resolveAIGatewayConsumerGroupPolicies) —
// plus the managed_by drop.
func TestAIGatewayConsumerGroup_ToAIGWConsumerGroup(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, AddToScheme(scheme))

	// newReferencedPolicy returns a fresh object per subtest: the fake client's tracker mutates
	// the objects it's given (SetResourceVersion on Build), so parallel subtests sharing one
	// instance race.
	newReferencedPolicy := func() *AIGatewayPolicy {
		return &AIGatewayPolicy{
			Name: "ai-gw-policy", Namespace: "default",
			Spec: AIGatewayPolicySpec{
				APISpec: AIGatewayPolicyAPISpec{
					Name: "aigw-policy",
					Type: "rate-limiting",
				},
			},
		}
	}

	tests := []struct {
		name    string
		obj     *AIGatewayConsumerGroup
		want    *aigw.ConsumerGroup
		wantErr string
	}{
		{
			name: "policies resolved by name, labels kept, managed_by dropped",
			obj: &AIGatewayConsumerGroup{
				Name: "sample-ai-gw-consumer-group", Namespace: "default",
				Spec: AIGatewayConsumerGroupSpec{
					APISpec: AIGatewayConsumerGroupAPISpec{
						Name:        "dev-users",
						DisplayName: "Dev Users",
						Labels:      PublicLabels{"app": "test1", "env": "test"},
						ManagedBy:   ManagedBy{"kong-operator": "true"},
						Policies:    []AIGatewayPolicyRef{{Name: "ai-gw-policy"}},
					},
				},
			},
			want: &aigw.ConsumerGroup{
				Name:        "dev-users",
				DisplayName: "Dev Users",
				Labels:      aigw.Labels{"app": "test1", "env": "test"},
				Policies:    []string{"aigw-policy"},
			},
		},
		{
			name: "no policies",
			obj: &AIGatewayConsumerGroup{
				Name: "sample-ai-gw-consumer-group-empty", Namespace: "default",
				Spec: AIGatewayConsumerGroupSpec{
					APISpec: AIGatewayConsumerGroupAPISpec{
						Name:        "empty-group",
						DisplayName: "Empty Group",
					},
				},
			},
			want: &aigw.ConsumerGroup{
				Name:        "empty-group",
				DisplayName: "Empty Group",
			},
		},
		{
			name: "dangling policy reference",
			obj: &AIGatewayConsumerGroup{
				Name: "sample-ai-gw-consumer-group-dangling", Namespace: "default",
				Spec: AIGatewayConsumerGroupSpec{
					APISpec: AIGatewayConsumerGroupAPISpec{
						Name:        "dangling-group",
						DisplayName: "Dangling Group",
						Policies:    []AIGatewayPolicyRef{{Name: "does-not-exist"}},
					},
				},
			},
			wantErr: "policies",
		},
		{
			name: "cross-namespace policy reference rejected",
			obj: &AIGatewayConsumerGroup{
				Name: "sample-ai-gw-consumer-group-cross-ns", Namespace: "default",
				Spec: AIGatewayConsumerGroupSpec{
					APISpec: AIGatewayConsumerGroupAPISpec{
						Name:        "cross-ns-group",
						DisplayName: "Cross NS Group",
						Policies:    []AIGatewayPolicyRef{{Name: "ai-gw-policy", Namespace: "other-namespace"}},
					},
				},
			},
			wantErr: "cross-namespace reference",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(newReferencedPolicy()).Build()
			got, err := tt.obj.ToAIGWConsumerGroup(t.Context(), cl)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestAIGatewayConsumerGroup_ToAIGWConsumerGroup_StrictRoundTrip guards against a dropped or
// renamed field: it decodes marshalAIGWConsumerGroupPayload's output with yaml.v3's
// KnownFields(true), which errors on any key aigw.ConsumerGroup doesn't recognize. See
// aigatewaymodel_aigw_manual_test.go for the rationale.
func TestAIGatewayConsumerGroup_ToAIGWConsumerGroup_StrictRoundTrip(t *testing.T) {
	t.Parallel()

	spec := &AIGatewayConsumerGroupAPISpec{
		Name:        "dev-users",
		DisplayName: "Dev Users",
		Labels:      PublicLabels{"app": "test1"},
		// Policies are stripped by the payload builder (they are re-attached resolved by
		// ToAIGWConsumerGroup), so the strict decode must not see them.
		Policies: []AIGatewayPolicyRef{{Name: "ai-gw-policy"}},
	}
	data, err := spec.marshalAIGWConsumerGroupPayload()
	require.NoError(t, err)

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var group aigw.ConsumerGroup
	require.NoError(t, dec.Decode(&group))

	require.Equal(t, "dev-users", group.Name)
	require.Equal(t, "Dev Users", group.DisplayName)
	require.Nil(t, group.Policies)
}
