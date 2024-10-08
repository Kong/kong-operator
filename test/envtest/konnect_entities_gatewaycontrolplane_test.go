package envtest

import (
	"context"
	"errors"
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
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/kong/gateway-operator/controller/konnect"
	"github.com/kong/gateway-operator/controller/konnect/ops"
	"github.com/kong/gateway-operator/test/helpers/deploy"

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

var konnectGatewayControlPlaneTestCases = []konnectEntityReconcilerTestCase{
	{
		name: "should create control plane successfully",
		objectOps: func(ctx context.Context, t *testing.T, cl client.Client, ns *corev1.Namespace) {
			auth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, cl)
			deploy.KonnectGatewayControlPlane(t, ctx, cl, auth,
				func(obj client.Object) {
					cp := obj.(*konnectv1alpha1.KonnectGatewayControlPlane)
					cp.Name = "cp-1"
					cp.Spec.Name = "cp-1"
					cp.Spec.Description = lo.ToPtr("test control plane 1")
				},
			)
		},
		mockExpectations: func(t *testing.T, sdk *ops.MockSDKWrapper, ns *corev1.Namespace) {
			sdk.ControlPlaneSDK.EXPECT().
				CreateControlPlane(
					mock.Anything,
					mock.MatchedBy(func(req sdkkonnectcomp.CreateControlPlaneRequest) bool {
						return req.Name == "cp-1" &&
							req.Description != nil && *req.Description == "test control plane 1"
					}),
				).
				Return(
					&sdkkonnectops.CreateControlPlaneResponse{
						ControlPlane: &sdkkonnectcomp.ControlPlane{
							ID: lo.ToPtr("12345"),
						},
					},
					nil)
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
			assert.True(t, conditionsContainProgrammedTrue(cp.Status.Conditions),
				"Programmed condition should be set and it status should be true",
			)
			assert.True(t, controllerutil.ContainsFinalizer(cp, konnect.KonnectCleanupFinalizer),
				"Finalizer should be set on control plane group",
			)
		},
	},
	{
		name: "should create control plane group and control plane as member successfully",
		objectOps: func(ctx context.Context, t *testing.T, cl client.Client, ns *corev1.Namespace) {
			auth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, cl)
			deploy.KonnectGatewayControlPlane(t, ctx, cl, auth,
				func(obj client.Object) {
					cp := obj.(*konnectv1alpha1.KonnectGatewayControlPlane)
					cp.Name = "cp-groupmember-1"
					cp.Spec.Name = "cp-groupmember-1"
				},
			)
			deploy.KonnectGatewayControlPlane(t, ctx, cl, auth,
				func(obj client.Object) {
					cp := obj.(*konnectv1alpha1.KonnectGatewayControlPlane)
					cp.Name = "cp-2"
					cp.Spec.Name = "cp-2"
					cp.Spec.ClusterType = lo.ToPtr(sdkkonnectcomp.ClusterTypeClusterTypeControlPlaneGroup)
					cp.Spec.Members = []corev1.LocalObjectReference{
						{
							Name: "cp-groupmember-1",
						},
					}
				},
			)
		},
		mockExpectations: func(t *testing.T, sdk *ops.MockSDKWrapper, ns *corev1.Namespace) {
			sdk.ControlPlaneSDK.EXPECT().
				CreateControlPlane(
					mock.Anything,
					mock.MatchedBy(func(req sdkkonnectcomp.CreateControlPlaneRequest) bool {
						return req.Name == "cp-groupmember-1"
					}),
				).
				Return(
					&sdkkonnectops.CreateControlPlaneResponse{
						ControlPlane: &sdkkonnectcomp.ControlPlane{
							ID: lo.ToPtr("12345"),
						},
					},
					nil)
			sdk.ControlPlaneSDK.EXPECT().
				CreateControlPlane(
					mock.Anything,
					mock.MatchedBy(func(req sdkkonnectcomp.CreateControlPlaneRequest) bool {
						return req.Name == "cp-2" &&
							req.ClusterType != nil && *req.ClusterType == sdkkonnectcomp.ClusterTypeClusterTypeControlPlaneGroup
					}),
				).
				Return(
					&sdkkonnectops.CreateControlPlaneResponse{
						ControlPlane: &sdkkonnectcomp.ControlPlane{
							ID: lo.ToPtr("12346"),
						},
					},
					nil)

			sdk.ControlPlaneGroupSDK.EXPECT().
				PutControlPlanesIDGroupMemberships(
					mock.Anything,
					"12346",
					&sdkkonnectcomp.GroupMembership{
						Members: []sdkkonnectcomp.Members{
							{
								ID: lo.ToPtr("12345"),
							},
						},
					},
				).
				Return(
					&sdkkonnectops.PutControlPlanesIDGroupMembershipsResponse{},
					nil,
				)

			sdk.ControlPlaneSDK.EXPECT().
				UpdateControlPlane(
					mock.Anything,
					"12346",
					mock.MatchedBy(func(req sdkkonnectcomp.UpdateControlPlaneRequest) bool {
						return req.Name != nil && *req.Name == "cp-2"
					}),
				).
				Return(
					&sdkkonnectops.UpdateControlPlaneResponse{
						ControlPlane: &sdkkonnectcomp.ControlPlane{
							ID: lo.ToPtr("12346"),
						},
					},
					nil)
			// verify that mock SDK is called as expected.
			t.Cleanup(func() {
				require.True(t, sdk.ControlPlaneSDK.AssertExpectations(t))
				require.True(t, sdk.ControlPlaneGroupSDK.AssertExpectations(t))
			})
		},
		eventuallyPredicate: func(ctx context.Context, t *assert.CollectT, cl client.Client, ns *corev1.Namespace) {
			cp := &konnectv1alpha1.KonnectGatewayControlPlane{}
			if !assert.NoError(t,
				cl.Get(ctx,
					k8stypes.NamespacedName{
						Namespace: ns.Name,
						Name:      "cp-groupmember-1",
					},
					cp,
				),
			) {
				return
			}

			assert.Equal(t, "12345", cp.Status.ID)
			assert.True(t, conditionsContainProgrammedTrue(cp.Status.Conditions),
				"Programmed condition should be set and it status should be true",
			)
			assert.True(t, controllerutil.ContainsFinalizer(cp, konnect.KonnectCleanupFinalizer),
				"Finalizer should be set on control plane",
			)

			cpGroup := &konnectv1alpha1.KonnectGatewayControlPlane{}
			if !assert.NoError(t,
				cl.Get(ctx,
					k8stypes.NamespacedName{
						Namespace: ns.Name,
						Name:      "cp-2",
					},
					cpGroup,
				),
			) {
				return
			}

			assert.Equal(t, "12346", cpGroup.Status.ID)
			assert.True(t, conditionsContainProgrammedTrue(cpGroup.Status.Conditions),
				"Programmed condition should be set and it status should be true",
			)
			assert.True(t, controllerutil.ContainsFinalizer(cpGroup, konnect.KonnectCleanupFinalizer),
				"Finalizer should be set on control plane group",
			)
		},
	},
	{
		name: "control plane group with members when receiving an error from PutControlPlanesIDGroupMemberships, correctly sets the ID and finalizer on group",
		objectOps: func(ctx context.Context, t *testing.T, cl client.Client, ns *corev1.Namespace) {
			auth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, cl)
			deploy.KonnectGatewayControlPlane(t, ctx, cl, auth,
				func(obj client.Object) {
					cp := obj.(*konnectv1alpha1.KonnectGatewayControlPlane)
					cp.Name = "cp-groupmember-2"
					cp.Spec.Name = "cp-groupmember-2"
				},
			)
			deploy.KonnectGatewayControlPlane(t, ctx, cl, auth,
				func(obj client.Object) {
					cp := obj.(*konnectv1alpha1.KonnectGatewayControlPlane)
					cp.Name = "cp-3"
					cp.Spec.Name = "cp-3"
					cp.Spec.ClusterType = lo.ToPtr(sdkkonnectcomp.ClusterTypeClusterTypeControlPlaneGroup)
					cp.Spec.Members = []corev1.LocalObjectReference{
						{
							Name: "cp-groupmember-2",
						},
					}
				},
			)
		},
		mockExpectations: func(t *testing.T, sdk *ops.MockSDKWrapper, ns *corev1.Namespace) {
			sdk.ControlPlaneSDK.EXPECT().
				CreateControlPlane(
					mock.Anything,
					mock.MatchedBy(func(req sdkkonnectcomp.CreateControlPlaneRequest) bool {
						return req.Name == "cp-groupmember-2"
					}),
				).
				Return(
					&sdkkonnectops.CreateControlPlaneResponse{
						ControlPlane: &sdkkonnectcomp.ControlPlane{
							ID: lo.ToPtr("12345"),
						},
					},
					nil,
				)
			sdk.ControlPlaneSDK.EXPECT().
				CreateControlPlane(
					mock.Anything,
					mock.MatchedBy(func(req sdkkonnectcomp.CreateControlPlaneRequest) bool {
						return req.Name == "cp-3" &&
							req.ClusterType != nil &&
							*req.ClusterType == sdkkonnectcomp.ClusterTypeClusterTypeControlPlaneGroup
					}),
				).
				Return(
					&sdkkonnectops.CreateControlPlaneResponse{
						ControlPlane: &sdkkonnectcomp.ControlPlane{
							ID: lo.ToPtr("123467"),
						},
					},
					nil,
				)

			sdk.ControlPlaneGroupSDK.EXPECT().
				PutControlPlanesIDGroupMemberships(
					mock.Anything,
					"123467",
					&sdkkonnectcomp.GroupMembership{
						Members: []sdkkonnectcomp.Members{
							{
								ID: lo.ToPtr("12345"),
							},
						},
					},
				).
				Return(
					&sdkkonnectops.PutControlPlanesIDGroupMembershipsResponse{},
					errors.New("some error"),
				)

			sdk.ControlPlaneSDK.EXPECT().
				UpdateControlPlane(
					mock.Anything,
					"123467",
					mock.MatchedBy(func(req sdkkonnectcomp.UpdateControlPlaneRequest) bool {
						return req.Name != nil && *req.Name == "cp-3"
					}),
				).
				Return(
					&sdkkonnectops.UpdateControlPlaneResponse{
						ControlPlane: &sdkkonnectcomp.ControlPlane{
							ID: lo.ToPtr("123467"),
						},
					},
					nil)
			// verify that mock SDK is called as expected.
			t.Cleanup(func() {
				require.True(t, sdk.ControlPlaneSDK.AssertExpectations(t))
				require.True(t, sdk.ControlPlaneGroupSDK.AssertExpectations(t))
			})
		},
		eventuallyPredicate: func(ctx context.Context, t *assert.CollectT, cl client.Client, ns *corev1.Namespace) {
			cp := &konnectv1alpha1.KonnectGatewayControlPlane{}
			if !assert.NoError(t,
				cl.Get(ctx,
					k8stypes.NamespacedName{
						Namespace: ns.Name,
						Name:      "cp-groupmember-2",
					},
					cp,
				),
			) {
				return
			}
			assert.True(t, controllerutil.ContainsFinalizer(cp, konnect.KonnectCleanupFinalizer),
				"Finalizer should be set on control plane",
			)

			cpGroup := &konnectv1alpha1.KonnectGatewayControlPlane{}
			if !assert.NoError(t,
				cl.Get(ctx,
					k8stypes.NamespacedName{
						Namespace: ns.Name,
						Name:      "cp-3",
					},
					cpGroup,
				),
			) {
				return
			}

			assert.Equal(t, "123467", cpGroup.Status.ID)
			assert.True(t, conditionsContainProgrammedFalse(cpGroup.Status.Conditions),
				"Programmed condition should be set and its status should be false because of an error returned by Konnect API when setting group members",
			)
			assert.True(t, controllerutil.ContainsFinalizer(cpGroup, konnect.KonnectCleanupFinalizer),
				"Finalizer should be set on control plane group",
			)
		},
	},
}

func conditionsContainProgrammed(conds []metav1.Condition, status metav1.ConditionStatus) bool {
	return lo.ContainsBy(conds,
		func(condition metav1.Condition) bool {
			return condition.Type == konnectv1alpha1.KonnectEntityProgrammedConditionType &&
				condition.Status == status
		},
	)
}

func conditionsContainProgrammedFalse(conds []metav1.Condition) bool {
	return conditionsContainProgrammed(conds, metav1.ConditionFalse)
}

func conditionsContainProgrammedTrue(conds []metav1.Condition) bool {
	return conditionsContainProgrammed(conds, metav1.ConditionTrue)
}
