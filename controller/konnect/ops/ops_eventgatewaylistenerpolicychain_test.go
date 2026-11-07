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

func TestListenerPolicyChainGuardedOperations(t *testing.T) {
	t.Parallel()
	for _, op := range []Op{CreateOp, UpdateOp} {
		for _, tt := range []struct {
			name                                   string
			desired, remote, response              []string
			getErr, putErr                         error
			nilGet, nilGetBody, nilPut, nilPutBody bool
			resolved, invalidRef, wantPUT          bool
			wantErr                                string
			retryAfter                             time.Duration
		}{
			{name: "ordered complete set", desired: []string{"second", "first"}, remote: []string{"first", "second"}, response: []string{"second", "first"}, wantPUT: true},
			{name: "explicit empty", desired: []string{}, remote: []string{}, response: []string{}, wantPUT: true},
			{name: "resolved snapshots", desired: []string{"second", "first"}, remote: []string{"first", "second"}, response: []string{"second", "first"}, resolved: true, wantPUT: true},
			{name: "missing declaration", wantErr: "supplied explicitly"},
			{name: "nil snapshot is not empty", resolved: true, wantErr: "supplied explicitly"},
			{name: "unready reference", desired: []string{"first"}, invalidRef: true, wantErr: "not programmed"},
			{name: "incomplete set", desired: []string{"first"}, remote: []string{"first", "second"}, wantErr: "complete remote listener policy chain"},
			{name: "same count different set", desired: []string{"first", "second"}, remote: []string{"first", "foreign"}, wantErr: "complete remote listener policy chain"},
			{name: "missing remote policy", desired: []string{"first", "second"}, remote: []string{"first"}, wantErr: "complete remote listener policy chain"},
			{name: "empty cannot clear", desired: []string{}, remote: []string{"first"}, wantErr: "complete remote listener policy chain"},
			{name: "recreated policy ID", desired: []string{"first"}, remote: []string{"previous-id"}, wantErr: "complete remote listener policy chain"},
			{name: "duplicate IDs", desired: []string{"first", "first"}, remote: []string{"first"}, wantErr: "unique, non-empty"},
			{name: "empty desired ID snapshot", desired: []string{""}, remote: []string{}, resolved: true, wantErr: "unique, non-empty"},
			{name: "duplicate remote IDs", desired: []string{"first"}, remote: []string{"first", "first"}, wantErr: "unique, non-empty"},
			{name: "empty remote ID", desired: []string{}, remote: []string{""}, wantErr: "unique, non-empty"},
			{name: "null remote list", desired: []string{}, wantErr: "explicit policy list"},
			{name: "GET failure", desired: []string{}, getErr: errors.New("GET unavailable"), wantErr: "GET unavailable"},
			{name: "GET 404", desired: []string{}, getErr: &sdkkonnecterrs.NotFoundError{Status: http.StatusNotFound, Title: "listener not found"}, wantErr: "listener not found"},
			{name: "GET generic 404", desired: []string{}, getErr: &sdkkonnecterrs.SDKError{StatusCode: http.StatusNotFound, Message: "listener not found"}, wantErr: "listener not found"},
			{name: "GET bad request", desired: []string{}, getErr: &sdkkonnecterrs.BadRequestError{Status: http.StatusBadRequest, Title: "invalid request"}, wantErr: "invalid request"},
			{name: "GET forbidden", desired: []string{}, getErr: &sdkkonnecterrs.SDKError{StatusCode: http.StatusForbidden, Message: "access denied"}, wantErr: "access denied"},
			{name: "GET rate limit", desired: []string{}, getErr: &sdkkonnecterrs.SDKError{
				StatusCode: http.StatusTooManyRequests, Message: "rate limited",
				RawResponse: &http.Response{Header: http.Header{"Retry-After": []string{"3"}}},
			}, wantErr: "rate limited", retryAfter: 3 * time.Second},
			{name: "GET nil response", desired: []string{}, nilGet: true, wantErr: ErrNilResponse.Error()},
			{name: "GET nil body", desired: []string{}, nilGetBody: true, wantErr: ErrNilResponse.Error()},
			{name: "PUT failure", desired: []string{}, remote: []string{}, wantPUT: true, putErr: errors.New("PUT unavailable"), wantErr: "PUT unavailable"},
			{name: "PUT bad request", desired: []string{}, remote: []string{}, wantPUT: true, putErr: &sdkkonnecterrs.BadRequestError{Status: http.StatusBadRequest, Title: "invalid request"}, wantErr: "invalid request"},
			{name: "PUT conflict does not list for UID", desired: []string{}, remote: []string{}, wantPUT: true, putErr: &sdkkonnecterrs.SDKError{StatusCode: http.StatusConflict, Message: "concurrent update"}, wantErr: "concurrent update"},
			{name: "PUT rate limit", desired: []string{}, remote: []string{}, wantPUT: true, putErr: &sdkkonnecterrs.SDKError{
				StatusCode: http.StatusTooManyRequests, Message: "rate limited",
				RawResponse: &http.Response{Header: http.Header{"Retry-After": []string{"3"}}},
			}, wantErr: "rate limited", retryAfter: 3 * time.Second},
			{name: "PUT nil response", desired: []string{}, remote: []string{}, wantPUT: true, nilPut: true, wantErr: ErrNilResponse.Error()},
			{name: "PUT nil body", desired: []string{}, remote: []string{}, wantPUT: true, nilPutBody: true, wantErr: ErrNilResponse.Error()},
			{name: "PUT null is not empty", desired: []string{}, remote: []string{}, wantPUT: true, wantErr: "does not match"},
			{name: "PUT ignored order", desired: []string{"second", "first"}, remote: []string{"first", "second"}, response: []string{"first", "second"}, wantPUT: true, wantErr: "does not match"},
			{name: "PUT omitted policy", desired: []string{"second", "first"}, remote: []string{"first", "second"}, response: []string{"second"}, wantPUT: true, wantErr: "does not match"},
			{name: "PUT added policy", desired: []string{"first"}, remote: []string{"first"}, response: []string{"first", "foreign"}, wantPUT: true, wantErr: "does not match"},
			{name: "PUT duplicate policy", desired: []string{"first"}, remote: []string{"first"}, response: []string{"first", "first"}, wantPUT: true, wantErr: "does not match"},
		} {
			t.Run(string(op)+"/"+tt.name, func(t *testing.T) {
				parent := commonv1alpha1.ObjectRef{Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
					NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "listener"}}
				chain := &configurationv1alpha1.EventGatewayListenerPolicyChain{
					Name: "chain", Namespace: "default", UID: "chain-uid", Generation: 1,
					Spec: configurationv1alpha1.EventGatewayListenerPolicyChainSpec{EventGatewayListenerRef: parent},
				}
				chain.SetGatewayID("gateway-id")
				chain.SetEventGatewayListenerID("listener-id")
				if tt.desired != nil {
					chain.Spec.APISpec.Policies = []configurationv1alpha1.EventGatewayListenerPolicyRef{}
				}
				var policies []client.Object
				for i, id := range tt.desired {
					name := []string{"policy-a", "policy-b"}[i]
					chain.Spec.APISpec.Policies = append(chain.Spec.APISpec.Policies, configurationv1alpha1.EventGatewayListenerPolicyRef{Name: name})
					policy := &configurationv1alpha1.EventGatewayListenerPolicy{
						Name: name, Namespace: chain.Namespace, Generation: 1,
						Spec: configurationv1alpha1.EventGatewayListenerPolicySpec{EventGatewayListenerRef: parent},
					}
					policy.SetKonnectID(id)
					policy.SetGatewayID("gateway-id")
					policy.SetEventGatewayListenerID("listener-id")
					SetKonnectEntityProgrammedConditionTrue(policy)
					if tt.invalidRef {
						policy.Generation++
					}
					policies = append(policies, policy)
				}
				reads := 0
				cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(policies...).
					WithInterceptorFuncs(interceptor.Funcs{
						Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
							reads++
							return cl.Get(ctx, key, obj, opts...)
						},
					}).Build()
				ctx := t.Context()
				if tt.resolved {
					ctx = WithResolvedPolicyChainIDs(ctx, chain, tt.desired)
				}
				sdk := sdkmocks.NewMockSDKWrapperWithT(t)
				policySDK := sdk.EventGatewayListenerPoliciesSDK
				if tt.desired != nil && !tt.invalidRef {
					var response *sdkkonnectops.GetEventGatewayListenerPolicyChainResponse
					if !tt.nilGet {
						response = &sdkkonnectops.GetEventGatewayListenerPolicyChainResponse{}
						if !tt.nilGetBody {
							response.EventGatewayPolicyChainResponse = &sdkkonnectcomp.EventGatewayPolicyChainResponse{Policies: tt.remote}
						}
					}
					policySDK.EXPECT().GetEventGatewayListenerPolicyChain(mock.Anything, "gateway-id", "listener-id").Return(response, tt.getErr).Once()
				}
				if tt.wantPUT {
					var response *sdkkonnectops.UpdateEventGatewayListenerPolicyChainResponse
					if !tt.nilPut {
						response = &sdkkonnectops.UpdateEventGatewayListenerPolicyChainResponse{}
						if !tt.nilPutBody {
							response.EventGatewayPolicyChainResponse = &sdkkonnectcomp.EventGatewayPolicyChainResponse{Policies: tt.response}
						}
					}
					policySDK.EXPECT().UpdateEventGatewayListenerPolicyChain(mock.Anything,
						sdkkonnectops.UpdateEventGatewayListenerPolicyChainRequest{
							GatewayID: "gateway-id", ListenerID: "listener-id",
							UpdateEventGatewayPolicyChainRequest: &sdkkonnectcomp.UpdateEventGatewayPolicyChainRequest{Policies: tt.desired},
						}).Return(response, tt.putErr).Once()
				}
				var err error
				if op == CreateOp {
					_, err = Create(ctx, sdk, cl, &metricsmocks.MockRecorder{}, chain)
				} else {
					_, err = Update(ctx, sdk, 0, cl, &metricsmocks.MockRecorder{}, chain)
				}
				if tt.resolved {
					require.Zero(t, reads)
				} else {
					require.Equal(t, len(tt.desired), reads)
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
					require.Contains(t, programmed.Message, tt.wantErr)
				} else {
					require.NoError(t, err)
					require.Equal(t, metav1.ConditionTrue, programmed.Status)
				}
			})
		}
	}
}

func TestListenerPolicyChainSnapshotScopeAndDelete(t *testing.T) {
	t.Parallel()
	chain := &configurationv1alpha1.EventGatewayListenerPolicyChain{
		Name: "chain", Namespace: "default", UID: "chain-uid", Generation: 1,
	}
	ids := []string{"second", "first"}
	ctx := WithResolvedPolicyChainIDs(t.Context(), chain, ids)
	ids[0] = "changed"
	got, ok := resolvedPolicyChainIDsFromContext(ctx, chain.DeepCopy())
	require.True(t, ok)
	require.Equal(t, []string{"second", "first"}, got)
	cluster := &configurationv1alpha1.EventGatewayVirtualClusterPolicyChain{
		Name: chain.Name, Namespace: chain.Namespace, UID: chain.UID, Generation: chain.Generation,
	}
	_, ok = resolvedPolicyChainIDsFromContext(ctx, cluster)
	require.False(t, ok, "listener snapshots must not apply to cluster chains")
	recreated := chain.DeepCopy()
	recreated.UID = "new-uid"
	_, ok = resolvedPolicyChainIDsFromContext(ctx, recreated)
	require.False(t, ok)
	newGeneration := chain.DeepCopy()
	newGeneration.Generation++
	_, ok = resolvedPolicyChainIDsFromContext(ctx, newGeneration)
	require.False(t, ok)

	sdk := sdkmocks.NewMockSDKWrapperWithT(t)
	cl := fake.NewClientBuilder().WithScheme(scheme.Get()).Build()
	require.NoError(t, Delete(t.Context(), sdk, cl, &metricsmocks.MockRecorder{}, chain))
}
