package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestAIGatewayPolicy_CustomPolicyRef(t *testing.T) {
	const gatewayID = "gateway-1"

	newPolicy := func() *AIGatewayPolicy {
		p := &AIGatewayPolicy{
			Name: "uses-custom-policy", Namespace: "default",
			Spec: AIGatewayPolicySpec{
				APISpec: AIGatewayPolicyAPISpec{
					Name:        "uses-custom-policy",
					DisplayName: "Uses the custom policy",
					Enabled:     "Enabled",
					Global:      "Enabled",
					Config: AIGatewayPolicyConfigDataSource{
						Type:  "inline",
						Value: &apiextensionsv1.JSON{Raw: []byte(`{"header_name":"X-Custom"}`)},
					},
				},
			},
		}
		p.SetGatewayID(gatewayID)
		return p
	}
	newCustomPolicy := func(konnectID string) *AIGatewayCustomPolicy {
		cp := &AIGatewayCustomPolicy{
			Name: "my-custom-policy", Namespace: "default",
			Spec: AIGatewayCustomPolicySpec{
				APISpec: AIGatewayCustomPolicyAPISpec{
					AIGatewayCustomPolicyConfig: &AIGatewayCustomPolicyConfig{
						Type: AIGatewayCustomPolicyConfigTypeStreaming,
						Streaming: &CreateAIGatewayCustomPolicyStreamingRequest{
							Name:        "my-streaming-custom-policy",
							DisplayName: "My streaming custom policy",
							Schema:      ConfigMapDataSource{Type: ConfigMapDataSourceTypeInline, Value: new("return {}")},
							Handler:     ConfigMapDataSource{Type: ConfigMapDataSourceTypeInline, Value: new("return {}")},
						},
					},
				},
			},
		}
		cp.SetGatewayID(gatewayID)
		cp.SetKonnectID(konnectID)
		return cp
	}
	newClient := func(t *testing.T, objs ...client.Object) client.Client {
		scheme := runtime.NewScheme()
		require.NoError(t, AddToScheme(scheme))
		return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	}

	t.Run("resolved custom policy's Konnect name is sent as type", func(t *testing.T) {
		policy := newPolicy()
		policy.Spec.APISpec.CustomPolicyRef = AIGatewayCustomPolicyRef{Name: "my-custom-policy"}
		cl := newClient(t, newCustomPolicy("custom-policy-konnect-id"))

		create, err := policy.ToCreateAIGatewayPolicyRequest(t.Context(), cl)
		require.NoError(t, err)
		assert.Equal(t, "my-streaming-custom-policy", create.GetType())
		assert.True(t, create.GetGlobal() != nil && *create.GetGlobal())

		update, err := policy.ToUpdateAIGatewayPolicyRequest(t.Context(), cl)
		require.NoError(t, err)
		assert.Equal(t, "my-streaming-custom-policy", update.GetType())
	})

	t.Run("literal type is sent unchanged when no reference is set", func(t *testing.T) {
		policy := newPolicy()
		policy.Spec.APISpec.Type = "response-transformer"
		cl := newClient(t)

		create, err := policy.ToCreateAIGatewayPolicyRequest(t.Context(), cl)
		require.NoError(t, err)
		assert.Equal(t, "response-transformer", create.GetType())
	})

	t.Run("not yet programmed custom policy is reported", func(t *testing.T) {
		policy := newPolicy()
		policy.Spec.APISpec.CustomPolicyRef = AIGatewayCustomPolicyRef{Name: "my-custom-policy"}
		cl := newClient(t, newCustomPolicy(""))

		_, err := policy.ToCreateAIGatewayPolicyRequest(t.Context(), cl)
		var notProgrammed ReferenceNotProgrammedError
		require.ErrorAs(t, err, &notProgrammed)
	})

	t.Run("custom policy being deleted is reported", func(t *testing.T) {
		policy := newPolicy()
		policy.Spec.APISpec.CustomPolicyRef = AIGatewayCustomPolicyRef{Name: "my-custom-policy"}
		deleting := newCustomPolicy("custom-policy-konnect-id")
		deleting.Finalizers = []string{"konnect.konghq.com/delete"}
		deleting.DeletionTimestamp = new(metav1.Now())
		cl := newClient(t, deleting)

		_, err := policy.ToCreateAIGatewayPolicyRequest(t.Context(), cl)
		var beingDeleted ReferenceBeingDeletedError
		require.ErrorAs(t, err, &beingDeleted)
		assert.Equal(t, "my-custom-policy", beingDeleted.Name)
	})

	t.Run("missing custom policy is reported", func(t *testing.T) {
		policy := newPolicy()
		policy.Spec.APISpec.CustomPolicyRef = AIGatewayCustomPolicyRef{Name: "my-custom-policy"}
		cl := newClient(t)

		_, err := policy.ToCreateAIGatewayPolicyRequest(t.Context(), cl)
		var notFound ReferenceNotFoundError
		require.ErrorAs(t, err, &notFound)
	})

	t.Run("custom policy on another gateway is rejected", func(t *testing.T) {
		policy := newPolicy()
		policy.Spec.APISpec.CustomPolicyRef = AIGatewayCustomPolicyRef{Name: "my-custom-policy"}
		other := newCustomPolicy("custom-policy-konnect-id")
		other.SetGatewayID("gateway-2")
		cl := newClient(t, other)

		_, err := policy.ToCreateAIGatewayPolicyRequest(t.Context(), cl)
		var differentGateway ReferenceDifferentGatewayError
		require.ErrorAs(t, err, &differentGateway)
	})
}
