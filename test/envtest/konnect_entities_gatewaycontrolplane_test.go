package envtest

import (
	"context"
	"errors"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	konnectv1alpha1 "github.com/kong/kong-operator/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/controller/konnect"
	"github.com/kong/kong-operator/controller/konnect/ops"
	"github.com/kong/kong-operator/test/helpers/deploy"
	"github.com/kong/kong-operator/test/mocks/sdkmocks"
)

var konnectGatewayControlPlaneTestCases = []konnectEntityReconcilerTestCase{
	{
		name: "should create control plane successfully",
		objectOps: func(ctx context.Context, t *testing.T, cl client.Client, ns *corev1.Namespace) {
			auth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, cl)
			deploy.KonnectGatewayControlPlane(t, ctx, cl, auth,
				func(obj client.Object) {
					cp := obj.(*konnectv1alpha2.KonnectGatewayControlPlane)
					cp.Name = "cp-1"
					cp.SetKonnectName("cp-1")
					cp.SetKonnectDescription(lo.ToPtr("test control plane 1"))
				},
			)
		},
		mockExpectations: func(t *testing.T, sdk *sdkmocks.MockSDKWrapper, cl client.Client, ns *corev1.Namespace) {
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
							ID: "12345",
						},
					},
					nil,
				)

			sdk.ControlPlaneSDK.EXPECT().
				ListControlPlanes(
					mock.Anything,
					mock.MatchedBy(func(r sdkkonnectops.ListControlPlanesRequest) bool {
						return *r.Filter.ID.Eq == "12345"
					}),
				).
				Return(
					&sdkkonnectops.ListControlPlanesResponse{
						ListControlPlanesResponse: &sdkkonnectcomp.ListControlPlanesResponse{
							Data: []sdkkonnectcomp.ControlPlane{
								{
									ID: "12345",
									Config: sdkkonnectcomp.ControlPlaneConfig{
										ControlPlaneEndpoint: "https://control-plane-endpoint",
										TelemetryEndpoint:    "https://telemetry-endpoint",
									},
								},
							},
						},
					},
					nil,
				)
		},
		eventuallyPredicate: func(ctx context.Context, t *assert.CollectT, cl client.Client, ns *corev1.Namespace) {
			cp := &konnectv1alpha2.KonnectGatewayControlPlane{}
			require.NoError(t,
				cl.Get(ctx,
					k8stypes.NamespacedName{
						Namespace: ns.Name,
						Name:      "cp-1",
					},
					cp,
				),
			)

			assert.Equal(t, "12345", cp.Status.ID)
			assert.True(t, conditionsContainProgrammedTrue(cp.Status.Conditions),
				"Programmed condition should be set and it status should be true",
			)
			assert.True(t, controllerutil.ContainsFinalizer(cp, konnect.KonnectCleanupFinalizer),
				"Finalizer should be set on control plane group",
			)
			require.NotNil(t, cp.Status.Endpoints)
			assert.Equal(t, "https://control-plane-endpoint", cp.Status.Endpoints.ControlPlaneEndpoint)
			assert.Equal(t, "https://telemetry-endpoint", cp.Status.Endpoints.TelemetryEndpoint)
		},
	},
	{
		name: "should create control plane group and control plane as member successfully",
		objectOps: func(ctx context.Context, t *testing.T, cl client.Client, ns *corev1.Namespace) {
			auth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, cl)
			deploy.KonnectGatewayControlPlane(t, ctx, cl, auth,
				func(obj client.Object) {
					cp := obj.(*konnectv1alpha2.KonnectGatewayControlPlane)
					cp.Name = "cp-groupmember-1"
					cp.SetKonnectName("cp-groupmember-1")
				},
			)
			deploy.KonnectGatewayControlPlane(t, ctx, cl, auth,
				func(obj client.Object) {
					cp := obj.(*konnectv1alpha2.KonnectGatewayControlPlane)
					cp.Name = "cp-2"
					cp.SetKonnectName("cp-2")
					cp.SetKonnectClusterType(lo.ToPtr(sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlaneGroup))
					cp.Spec.Members = []corev1.LocalObjectReference{
						{
							Name: "cp-groupmember-1",
						},
					}
				},
			)
		},
		mockExpectations: func(t *testing.T, sdk *sdkmocks.MockSDKWrapper, cl client.Client, ns *corev1.Namespace) {
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
							ID: "12345",
						},
					},
					nil)
			sdk.ControlPlaneSDK.EXPECT().
				CreateControlPlane(
					mock.Anything,
					mock.MatchedBy(func(req sdkkonnectcomp.CreateControlPlaneRequest) bool {
						return req.Name == "cp-2" &&
							req.ClusterType != nil && *req.ClusterType == sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlaneGroup
					}),
				).
				Return(
					&sdkkonnectops.CreateControlPlaneResponse{
						ControlPlane: &sdkkonnectcomp.ControlPlane{
							ID: "12346",
						},
					},
					nil)

			sdk.ControlPlaneSDK.EXPECT().
				ListControlPlanes(
					mock.Anything,
					mock.MatchedBy(func(r sdkkonnectops.ListControlPlanesRequest) bool {
						return *r.Filter.ID.Eq == "12346"
					}),
				).
				Return(
					&sdkkonnectops.ListControlPlanesResponse{
						ListControlPlanesResponse: &sdkkonnectcomp.ListControlPlanesResponse{
							Data: []sdkkonnectcomp.ControlPlane{
								{
									ID: "12346",
									Config: sdkkonnectcomp.ControlPlaneConfig{
										ControlPlaneEndpoint: "https://control-plane-endpoint",
										TelemetryEndpoint:    "https://telemetry-endpoint",
									},
								},
							},
						},
					},
					nil,
				)

			sdk.ControlPlaneGroupSDK.EXPECT().
				PutControlPlanesIDGroupMemberships(
					mock.Anything,
					"12346",
					&sdkkonnectcomp.GroupMembership{
						Members: []sdkkonnectcomp.Members{
							{
								ID: "12345",
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
							ID: "12346",
						},
					},
					nil).
				// NOTE: UpdateControlPlane can be called depending on the order
				// of the events in the queue: either the group itself or the member
				// control plane can be created first.
				Maybe()
		},
		eventuallyPredicate: func(ctx context.Context, t *assert.CollectT, cl client.Client, ns *corev1.Namespace) {
			cp := &konnectv1alpha2.KonnectGatewayControlPlane{}
			require.NoError(t,
				cl.Get(ctx,
					k8stypes.NamespacedName{
						Namespace: ns.Name,
						Name:      "cp-groupmember-1",
					},
					cp,
				),
			)

			assert.Equal(t, "12345", cp.Status.ID)
			assert.True(t, conditionsContainProgrammedTrue(cp.Status.Conditions),
				"Programmed condition should be set and it status should be true",
			)
			assert.True(t, controllerutil.ContainsFinalizer(cp, konnect.KonnectCleanupFinalizer),
				"Finalizer should be set on control plane",
			)
			require.NotNil(t, cp.Status.Endpoints)
			assert.Equal(t, "https://control-plane-endpoint", cp.Status.Endpoints.ControlPlaneEndpoint)
			assert.Equal(t, "https://telemetry-endpoint", cp.Status.Endpoints.TelemetryEndpoint)

			cpGroup := &konnectv1alpha2.KonnectGatewayControlPlane{}
			require.NoError(t,
				cl.Get(ctx,
					k8stypes.NamespacedName{
						Namespace: ns.Name,
						Name:      "cp-2",
					},
					cpGroup,
				),
			)

			assert.Equal(t, "12346", cpGroup.Status.ID)
			assert.True(t, conditionsContainProgrammedTrue(cpGroup.Status.Conditions),
				"Programmed condition should be set and it status should be true",
			)
			assert.True(t, conditionsContainMembersRefResolvedTrue(cpGroup.Status.Conditions),
				"MembersReferenceResolved condition should be set and it status should be true",
			)
			assert.True(t, controllerutil.ContainsFinalizer(cpGroup, konnect.KonnectCleanupFinalizer),
				"Finalizer should be set on control plane group",
			)
			require.NotNil(t, cpGroup.Status.Endpoints)
			assert.Equal(t, "https://control-plane-endpoint", cpGroup.Status.Endpoints.ControlPlaneEndpoint)
			assert.Equal(t, "https://telemetry-endpoint", cpGroup.Status.Endpoints.TelemetryEndpoint)
		},
	},
	{
		name: "control plane group with members when receiving an error from PutControlPlanesIDGroupMemberships, correctly sets the ID and finalizer on group",
		objectOps: func(ctx context.Context, t *testing.T, cl client.Client, ns *corev1.Namespace) {
			auth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, cl)
			deploy.KonnectGatewayControlPlane(t, ctx, cl, auth,
				func(obj client.Object) {
					cp := obj.(*konnectv1alpha2.KonnectGatewayControlPlane)
					cp.Name = "cp-groupmember-2"
					cp.SetKonnectName("cp-groupmember-2")
				},
			)
			deploy.KonnectGatewayControlPlane(t, ctx, cl, auth,
				func(obj client.Object) {
					cp := obj.(*konnectv1alpha2.KonnectGatewayControlPlane)
					cp.Name = "cp-3"
					cp.SetKonnectName("cp-3")
					cp.SetKonnectClusterType(lo.ToPtr(sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlaneGroup))
					cp.Spec.Members = []corev1.LocalObjectReference{
						{
							Name: "cp-groupmember-2",
						},
					}
				},
			)
		},
		mockExpectations: func(t *testing.T, sdk *sdkmocks.MockSDKWrapper, cl client.Client, ns *corev1.Namespace) {
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
							ID: "12345",
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
							*req.ClusterType == sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlaneGroup
					}),
				).
				Return(
					&sdkkonnectops.CreateControlPlaneResponse{
						ControlPlane: &sdkkonnectcomp.ControlPlane{
							ID: "123467",
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
								ID: "12345",
							},
						},
					},
				).
				Return(
					&sdkkonnectops.PutControlPlanesIDGroupMembershipsResponse{},
					errors.New("some error"),
				)

			sdk.ControlPlaneSDK.EXPECT().
				ListControlPlanes(
					mock.Anything,
					mock.MatchedBy(func(r sdkkonnectops.ListControlPlanesRequest) bool {
						return *r.Filter.ID.Eq == "123467"
					}),
				).
				Return(
					&sdkkonnectops.ListControlPlanesResponse{
						ListControlPlanesResponse: &sdkkonnectcomp.ListControlPlanesResponse{
							Data: []sdkkonnectcomp.ControlPlane{
								{
									ID: "123467",
									Config: sdkkonnectcomp.ControlPlaneConfig{
										ControlPlaneEndpoint: "https://control-plane-endpoint",
										TelemetryEndpoint:    "https://telemetry-endpoint",
									},
								},
							},
						},
					},
					nil,
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
							ID: "123467",
						},
					},
					nil,
				).
				// NOTE: UpdateControlPlane can be called depending on the order
				// of the events in the queue: either the group itself or the member
				// control plane can be created first.
				Maybe()
		},
		eventuallyPredicate: func(ctx context.Context, t *assert.CollectT, cl client.Client, ns *corev1.Namespace) {
			cp := &konnectv1alpha2.KonnectGatewayControlPlane{}
			require.NoError(t,
				cl.Get(ctx,
					k8stypes.NamespacedName{
						Namespace: ns.Name,
						Name:      "cp-groupmember-2",
					},
					cp,
				),
			)

			assert.True(t, conditionsContainProgrammedTrue(cp.Status.Conditions),
				"Programmed condition should be set and its status should be true",
			)
			assert.True(t, controllerutil.ContainsFinalizer(cp, konnect.KonnectCleanupFinalizer),
				"Finalizer should be set on control plane",
			)

			cpGroup := &konnectv1alpha2.KonnectGatewayControlPlane{}
			require.NoError(t,
				cl.Get(ctx,
					k8stypes.NamespacedName{
						Namespace: ns.Name,
						Name:      "cp-3",
					},
					cpGroup,
				),
			)

			assert.Equal(t, "123467", cpGroup.Status.ID)
			assert.True(t, conditionsContainProgrammedFalse(cpGroup.Status.Conditions),
				"Programmed condition should be set and its status should be false because of an error returned by Konnect API when setting group members",
			)
			assert.True(t, conditionsContainMembersRefResolvedFalse(cpGroup.Status.Conditions),
				"MembersReferenceResolved condition should be set and it status should be false",
			)
			assert.True(t, controllerutil.ContainsFinalizer(cpGroup, konnect.KonnectCleanupFinalizer),
				"Finalizer should be set on control plane group",
			)
		},
	},
	{
		name: "receiving HTTP Conflict 409 on creation results in lookup by UID and setting Konnect ID",
		objectOps: func(ctx context.Context, t *testing.T, cl client.Client, ns *corev1.Namespace) {
			auth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, cl)
			deploy.KonnectGatewayControlPlane(t, ctx, cl, auth,
				func(obj client.Object) {
					cp := obj.(*konnectv1alpha2.KonnectGatewayControlPlane)
					cp.Name = "cp-4"
					cp.SetKonnectName("cp-4")
				},
			)
		},
		mockExpectations: func(t *testing.T, sdk *sdkmocks.MockSDKWrapper, cl client.Client, ns *corev1.Namespace) {
			sdk.ControlPlaneSDK.EXPECT().
				CreateControlPlane(
					mock.Anything,
					mock.MatchedBy(func(req sdkkonnectcomp.CreateControlPlaneRequest) bool {
						return req.Name == "cp-4"
					}),
				).
				Return(
					nil,
					&sdkkonnecterrs.ConflictError{},
				)

			sdk.ControlPlaneSDK.EXPECT().
				ListControlPlanes(
					mock.Anything,
					mock.MatchedBy(func(r sdkkonnectops.ListControlPlanesRequest) bool {
						var cp konnectv1alpha2.KonnectGatewayControlPlane
						require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Name: "cp-4"}, &cp))
						// On conflict, we list cps by UID and check if there is already one created.
						return r.FilterLabels != nil && *r.FilterLabels == ops.KubernetesUIDLabelKey+":"+string(cp.UID)
					}),
				).
				Return(
					&sdkkonnectops.ListControlPlanesResponse{
						ListControlPlanesResponse: &sdkkonnectcomp.ListControlPlanesResponse{
							Data: []sdkkonnectcomp.ControlPlane{
								{
									ID: "123456",
									Config: sdkkonnectcomp.ControlPlaneConfig{
										ControlPlaneEndpoint: "https://control-plane-endpoint",
										TelemetryEndpoint:    "https://telemetry-endpoint",
									},
								},
							},
						},
					},
					nil,
				)

			sdk.ControlPlaneSDK.EXPECT().
				ListControlPlanes(
					mock.Anything,
					mock.MatchedBy(func(r sdkkonnectops.ListControlPlanesRequest) bool {
						return *r.Filter.ID.Eq == "123456"
					}),
				).
				Return(
					&sdkkonnectops.ListControlPlanesResponse{
						ListControlPlanesResponse: &sdkkonnectcomp.ListControlPlanesResponse{
							Data: []sdkkonnectcomp.ControlPlane{
								{
									ID: "123456",
									Config: sdkkonnectcomp.ControlPlaneConfig{
										ControlPlaneEndpoint: "https://control-plane-endpoint",
										TelemetryEndpoint:    "https://telemetry-endpoint",
									},
								},
							},
						},
					},
					nil,
				)
		},
		eventuallyPredicate: func(ctx context.Context, t *assert.CollectT, cl client.Client, ns *corev1.Namespace) {
			cp := &konnectv1alpha2.KonnectGatewayControlPlane{}
			require.NoError(t,
				cl.Get(ctx,
					k8stypes.NamespacedName{
						Namespace: ns.Name,
						Name:      "cp-4",
					},
					cp,
				),
			)

			assert.Equal(t, "123456", cp.Status.ID, "ID should be set")
			assert.True(t, conditionsContainProgrammedTrue(cp.Status.Conditions),
				"Programmed condition should be set and its status should be true",
			)
			assert.True(t, controllerutil.ContainsFinalizer(cp, konnect.KonnectCleanupFinalizer),
				"Finalizer should be set on control plane",
			)
			require.NotNil(t, cp.Status.Endpoints)
			assert.Equal(t, "https://control-plane-endpoint", cp.Status.Endpoints.ControlPlaneEndpoint)
			assert.Equal(t, "https://telemetry-endpoint", cp.Status.Endpoints.TelemetryEndpoint)
		},
	},
	{
		name: "receiving HTTP Conflict 409 on creation for creating control plane group should have members set",
		objectOps: func(ctx context.Context, t *testing.T, cl client.Client, ns *corev1.Namespace) {
			auth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, cl)
			deploy.KonnectGatewayControlPlane(t, ctx, cl, auth,
				func(obj client.Object) {
					cp := obj.(*konnectv1alpha2.KonnectGatewayControlPlane)
					cp.Name = "cp-5"
					cp.SetKonnectName("cp-5")
				},
			)

			deploy.KonnectGatewayControlPlane(t, ctx, cl, auth,
				func(obj client.Object) {
					cp := obj.(*konnectv1alpha2.KonnectGatewayControlPlane)
					cp.Name = "cp-group-1"
					cp.SetKonnectName("cp-group-1")
					cp.SetKonnectClusterType(lo.ToPtr(sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlaneGroup))
					cp.Spec.Members = []corev1.LocalObjectReference{
						{Name: "cp-5"},
					}
				},
			)
		},
		mockExpectations: func(t *testing.T, sdk *sdkmocks.MockSDKWrapper, cl client.Client, ns *corev1.Namespace) {
			sdk.ControlPlaneSDK.EXPECT().
				CreateControlPlane(
					mock.Anything,
					mock.MatchedBy(func(req sdkkonnectcomp.CreateControlPlaneRequest) bool {
						return req.Name == "cp-5"
					}),
				).
				Return(
					&sdkkonnectops.CreateControlPlaneResponse{
						ControlPlane: &sdkkonnectcomp.ControlPlane{
							ID: "123456",
						},
					}, nil,
				)

			sdk.ControlPlaneSDK.EXPECT().
				CreateControlPlane(
					mock.Anything,
					mock.MatchedBy(func(req sdkkonnectcomp.CreateControlPlaneRequest) bool {
						return req.Name == "cp-group-1"
					}),
				).
				Return(
					nil,
					&sdkkonnecterrs.ConflictError{},
				)

			sdk.ControlPlaneSDK.EXPECT().
				ListControlPlanes(
					mock.Anything,
					mock.MatchedBy(func(r sdkkonnectops.ListControlPlanesRequest) bool {
						var cp konnectv1alpha2.KonnectGatewayControlPlane
						require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Name: "cp-group-1"}, &cp))
						// On conflict, we list cps by UID and check if there is already one created.
						return r.FilterLabels != nil && *r.FilterLabels == ops.KubernetesUIDLabelKey+":"+string(cp.UID)
					}),
				).
				Return(
					&sdkkonnectops.ListControlPlanesResponse{
						ListControlPlanesResponse: &sdkkonnectcomp.ListControlPlanesResponse{
							Data: []sdkkonnectcomp.ControlPlane{
								{
									ID: "group-123456",
									Config: sdkkonnectcomp.ControlPlaneConfig{
										ControlPlaneEndpoint: "https://control-plane-endpoint",
										TelemetryEndpoint:    "https://telemetry-endpoint",
									},
								},
							},
						},
					},
					nil,
				)

			sdk.ControlPlaneSDK.EXPECT().
				ListControlPlanes(
					mock.Anything,
					mock.MatchedBy(func(r sdkkonnectops.ListControlPlanesRequest) bool {
						return *r.Filter.ID.Eq == "group-123456"
					}),
				).
				Return(
					&sdkkonnectops.ListControlPlanesResponse{
						ListControlPlanesResponse: &sdkkonnectcomp.ListControlPlanesResponse{
							Data: []sdkkonnectcomp.ControlPlane{
								{
									ID: "group-123456",
									Config: sdkkonnectcomp.ControlPlaneConfig{
										ControlPlaneEndpoint: "https://control-plane-endpoint",
										TelemetryEndpoint:    "https://telemetry-endpoint",
									},
								},
							},
						},
					},
					nil,
				)

			sdk.ControlPlaneSDK.EXPECT().
				ListControlPlanes(
					mock.Anything,
					mock.MatchedBy(func(r sdkkonnectops.ListControlPlanesRequest) bool {
						return *r.Filter.ID.Eq == "123456"
					}),
				).
				Return(
					&sdkkonnectops.ListControlPlanesResponse{
						ListControlPlanesResponse: &sdkkonnectcomp.ListControlPlanesResponse{
							Data: []sdkkonnectcomp.ControlPlane{
								{
									ID: "123456",
									Config: sdkkonnectcomp.ControlPlaneConfig{
										ControlPlaneEndpoint: "https://control-plane-endpoint",
										TelemetryEndpoint:    "https://telemetry-endpoint",
									},
								},
							},
						},
					},
					nil,
				)

			sdk.ControlPlaneGroupSDK.EXPECT().
				PutControlPlanesIDGroupMemberships(
					mock.Anything,
					"group-123456",
					&sdkkonnectcomp.GroupMembership{
						Members: []sdkkonnectcomp.Members{
							{
								ID: "123456",
							},
						},
					},
				).
				Return(&sdkkonnectops.PutControlPlanesIDGroupMembershipsResponse{}, nil)

			sdk.ControlPlaneSDK.EXPECT().
				UpdateControlPlane(
					mock.Anything,
					"group-123456",
					mock.MatchedBy(func(req sdkkonnectcomp.UpdateControlPlaneRequest) bool {
						return req.Name != nil && *req.Name == "cp-group-1"
					}),
				).
				Return(
					&sdkkonnectops.UpdateControlPlaneResponse{
						ControlPlane: &sdkkonnectcomp.ControlPlane{
							ID: "group-123456",
						},
					},
					nil,
				).
				// NOTE: UpdateControlPlane can be called depending on the order
				// of the events in the queue: either the group itself or the member
				// control plane can be created first.
				Maybe()
		},
		eventuallyPredicate: func(ctx context.Context, t *assert.CollectT, cl client.Client, ns *corev1.Namespace) {
			cpGroup := &konnectv1alpha2.KonnectGatewayControlPlane{}
			require.NoError(t,
				cl.Get(ctx,
					k8stypes.NamespacedName{
						Namespace: ns.Name,
						Name:      "cp-group-1",
					},
					cpGroup,
				),
			)

			assert.Equal(t, "group-123456", cpGroup.Status.ID, "ID should be set")
			assert.True(t, conditionsContainProgrammedTrue(cpGroup.Status.Conditions),
				"Programmed condition should be set and its status should be true",
			)
			assert.True(t, conditionsContainMembersRefResolvedTrue(cpGroup.Status.Conditions),
				"MembersReferenceResolved condition should be set and its status should be true",
			)
			require.NotNil(t, cpGroup.Status.Endpoints)
			assert.Equal(t, "https://control-plane-endpoint", cpGroup.Status.Endpoints.ControlPlaneEndpoint)
			assert.Equal(t, "https://telemetry-endpoint", cpGroup.Status.Endpoints.TelemetryEndpoint)
		},
	},
	{
		name: "control plane group members set are set to 0 members when no members are listed in the spec",
		objectOps: func(ctx context.Context, t *testing.T, cl client.Client, ns *corev1.Namespace) {
			auth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, cl)

			deploy.KonnectGatewayControlPlane(t, ctx, cl, auth,
				func(obj client.Object) {
					cp := obj.(*konnectv1alpha2.KonnectGatewayControlPlane)
					cp.Name = "cp-group-no-members"
					cp.SetKonnectName("cp-group-no-members")
					cp.SetKonnectClusterType(lo.ToPtr(sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlaneGroup))
				},
			)
		},
		mockExpectations: func(t *testing.T, sdk *sdkmocks.MockSDKWrapper, cl client.Client, ns *corev1.Namespace) {
			sdk.ControlPlaneSDK.EXPECT().
				CreateControlPlane(
					mock.Anything,
					mock.MatchedBy(func(req sdkkonnectcomp.CreateControlPlaneRequest) bool {
						return req.Name == "cp-group-no-members"
					}),
				).
				Return(
					&sdkkonnectops.CreateControlPlaneResponse{
						ControlPlane: &sdkkonnectcomp.ControlPlane{
							ID: "cpg-id",
						},
					},
					nil,
				)

			sdk.ControlPlaneGroupSDK.EXPECT().
				PutControlPlanesIDGroupMemberships(
					mock.Anything,
					"cpg-id",
					&sdkkonnectcomp.GroupMembership{
						Members: []sdkkonnectcomp.Members{},
					},
				).
				Return(&sdkkonnectops.PutControlPlanesIDGroupMembershipsResponse{}, nil)

			sdk.ControlPlaneSDK.EXPECT().
				ListControlPlanes(
					mock.Anything,
					mock.MatchedBy(func(r sdkkonnectops.ListControlPlanesRequest) bool {
						return *r.Filter.ID.Eq == "cpg-id"
					}),
				).
				Return(
					&sdkkonnectops.ListControlPlanesResponse{
						ListControlPlanesResponse: &sdkkonnectcomp.ListControlPlanesResponse{
							Data: []sdkkonnectcomp.ControlPlane{
								{
									ID: "cpg-id",
									Config: sdkkonnectcomp.ControlPlaneConfig{
										ControlPlaneEndpoint: "https://control-plane-endpoint",
										TelemetryEndpoint:    "https://telemetry-endpoint",
									},
								},
							},
						},
					},
					nil,
				)
		},
		eventuallyPredicate: func(ctx context.Context, t *assert.CollectT, cl client.Client, ns *corev1.Namespace) {
			cpGroup := &konnectv1alpha2.KonnectGatewayControlPlane{}
			require.NoError(t,
				cl.Get(ctx,
					k8stypes.NamespacedName{
						Namespace: ns.Name,
						Name:      "cp-group-no-members",
					},
					cpGroup,
				),
			)

			assert.Equal(t, "cpg-id", cpGroup.Status.ID, "ID should be set")
			assert.True(t, conditionsContainProgrammedTrue(cpGroup.Status.Conditions),
				"Programmed condition should be set and its status should be true",
			)
			assert.True(t, conditionsContainMembersRefResolvedTrue(cpGroup.Status.Conditions),
				"MembersRefernceResolved condition should be set and its status should be true",
			)
			require.NotNil(t, cpGroup.Status.Endpoints)
			assert.Equal(t, "https://control-plane-endpoint", cpGroup.Status.Endpoints.ControlPlaneEndpoint)
			assert.Equal(t, "https://telemetry-endpoint", cpGroup.Status.Endpoints.TelemetryEndpoint)
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

func conditionsContainMembersRefResolved(conds []metav1.Condition, status metav1.ConditionStatus) bool {
	return lo.ContainsBy(conds,
		func(condition metav1.Condition) bool {
			return condition.Type == ops.ControlPlaneGroupMembersReferenceResolvedConditionType &&
				condition.Status == status
		},
	)
}

func conditionsContainMembersRefResolvedFalse(conds []metav1.Condition) bool {
	return conditionsContainMembersRefResolved(conds, metav1.ConditionFalse)
}

func conditionsContainMembersRefResolvedTrue(conds []metav1.Condition) bool {
	return conditionsContainMembersRefResolved(conds, metav1.ConditionTrue)
}
