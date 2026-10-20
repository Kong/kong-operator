package ops

import (
	"errors"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/Kong/sdk-konnect-go/test/mocks"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

func TestDeleteEventGatewayParentPolicy(t *testing.T) {
	t.Parallel()
	for _, side := range []string{"produce", "consume"} {
		t.Run(side, func(t *testing.T) {
			for _, state := range []string{"unprogrammed", "programmed", "terminating"} {
				t.Run(state, func(t *testing.T) {
					meta := metav1.ObjectMeta{Name: "child", Namespace: "default"}
					if state == "terminating" {
						meta.DeletionTimestamp = new(metav1.Now())
						meta.Finalizers = []string{"test"}
					}
					if side == "produce" {
						parent := testEventGatewayVirtualClusterProducePolicy()
						child := &configurationv1alpha1.EventGatewayVirtualClusterProducePolicy{
							ObjectMeta: meta,
							Spec: configurationv1alpha1.EventGatewayVirtualClusterProducePolicySpec{
								APISpec: configurationv1alpha1.EventGatewayVirtualClusterProducePolicyAPISpec{
									ParentPolicyRef: configurationv1alpha1.EventGatewayVirtualClusterProducePolicyRef{Name: parent.Name},
								},
							},
						}
						if state != "unprogrammed" {
							child.SetKonnectID("child-id")
						}
						cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(child).Build()
						sdk := mocks.NewMockEventGatewayVirtualClusterProducePoliciesSDK(t)
						sdk.EXPECT().GetEventGatewayVirtualClusterProducePolicy(mock.Anything,
							sdkkonnectops.GetEventGatewayVirtualClusterProducePolicyRequest{
								GatewayID: parent.GetGatewayID(), VirtualClusterID: parent.GetVirtualClusterID(), PolicyID: parent.GetKonnectID(),
							}).Return(&sdkkonnectops.GetEventGatewayVirtualClusterProducePolicyResponse{
							EventGatewayPolicy: &sdkkonnectcomp.EventGatewayPolicy{Type: "schema_validation"},
						}, nil).Once()
						err := deleteEventGatewayVirtualClusterProducePolicyGuarded(t.Context(), sdk, cl, parent)
						var blocked EventGatewayParentPolicyInUseError
						require.ErrorAs(t, err, &blocked)
						require.Equal(t, []string{"default/child"}, blocked.Users)
						require.ErrorContains(t, err, "delete the child policy objects first")
						// Even terminating children block until their Konnect cleanup
						// has finished and their object is gone.
						child.Finalizers = nil
						require.NoError(t, cl.Update(t.Context(), child))
						if state != "terminating" {
							require.NoError(t, cl.Delete(t.Context(), child))
						}
						sdk.EXPECT().DeleteEventGatewayVirtualClusterProducePolicy(mock.Anything, mock.Anything).
							Return(&sdkkonnectops.DeleteEventGatewayVirtualClusterProducePolicyResponse{}, nil).Once()
						require.NoError(t, deleteEventGatewayVirtualClusterProducePolicyGuarded(t.Context(), sdk, cl, parent))
					} else {
						parent := testEventGatewayVirtualClusterConsumePolicy()
						child := &configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy{
							ObjectMeta: meta,
							Spec: configurationv1alpha1.EventGatewayVirtualClusterConsumePolicySpec{
								APISpec: configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyAPISpec{
									ParentPolicyRef: configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyRef{Name: parent.Name},
								},
							},
						}
						if state != "unprogrammed" {
							child.SetKonnectID("child-id")
						}
						cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(child).Build()
						sdk := mocks.NewMockEventGatewayVirtualClusterConsumePoliciesSDK(t)
						sdk.EXPECT().GetEventGatewayVirtualClusterConsumePolicy(mock.Anything,
							sdkkonnectops.GetEventGatewayVirtualClusterConsumePolicyRequest{
								GatewayID: parent.GetGatewayID(), VirtualClusterID: parent.GetVirtualClusterID(), PolicyID: parent.GetKonnectID(),
							}).Return(&sdkkonnectops.GetEventGatewayVirtualClusterConsumePolicyResponse{
							EventGatewayPolicy: &sdkkonnectcomp.EventGatewayPolicy{Type: "schema_validation"},
						}, nil).Once()
						err := deleteEventGatewayVirtualClusterConsumePolicyGuarded(t.Context(), sdk, cl, parent)
						var blocked EventGatewayParentPolicyInUseError
						require.ErrorAs(t, err, &blocked)
						require.Equal(t, []string{"default/child"}, blocked.Users)
						child.Finalizers = nil
						require.NoError(t, cl.Update(t.Context(), child))
						if state != "terminating" {
							require.NoError(t, cl.Delete(t.Context(), child))
						}
						sdk.EXPECT().DeleteEventGatewayVirtualClusterConsumePolicy(mock.Anything, mock.Anything).
							Return(&sdkkonnectops.DeleteEventGatewayVirtualClusterConsumePolicyResponse{}, nil).Once()
						require.NoError(t, deleteEventGatewayVirtualClusterConsumePolicyGuarded(t.Context(), sdk, cl, parent))
					}
				})
			}
		})
	}
}

func TestDeleteEventGatewayReferencedPolicyType(t *testing.T) {
	t.Parallel()
	lookupError := errors.New("failed to read policy")
	for _, side := range []string{"produce", "consume"} {
		t.Run(side, func(t *testing.T) {
			for _, tc := range []struct {
				name        string
				policy      *sdkkonnectcomp.EventGatewayPolicy
				nilResponse bool
				lookupErr   error
				wantErr     error
				wantDelete  bool
			}{
				{name: "non-schema policy", policy: &sdkkonnectcomp.EventGatewayPolicy{Type: "modify_headers"}, wantDelete: true},
				{name: "already gone", lookupErr: &sdkkonnecterrs.NotFoundError{}},
				{name: "read failure", lookupErr: lookupError, wantErr: lookupError},
				{name: "nil response", nilResponse: true, wantErr: ErrNilResponse},
				{name: "missing policy response", wantErr: ErrNilResponse},
				{name: "missing policy type", policy: &sdkkonnectcomp.EventGatewayPolicy{}, wantErr: ErrNilResponse},
			} {
				t.Run(tc.name, func(t *testing.T) {
					var err error
					if side == "produce" {
						parent := testEventGatewayVirtualClusterProducePolicy()
						// A desired schemaValidation type is not proof that the
						// remote policy was created with that type.
						parent.Spec.APISpec.Type = configurationv1alpha1.EventGatewayVirtualClusterProducePolicyConfigTypeProduceSchemaValidationPolicy
						child := &configurationv1alpha1.EventGatewayVirtualClusterProducePolicy{
							Name: "child", Namespace: parent.Namespace,
							Spec: configurationv1alpha1.EventGatewayVirtualClusterProducePolicySpec{
								APISpec: configurationv1alpha1.EventGatewayVirtualClusterProducePolicyAPISpec{
									ParentPolicyRef: configurationv1alpha1.EventGatewayVirtualClusterProducePolicyRef{Name: parent.Name},
								},
							},
						}
						cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(child).Build()
						sdk := mocks.NewMockEventGatewayVirtualClusterProducePoliciesSDK(t)
						resp := &sdkkonnectops.GetEventGatewayVirtualClusterProducePolicyResponse{EventGatewayPolicy: tc.policy}
						if tc.nilResponse {
							resp = nil
						}
						sdk.EXPECT().GetEventGatewayVirtualClusterProducePolicy(mock.Anything, mock.Anything).
							Return(resp, tc.lookupErr).Once()
						if tc.wantDelete {
							sdk.EXPECT().DeleteEventGatewayVirtualClusterProducePolicy(mock.Anything, mock.Anything).
								Return(&sdkkonnectops.DeleteEventGatewayVirtualClusterProducePolicyResponse{}, nil).Once()
						}
						err = deleteEventGatewayVirtualClusterProducePolicyGuarded(t.Context(), sdk, cl, parent)
					} else {
						parent := testEventGatewayVirtualClusterConsumePolicy()
						parent.Spec.APISpec.Type = configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyConfigTypeConsumeSchemaValidationPolicy
						child := &configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy{
							Name: "child", Namespace: parent.Namespace,
							Spec: configurationv1alpha1.EventGatewayVirtualClusterConsumePolicySpec{
								APISpec: configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyAPISpec{
									ParentPolicyRef: configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyRef{Name: parent.Name},
								},
							},
						}
						cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(child).Build()
						sdk := mocks.NewMockEventGatewayVirtualClusterConsumePoliciesSDK(t)
						resp := &sdkkonnectops.GetEventGatewayVirtualClusterConsumePolicyResponse{EventGatewayPolicy: tc.policy}
						if tc.nilResponse {
							resp = nil
						}
						sdk.EXPECT().GetEventGatewayVirtualClusterConsumePolicy(mock.Anything, mock.Anything).
							Return(resp, tc.lookupErr).Once()
						if tc.wantDelete {
							sdk.EXPECT().DeleteEventGatewayVirtualClusterConsumePolicy(mock.Anything, mock.Anything).
								Return(&sdkkonnectops.DeleteEventGatewayVirtualClusterConsumePolicyResponse{}, nil).Once()
						}
						err = deleteEventGatewayVirtualClusterConsumePolicyGuarded(t.Context(), sdk, cl, parent)
					}
					if tc.wantErr != nil {
						require.ErrorIs(t, err, tc.wantErr)
					} else {
						require.NoError(t, err)
					}
				})
			}
		})
	}
}

func TestDeleteEventGatewayParentPolicyListFailure(t *testing.T) {
	t.Parallel()
	cl := fake.NewClientBuilder().Build()
	produceSDK := mocks.NewMockEventGatewayVirtualClusterProducePoliciesSDK(t)
	require.ErrorContains(t, deleteEventGatewayVirtualClusterProducePolicyGuarded(t.Context(), produceSDK, cl, testEventGatewayVirtualClusterProducePolicy()), "listing child produce policies")
	consumeSDK := mocks.NewMockEventGatewayVirtualClusterConsumePoliciesSDK(t)
	require.ErrorContains(t, deleteEventGatewayVirtualClusterConsumePolicyGuarded(t.Context(), consumeSDK, cl, testEventGatewayVirtualClusterConsumePolicy()), "listing child consume policies")
}
