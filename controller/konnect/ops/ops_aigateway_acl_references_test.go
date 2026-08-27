package ops

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
)

// aclReferencesScheme returns a scheme with the aiconfiguration v1alpha1 types
// registered so a fake client can serve referenced AIGatewayConsumerGroup CRs.
func aclReferencesScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, aiconfigurationv1alpha1.AddToScheme(scheme))
	return scheme
}

// programmedConsumerGroup builds an AIGatewayConsumerGroup that already has a Konnect ID
// and a Konnect name, i.e. a reference target that resolves successfully.
func programmedConsumerGroup(name, namespace, konnectName, konnectID string) *aiconfigurationv1alpha1.AIGatewayConsumerGroup {
	c := &aiconfigurationv1alpha1.AIGatewayConsumerGroup{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: aiconfigurationv1alpha1.AIGatewayConsumerGroupSpec{
			APISpec: aiconfigurationv1alpha1.AIGatewayConsumerGroupAPISpec{
				Name: aiconfigurationv1alpha1.AIGatewayEntityIdentifier(konnectName),
			},
		},
	}
	c.SetKonnectID(konnectID)
	return c
}

// programmedModelProvider builds an AIGatewayModelProvider that already has a
// Konnect ID, i.e. a reference target that resolves successfully. Its
// resolvesTo:name Konnect name is left empty (GetKonnectName's union type
// switch returns "" when the provider config isn't set), which is fine here
// since these tests only need the reference to resolve, not a specific name.
func programmedModelProvider(name, namespace, konnectID string) *aiconfigurationv1alpha1.AIGatewayModelProvider {
	p := &aiconfigurationv1alpha1.AIGatewayModelProvider{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
	}
	p.SetKonnectID(konnectID)
	return p
}

// programmedAuthStrategy builds an AIGatewayAuthStrategy that already has a
// Konnect ID and a Konnect name, i.e. a reference target that resolves
// successfully.
func programmedAuthStrategy(name, namespace, konnectName, konnectID string) *aiconfigurationv1alpha1.AIGatewayAuthStrategy {
	s := &aiconfigurationv1alpha1.AIGatewayAuthStrategy{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: aiconfigurationv1alpha1.AIGatewayAuthStrategySpec{
			APISpec: aiconfigurationv1alpha1.AIGatewayAuthStrategyAPISpec{
				AIGatewayAuthStrategyConfig: &aiconfigurationv1alpha1.AIGatewayAuthStrategyConfig{
					Type: aiconfigurationv1alpha1.AIGatewayAuthStrategyConfigTypeKeyAuth,
					KeyAuth: &aiconfigurationv1alpha1.AIGatewayAuthStrategyKeyAuth{
						Name:        aiconfigurationv1alpha1.AIGatewayEntityIdentifier(konnectName),
						DisplayName: konnectName,
					},
				},
			},
		},
	}
	s.SetKonnectID(konnectID)
	return s
}

func programmedPolicy(name, namespace, konnectID, gatewayID, specName string) *aiconfigurationv1alpha1.AIGatewayPolicy {
	p := &aiconfigurationv1alpha1.AIGatewayPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: aiconfigurationv1alpha1.AIGatewayPolicySpec{
			APISpec: aiconfigurationv1alpha1.AIGatewayPolicyAPISpec{
				Name: aiconfigurationv1alpha1.AIGatewayEntityIdentifier(specName),
			},
		},
	}
	p.SetKonnectID(konnectID)
	p.SetGatewayID(gatewayID)
	return p
}

func testAgentWithPolicyRef(namespace string, ref aiconfigurationv1alpha1.AIGatewayPolicyRef) *aiconfigurationv1alpha1.AIGatewayAgent {
	return &aiconfigurationv1alpha1.AIGatewayAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: namespace},
		Spec: aiconfigurationv1alpha1.AIGatewayAgentSpec{
			APISpec: aiconfigurationv1alpha1.AIGatewayAgentAPISpec{
				Name: aiconfigurationv1alpha1.AIGatewayEntityIdentifier("agent-name"),
				Policies: []aiconfigurationv1alpha1.AIGatewayPolicyRef{
					ref,
				},
			},
		},
	}
}

// TestToCreateAIGatewayAgentRequest_ResolvesACLAllowRefs verifies that an agent
// with a single allow ACL reference to a programmed AIGatewayConsumerGroup
// resolves to the consumer group's Konnect name in the SDK request's
// access.acls.allow.allow union arm.
func TestToCreateAIGatewayAgentRequest_ResolvesACLAllowRefs(t *testing.T) {
	t.Parallel()

	consumerGroup := programmedConsumerGroup("consumer-group-1", "ns", "konnect-consumer-group-name", "kid-consumer-group-1")

	agent := &aiconfigurationv1alpha1.AIGatewayAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "ns"},
		Spec: aiconfigurationv1alpha1.AIGatewayAgentSpec{
			APISpec: aiconfigurationv1alpha1.AIGatewayAgentAPISpec{
				Name: aiconfigurationv1alpha1.AIGatewayEntityIdentifier("agent-name"),
				Access: aiconfigurationv1alpha1.AIGatewayAgentAccess{
					Acls: &aiconfigurationv1alpha1.AIGatewayAgentAccessAcls{
						Type: aiconfigurationv1alpha1.AIGatewayAgentAccessAclsTypeAllow,
						Allow: &aiconfigurationv1alpha1.AIGatewayAllowACL{
							Allow: []aiconfigurationv1alpha1.AIGatewayACLRef{
								{Kind: "AIGatewayConsumerGroup", Name: "consumer-group-1"},
							},
						},
					},
				},
			},
		},
	}

	cl := fake.NewClientBuilder().WithScheme(aclReferencesScheme(t)).WithObjects(consumerGroup).Build()

	req, err := agent.ToCreateAIGatewayAgentRequest(t.Context(), cl)
	require.NoError(t, err)
	require.NotNil(t, req.Access)
	require.NotNil(t, req.Access.Acls)
	require.NotNil(t, req.Access.Acls.AIGatewayAllowACL, "the allow union arm must be selected")
	assert.Equal(t, []string{"konnect-consumer-group-name"}, req.Access.Acls.AIGatewayAllowACL.Allow)
	assert.Nil(t, req.Access.Acls.AIGatewayDenyACL, "the deny union arm must not be set")
}

// TestToCreateAIGatewayAgentRequest_ACLRefNotProgrammed asserts the reference
// resolution surfaces a not-programmed error when the referenced consumer group has
// no Konnect ID yet.
func TestToCreateAIGatewayAgentRequest_ACLRefNotProgrammed(t *testing.T) {
	t.Parallel()

	consumerGroup := &aiconfigurationv1alpha1.AIGatewayConsumerGroup{
		ObjectMeta: metav1.ObjectMeta{Name: "consumer-group-1", Namespace: "ns"},
	}

	agent := &aiconfigurationv1alpha1.AIGatewayAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "ns"},
		Spec: aiconfigurationv1alpha1.AIGatewayAgentSpec{
			APISpec: aiconfigurationv1alpha1.AIGatewayAgentAPISpec{
				Access: aiconfigurationv1alpha1.AIGatewayAgentAccess{
					Acls: &aiconfigurationv1alpha1.AIGatewayAgentAccessAcls{
						Type: aiconfigurationv1alpha1.AIGatewayAgentAccessAclsTypeAllow,
						Allow: &aiconfigurationv1alpha1.AIGatewayAllowACL{
							Allow: []aiconfigurationv1alpha1.AIGatewayACLRef{
								{Kind: "AIGatewayConsumerGroup", Name: "consumer-group-1"},
							},
						},
					},
				},
			},
		},
	}

	cl := fake.NewClientBuilder().WithScheme(aclReferencesScheme(t)).WithObjects(consumerGroup).Build()

	_, err := agent.ToCreateAIGatewayAgentRequest(t.Context(), cl)
	require.Error(t, err)
	require.ErrorContains(t, err, "not programmed")
}

func TestToCreateAIGatewayAgentRequest_AllowsExplicitSameNamespacePolicyRef(t *testing.T) {
	t.Parallel()

	policy := programmedPolicy("policy-1", "ns", "kid-policy-1", "gw-1", "konnect-policy-name")
	agent := testAgentWithPolicyRef("ns", aiconfigurationv1alpha1.AIGatewayPolicyRef{
		Namespace: "ns",
		Name:      "policy-1",
	})
	agent.SetGatewayID("gw-1")

	cl := fake.NewClientBuilder().WithScheme(aclReferencesScheme(t)).WithObjects(policy).Build()

	req, err := agent.ToCreateAIGatewayAgentRequest(t.Context(), cl)
	require.NoError(t, err)
	require.Equal(t, []string{"konnect-policy-name"}, req.Policies)
}

func TestToCreateAIGatewayAgentRequest_RejectsCrossNamespacePolicyRef(t *testing.T) {
	t.Parallel()

	policy := programmedPolicy("policy-1", "other-ns", "kid-policy-1", "gw-1", "konnect-policy-name")
	agent := testAgentWithPolicyRef("ns", aiconfigurationv1alpha1.AIGatewayPolicyRef{
		Namespace: "other-ns",
		Name:      "policy-1",
	})
	agent.SetGatewayID("gw-1")

	cl := fake.NewClientBuilder().WithScheme(aclReferencesScheme(t)).WithObjects(policy).Build()

	_, err := agent.ToCreateAIGatewayAgentRequest(t.Context(), cl)
	require.Error(t, err)
	require.ErrorContains(t, err, "cross-namespace reference")
}

func TestToCreateAIGatewayAgentRequest_RejectsPolicyRefFromDifferentGateway(t *testing.T) {
	t.Parallel()

	policy := programmedPolicy("policy-1", "ns", "kid-policy-1", "gw-2", "konnect-policy-name")
	agent := testAgentWithPolicyRef("ns", aiconfigurationv1alpha1.AIGatewayPolicyRef{
		Name: "policy-1",
	})
	agent.SetGatewayID("gw-1")

	cl := fake.NewClientBuilder().WithScheme(aclReferencesScheme(t)).WithObjects(policy).Build()

	_, err := agent.ToCreateAIGatewayAgentRequest(t.Context(), cl)
	require.Error(t, err)
	require.ErrorContains(t, err, `belongs to Gateway "gw-2", not referrer Gateway "gw-1"`)
}

// TestToCreateAIGatewayModelRequest_PreservesAuthStrategiesSibling verifies
// that rebuilding the referenced acls union in the model payload does not drop
// the auth_strategies sibling that lives next to acls under access, and
// that the auth strategy reference itself resolves to its Konnect name.
func TestToCreateAIGatewayModelRequest_PreservesAuthStrategiesSibling(t *testing.T) {
	t.Parallel()

	consumerGroup := programmedConsumerGroup("consumer-group-1", "default", "konnect-consumer-group-name", "kid-consumer-group-1")
	// testGeneratedAIGatewayModelForSDKOps's fixture targets a provider named
	// "provider-1" in the model's own ("default") namespace.
	provider := programmedModelProvider("provider-1", "default", "kid-provider-1")
	authStrategy := programmedAuthStrategy("auth-strategy-1", "default", "konnect-auth-strategy-name", "kid-auth-strategy-1")

	model := testGeneratedAIGatewayModelForSDKOps()
	model.Spec.APISpec.API.Access = aiconfigurationv1alpha1.AIGatewayModelAccess{
		AuthStrategies: []aiconfigurationv1alpha1.AIGatewayAuthStrategyRef{
			{
				Name: "auth-strategy-1",
			},
		},
		Acls: &aiconfigurationv1alpha1.AIGatewayModelAccessAcls{
			Type: aiconfigurationv1alpha1.AIGatewayModelAccessAclsTypeAllow,
			Allow: &aiconfigurationv1alpha1.AIGatewayAllowACL{
				Allow: []aiconfigurationv1alpha1.AIGatewayACLRef{
					{Kind: "AIGatewayConsumerGroup", Name: "consumer-group-1"},
				},
			},
		},
	}

	cl := fake.NewClientBuilder().WithScheme(aclReferencesScheme(t)).WithObjects(consumerGroup, provider, authStrategy).Build()

	req, err := model.ToCreateAIGatewayModelRequest(t.Context(), cl)
	require.NoError(t, err)
	require.NotNil(t, req.AIGatewayModelAPI, "the api union arm must be selected")

	// The resolved acls arm is present with the consumer group's Konnect name.
	require.NotNil(t, req.AIGatewayModelAPI.Access.Acls)
	require.NotNil(t, req.AIGatewayModelAPI.Access.Acls.AIGatewayAllowACL)
	assert.Equal(t, []string{"konnect-consumer-group-name"}, req.AIGatewayModelAPI.Access.Acls.AIGatewayAllowACL.Allow)

	// The auth_strategies sibling survived the union rebuild, resolved to
	// the referenced AIGatewayAuthStrategy's Konnect name.
	assert.Equal(t, []string{"konnect-auth-strategy-name"}, req.AIGatewayModelAPI.Access.AuthStrategies)
}
