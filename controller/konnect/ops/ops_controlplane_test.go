package ops

import (
	"context"
	"testing"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	mock "github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kong/gateway-operator/modules/manager/scheme"
	"github.com/kong/gateway-operator/pkg/consts"

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

func TestCreateControlPlane(t *testing.T) {
	ctx := context.Background()
	testCases := []struct {
		name           string
		mockCPTuple    func(*testing.T) (*MockControlPlaneSDK, *MockControlPlaneGroupSDK, *konnectv1alpha1.KonnectGatewayControlPlane)
		expectedErr    bool
		expectedID     string
		expectedReason consts.ConditionReason
	}{
		{
			name: "success",
			mockCPTuple: func(t *testing.T) (*MockControlPlaneSDK, *MockControlPlaneGroupSDK, *konnectv1alpha1.KonnectGatewayControlPlane) {
				sdk := NewMockControlPlaneSDK(t)
				sdkGroups := NewMockControlPlaneGroupSDK(t)
				cp := &konnectv1alpha1.KonnectGatewayControlPlane{
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: sdkkonnectcomp.CreateControlPlaneRequest{
							Name: "cp-1",
						},
					},
				}

				expectedRequest := cp.Spec.CreateControlPlaneRequest
				expectedRequest.Labels = WithKubernetesMetadataLabels(cp, expectedRequest.Labels)
				sdk.
					EXPECT().
					CreateControlPlane(ctx, expectedRequest).
					Return(
						&sdkkonnectops.CreateControlPlaneResponse{
							ControlPlane: &sdkkonnectcomp.ControlPlane{
								ID: lo.ToPtr("12345"),
							},
						},
						nil,
					)

				return sdk, sdkGroups, cp
			},
			expectedID: "12345",
		},
		{
			name: "fail",
			mockCPTuple: func(t *testing.T) (*MockControlPlaneSDK, *MockControlPlaneGroupSDK, *konnectv1alpha1.KonnectGatewayControlPlane) {
				sdk := NewMockControlPlaneSDK(t)
				sdkGroups := NewMockControlPlaneGroupSDK(t)
				cp := &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cp-1",
						Namespace: "default",
					},
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: sdkkonnectcomp.CreateControlPlaneRequest{
							Name: "cp-1",
						},
					},
				}

				expectedRequest := cp.Spec.CreateControlPlaneRequest
				expectedRequest.Labels = WithKubernetesMetadataLabels(cp, expectedRequest.Labels)
				sdk.
					EXPECT().
					CreateControlPlane(ctx, expectedRequest).
					Return(
						nil,
						&sdkkonnecterrs.BadRequestError{
							Status: 400,
							Detail: "bad request",
						},
					)

				return sdk, sdkGroups, cp
			},
			expectedErr:    true,
			expectedReason: consts.KonnectEntitiesFailedToCreateReason,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sdk, sdkGroups, cp := tc.mockCPTuple(t)
			fakeClient := fake.NewClientBuilder().Build()

			id, reason, err := createControlPlane(ctx, sdk, sdkGroups, fakeClient, cp)
			if tc.expectedErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tc.expectedID, id)
			assert.Equal(t, tc.expectedReason, reason)
		})
	}
}

func TestDeleteControlPlane(t *testing.T) {
	ctx := context.Background()
	testCases := []struct {
		name        string
		mockCPPair  func(*testing.T) (*MockControlPlaneSDK, *konnectv1alpha1.KonnectGatewayControlPlane)
		expectedErr bool
		assertions  func(*testing.T, *konnectv1alpha1.KonnectGatewayControlPlane)
	}{
		{
			name: "success",
			mockCPPair: func(t *testing.T) (*MockControlPlaneSDK, *konnectv1alpha1.KonnectGatewayControlPlane) {
				sdk := NewMockControlPlaneSDK(t)
				cp := &konnectv1alpha1.KonnectGatewayControlPlane{
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: sdkkonnectcomp.CreateControlPlaneRequest{
							Name: "cp-1",
						},
					},
					Status: konnectv1alpha1.KonnectGatewayControlPlaneStatus{
						KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{
							ID: "12345",
						},
					},
				}
				sdk.
					EXPECT().
					DeleteControlPlane(ctx, "12345").
					Return(
						&sdkkonnectops.DeleteControlPlaneResponse{
							StatusCode: 200,
						},
						nil,
					)

				return sdk, cp
			},
		},
		{
			name: "fail",
			mockCPPair: func(t *testing.T) (*MockControlPlaneSDK, *konnectv1alpha1.KonnectGatewayControlPlane) {
				sdk := NewMockControlPlaneSDK(t)
				cp := &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cp-1",
						Namespace: "default",
					},
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: sdkkonnectcomp.CreateControlPlaneRequest{
							Name: "cp-1",
						},
					},
					Status: konnectv1alpha1.KonnectGatewayControlPlaneStatus{
						KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{
							ID: "12345",
						},
					},
				}
				sdk.
					EXPECT().
					DeleteControlPlane(ctx, "12345").
					Return(
						nil,
						&sdkkonnecterrs.BadRequestError{
							Status: 400,
							Detail: "bad request",
						},
					)

				return sdk, cp
			},
			expectedErr: true,
		},
		{
			name: "not found error is ignored and considered a success when trying to delete",
			mockCPPair: func(t *testing.T) (*MockControlPlaneSDK, *konnectv1alpha1.KonnectGatewayControlPlane) {
				sdk := NewMockControlPlaneSDK(t)
				cp := &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cp-1",
						Namespace: "default",
					},
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: sdkkonnectcomp.CreateControlPlaneRequest{
							Name: "cp-1",
						},
					},
					Status: konnectv1alpha1.KonnectGatewayControlPlaneStatus{
						KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{
							ID: "12345",
						},
					},
				}
				sdk.
					EXPECT().
					DeleteControlPlane(ctx, "12345").
					Return(
						nil,
						&sdkkonnecterrs.NotFoundError{
							Status: 404,
							Detail: "not found",
						},
					)

				return sdk, cp
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sdk, cp := tc.mockCPPair(t)

			err := deleteControlPlane(ctx, sdk, cp)

			if tc.assertions != nil {
				tc.assertions(t, cp)
			}

			if tc.expectedErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
		})
	}
}

func TestUpdateControlPlane(t *testing.T) {
	ctx := context.Background()
	testCases := []struct {
		name           string
		mockCPTuple    func(*testing.T) (*MockControlPlaneSDK, *MockControlPlaneGroupSDK, *konnectv1alpha1.KonnectGatewayControlPlane)
		expectedErr    bool
		expectedID     string
		expectedReason consts.ConditionReason
	}{
		{
			name: "success",
			mockCPTuple: func(t *testing.T) (*MockControlPlaneSDK, *MockControlPlaneGroupSDK, *konnectv1alpha1.KonnectGatewayControlPlane) {
				sdk := NewMockControlPlaneSDK(t)
				sdkGroups := NewMockControlPlaneGroupSDK(t)
				cp := &konnectv1alpha1.KonnectGatewayControlPlane{
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: sdkkonnectcomp.CreateControlPlaneRequest{
							Name: "cp-1",
						},
					},
					Status: konnectv1alpha1.KonnectGatewayControlPlaneStatus{
						KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{
							ID: "12345",
						},
					},
				}
				sdk.
					EXPECT().
					UpdateControlPlane(ctx, "12345",
						sdkkonnectcomp.UpdateControlPlaneRequest{
							Name:        sdkkonnectgo.String(cp.Spec.Name),
							Description: cp.Spec.Description,
							AuthType:    (*sdkkonnectcomp.UpdateControlPlaneRequestAuthType)(cp.Spec.AuthType),
							ProxyUrls:   cp.Spec.ProxyUrls,
							Labels:      WithKubernetesMetadataLabels(cp, cp.Spec.Labels),
						},
					).
					Return(
						&sdkkonnectops.UpdateControlPlaneResponse{
							ControlPlane: &sdkkonnectcomp.ControlPlane{
								ID: lo.ToPtr("12345"),
							},
						},
						nil,
					)

				return sdk, sdkGroups, cp
			},
			expectedID: "12345",
		},
		{
			name: "fail",
			mockCPTuple: func(t *testing.T) (*MockControlPlaneSDK, *MockControlPlaneGroupSDK, *konnectv1alpha1.KonnectGatewayControlPlane) {
				sdk := NewMockControlPlaneSDK(t)
				sdkGroups := NewMockControlPlaneGroupSDK(t)
				cp := &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cp-1",
						Namespace: "default",
					},
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: sdkkonnectcomp.CreateControlPlaneRequest{
							Name: "cp-1",
						},
					},
					Status: konnectv1alpha1.KonnectGatewayControlPlaneStatus{
						KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{
							ID: "12345",
						},
					},
				}

				sdk.
					EXPECT().
					UpdateControlPlane(ctx, "12345",
						sdkkonnectcomp.UpdateControlPlaneRequest{
							Name:        sdkkonnectgo.String(cp.Spec.Name),
							Description: cp.Spec.Description,
							AuthType:    (*sdkkonnectcomp.UpdateControlPlaneRequestAuthType)(cp.Spec.AuthType),
							ProxyUrls:   cp.Spec.ProxyUrls,
							Labels:      WithKubernetesMetadataLabels(cp, cp.Spec.Labels),
						},
					).
					Return(
						nil,
						&sdkkonnecterrs.BadRequestError{
							Status: 400,
							Detail: "bad request",
						},
					)

				return sdk, sdkGroups, cp
			},
			expectedReason: consts.KonnectEntitiesFailedToUpdateReason,
			expectedErr:    true,
		},
		{
			name: "when not found then try to create",
			mockCPTuple: func(t *testing.T) (*MockControlPlaneSDK, *MockControlPlaneGroupSDK, *konnectv1alpha1.KonnectGatewayControlPlane) {
				sdk := NewMockControlPlaneSDK(t)
				sdkGroups := NewMockControlPlaneGroupSDK(t)
				cp := &konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cp-1",
						Namespace: "default",
					},
					Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: sdkkonnectcomp.CreateControlPlaneRequest{
							Name: "cp-1",
						},
					},
					Status: konnectv1alpha1.KonnectGatewayControlPlaneStatus{
						KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{
							ID: "12345",
						},
					},
				}

				sdk.
					EXPECT().
					UpdateControlPlane(ctx, "12345",
						sdkkonnectcomp.UpdateControlPlaneRequest{
							Name:        sdkkonnectgo.String(cp.Spec.Name),
							Description: cp.Spec.Description,
							AuthType:    (*sdkkonnectcomp.UpdateControlPlaneRequestAuthType)(cp.Spec.AuthType),
							ProxyUrls:   cp.Spec.ProxyUrls,
							Labels:      WithKubernetesMetadataLabels(cp, cp.Spec.Labels),
						},
					).
					Return(
						nil,
						&sdkkonnecterrs.NotFoundError{
							Status: 404,
							Detail: "not found",
						},
					)

				expectedRequest := cp.Spec.CreateControlPlaneRequest
				expectedRequest.Labels = WithKubernetesMetadataLabels(cp, expectedRequest.Labels)
				sdk.
					EXPECT().
					CreateControlPlane(ctx, expectedRequest).
					Return(
						&sdkkonnectops.CreateControlPlaneResponse{
							ControlPlane: &sdkkonnectcomp.ControlPlane{
								ID: lo.ToPtr("12345"),
							},
						},
						nil,
					)

				return sdk, sdkGroups, cp
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sdk, sdkGroups, cp := tc.mockCPTuple(t)
			fakeClient := fake.NewClientBuilder().Build()

			id, reason, err := updateControlPlane(ctx, sdk, sdkGroups, fakeClient, cp)
			if tc.expectedErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tc.expectedID, id)
			assert.Equal(t, tc.expectedReason, reason)
		})
	}
}

func TestCreateAndUpdateControlPlane_KubernetesMetadataConsistency(t *testing.T) {
	var (
		ctx = context.Background()
		cp  = &konnectv1alpha1.KonnectGatewayControlPlane{
			TypeMeta: metav1.TypeMeta{
				Kind:       "KonnectGatewayControlPlane",
				APIVersion: "konnect.konghq.com/v1alpha1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:       "cp-1",
				Namespace:  "default",
				UID:        k8stypes.UID(uuid.NewString()),
				Generation: 2,
			},
			Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
				CreateControlPlaneRequest: sdkkonnectcomp.CreateControlPlaneRequest{
					Name: "cp-1",
				},
			},
		}
		sdk              = NewMockSDKFactory(t)
		sdkControlPlanes = sdk.SDK.ControlPlaneSDK
		fakeClient       = fake.NewClientBuilder().Build()
	)

	t.Log("Triggering CreateControlPlane with expected labels")
	expectedLabels := map[string]string{
		"k8s-name":       "cp-1",
		"k8s-namespace":  "default",
		"k8s-uid":        string(cp.GetUID()),
		"k8s-kind":       "KonnectGatewayControlPlane",
		"k8s-group":      "konnect.konghq.com",
		"k8s-version":    "v1alpha1",
		"k8s-generation": "2",
	}
	sdkControlPlanes.EXPECT().
		CreateControlPlane(ctx, sdkkonnectcomp.CreateControlPlaneRequest{
			Name:   "cp-1",
			Labels: expectedLabels,
		}).
		Return(&sdkkonnectops.CreateControlPlaneResponse{
			ControlPlane: &sdkkonnectcomp.ControlPlane{
				ID: lo.ToPtr("12345"),
			},
		}, nil)
	_, err := Create(ctx, sdk.SDK, fakeClient, cp)
	require.NoError(t, err)

	t.Log("Triggering UpdateControlPlane with expected labels")
	sdkControlPlanes.EXPECT().
		UpdateControlPlane(ctx, "12345", sdkkonnectcomp.UpdateControlPlaneRequest{
			Name:   lo.ToPtr("cp-1"),
			Labels: expectedLabels,
		}).
		Return(&sdkkonnectops.UpdateControlPlaneResponse{
			ControlPlane: &sdkkonnectcomp.ControlPlane{
				ID: lo.ToPtr("12345"),
			},
		}, nil)
	_, err = Update(ctx, sdk.SDK, 0, fakeClient, cp)
	require.NoError(t, err)
}

func TestSetGroupMembers(t *testing.T) {
	testcases := []struct {
		name        string
		group       *konnectv1alpha1.KonnectGatewayControlPlane
		cps         []client.Object
		sdk         func(t *testing.T) *MockControlPlaneGroupSDK
		expectedErr bool
	}{
		{
			name: "no members",
			group: &konnectv1alpha1.KonnectGatewayControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cp-group",
					Namespace: "default",
				},
				Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
					CreateControlPlaneRequest: sdkkonnectcomp.CreateControlPlaneRequest{
						Name:        "cp-group",
						ClusterType: lo.ToPtr(sdkkonnectcomp.ClusterTypeClusterTypeControlPlaneGroup),
					},
				},
			},
			sdk: func(t *testing.T) *MockControlPlaneGroupSDK {
				sdk := NewMockControlPlaneGroupSDK(t)
				return sdk
			},
		},
		{
			name: "1 member with Konnect Status ID",
			group: &konnectv1alpha1.KonnectGatewayControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cp-group",
					Namespace: "default",
				},
				Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
					CreateControlPlaneRequest: sdkkonnectcomp.CreateControlPlaneRequest{
						Name:        "cp-group",
						ClusterType: lo.ToPtr(sdkkonnectcomp.ClusterTypeClusterTypeControlPlaneGroup),
					},
					Members: []corev1.LocalObjectReference{
						{
							Name: "cp-1",
						},
					},
				},
			},
			cps: []client.Object{
				&konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cp-1",
						Namespace: "default",
					},
					Status: konnectv1alpha1.KonnectGatewayControlPlaneStatus{
						KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{
							ID: "cp-12345",
						},
					},
				},
			},
			sdk: func(t *testing.T) *MockControlPlaneGroupSDK {
				sdk := NewMockControlPlaneGroupSDK(t)
				sdk.EXPECT().
					PutControlPlanesIDGroupMemberships(
						mock.Anything,
						"cpg-12345",
						&sdkkonnectcomp.GroupMembership{
							Members: []sdkkonnectcomp.Members{
								{
									ID: lo.ToPtr("cp-12345"),
								},
							},
						},
					).
					Return(
						&sdkkonnectops.PutControlPlanesIDGroupMembershipsResponse{},
						nil,
					)
				return sdk
			},
		},
		{
			name: "1 member without Konnect Status ID",
			group: &konnectv1alpha1.KonnectGatewayControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cp-group",
					Namespace: "default",
				},
				Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
					CreateControlPlaneRequest: sdkkonnectcomp.CreateControlPlaneRequest{
						Name:        "cp-group",
						ClusterType: lo.ToPtr(sdkkonnectcomp.ClusterTypeClusterTypeControlPlaneGroup),
					},
					Members: []corev1.LocalObjectReference{
						{
							Name: "cp-1",
						},
					},
				},
			},
			cps: []client.Object{
				&konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cp-1",
						Namespace: "default",
					},
					Status: konnectv1alpha1.KonnectGatewayControlPlaneStatus{},
				},
			},
			sdk: func(t *testing.T) *MockControlPlaneGroupSDK {
				sdk := NewMockControlPlaneGroupSDK(t)
				return sdk
			},
			expectedErr: true,
		},
		{
			name: "2 member with Konnect Status IDs",
			group: &konnectv1alpha1.KonnectGatewayControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cp-group",
					Namespace: "default",
				},
				Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
					CreateControlPlaneRequest: sdkkonnectcomp.CreateControlPlaneRequest{
						Name:        "cp-group",
						ClusterType: lo.ToPtr(sdkkonnectcomp.ClusterTypeClusterTypeControlPlaneGroup),
					},
					Members: []corev1.LocalObjectReference{
						{
							Name: "cp-1",
						},
						{
							Name: "cp-2",
						},
					},
				},
			},
			cps: []client.Object{
				&konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cp-1",
						Namespace: "default",
					},
					Status: konnectv1alpha1.KonnectGatewayControlPlaneStatus{
						KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{
							ID: "cp-12345",
						},
					},
				},
				&konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cp-2",
						Namespace: "default",
					},
					Status: konnectv1alpha1.KonnectGatewayControlPlaneStatus{
						KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{
							ID: "cp-12346",
						},
					},
				},
			},
			sdk: func(t *testing.T) *MockControlPlaneGroupSDK {
				sdk := NewMockControlPlaneGroupSDK(t)
				sdk.EXPECT().
					PutControlPlanesIDGroupMemberships(
						mock.Anything,
						"cpg-12345",
						&sdkkonnectcomp.GroupMembership{
							Members: []sdkkonnectcomp.Members{
								{
									ID: lo.ToPtr("cp-12345"),
								},
								{
									ID: lo.ToPtr("cp-12346"),
								},
							},
						},
					).
					Return(
						&sdkkonnectops.PutControlPlanesIDGroupMembershipsResponse{},
						nil,
					)
				return sdk
			},
		},
		{
			name: "2 member, 1 with Konnect Status IDs, 1 without it",
			group: &konnectv1alpha1.KonnectGatewayControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cp-group",
					Namespace: "default",
				},
				Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
					CreateControlPlaneRequest: sdkkonnectcomp.CreateControlPlaneRequest{
						Name:        "cp-group",
						ClusterType: lo.ToPtr(sdkkonnectcomp.ClusterTypeClusterTypeControlPlaneGroup),
					},
					Members: []corev1.LocalObjectReference{
						{
							Name: "cp-1",
						},
						{
							Name: "cp-2",
						},
					},
				},
			},
			cps: []client.Object{
				&konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cp-1",
						Namespace: "default",
					},
					Status: konnectv1alpha1.KonnectGatewayControlPlaneStatus{
						KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{
							ID: "cp-12345",
						},
					},
				},
				&konnectv1alpha1.KonnectGatewayControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cp-2",
						Namespace: "default",
					},
				},
			},
			sdk: func(t *testing.T) *MockControlPlaneGroupSDK {
				sdk := NewMockControlPlaneGroupSDK(t)
				return sdk
			},
			expectedErr: true,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme.Get()).
				WithObjects(tc.group).
				WithObjects(tc.cps...).
				Build()

			sdk := tc.sdk(t)
			err := setGroupMembers(context.Background(), fakeClient, tc.group, "cpg-12345", sdk)
			if tc.expectedErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.True(t, sdk.AssertExpectations(t))
		})
	}
}
