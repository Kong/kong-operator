package ops

import (
	"context"
	"errors"
	"net/http"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
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

func TestPolicyChainGuardedOperations(t *testing.T) {
	t.Parallel()
	for _, op := range []Op{CreateOp, UpdateOp} {
		for _, tt := range []struct {
			name       string
			desired    []string
			remote     []string
			response   []string
			getErr     error
			putErr     error
			nilGet     bool
			nilPut     bool
			invalidRef bool
			resolved   bool
			wantPUT    bool
			wantErr    string
		}{
			{name: "reorder complete set", desired: []string{"second", "first"}, remote: []string{"first", "second"}, response: []string{"second", "first"}, wantPUT: true},
			{name: "explicit empty remote", desired: []string{}, remote: []string{}, response: []string{}, wantPUT: true},
			{name: "reuse resolved order", desired: []string{"second", "first"}, remote: []string{"first", "second"}, response: []string{"second", "first"}, resolved: true, wantPUT: true},
			{name: "reuse explicit empty resolution", desired: []string{}, remote: []string{}, response: []string{}, resolved: true, wantPUT: true},
			{name: "resolved IDs still require complete set", desired: []string{"first"}, remote: []string{"first", "second"}, resolved: true, wantErr: "complete remote cluster policy chain"},
			{name: "nil resolution must not bypass validation", resolved: true, wantErr: "supplied explicitly"},
			{name: "omit list", wantErr: "supplied explicitly"},
			{name: "empty with remote policy", desired: []string{}, remote: []string{"first"}, wantErr: "complete remote cluster policy chain"},
			{name: "partial list", desired: []string{"first"}, remote: []string{"first", "second"}, wantErr: "complete remote cluster policy chain"},
			{name: "stale policy ID", desired: []string{"first"}, remote: []string{"previous-id"}, wantErr: "complete remote cluster policy chain"},
			{name: "duplicate remote IDs", desired: []string{"first"}, remote: []string{"first", "first"}, wantErr: "unique, non-empty"},
			{name: "duplicate resolved IDs", desired: []string{"first", "first"}, remote: []string{"first"}, wantErr: "unique, non-empty"},
			{name: "invalid reference", desired: []string{"first"}, invalidRef: true, wantErr: "not programmed"},
			{name: "GET error", desired: []string{}, getErr: errors.New("GET unavailable"), wantErr: "GET unavailable"},
			{name: "GET typed 404", desired: []string{}, getErr: &sdkkonnecterrs.NotFoundError{Status: http.StatusNotFound, Title: "Not Found"}, wantErr: "Not Found"},
			{name: "GET generic 404", desired: []string{}, getErr: &sdkkonnecterrs.SDKError{StatusCode: http.StatusNotFound, Message: "virtual cluster not found"}, wantErr: "virtual cluster not found"},
			{name: "GET nil response", desired: []string{}, nilGet: true, wantErr: ErrNilResponse.Error()},
			{name: "PUT error", desired: []string{}, putErr: errors.New("PUT unavailable"), wantPUT: true, wantErr: "PUT unavailable"},
			{name: "PUT nil response", desired: []string{}, nilPut: true, wantPUT: true, wantErr: ErrNilResponse.Error()},
			{name: "PUT ignored order", desired: []string{"second", "first"}, remote: []string{"first", "second"}, response: []string{"first", "second"}, wantPUT: true, wantErr: "does not match"},
		} {
			t.Run(string(op)+"/"+tt.name, func(t *testing.T) {
				parent := commonv1alpha1.ObjectRef{Type: commonv1alpha1.ObjectRefTypeNamespacedRef, NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "vc"}}
				chain := &configurationv1alpha1.EventGatewayVirtualClusterPolicyChain{
					Name: "chain", Namespace: "default", Generation: 1,
					Spec: configurationv1alpha1.EventGatewayVirtualClusterPolicyChainSpec{EventGatewayVirtualClusterRef: parent},
				}
				chain.SetGatewayID("gateway-id")
				chain.SetVirtualClusterID("vc-id")
				var policies []client.Object
				if tt.desired != nil {
					chain.Spec.APISpec.Policies = []configurationv1alpha1.EventGatewayVirtualClusterPolicyRef{}
				}
				for i, id := range tt.desired {
					name := []string{"policy-a", "policy-b"}[i]
					chain.Spec.APISpec.Policies = append(chain.Spec.APISpec.Policies, configurationv1alpha1.EventGatewayVirtualClusterPolicyRef{Name: name})
					p := &configurationv1alpha1.EventGatewayVirtualClusterPolicy{
						Name: name, Namespace: "default", Generation: 1,
						Spec: configurationv1alpha1.EventGatewayVirtualClusterPolicySpec{EventGatewayVirtualClusterRef: parent},
					}
					p.SetKonnectID(id)
					p.SetGatewayID("gateway-id")
					p.SetVirtualClusterID("vc-id")
					SetKonnectEntityProgrammedConditionTrue(p)
					if tt.invalidRef {
						p.Status.Conditions = nil
					}
					policies = append(policies, p)
				}
				policyReads := 0
				cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(policies...).
					WithInterceptorFuncs(interceptor.Funcs{
						Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
							if _, ok := obj.(*configurationv1alpha1.EventGatewayVirtualClusterPolicy); ok {
								policyReads++
							}
							return cl.Get(ctx, key, obj, opts...)
						},
					}).Build()
				ctx := t.Context()
				if tt.resolved {
					ctx = WithResolvedPolicyChainIDs(ctx, chain, tt.desired)
				}
				sdk := sdkmocks.NewMockSDKWrapperWithT(t)
				policySDK := sdk.EventGatewayVirtualClusterPoliciesSDK
				if tt.desired != nil && !tt.invalidRef {
					var response *sdkkonnectops.GetEventGatewayVirtualClusterClusterLevelPolicyChainResponse
					if !tt.nilGet {
						response = &sdkkonnectops.GetEventGatewayVirtualClusterClusterLevelPolicyChainResponse{
							EventGatewayPolicyChainResponse: &sdkkonnectcomp.EventGatewayPolicyChainResponse{Policies: tt.remote},
						}
					}

					policySDK.EXPECT().GetEventGatewayVirtualClusterClusterLevelPolicyChain(mock.Anything, "gateway-id", "vc-id").
						Return(response, tt.getErr).Once()
				}
				if tt.wantPUT {
					var response *sdkkonnectops.UpdateEventGatewayVirtualClusterClusterLevelPolicyChainResponse
					if !tt.nilPut {
						response = &sdkkonnectops.UpdateEventGatewayVirtualClusterClusterLevelPolicyChainResponse{
							EventGatewayPolicyChainResponse: &sdkkonnectcomp.EventGatewayPolicyChainResponse{Policies: tt.response},
						}
					}
					policySDK.EXPECT().UpdateEventGatewayVirtualClusterClusterLevelPolicyChain(mock.Anything,
						sdkkonnectops.UpdateEventGatewayVirtualClusterClusterLevelPolicyChainRequest{
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
				if tt.resolved {
					require.Zero(t, policyReads, "resolved IDs must not trigger another reference lookup")
				} else {
					require.Equal(t, len(tt.desired), policyReads, "standalone operations must resolve their references")
				}
				if tt.getErr != nil {
					require.ErrorIs(t, err, tt.getErr)
				}
				if !tt.wantPUT {
					policySDK.AssertNotCalled(t, "UpdateEventGatewayVirtualClusterClusterLevelPolicyChain", mock.Anything, mock.Anything)
				}
				programmed := meta.FindStatusCondition(chain.Status.Conditions, "Programmed")
				require.NotNil(t, programmed)
				require.Empty(t, chain.GetKonnectID())
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

func TestResolvedPolicyChainIDsContext(t *testing.T) {
	t.Parallel()
	chain := &configurationv1alpha1.EventGatewayVirtualClusterPolicyChain{
		Name: "chain", Namespace: "default", UID: "chain-uid", Generation: 1,
	}
	ctx := t.Context()
	ids := []string{"second", "first"}
	resolvedCtx := WithResolvedPolicyChainIDs(ctx, chain, ids)
	ids[0] = "changed"
	got, ok := resolvedPolicyChainIDsFromContext(resolvedCtx, chain.DeepCopy())
	require.True(t, ok)
	require.Equal(t, []string{"second", "first"}, got)
	_, ok = resolvedPolicyChainIDsFromContext(ctx, chain)
	require.False(t, ok, "resolution must not escape into the original context")
	_, ok = resolvedPolicyChainIDsFromContext(WithResolvedPolicyChainIDs(ctx, chain, nil), chain)
	require.False(t, ok, "nil IDs are not a validated empty list")

	for _, tt := range []struct {
		name   string
		change func(*configurationv1alpha1.EventGatewayVirtualClusterPolicyChain)
	}{
		{name: "different name", change: func(c *configurationv1alpha1.EventGatewayVirtualClusterPolicyChain) { c.Name = "other" }},
		{name: "different namespace", change: func(c *configurationv1alpha1.EventGatewayVirtualClusterPolicyChain) { c.Namespace = "other" }},
		{name: "recreated chain", change: func(c *configurationv1alpha1.EventGatewayVirtualClusterPolicyChain) { c.UID = "new-uid" }},
		{name: "different generation", change: func(c *configurationv1alpha1.EventGatewayVirtualClusterPolicyChain) { c.Generation++ }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			other := chain.DeepCopy()
			tt.change(other)
			_, ok := resolvedPolicyChainIDsFromContext(resolvedCtx, other)
			require.False(t, ok)
		})
	}

	emptyCtx := WithResolvedPolicyChainIDs(ctx, chain, []string{})
	got, ok = resolvedPolicyChainIDsFromContext(emptyCtx, chain)
	require.True(t, ok, "an empty resolution must be distinguishable from an absent result")
	require.NotNil(t, got)
	require.Empty(t, got)
}

func TestPolicyChainDeleteRetainsRemoteConfiguration(t *testing.T) {
	t.Parallel()
	// No SDK expectations, IDs, or policy objects: deletion never calls Konnect.
	sdk := sdkmocks.NewMockSDKWrapperWithT(t)
	cl := fake.NewClientBuilder().WithScheme(scheme.Get()).Build()
	chain := &configurationv1alpha1.EventGatewayVirtualClusterPolicyChain{Name: "chain", Namespace: "default"}
	require.NoError(t, Delete(t.Context(), sdk, cl, &metricsmocks.MockRecorder{}, chain))
}
