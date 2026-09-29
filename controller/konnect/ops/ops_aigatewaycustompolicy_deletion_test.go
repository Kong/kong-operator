package ops

import (
	"errors"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	sdkmocks "github.com/Kong/sdk-konnect-go/test/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/index"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
)

func TestDeleteAIGatewayCustomPolicyGuarded(t *testing.T) {
	t.Parallel()

	const (
		gatewayID       = "gateway-1"
		customPolicyID  = "custom-policy-id"
		customPolicyKon = "my-streaming-policy"
	)

	// Each subtest uses its own instance: the ops layer mutates the
	// BadRequestError in place (clearing Instance), and subtests run in parallel.
	newBadRequestErr := func() *sdkkonnecterrs.BadRequestError {
		return &sdkkonnecterrs.BadRequestError{
			Status: 400,
			Title:  "Bad Request",
			Detail: "server wording is not part of the contract",
		}
	}
	newCustomPolicy := func() *aiconfigurationv1alpha1.AIGatewayCustomPolicy {
		obj := testAIGatewayCustomPolicy()
		obj.SetKonnectID(customPolicyID)
		return obj
	}
	newPolicy := func(name string, mutate func(*aiconfigurationv1alpha1.AIGatewayPolicy)) *aiconfigurationv1alpha1.AIGatewayPolicy {
		p := &aiconfigurationv1alpha1.AIGatewayPolicy{
			Name: name, Namespace: "default",
		}
		p.SetGatewayID(gatewayID)
		mutate(p)
		return p
	}
	newClient := func(t *testing.T, objs ...client.Object) client.Client {
		t.Helper()
		builder := fake.NewClientBuilder().WithScheme(managerscheme.Get()).WithObjects(objs...)
		for _, opt := range index.OptionsForAIGatewayPolicy() {
			builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
		}
		return builder.Build()
	}
	listPolicies := func(policies ...sdkkonnectcomp.AIGatewayPolicy) *sdkkonnectops.ListAiGatewayPoliciesResponse {
		return &sdkkonnectops.ListAiGatewayPoliciesResponse{
			ListAIGatewayPoliciesResponse: &sdkkonnectcomp.ListAIGatewayPoliciesResponse{Data: policies},
		}
	}

	t.Run("delete succeeds when the custom policy is unused", func(t *testing.T) {
		t.Parallel()

		customPoliciesSDK := sdkmocks.NewMockAIGatewayCustomPoliciesSDK(t)
		policiesSDK := sdkmocks.NewMockAIGatewayPoliciesSDK(t)
		customPoliciesSDK.EXPECT().
			DeleteAiGatewayCustomPolicy(mock.Anything, gatewayID, customPolicyID).
			Return(&sdkkonnectops.DeleteAiGatewayCustomPolicyResponse{}, nil).
			Once()

		require.NoError(t, deleteAIGatewayCustomPolicyGuarded(t.Context(), customPoliciesSDK, policiesSDK, newClient(t), newCustomPolicy()))
	})

	t.Run("in-use deletion names the policies in the cluster using it", func(t *testing.T) {
		t.Parallel()

		customPolicy := newCustomPolicy()
		viaRef := newPolicy("via-ref", func(p *aiconfigurationv1alpha1.AIGatewayPolicy) {
			p.Spec.APISpec.CustomPolicyRef = aiconfigurationv1alpha1.AIGatewayCustomPolicyRef{Name: customPolicy.GetName()}
			p.SetKonnectID("id-via-ref")
		})
		viaType := newPolicy("via-type", func(p *aiconfigurationv1alpha1.AIGatewayPolicy) {
			p.Spec.APISpec.Type = customPolicyKon
			p.SetKonnectID("id-via-type")
		})
		// Referencing the custom policy without being in Konnect (e.g. not
		// programmed yet) does not block the deletion.
		notInKonnect := newPolicy("not-in-konnect", func(p *aiconfigurationv1alpha1.AIGatewayPolicy) {
			p.Spec.APISpec.CustomPolicyRef = aiconfigurationv1alpha1.AIGatewayCustomPolicyRef{Name: customPolicy.GetName()}
		})
		otherGateway := newPolicy("other-gateway", func(p *aiconfigurationv1alpha1.AIGatewayPolicy) {
			p.Spec.APISpec.Type = customPolicyKon
			p.SetGatewayID("gateway-2")
			p.SetKonnectID("id-other-gateway")
		})
		unrelated := newPolicy("unrelated", func(p *aiconfigurationv1alpha1.AIGatewayPolicy) {
			p.Spec.APISpec.Type = "response-transformer"
			p.SetKonnectID("id-unrelated")
		})

		customPoliciesSDK := sdkmocks.NewMockAIGatewayCustomPoliciesSDK(t)
		policiesSDK := sdkmocks.NewMockAIGatewayPoliciesSDK(t)
		customPoliciesSDK.EXPECT().
			DeleteAiGatewayCustomPolicy(mock.Anything, gatewayID, customPolicyID).
			Return(nil, newBadRequestErr()).
			Once()
		policiesSDK.EXPECT().
			ListAiGatewayPolicies(mock.Anything, mock.MatchedBy(func(r sdkkonnectops.ListAiGatewayPoliciesRequest) bool {
				return r.GatewayID == gatewayID
			})).
			Return(listPolicies(
				sdkkonnectcomp.AIGatewayPolicy{ID: "id-via-ref", Name: "via-ref", Type: customPolicyKon},
				sdkkonnectcomp.AIGatewayPolicy{ID: "id-via-type", Name: "via-type", Type: customPolicyKon},
				sdkkonnectcomp.AIGatewayPolicy{ID: "id-unrelated", Name: "unrelated", Type: "response-transformer"},
			), nil).
			Once()

		err := deleteAIGatewayCustomPolicyGuarded(
			t.Context(), customPoliciesSDK, policiesSDK,
			newClient(t, customPolicy, viaRef, viaType, notInKonnect, otherGateway, unrelated), customPolicy,
		)

		var inUse AIGatewayCustomPolicyInUseError
		require.ErrorAs(t, err, &inUse)
		assert.Equal(t, customPolicyKon, inUse.CustomPolicyName)
		assert.Equal(t, []string{"default/via-ref", "default/via-type"}, inUse.Users)
		assert.Equal(t, []string{"via-ref", "via-type"}, inUse.KonnectPolicies)
		assert.Empty(t, inUse.UnmanagedKonnectPolicies)
		assert.Equal(t,
			"deletion blocked: the custom policy is in use by AIGatewayPolicy default/via-ref, default/via-type; "+
				"delete them or stop using the custom policy and the deletion will proceed automatically",
			inUse.DeletionBlockedMessage(),
		)

		// The reconciler reports it as a blocked deletion, not a failure.
		var blocked DeletionBlockedError
		require.ErrorAs(t, err, &blocked)
	})

	t.Run("in-use deletion by policies managed outside the cluster names the Konnect policies", func(t *testing.T) {
		t.Parallel()

		customPoliciesSDK := sdkmocks.NewMockAIGatewayCustomPoliciesSDK(t)
		policiesSDK := sdkmocks.NewMockAIGatewayPoliciesSDK(t)
		customPoliciesSDK.EXPECT().
			DeleteAiGatewayCustomPolicy(mock.Anything, gatewayID, customPolicyID).
			Return(nil, newBadRequestErr()).
			Once()
		// Two pages: the probe follows the cursor, which Konnect returns
		// inside the next page URI.
		next := "/v1/ai-gateways/" + gatewayID + "/policies?page%5Bafter%5D=cursor-2&page%5Bsize%5D=100"
		policiesSDK.EXPECT().
			ListAiGatewayPolicies(mock.Anything, mock.MatchedBy(func(r sdkkonnectops.ListAiGatewayPoliciesRequest) bool {
				return r.PageAfter == nil
			})).
			Return(&sdkkonnectops.ListAiGatewayPoliciesResponse{
				ListAIGatewayPoliciesResponse: &sdkkonnectcomp.ListAIGatewayPoliciesResponse{
					Data: []sdkkonnectcomp.AIGatewayPolicy{{Name: "unrelated", Type: "response-transformer"}},
					Meta: sdkkonnectcomp.CursorMeta{Page: sdkkonnectcomp.CursorMetaPage{Next: &next}},
				},
			}, nil).
			Once()
		policiesSDK.EXPECT().
			ListAiGatewayPolicies(mock.Anything, mock.MatchedBy(func(r sdkkonnectops.ListAiGatewayPoliciesRequest) bool {
				return r.PageAfter != nil && *r.PageAfter == "cursor-2"
			})).
			Return(listPolicies(sdkkonnectcomp.AIGatewayPolicy{Name: "created-in-konnect", Type: customPolicyKon}), nil).
			Once()

		err := deleteAIGatewayCustomPolicyGuarded(t.Context(), customPoliciesSDK, policiesSDK, newClient(t), newCustomPolicy())

		var inUse AIGatewayCustomPolicyInUseError
		require.ErrorAs(t, err, &inUse)
		assert.Empty(t, inUse.Users)
		assert.Equal(t, []string{"created-in-konnect"}, inUse.KonnectPolicies)
		assert.Contains(t, inUse.DeletionBlockedMessage(), "in use by Konnect policies created-in-konnect")
	})

	t.Run("in-use deletion names both the policies in the cluster and the Konnect-only ones", func(t *testing.T) {
		t.Parallel()

		customPolicy := newCustomPolicy()
		viaRef := newPolicy("via-ref", func(p *aiconfigurationv1alpha1.AIGatewayPolicy) {
			p.Spec.APISpec.CustomPolicyRef = aiconfigurationv1alpha1.AIGatewayCustomPolicyRef{Name: customPolicy.GetName()}
			p.SetKonnectID("id-via-ref")
		})

		customPoliciesSDK := sdkmocks.NewMockAIGatewayCustomPoliciesSDK(t)
		policiesSDK := sdkmocks.NewMockAIGatewayPoliciesSDK(t)
		customPoliciesSDK.EXPECT().
			DeleteAiGatewayCustomPolicy(mock.Anything, gatewayID, customPolicyID).
			Return(nil, newBadRequestErr()).
			Once()
		policiesSDK.EXPECT().
			ListAiGatewayPolicies(mock.Anything, mock.Anything).
			Return(listPolicies(
				sdkkonnectcomp.AIGatewayPolicy{ID: "id-via-ref", Name: "via-ref", Type: customPolicyKon},
				sdkkonnectcomp.AIGatewayPolicy{ID: "id-konnect-only", Name: "konnect-only", Type: customPolicyKon},
			), nil).
			Once()

		err := deleteAIGatewayCustomPolicyGuarded(
			t.Context(), customPoliciesSDK, policiesSDK, newClient(t, customPolicy, viaRef), customPolicy,
		)

		var inUse AIGatewayCustomPolicyInUseError
		require.ErrorAs(t, err, &inUse)
		assert.Equal(t, []string{"default/via-ref"}, inUse.Users)
		assert.Equal(t, []string{"konnect-only"}, inUse.UnmanagedKonnectPolicies)
		assert.Equal(t,
			"deletion blocked: the custom policy is in use by AIGatewayPolicy default/via-ref, and by Konnect policies "+
				"konnect-only, which are not managed from this cluster; delete them or stop using the custom policy "+
				"and the deletion will proceed automatically",
			inUse.DeletionBlockedMessage(),
		)
	})

	t.Run("a next page cursor that does not advance stops the probe", func(t *testing.T) {
		t.Parallel()

		customPoliciesSDK := sdkmocks.NewMockAIGatewayCustomPoliciesSDK(t)
		policiesSDK := sdkmocks.NewMockAIGatewayPoliciesSDK(t)
		customPoliciesSDK.EXPECT().
			DeleteAiGatewayCustomPolicy(mock.Anything, gatewayID, customPolicyID).
			Return(nil, newBadRequestErr()).
			Once()
		next := "/v1/ai-gateways/" + gatewayID + "/policies?page%5Bafter%5D=cursor-2"
		// Every page points to the same next page: the probe must stop after
		// requesting it once rather than loop forever.
		policiesSDK.EXPECT().
			ListAiGatewayPolicies(mock.Anything, mock.Anything).
			Return(&sdkkonnectops.ListAiGatewayPoliciesResponse{
				ListAIGatewayPoliciesResponse: &sdkkonnectcomp.ListAIGatewayPoliciesResponse{
					Data: []sdkkonnectcomp.AIGatewayPolicy{{Name: "uses-it", Type: customPolicyKon}},
					Meta: sdkkonnectcomp.CursorMeta{Page: sdkkonnectcomp.CursorMetaPage{Next: &next}},
				},
			}, nil).
			Twice()

		err := deleteAIGatewayCustomPolicyGuarded(t.Context(), customPoliciesSDK, policiesSDK, newClient(t), newCustomPolicy())
		require.Error(t, err)
		_, blocked := errors.AsType[DeletionBlockedError](err)
		assert.False(t, blocked)
	})

	t.Run("bad request not caused by policies is returned as is", func(t *testing.T) {
		t.Parallel()

		customPoliciesSDK := sdkmocks.NewMockAIGatewayCustomPoliciesSDK(t)
		policiesSDK := sdkmocks.NewMockAIGatewayPoliciesSDK(t)
		customPoliciesSDK.EXPECT().
			DeleteAiGatewayCustomPolicy(mock.Anything, gatewayID, customPolicyID).
			Return(nil, newBadRequestErr()).
			Once()
		policiesSDK.EXPECT().
			ListAiGatewayPolicies(mock.Anything, mock.Anything).
			Return(listPolicies(sdkkonnectcomp.AIGatewayPolicy{Name: "unrelated", Type: "response-transformer"}), nil).
			Once()

		err := deleteAIGatewayCustomPolicyGuarded(t.Context(), customPoliciesSDK, policiesSDK, newClient(t), newCustomPolicy())
		require.Error(t, err)
		_, blocked := errors.AsType[DeletionBlockedError](err)
		assert.False(t, blocked)
	})

	t.Run("failure to list policies returns the delete error", func(t *testing.T) {
		t.Parallel()

		customPoliciesSDK := sdkmocks.NewMockAIGatewayCustomPoliciesSDK(t)
		policiesSDK := sdkmocks.NewMockAIGatewayPoliciesSDK(t)
		customPoliciesSDK.EXPECT().
			DeleteAiGatewayCustomPolicy(mock.Anything, gatewayID, customPolicyID).
			Return(nil, newBadRequestErr()).
			Once()
		policiesSDK.EXPECT().
			ListAiGatewayPolicies(mock.Anything, mock.Anything).
			Return(nil, errors.New("list failed")).
			Once()

		err := deleteAIGatewayCustomPolicyGuarded(t.Context(), customPoliciesSDK, policiesSDK, newClient(t), newCustomPolicy())
		require.Error(t, err)
		_, blocked := errors.AsType[DeletionBlockedError](err)
		assert.False(t, blocked)
	})

	t.Run("non bad request errors are not probed", func(t *testing.T) {
		t.Parallel()

		customPoliciesSDK := sdkmocks.NewMockAIGatewayCustomPoliciesSDK(t)
		policiesSDK := sdkmocks.NewMockAIGatewayPoliciesSDK(t)
		customPoliciesSDK.EXPECT().
			DeleteAiGatewayCustomPolicy(mock.Anything, gatewayID, customPolicyID).
			Return(nil, errors.New("connection reset")).
			Once()

		require.Error(t, deleteAIGatewayCustomPolicyGuarded(t.Context(), customPoliciesSDK, policiesSDK, newClient(t), newCustomPolicy()))
	})
}

func TestClearInstanceFromErrorPreservesCustomPolicyInUseWrapper(t *testing.T) {
	t.Parallel()

	badRequest := &sdkkonnecterrs.BadRequestError{
		Status:   400,
		Detail:   "server wording is not part of the contract",
		Instance: "trace-id",
	}
	err := AIGatewayCustomPolicyInUseError{
		CustomPolicyName: "my-custom-policy",
		KonnectPolicies:  []string{"uses-it"},
		Err:              badRequest,
	}

	cleared := ClearInstanceFromError(err)
	inUse, ok := errors.AsType[AIGatewayCustomPolicyInUseError](cleared)
	require.True(t, ok, "wrapper must be preserved, got %T (%v)", cleared, cleared)
	assert.Equal(t, "my-custom-policy", inUse.CustomPolicyName)
	assert.Empty(t, badRequest.Instance, "instance must be cleared in place")
}
