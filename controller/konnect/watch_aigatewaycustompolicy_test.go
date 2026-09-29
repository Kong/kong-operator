package konnect

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/index"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

func TestEnqueueAIGatewayCustomPolicyForAIGatewayPolicy(t *testing.T) {
	const gatewayID = "gateway-1"

	newCustomPolicy := func(name, konnectName, gateway string) *aiconfigurationv1alpha1.AIGatewayCustomPolicy {
		cp := &aiconfigurationv1alpha1.AIGatewayCustomPolicy{
			Name: name, Namespace: "default",
			Spec: aiconfigurationv1alpha1.AIGatewayCustomPolicySpec{
				APISpec: aiconfigurationv1alpha1.AIGatewayCustomPolicyAPISpec{
					AIGatewayCustomPolicyConfig: &aiconfigurationv1alpha1.AIGatewayCustomPolicyConfig{
						Type: aiconfigurationv1alpha1.AIGatewayCustomPolicyConfigTypeInstalled,
						Installed: &aiconfigurationv1alpha1.CreateAIGatewayCustomPolicyInstalledRequest{
							Name:        aiconfigurationv1alpha1.AIGatewayEntityIdentifier(konnectName),
							DisplayName: konnectName,
							Schema:      "return {}",
						},
					},
				},
			},
		}
		cp.SetGatewayID(gateway)
		return cp
	}
	referenced := newCustomPolicy("referenced", "referenced-konnect", gatewayID)
	byType := newCustomPolicy("by-type", "by-type-konnect", gatewayID)
	otherGateway := newCustomPolicy("other-gateway", "by-type-konnect", "gateway-2")
	builder := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(referenced, byType, otherGateway)
	for _, opt := range index.OptionsForAIGatewayCustomPolicy() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	cl := builder.Build()
	enqueue := enqueueAIGatewayCustomPolicyForAIGatewayPolicy(cl)

	newPolicy := func(mutate func(*aiconfigurationv1alpha1.AIGatewayPolicy)) *aiconfigurationv1alpha1.AIGatewayPolicy {
		p := &aiconfigurationv1alpha1.AIGatewayPolicy{
			Name: "policy", Namespace: "default",
		}
		p.SetGatewayID(gatewayID)
		mutate(p)
		return p
	}
	request := func(name string) reconcile.Request {
		return reconcile.Request{Namespace: "default", Name: name}
	}

	t.Run("policy using customPolicyRef enqueues the referenced custom policy", func(t *testing.T) {
		p := newPolicy(func(p *aiconfigurationv1alpha1.AIGatewayPolicy) {
			p.Spec.APISpec.CustomPolicyRef = aiconfigurationv1alpha1.AIGatewayCustomPolicyRef{Name: "referenced"}
		})
		assert.Equal(t, []reconcile.Request{request("referenced")}, enqueue(t.Context(), p))
	})

	t.Run("policy using type enqueues the custom policy with that Konnect name on the same gateway", func(t *testing.T) {
		p := newPolicy(func(p *aiconfigurationv1alpha1.AIGatewayPolicy) {
			p.Spec.APISpec.Type = "by-type-konnect"
		})
		assert.Equal(t, []reconcile.Request{request("by-type")}, enqueue(t.Context(), p))
	})

	t.Run("policy using a built-in plugin enqueues nothing", func(t *testing.T) {
		p := newPolicy(func(p *aiconfigurationv1alpha1.AIGatewayPolicy) {
			p.Spec.APISpec.Type = "response-transformer"
		})
		assert.Empty(t, enqueue(t.Context(), p))
	})

	t.Run("other objects enqueue nothing", func(t *testing.T) {
		require.Empty(t, enqueue(t.Context(), referenced))
	})
}

func TestEnqueueAIGatewayPolicyForAIGatewayCustomPolicy(t *testing.T) {
	const gatewayID = "gateway-1"

	customPolicy := &aiconfigurationv1alpha1.AIGatewayCustomPolicy{
		Name: "custom", Namespace: "default",
		Spec: aiconfigurationv1alpha1.AIGatewayCustomPolicySpec{
			APISpec: aiconfigurationv1alpha1.AIGatewayCustomPolicyAPISpec{
				AIGatewayCustomPolicyConfig: &aiconfigurationv1alpha1.AIGatewayCustomPolicyConfig{
					Type: aiconfigurationv1alpha1.AIGatewayCustomPolicyConfigTypeInstalled,
					Installed: &aiconfigurationv1alpha1.CreateAIGatewayCustomPolicyInstalledRequest{
						Name:        "custom-konnect",
						DisplayName: "custom-konnect",
						Schema:      "return {}",
					},
				},
			},
		},
	}
	customPolicy.SetGatewayID(gatewayID)

	newPolicy := func(name, gateway string, mutate func(*aiconfigurationv1alpha1.AIGatewayPolicy)) *aiconfigurationv1alpha1.AIGatewayPolicy {
		p := &aiconfigurationv1alpha1.AIGatewayPolicy{
			Name: name, Namespace: "default",
		}
		p.SetGatewayID(gateway)
		mutate(p)
		return p
	}
	viaRef := newPolicy("via-ref", gatewayID, func(p *aiconfigurationv1alpha1.AIGatewayPolicy) {
		p.Spec.APISpec.CustomPolicyRef = aiconfigurationv1alpha1.AIGatewayCustomPolicyRef{Name: "custom"}
	})
	viaType := newPolicy("via-type", gatewayID, func(p *aiconfigurationv1alpha1.AIGatewayPolicy) {
		p.Spec.APISpec.Type = "custom-konnect"
	})
	otherGateway := newPolicy("other-gateway", "gateway-2", func(p *aiconfigurationv1alpha1.AIGatewayPolicy) {
		p.Spec.APISpec.Type = "custom-konnect"
	})
	builtIn := newPolicy("built-in", gatewayID, func(p *aiconfigurationv1alpha1.AIGatewayPolicy) {
		p.Spec.APISpec.Type = "response-transformer"
	})

	builder := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(viaRef, viaType, otherGateway, builtIn)
	for _, opt := range index.OptionsForAIGatewayPolicy() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	enqueue := enqueueAIGatewayPolicyForAIGatewayCustomPolicy(builder.Build())

	t.Run("enqueues the policies using the custom policy through customPolicyRef or type", func(t *testing.T) {
		assert.ElementsMatch(t,
			[]reconcile.Request{
				{Namespace: "default", Name: "via-ref"},
				{Namespace: "default", Name: "via-type"},
			},
			enqueue(t.Context(), customPolicy),
		)
	})

	t.Run("custom policy not yet in an AI Gateway enqueues only the references", func(t *testing.T) {
		unattached := customPolicy.DeepCopy()
		unattached.SetGatewayID("")
		assert.Equal(t,
			[]reconcile.Request{{Namespace: "default", Name: "via-ref"}},
			enqueue(t.Context(), unattached),
		)
	})
}
