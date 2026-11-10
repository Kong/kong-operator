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

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

func TestProducePolicyChainGuardedOperations(t *testing.T) {
	t.Parallel()
	for _, op := range []Op{CreateOp, UpdateOp} {
		for _, tt := range []struct {
			name                                   string
			desired, remote, response              []string
			getErr, putErr                         error
			nilGet, nilGetBody, nilPut, nilPutBody bool
			put                                    bool
			wantErr                                string
			retryAfter                             time.Duration
		}{
			{name: "nested order", desired: []string{"parent", "second", "first", "root"}, remote: []string{"root", "parent", "first", "second"}, response: []string{"parent", "second", "first", "root"}, put: true},
			{name: "explicit empty", desired: []string{}, remote: []string{}, response: []string{}, put: true},
			{name: "missing snapshot is not empty", wantErr: "supplied explicitly"},
			{name: "roots only", desired: []string{"parent", "root"}, remote: []string{"parent", "child", "root"}, wantErr: "complete remote produce policy chain"},
			{name: "children only", desired: []string{"child"}, remote: []string{"parent", "child"}, wantErr: "complete remote produce policy chain"},
			{name: "foreign ID", desired: []string{"first"}, remote: []string{"foreign"}, wantErr: "complete remote produce policy chain"},
			{name: "empty cannot clear", desired: []string{}, remote: []string{"first"}, wantErr: "complete remote produce policy chain"},
			{name: "duplicate desired", desired: []string{"first", "first"}, remote: []string{"first"}, wantErr: "unique, non-empty"},
			{name: "duplicate remote", desired: []string{"first"}, remote: []string{"first", "first"}, wantErr: "unique, non-empty"},
			{name: "empty desired ID", desired: []string{""}, remote: []string{}, wantErr: "unique, non-empty"},
			{name: "empty remote ID", desired: []string{}, remote: []string{""}, wantErr: "unique, non-empty"},
			{name: "null remote list", desired: []string{}, wantErr: "explicit policy list"},
			{name: "GET failure", desired: []string{}, getErr: errors.New("GET unavailable"), wantErr: "GET unavailable"},
			{name: "GET 404", desired: []string{}, getErr: &sdkkonnecterrs.NotFoundError{Status: http.StatusNotFound, Title: "virtual cluster not found"}, wantErr: "virtual cluster not found"},
			{name: "GET generic 404", desired: []string{}, getErr: &sdkkonnecterrs.SDKError{StatusCode: http.StatusNotFound, Message: "virtual cluster not found"}, wantErr: "virtual cluster not found"},
			{name: "GET rate limit", desired: []string{}, getErr: &sdkkonnecterrs.SDKError{
				StatusCode: http.StatusTooManyRequests, Message: "rate limited",
				RawResponse: &http.Response{Header: http.Header{"Retry-After": []string{"3"}}},
			}, wantErr: "rate limited", retryAfter: 3 * time.Second},
			{name: "GET nil response", desired: []string{}, nilGet: true, wantErr: ErrNilResponse.Error()},
			{name: "GET nil body", desired: []string{}, nilGetBody: true, wantErr: ErrNilResponse.Error()},
			{name: "PUT conflict", desired: []string{}, remote: []string{}, put: true, putErr: &sdkkonnecterrs.SDKError{StatusCode: http.StatusConflict, Message: "concurrent policy-set change"}, wantErr: "concurrent policy-set change"},
			{name: "PUT rate limit", desired: []string{}, remote: []string{}, put: true, putErr: &sdkkonnecterrs.SDKError{
				StatusCode: http.StatusTooManyRequests, Message: "rate limited",
				RawResponse: &http.Response{Header: http.Header{"Retry-After": []string{"3"}}},
			}, wantErr: "rate limited", retryAfter: 3 * time.Second},
			{name: "PUT nil response", desired: []string{}, remote: []string{}, put: true, nilPut: true, wantErr: ErrNilResponse.Error()},
			{name: "PUT nil body", desired: []string{}, remote: []string{}, put: true, nilPutBody: true, wantErr: ErrNilResponse.Error()},
			{name: "PUT null is not empty", desired: []string{}, remote: []string{}, put: true, wantErr: "does not match"},
			{name: "PUT ignored order", desired: []string{"second", "first"}, remote: []string{"first", "second"}, response: []string{"first", "second"}, put: true, wantErr: "does not match"},
			{name: "PUT omitted child", desired: []string{"parent", "child"}, remote: []string{"parent", "child"}, response: []string{"parent"}, put: true, wantErr: "does not match"},
		} {
			t.Run(string(op)+"/"+tt.name, func(t *testing.T) {
				chain := &configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain{
					Name: "chain", Namespace: "default", UID: "chain-uid", Generation: 1,
				}
				chain.SetGatewayID("gateway-id")
				chain.SetVirtualClusterID("vc-id")
				ctx := WithResolvedPolicyChainIDs(t.Context(), chain, tt.desired)
				cl := fake.NewClientBuilder().WithScheme(scheme.Get()).
					WithInterceptorFuncs(interceptor.Funcs{Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
						t.Fatal("operation must reuse its validated snapshots")
						return nil
					}}).Build()
				sdk := sdkmocks.NewMockSDKWrapperWithT(t)
				policySDK := sdk.EventGatewayVirtualClusterProducePoliciesSDK
				if tt.desired != nil {
					var response *sdkkonnectops.GetEventGatewayVirtualClusterProducePolicyChainResponse
					if !tt.nilGet {
						response = &sdkkonnectops.GetEventGatewayVirtualClusterProducePolicyChainResponse{}
						if !tt.nilGetBody {
							response.EventGatewayPolicyChainResponse = &sdkkonnectcomp.EventGatewayPolicyChainResponse{Policies: tt.remote}
						}
					}
					policySDK.EXPECT().GetEventGatewayVirtualClusterProducePolicyChain(mock.Anything, "gateway-id", "vc-id").Return(response, tt.getErr).Once()
				}
				if tt.put {
					var response *sdkkonnectops.UpdateEventGatewayVirtualClusterProducePolicyChainResponse
					if !tt.nilPut {
						response = &sdkkonnectops.UpdateEventGatewayVirtualClusterProducePolicyChainResponse{}
						if !tt.nilPutBody {
							response.EventGatewayPolicyChainResponse = &sdkkonnectcomp.EventGatewayPolicyChainResponse{Policies: tt.response}
						}
					}
					policySDK.EXPECT().UpdateEventGatewayVirtualClusterProducePolicyChain(mock.Anything,
						sdkkonnectops.UpdateEventGatewayVirtualClusterProducePolicyChainRequest{
							GatewayID: "gateway-id", VirtualClusterID: "vc-id",
							UpdateEventGatewayPolicyChainRequest: &sdkkonnectcomp.UpdateEventGatewayPolicyChainRequest{Policies: tt.desired},
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
				if tt.retryAfter != 0 {
					limited, ok := errors.AsType[RateLimitError](err)
					require.True(t, ok)
					require.Equal(t, tt.retryAfter, limited.RetryAfter)
				}
				programmed := apimeta.FindStatusCondition(chain.Status.Conditions, "Programmed")
				require.NotNil(t, programmed)
				require.Empty(t, chain.GetKonnectID())
				require.False(t, chain.PersistsKonnectID())
				if tt.wantErr != "" {
					require.ErrorContains(t, err, tt.wantErr)
					require.Equal(t, metav1.ConditionFalse, programmed.Status)
				} else {
					require.NoError(t, err)
					require.Equal(t, metav1.ConditionTrue, programmed.Status)
				}
			})
		}
	}
}

func TestProducePolicyChainDeleteRetainsRemoteConfiguration(t *testing.T) {
	t.Parallel()
	chain := &configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain{Name: "chain", Namespace: "default"}
	cl := fake.NewClientBuilder().WithScheme(scheme.Get()).Build()
	sdk := sdkmocks.NewMockSDKWrapperWithT(t)
	require.NoError(t, Delete(context.Background(), sdk, cl, &metricsmocks.MockRecorder{}, chain))
}

func TestProducePolicyChainScopeWatchWithinSyncPeriod(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name            string
		desired, remote []string
		put             bool
		wantErr         string
	}{
		{name: "unchanged order", desired: []string{"parent", "child"}, remote: []string{"parent", "child"}},
		{name: "unchanged empty", desired: []string{}, remote: []string{}},
		{name: "ordering drift", desired: []string{"second", "first"}, remote: []string{"first", "second"}, put: true},
		{name: "recreated policy", desired: []string{"parent", "new-child"}, remote: []string{"new-child", "parent"}, put: true},
		{name: "unlisted policy", desired: []string{"parent"}, remote: []string{"parent", "extra"}, wantErr: "complete remote produce policy chain"},
		{name: "malformed remote", desired: []string{}, wantErr: "explicit policy list"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			chain := &configurationv1alpha1.EventGatewayVirtualClusterProducePolicyChain{
				Name: "chain", Namespace: "default", UID: "chain-uid", Generation: 1,
			}
			chain.SetGatewayID("gateway-id")
			chain.SetVirtualClusterID("vc-id")
			SetKonnectEntityProgrammedConditionTrue(chain)
			before := *apimeta.FindStatusCondition(chain.Status.Conditions, "Programmed")
			ctx := WithResolvedPolicyChainIDs(t.Context(), chain, tt.desired)
			cl := fake.NewClientBuilder().WithScheme(scheme.Get()).Build()
			sdk := sdkmocks.NewMockSDKWrapperWithT(t)
			policySDK := sdk.EventGatewayVirtualClusterProducePoliciesSDK
			policySDK.EXPECT().GetEventGatewayVirtualClusterProducePolicyChain(mock.Anything, "gateway-id", "vc-id").
				Return(&sdkkonnectops.GetEventGatewayVirtualClusterProducePolicyChainResponse{
					EventGatewayPolicyChainResponse: &sdkkonnectcomp.EventGatewayPolicyChainResponse{Policies: tt.remote},
				}, nil).Once()
			if tt.put {
				policySDK.EXPECT().UpdateEventGatewayVirtualClusterProducePolicyChain(mock.Anything,
					sdkkonnectops.UpdateEventGatewayVirtualClusterProducePolicyChainRequest{
						GatewayID: "gateway-id", VirtualClusterID: "vc-id",
						UpdateEventGatewayPolicyChainRequest: &sdkkonnectcomp.UpdateEventGatewayPolicyChainRequest{Policies: tt.desired},
					}).Return(&sdkkonnectops.UpdateEventGatewayVirtualClusterProducePolicyChainResponse{
					EventGatewayPolicyChainResponse: &sdkkonnectcomp.EventGatewayPolicyChainResponse{Policies: tt.desired},
				}, nil).Once()
			}
			result, err := Update(ctx, sdk, time.Hour, cl, &metricsmocks.MockRecorder{}, chain)
			programmed := apimeta.FindStatusCondition(chain.Status.Conditions, "Programmed")
			require.NotNil(t, programmed)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				require.Equal(t, metav1.ConditionFalse, programmed.Status)
			} else {
				require.NoError(t, err)
				require.Equal(t, metav1.ConditionTrue, programmed.Status)
				if !tt.put {
					require.Equal(t, before, *programmed, "no-op checks must not cause another status watch event")
					require.Positive(t, result.RequeueAfter)
					require.LessOrEqual(t, result.RequeueAfter, time.Hour)
				}
			}
		})
	}
}
