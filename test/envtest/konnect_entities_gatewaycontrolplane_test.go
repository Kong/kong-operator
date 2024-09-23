package envtest

import (
	"context"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/controller/konnect/conditions"
	"github.com/kong/gateway-operator/controller/konnect/ops"

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

var konnectGatewayControlPlaneTestCases = []konnectEntityReconcilerTestCase{
	{
		name: "should create control plane successfully",
		objectOps: func(ctx context.Context, t *testing.T, cl client.Client, ns *corev1.Namespace) {
			auth := &konnectv1alpha1.KonnectAPIAuthConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "auth",
					Namespace: ns.Name,
				},
				Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
					Type:      konnectv1alpha1.KonnectAPIAuthTypeToken,
					Token:     "kpat_test",
					ServerURL: "127.0.0.1",
				},
			}
			require.NoError(t, cl.Create(ctx, auth))
			// We cannot create KonnectAPIAuthConfiguration with specified status, so we update the status after creating it.
			auth.Status = konnectv1alpha1.KonnectAPIAuthConfigurationStatus{
				OrganizationID: "org-1",
				ServerURL:      "127.0.0.1",
				Conditions: []metav1.Condition{
					{
						Type:               conditions.KonnectEntityAPIAuthConfigurationValidConditionType,
						ObservedGeneration: auth.GetGeneration(),
						Status:             metav1.ConditionTrue,
						Reason:             conditions.KonnectEntityAPIAuthConfigurationReasonValid,
						LastTransitionTime: metav1.Now(),
					},
				},
			}
			require.NoError(t, cl.Status().Update(ctx, auth))
			// Create KonnectGatewayControlPlane.
			cp := &konnectv1alpha1.KonnectGatewayControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cp-1",
					Namespace: ns.Name,
				},
				Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
					KonnectConfiguration: konnectv1alpha1.KonnectConfiguration{
						APIAuthConfigurationRef: konnectv1alpha1.KonnectAPIAuthConfigurationRef{
							Name: "auth",
						},
					},
					CreateControlPlaneRequest: sdkkonnectcomp.CreateControlPlaneRequest{
						Name:        "cp-1",
						Description: lo.ToPtr("test control plane 1"),
					},
				},
			}
			require.NoError(t, cl.Create(ctx, cp))
		},
		mockExpectations: func(t *testing.T, sdk *ops.MockSDKWrapper, ns *corev1.Namespace) {
			sdk.ControlPlaneSDK.EXPECT().CreateControlPlane(mock.Anything, mock.MatchedBy(func(req sdkkonnectcomp.CreateControlPlaneRequest) bool {
				return req.Name == "cp-1" &&
					req.Description != nil && *req.Description == "test control plane 1"
			})).Return(&sdkkonnectops.CreateControlPlaneResponse{
				ControlPlane: &sdkkonnectcomp.ControlPlane{
					ID: lo.ToPtr("12345"),
				},
			}, nil)
			// verify that mock SDK is called as expected.
			t.Cleanup(func() {
				require.True(t, sdk.ControlPlaneSDK.AssertExpectations(t))
			})
		},
		eventuallyPredicate: func(ctx context.Context, t *assert.CollectT, cl client.Client, ns *corev1.Namespace) {
			cp := &konnectv1alpha1.KonnectGatewayControlPlane{}
			if !assert.NoError(t,
				cl.Get(ctx,
					k8stypes.NamespacedName{
						Namespace: ns.Name,
						Name:      "cp-1",
					},
					cp,
				),
			) {
				return
			}

			assert.Equal(t, "12345", cp.Status.ID)
			assert.True(t,
				lo.ContainsBy(cp.Status.Conditions, func(condition metav1.Condition) bool {
					return condition.Type == conditions.KonnectEntityProgrammedConditionType &&
						condition.Status == metav1.ConditionTrue
				}),
				"Programmed condition should be set and it status should be true",
			)
		},
	},
}
