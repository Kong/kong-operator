package ops

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

func TestConsumePolicyChainGuardedOperations(t *testing.T) {
	t.Parallel()
	for _, op := range []Op{CreateOp, UpdateOp} {
		for _, tt := range []struct {
			name                                   string
			desired, remote, response              []string
			getErr, putErr                         error
			nilGet, nilGetBody, nilPut, nilPutBody bool
			put                                    bool
			err                                    string
		}{
			{name: "nested group reorder", desired: []string{"parent", "second", "first", "root"}, remote: []string{"root", "parent", "first", "second"}, response: []string{"parent", "second", "first", "root"}, put: true},
			{name: "unchanged", desired: []string{"parent", "child"}, remote: []string{"parent", "child"}},
			{name: "empty", desired: []string{}, remote: []string{}},
			{name: "missing snapshot", err: "supplied explicitly"},
			{name: "roots only", desired: []string{"parent"}, remote: []string{"parent", "child"}, err: "complete remote consume policy chain"},
			{name: "children only", desired: []string{"child"}, remote: []string{"parent", "child"}, err: "complete remote consume policy chain"},
			{name: "unknown ID", desired: []string{"foreign"}, remote: []string{"parent"}, err: "complete remote consume policy chain"},
			{name: "empty cannot clear", desired: []string{}, remote: []string{"parent"}, err: "complete remote consume policy chain"},
			{name: "duplicate desired", desired: []string{"parent", "parent"}, remote: []string{"parent"}, err: "unique, non-empty"},
			{name: "duplicate remote", desired: []string{"parent"}, remote: []string{"parent", "parent"}, err: "unique, non-empty"},
			{name: "empty ID", desired: []string{""}, remote: []string{""}, err: "unique, non-empty"},
			{name: "null remote", desired: []string{}, err: "explicit policy list"},
			{name: "GET failure", desired: []string{}, getErr: errors.New("GET unavailable"), err: "GET unavailable"},
			{name: "GET 404", desired: []string{}, getErr: &sdkkonnecterrs.SDKError{StatusCode: http.StatusNotFound, Message: "virtual cluster not found"}, err: "virtual cluster not found"},
			{name: "GET nil response", desired: []string{}, nilGet: true, err: ErrNilResponse.Error()},
			{name: "GET nil body", desired: []string{}, nilGetBody: true, err: ErrNilResponse.Error()},
			{name: "PUT conflict", desired: []string{"second", "first"}, remote: []string{"first", "second"}, put: true, putErr: &sdkkonnecterrs.SDKError{StatusCode: http.StatusConflict, Message: "concurrent policy-set change"}, err: "concurrent policy-set change"},
			{name: "PUT rate limit", desired: []string{"second", "first"}, remote: []string{"first", "second"}, put: true, putErr: &sdkkonnecterrs.SDKError{
				StatusCode: http.StatusTooManyRequests, Message: "rate limited",
				RawResponse: &http.Response{Header: http.Header{"Retry-After": []string{"3"}}},
			}, err: "rate limited"},
			{name: "PUT nil response", desired: []string{"second", "first"}, remote: []string{"first", "second"}, put: true, nilPut: true, err: ErrNilResponse.Error()},
			{name: "PUT nil body", desired: []string{"second", "first"}, remote: []string{"first", "second"}, put: true, nilPutBody: true, err: ErrNilResponse.Error()},
			{name: "PUT null policies", desired: []string{"second", "first"}, remote: []string{"first", "second"}, put: true, err: "does not match"},
			{name: "PUT ignored order", desired: []string{"second", "first"}, remote: []string{"first", "second"}, response: []string{"first", "second"}, put: true, err: "does not match"},
			{name: "PUT incomplete set", desired: []string{"second", "first"}, remote: []string{"first", "second"}, response: []string{"second"}, put: true, err: "does not match"},
			{name: "PUT unknown ID", desired: []string{"second", "first"}, remote: []string{"first", "second"}, response: []string{"second", "foreign"}, put: true, err: "does not match"},
			{name: "PUT duplicate ID", desired: []string{"second", "first"}, remote: []string{"first", "second"}, response: []string{"second", "second"}, put: true, err: "does not match"},
		} {
			t.Run(string(op)+"/"+tt.name, func(t *testing.T) {
				chain := &configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChain{Name: "chain", Namespace: "default", UID: "uid", Generation: 1}
				chain.SetGatewayID("gateway-id")
				chain.SetVirtualClusterID("vc-id")
				ctx := WithResolvedPolicyChainIDs(t.Context(), chain, tt.desired)
				cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithInterceptorFuncs(interceptor.Funcs{
					Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
						t.Fatal("operation must reuse its validated reference snapshots")
						return nil
					},
				}).Build()
				sdk := sdkmocks.NewMockSDKWrapperWithT(t)
				gotRemote := false
				if tt.desired != nil {
					var response *sdkkonnectops.GetEventGatewayVirtualClusterConsumePolicyChainResponse
					if !tt.nilGet {
						response = &sdkkonnectops.GetEventGatewayVirtualClusterConsumePolicyChainResponse{}
						if !tt.nilGetBody {
							response.EventGatewayPolicyChainResponse = &sdkkonnectcomp.EventGatewayPolicyChainResponse{Policies: tt.remote}
						}
					}
					sdk.EventGatewayVirtualClusterConsumePoliciesSDK.EXPECT().GetEventGatewayVirtualClusterConsumePolicyChain(mock.Anything, "gateway-id", "vc-id").
						Run(func(context.Context, string, string, ...sdkkonnectops.Option) { gotRemote = true }).
						Return(response, tt.getErr).Once()
				}
				if tt.put {
					var response *sdkkonnectops.UpdateEventGatewayVirtualClusterConsumePolicyChainResponse
					if !tt.nilPut {
						response = &sdkkonnectops.UpdateEventGatewayVirtualClusterConsumePolicyChainResponse{}
						if !tt.nilPutBody {
							response.EventGatewayPolicyChainResponse = &sdkkonnectcomp.EventGatewayPolicyChainResponse{Policies: tt.response}
						}
					}
					sdk.EventGatewayVirtualClusterConsumePoliciesSDK.EXPECT().UpdateEventGatewayVirtualClusterConsumePolicyChain(mock.Anything,
						sdkkonnectops.UpdateEventGatewayVirtualClusterConsumePolicyChainRequest{
							GatewayID: "gateway-id", VirtualClusterID: "vc-id",
							UpdateEventGatewayPolicyChainRequest: &sdkkonnectcomp.UpdateEventGatewayPolicyChainRequest{Policies: tt.desired},
						}).Run(func(context.Context, sdkkonnectops.UpdateEventGatewayVirtualClusterConsumePolicyChainRequest, ...sdkkonnectops.Option) {
						require.True(t, gotRemote)
					}).Return(response, tt.putErr).Once()
				}
				var err error
				if op == CreateOp {
					_, err = Create(ctx, sdk, cl, &metricsmocks.MockRecorder{}, chain)
				} else {
					_, err = Update(ctx, sdk, 0, cl, &metricsmocks.MockRecorder{}, chain)
				}
				if tt.getErr != nil {
					require.ErrorIs(t, err, tt.getErr)
				}
				if tt.putErr != nil {
					require.ErrorIs(t, err, tt.putErr)
				}
				if tt.name == "PUT rate limit" {
					limited, ok := errors.AsType[RateLimitError](err)
					require.True(t, ok)
					require.Equal(t, 3*time.Second, limited.RetryAfter)
				}
				condition := apimeta.FindStatusCondition(chain.GetConditions(), "Programmed")
				require.NotNil(t, condition)
				require.Empty(t, chain.GetKonnectID())
				if tt.err != "" {
					require.ErrorContains(t, err, tt.err)
					require.Equal(t, metav1.ConditionFalse, condition.Status)
				} else {
					require.NoError(t, err)
					require.Equal(t, metav1.ConditionTrue, condition.Status)
				}
			})
		}
	}
}

func TestConsumePolicyChainChecksWithinSyncPeriod(t *testing.T) {
	t.Parallel()
	for _, period := range []time.Duration{0, time.Hour} {
		for _, tt := range []struct {
			name            string
			desired, remote []string
			put             bool
			err             string
		}{
			{name: "unchanged", desired: []string{"parent", "child"}, remote: []string{"parent", "child"}},
			{name: "unchanged empty", desired: []string{}, remote: []string{}},
			{name: "drift", desired: []string{"second", "first"}, remote: []string{"first", "second"}, put: true},
			{name: "recreated ID", desired: []string{"parent", "new-child"}, remote: []string{"new-child", "parent"}, put: true},
			{name: "unlisted ID", desired: []string{"parent"}, remote: []string{"parent", "extra"}, err: "complete remote consume policy chain"},
		} {
			t.Run(period.String()+"/"+tt.name, func(t *testing.T) {
				chain := &configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChain{Name: "chain", Namespace: "default", Generation: 1}
				chain.SetGatewayID("gateway-id")
				chain.SetVirtualClusterID("vc-id")
				SetKonnectEntityProgrammedConditionTrue(chain)
				before := *apimeta.FindStatusCondition(chain.GetConditions(), "Programmed")
				cl := fake.NewClientBuilder().WithScheme(scheme.Get()).Build()
				sdk := sdkmocks.NewMockSDKWrapperWithT(t)
				sdk.EventGatewayVirtualClusterConsumePoliciesSDK.EXPECT().GetEventGatewayVirtualClusterConsumePolicyChain(mock.Anything, "gateway-id", "vc-id").
					Return(&sdkkonnectops.GetEventGatewayVirtualClusterConsumePolicyChainResponse{
						EventGatewayPolicyChainResponse: &sdkkonnectcomp.EventGatewayPolicyChainResponse{Policies: tt.remote},
					}, nil).Once()
				if tt.put {
					sdk.EventGatewayVirtualClusterConsumePoliciesSDK.EXPECT().UpdateEventGatewayVirtualClusterConsumePolicyChain(mock.Anything, mock.Anything).
						Return(&sdkkonnectops.UpdateEventGatewayVirtualClusterConsumePolicyChainResponse{
							EventGatewayPolicyChainResponse: &sdkkonnectcomp.EventGatewayPolicyChainResponse{Policies: tt.desired},
						}, nil).Once()
				}
				result, err := Update(WithResolvedPolicyChainIDs(t.Context(), chain, tt.desired), sdk, period, cl, &metricsmocks.MockRecorder{}, chain)
				if tt.err != "" {
					require.ErrorContains(t, err, tt.err)
					require.True(t, apimeta.IsStatusConditionFalse(chain.GetConditions(), "Programmed"))
				} else {
					require.NoError(t, err)
					if !tt.put {
						require.Equal(t, before, *apimeta.FindStatusCondition(chain.GetConditions(), "Programmed"))
						require.LessOrEqual(t, result.RequeueAfter, period)
						if period > 0 {
							require.Positive(t, result.RequeueAfter)
						}
					}
				}
			})
		}
	}
}

func TestConsumePolicyChainAncestryAndRetention(t *testing.T) {
	t.Parallel()
	cl := fake.NewClientBuilder().WithScheme(scheme.Get()).Build()
	sdk := sdkmocks.NewMockSDKWrapperWithT(t)
	chain := &configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChain{}
	ctx := WithResolvedPolicyChainIDs(t.Context(), chain, []string{})
	_, err := applyEventGatewayVirtualClusterConsumePolicyChain(ctx, cl, sdk.GetEventGatewayVirtualClusterConsumePoliciesSDK(), chain, CreateOp)
	require.ErrorContains(t, err, "Gateway")
	chain.SetGatewayID("gateway-id")
	_, err = applyEventGatewayVirtualClusterConsumePolicyChain(ctx, cl, sdk.GetEventGatewayVirtualClusterConsumePoliciesSDK(), chain, UpdateOp)
	require.ErrorContains(t, err, "EventGatewayVirtualCluster")
	require.NoError(t, Delete(ctx, sdk, cl, &metricsmocks.MockRecorder{}, chain))
}

func TestConsumePolicyChainRejectsForeignOrganizationBeforeSDK(t *testing.T) {
	t.Parallel()
	for _, op := range []Op{CreateOp, UpdateOp} {
		t.Run(string(op), func(t *testing.T) {
			chain := &configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChain{Name: "chain", Namespace: "default", Generation: 1,
				Spec: configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChainSpec{
					EventGatewayVirtualClusterRef: commonv1alpha1.ObjectRef{
						Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
						NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "vc"},
					},
					APISpec: configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyChainAPISpec{
						Policies: []configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyRef{{Name: "policy"}},
					},
				},
			}
			chain.SetGatewayID("gateway-id")
			chain.SetVirtualClusterID("vc-id")
			chain.GetKonnectStatus().SetOrgID("chain-org")
			policy := &configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy{Name: "policy", Namespace: chain.Namespace, Generation: 1,
				Spec: configurationv1alpha1.EventGatewayVirtualClusterConsumePolicySpec{
					EventGatewayVirtualClusterRef: chain.GetParentRef(),
					APISpec: configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyAPISpec{
						EventGatewayVirtualClusterConsumePolicyConfig: &configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyConfig{
							Type:                   configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyConfigTypeSkipRecordPolicyCreate,
							SkipRecordPolicyCreate: &configurationv1alpha1.EventGatewaySkipRecordPolicyCreate{Name: "policy"},
						},
					},
				},
			}
			policy.SetKonnectID("policy-id")
			policy.SetGatewayID("gateway-id")
			policy.SetVirtualClusterID("vc-id")
			policy.GetKonnectStatus().SetOrgID("foreign-org")
			SetKonnectEntityProgrammedConditionTrue(policy)
			cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(policy).Build()
			sdk := sdkmocks.NewMockSDKWrapperWithT(t)
			var err error
			if op == CreateOp {
				_, err = Create(t.Context(), sdk, cl, &metricsmocks.MockRecorder{}, chain)
			} else {
				_, err = Update(t.Context(), sdk, time.Hour, cl, &metricsmocks.MockRecorder{}, chain)
			}
			require.ErrorContains(t, err, "policy default/policy belongs to Konnect organization foreign-org, expected organization chain-org")
			_, invalid := errors.AsType[configurationv1alpha1.PolicyChainInvalidError](err)
			require.True(t, invalid)
			require.True(t, apimeta.IsStatusConditionFalse(chain.GetConditions(), "Programmed"))
			sdk.EventGatewayVirtualClusterConsumePoliciesSDK.AssertNotCalled(t, "GetEventGatewayVirtualClusterConsumePolicyChain", mock.Anything, mock.Anything, mock.Anything)
			sdk.EventGatewayVirtualClusterConsumePoliciesSDK.AssertNotCalled(t, "UpdateEventGatewayVirtualClusterConsumePolicyChain", mock.Anything, mock.Anything)
		})
	}
}
