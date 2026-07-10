package ops

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
)

// aclReferencesScheme returns a scheme with the konnect v1alpha1 types
// registered so a fake client can serve referenced AIGatewayConsumer CRs.
func aclReferencesScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, konnectv1alpha1.AddToScheme(scheme))
	return scheme
}

// programmedConsumer builds an AIGatewayConsumer that already has a Konnect ID
// and a Konnect name, i.e. a reference target that resolves successfully.
func programmedConsumer(name, namespace, konnectName, konnectID string) *konnectv1alpha1.AIGatewayConsumer {
	c := &konnectv1alpha1.AIGatewayConsumer{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: konnectv1alpha1.AIGatewayConsumerSpec{
			APISpec: konnectv1alpha1.AIGatewayConsumerAPISpec{
				Name: konnectv1alpha1.AIGatewayEntityIdentifier(konnectName),
			},
		},
	}
	c.SetKonnectID(konnectID)
	return c
}

func programmedPolicy(name, namespace, konnectID, gatewayID string) *konnectv1alpha1.AIGatewayPolicy {
	p := &konnectv1alpha1.AIGatewayPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
	}
	p.SetKonnectID(konnectID)
	p.SetGatewayID(gatewayID)
	return p
}

func testAgentWithPolicyRef(namespace string, ref konnectv1alpha1.AIGatewayPolicyRef) *konnectv1alpha1.AIGatewayAgent {
	return &konnectv1alpha1.AIGatewayAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: namespace},
		Spec: konnectv1alpha1.AIGatewayAgentSpec{
			APISpec: konnectv1alpha1.AIGatewayAgentAPISpec{
				Name: konnectv1alpha1.AIGatewayEntityIdentifier("agent-name"),
				Policies: []konnectv1alpha1.AIGatewayPolicyRef{
					ref,
				},
			},
		},
	}
}

// TestToCreateAIGatewayAgentRequest_ResolvesACLAllowRefs verifies that an agent
// with a single allow ACL reference to a programmed AIGatewayConsumer resolves
// to the consumer's Konnect name in the SDK request's access.acls.allow.allow
// union arm.
func TestToCreateAIGatewayAgentRequest_ResolvesACLAllowRefs(t *testing.T) {
	t.Parallel()

	consumer := programmedConsumer("consumer-1", "ns", "konnect-consumer-name", "kid-consumer-1")

	agent := &konnectv1alpha1.AIGatewayAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "ns"},
		Spec: konnectv1alpha1.AIGatewayAgentSpec{
			APISpec: konnectv1alpha1.AIGatewayAgentAPISpec{
				Name: konnectv1alpha1.AIGatewayEntityIdentifier("agent-name"),
				Access: konnectv1alpha1.AIGatewayAgentAccess{
					Acls: &konnectv1alpha1.AIGatewayAgentAccessAcls{
						Type: konnectv1alpha1.AIGatewayAgentAccessAclsTypeAllow,
						Allow: &konnectv1alpha1.AIGatewayAllowACL{
							Allow: []konnectv1alpha1.AIGatewayACLRef{
								{Kind: "AIGatewayConsumer", Name: "consumer-1"},
							},
						},
					},
				},
			},
		},
	}

	cl := fake.NewClientBuilder().WithScheme(aclReferencesScheme(t)).WithObjects(consumer).Build()

	req, err := agent.ToCreateAIGatewayAgentRequest(t.Context(), cl)
	require.NoError(t, err)
	require.NotNil(t, req.Access)
	require.NotNil(t, req.Access.Acls)
	require.NotNil(t, req.Access.Acls.AIGatewayAllowACL, "the allow union arm must be selected")
	assert.Equal(t, []string{"konnect-consumer-name"}, req.Access.Acls.AIGatewayAllowACL.Allow)
	assert.Nil(t, req.Access.Acls.AIGatewayDenyACL, "the deny union arm must not be set")
}

// TestToCreateAIGatewayAgentRequest_ACLRefNotProgrammed asserts the reference
// resolution surfaces a not-programmed error when the referenced consumer has
// no Konnect ID yet.
func TestToCreateAIGatewayAgentRequest_ACLRefNotProgrammed(t *testing.T) {
	t.Parallel()

	consumer := &konnectv1alpha1.AIGatewayConsumer{
		ObjectMeta: metav1.ObjectMeta{Name: "consumer-1", Namespace: "ns"},
	}

	agent := &konnectv1alpha1.AIGatewayAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "ns"},
		Spec: konnectv1alpha1.AIGatewayAgentSpec{
			APISpec: konnectv1alpha1.AIGatewayAgentAPISpec{
				Access: konnectv1alpha1.AIGatewayAgentAccess{
					Acls: &konnectv1alpha1.AIGatewayAgentAccessAcls{
						Type: konnectv1alpha1.AIGatewayAgentAccessAclsTypeAllow,
						Allow: &konnectv1alpha1.AIGatewayAllowACL{
							Allow: []konnectv1alpha1.AIGatewayACLRef{
								{Kind: "AIGatewayConsumer", Name: "consumer-1"},
							},
						},
					},
				},
			},
		},
	}

	cl := fake.NewClientBuilder().WithScheme(aclReferencesScheme(t)).WithObjects(consumer).Build()

	_, err := agent.ToCreateAIGatewayAgentRequest(t.Context(), cl)
	require.Error(t, err)
	require.ErrorContains(t, err, "not programmed")
}

func TestToCreateAIGatewayAgentRequest_AllowsExplicitSameNamespacePolicyRef(t *testing.T) {
	t.Parallel()

	policy := programmedPolicy("policy-1", "ns", "kid-policy-1", "gw-1")
	agent := testAgentWithPolicyRef("ns", konnectv1alpha1.AIGatewayPolicyRef{
		Namespace: "ns",
		Name:      "policy-1",
	})
	agent.SetGatewayID("gw-1")

	cl := fake.NewClientBuilder().WithScheme(aclReferencesScheme(t)).WithObjects(policy).Build()

	req, err := agent.ToCreateAIGatewayAgentRequest(t.Context(), cl)
	require.NoError(t, err)
	require.Equal(t, []string{"kid-policy-1"}, req.Policies)
}

func TestToCreateAIGatewayAgentRequest_RejectsCrossNamespacePolicyRef(t *testing.T) {
	t.Parallel()

	policy := programmedPolicy("policy-1", "other-ns", "kid-policy-1", "gw-1")
	agent := testAgentWithPolicyRef("ns", konnectv1alpha1.AIGatewayPolicyRef{
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

	policy := programmedPolicy("policy-1", "ns", "kid-policy-1", "gw-2")
	agent := testAgentWithPolicyRef("ns", konnectv1alpha1.AIGatewayPolicyRef{
		Name: "policy-1",
	})
	agent.SetGatewayID("gw-1")

	cl := fake.NewClientBuilder().WithScheme(aclReferencesScheme(t)).WithObjects(policy).Build()

	_, err := agent.ToCreateAIGatewayAgentRequest(t.Context(), cl)
	require.Error(t, err)
	require.ErrorContains(t, err, `belongs to Gateway "gw-2", not referrer Gateway "gw-1"`)
}

// TestToCreateAIGatewayModelRequest_PreservesIdentityProvidersSibling verifies
// that rebuilding the referenced acls union in the model payload does not drop
// the identity_providers sibling that lives next to acls under access.
func TestToCreateAIGatewayModelRequest_PreservesIdentityProvidersSibling(t *testing.T) {
	t.Parallel()

	consumer := programmedConsumer("consumer-1", "default", "konnect-consumer-name", "kid-consumer-1")

	model := testGeneratedAIGatewayModelForSDKOps()
	model.Spec.APISpec.API.Access = konnectv1alpha1.AIGatewayModelAccess{
		IdentityProviders: []konnectv1alpha1.AIGatewayIdentityProviderReference{"idp-1"},
		Acls: &konnectv1alpha1.AIGatewayModelAccessAcls{
			Type: konnectv1alpha1.AIGatewayModelAccessAclsTypeAllow,
			Allow: &konnectv1alpha1.AIGatewayAllowACL{
				Allow: []konnectv1alpha1.AIGatewayACLRef{
					{Kind: "AIGatewayConsumer", Name: "consumer-1"},
				},
			},
		},
	}

	cl := fake.NewClientBuilder().WithScheme(aclReferencesScheme(t)).WithObjects(consumer).Build()

	req, err := model.ToCreateAIGatewayModelRequest(t.Context(), cl)
	require.NoError(t, err)
	require.NotNil(t, req.AIGatewayModelAPI, "the api union arm must be selected")

	// The resolved acls arm is present with the consumer's Konnect name.
	require.NotNil(t, req.AIGatewayModelAPI.Access.Acls)
	require.NotNil(t, req.AIGatewayModelAPI.Access.Acls.AIGatewayAllowACL)
	assert.Equal(t, []string{"konnect-consumer-name"}, req.AIGatewayModelAPI.Access.Acls.AIGatewayAllowACL.Allow)

	// The identity_providers sibling survived the union rebuild.
	assert.Equal(t, []string{"idp-1"}, req.AIGatewayModelAPI.Access.IdentityProviders)
}
