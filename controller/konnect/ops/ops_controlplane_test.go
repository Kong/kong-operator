package ops

import (
	"errors"
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

	kcfgconsts "github.com/kong/kong-operator/api/common/consts"
	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/modules/manager/scheme"
	"github.com/kong/kong-operator/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/test/mocks/sdkmocks"
)

func TestCreateControlPlane(t *testing.T) {
	const (
		cpID  = "cp-id"
		cpgID = "cpg-id"
	)
	ctx := t.Context()
	testCases := []struct {
		name                string
		mockCPTuple         func(*testing.T) (*sdkmocks.MockControlPlaneSDK, *sdkmocks.MockControlPlaneGroupSDK, *konnectv1alpha2.KonnectGatewayControlPlane)
		objects             []client.Object
		expectedErrContains string
		expectedErrType     error
		expectedID          string
	}{
		{
			name: "success",
			mockCPTuple: func(t *testing.T) (*sdkmocks.MockControlPlaneSDK, *sdkmocks.MockControlPlaneGroupSDK, *konnectv1alpha2.KonnectGatewayControlPlane) {
				sdk := sdkmocks.NewMockControlPlaneSDK(t)
				sdkGroups := sdkmocks.NewMockControlPlaneGroupSDK(t)
				cp := &konnectv1alpha2.KonnectGatewayControlPlane{
					Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
						Source: lo.ToPtr(commonv1alpha1.EntitySourceOrigin),
						CreateControlPlaneRequest: &sdkkonnectcomp.CreateControlPlaneRequest{
							Name: "cp-1",
						},
					},
				}

				expectedRequest := cp.Spec.CreateControlPlaneRequest.DeepCopy()
				expectedRequest.Labels = WithKubernetesMetadataLabels(cp, expectedRequest.Labels)
				sdk.
					EXPECT().
					CreateControlPlane(ctx, *expectedRequest).
					Return(
						&sdkkonnectops.CreateControlPlaneResponse{
							ControlPlane: &sdkkonnectcomp.ControlPlane{
								ID: cpID,
							},
						},
						nil,
					)

				return sdk, sdkGroups, cp
			},
			expectedID: cpID,
		},
		{
			name: "fail",
			mockCPTuple: func(t *testing.T) (*sdkmocks.MockControlPlaneSDK, *sdkmocks.MockControlPlaneGroupSDK, *konnectv1alpha2.KonnectGatewayControlPlane) {
				sdk := sdkmocks.NewMockControlPlaneSDK(t)
				sdkGroups := sdkmocks.NewMockControlPlaneGroupSDK(t)
				cp := &konnectv1alpha2.KonnectGatewayControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cp-1",
						Namespace: "default",
					},
					Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
						Source: lo.ToPtr(commonv1alpha1.EntitySourceOrigin),
						CreateControlPlaneRequest: &sdkkonnectcomp.CreateControlPlaneRequest{
							Name: "cp-1",
						},
					},
				}

				expectedRequest := cp.Spec.CreateControlPlaneRequest.DeepCopy()
				expectedRequest.Labels = WithKubernetesMetadataLabels(cp, expectedRequest.Labels)
				sdk.
					EXPECT().
					CreateControlPlane(ctx, *expectedRequest).
					Return(
						nil,
						&sdkkonnecterrs.BadRequestError{
							Status: 400,
							Detail: "bad request",
						},
					)

				return sdk, sdkGroups, cp
			},
			expectedErrContains: "failed to create KonnectGatewayControlPlane default/cp-1",
		},
		{
			name: "cp membership creation success",
			objects: []client.Object{
				&konnectv1alpha2.KonnectGatewayControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cp-1",
						Namespace: "default",
					},
					Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
						Source: lo.ToPtr(commonv1alpha1.EntitySourceOrigin),
						CreateControlPlaneRequest: &sdkkonnectcomp.CreateControlPlaneRequest{
							Name: "cp-1",
						},
					},
					Status: konnectv1alpha2.KonnectGatewayControlPlaneStatus{
						KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
							ID: cpID,
						},
						Conditions: []metav1.Condition{
							{
								Type:   konnectv1alpha1.KonnectEntityProgrammedConditionType,
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			mockCPTuple: func(t *testing.T) (*sdkmocks.MockControlPlaneSDK, *sdkmocks.MockControlPlaneGroupSDK, *konnectv1alpha2.KonnectGatewayControlPlane) {
				sdk := sdkmocks.NewMockControlPlaneSDK(t)
				sdkGroups := sdkmocks.NewMockControlPlaneGroupSDK(t)
				cp := &konnectv1alpha2.KonnectGatewayControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cpg-1",
						Namespace: "default",
					},
					Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: &sdkkonnectcomp.CreateControlPlaneRequest{
							ClusterType: lo.ToPtr(sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlaneGroup),
							Name:        "cpg-1",
						},
						Source: lo.ToPtr(commonv1alpha1.EntitySourceOrigin),
						Members: []corev1.LocalObjectReference{
							{
								Name: "cp-1",
							},
						},
					},
				}
				expectedRequest := cp.Spec.CreateControlPlaneRequest.DeepCopy()
				expectedRequest.Labels = WithKubernetesMetadataLabels(cp, expectedRequest.Labels)
				sdk.
					EXPECT().
					CreateControlPlane(ctx, *expectedRequest).
					Return(
						&sdkkonnectops.CreateControlPlaneResponse{
							ControlPlane: &sdkkonnectcomp.ControlPlane{
								ID: cpgID,
							},
						},
						nil,
					)

				sdkGroups.
					EXPECT().
					PutControlPlanesIDGroupMemberships(ctx, cpgID, &sdkkonnectcomp.GroupMembership{
						Members: []sdkkonnectcomp.Members{
							{
								ID: cpID,
							},
						},
					}).
					Return(&sdkkonnectops.PutControlPlanesIDGroupMembershipsResponse{}, nil)

				return sdk, sdkGroups, cp
			},
		},
		{
			name: "cp membership creation failure",
			objects: []client.Object{
				&konnectv1alpha2.KonnectGatewayControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cp-1",
						Namespace: "default",
					},
					Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
						Source: lo.ToPtr(commonv1alpha1.EntitySourceOrigin),
						CreateControlPlaneRequest: &sdkkonnectcomp.CreateControlPlaneRequest{
							Name: "cp-1",
						},
					},
					Status: konnectv1alpha2.KonnectGatewayControlPlaneStatus{
						KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
							ID: cpID,
						},
						Conditions: []metav1.Condition{
							{
								Type:   konnectv1alpha1.KonnectEntityProgrammedConditionType,
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			mockCPTuple: func(t *testing.T) (*sdkmocks.MockControlPlaneSDK, *sdkmocks.MockControlPlaneGroupSDK, *konnectv1alpha2.KonnectGatewayControlPlane) {
				sdk := sdkmocks.NewMockControlPlaneSDK(t)
				sdkGroups := sdkmocks.NewMockControlPlaneGroupSDK(t)
				cp := &konnectv1alpha2.KonnectGatewayControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cpg-1",
						Namespace: "default",
					},
					Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: &sdkkonnectcomp.CreateControlPlaneRequest{
							ClusterType: lo.ToPtr(sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlaneGroup),
							Name:        "cpg-1",
						},
						Source: lo.ToPtr(commonv1alpha1.EntitySourceOrigin),
						Members: []corev1.LocalObjectReference{
							{
								Name: "cp-1",
							},
						},
					},
				}
				expectedRequest := cp.Spec.CreateControlPlaneRequest.DeepCopy()
				expectedRequest.Labels = WithKubernetesMetadataLabels(cp, expectedRequest.Labels)
				sdk.
					EXPECT().
					CreateControlPlane(ctx, *expectedRequest).
					Return(
						&sdkkonnectops.CreateControlPlaneResponse{
							ControlPlane: &sdkkonnectcomp.ControlPlane{
								ID: cpgID,
							},
						},
						nil,
					)

				sdkGroups.
					EXPECT().
					PutControlPlanesIDGroupMemberships(ctx, cpgID, &sdkkonnectcomp.GroupMembership{
						Members: []sdkkonnectcomp.Members{
							{
								ID: cpID,
							},
						},
					}).
					Return(&sdkkonnectops.PutControlPlanesIDGroupMembershipsResponse{}, errors.New("failed to set group members"))

				return sdk, sdkGroups, cp
			},
			expectedErrContains: "failed to set members on control plane group default/cpg-1",
			expectedErrType:     KonnectEntityCreatedButRelationsFailedError{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sdk, sdkGroups, cp := tc.mockCPTuple(t)
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme.Get()).
				WithObjects(tc.objects...).
				Build()

			err := createControlPlane(ctx, sdk, sdkGroups, fakeClient, cp)
			if tc.expectedErrContains != "" {
				if tc.expectedErrType != nil {
					require.ErrorAs(t, err, &tc.expectedErrType)
				}
				assert.ErrorContains(t, err, tc.expectedErrContains)
				return
			}

			require.NoError(t, err)
			if tc.expectedID != "" {
				assert.Equal(t, tc.expectedID, cp.Status.ID)
			}
		})
	}
}

func TestDeleteControlPlane(t *testing.T) {
	ctx := t.Context()
	testCases := []struct {
		name        string
		mockCPPair  func(*testing.T) (*sdkmocks.MockControlPlaneSDK, *konnectv1alpha2.KonnectGatewayControlPlane)
		expectedErr bool
		assertions  func(*testing.T, *konnectv1alpha2.KonnectGatewayControlPlane)
	}{
		{
			name: "success",
			mockCPPair: func(t *testing.T) (*sdkmocks.MockControlPlaneSDK, *konnectv1alpha2.KonnectGatewayControlPlane) {
				sdk := sdkmocks.NewMockControlPlaneSDK(t)
				cp := &konnectv1alpha2.KonnectGatewayControlPlane{
					Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: &sdkkonnectcomp.CreateControlPlaneRequest{
							Name: "cp-1",
						},
						Source: lo.ToPtr(commonv1alpha1.EntitySourceOrigin),
					},
					Status: konnectv1alpha2.KonnectGatewayControlPlaneStatus{
						KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
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
			mockCPPair: func(t *testing.T) (*sdkmocks.MockControlPlaneSDK, *konnectv1alpha2.KonnectGatewayControlPlane) {
				sdk := sdkmocks.NewMockControlPlaneSDK(t)
				cp := &konnectv1alpha2.KonnectGatewayControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cp-1",
						Namespace: "default",
					},
					Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: &sdkkonnectcomp.CreateControlPlaneRequest{
							Name: "cp-1",
						},
						Source: lo.ToPtr(commonv1alpha1.EntitySourceOrigin),
					},
					Status: konnectv1alpha2.KonnectGatewayControlPlaneStatus{
						KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
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
			mockCPPair: func(t *testing.T) (*sdkmocks.MockControlPlaneSDK, *konnectv1alpha2.KonnectGatewayControlPlane) {
				sdk := sdkmocks.NewMockControlPlaneSDK(t)
				cp := &konnectv1alpha2.KonnectGatewayControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cp-1",
						Namespace: "default",
					},
					Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: &sdkkonnectcomp.CreateControlPlaneRequest{
							Name: "cp-1",
						},
						Source: lo.ToPtr(commonv1alpha1.EntitySourceOrigin),
					},
					Status: konnectv1alpha2.KonnectGatewayControlPlaneStatus{
						KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
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

			require.NoError(t, err)
		})
	}
}

func TestUpdateControlPlane(t *testing.T) {
	ctx := t.Context()
	testCases := []struct {
		name        string
		mockCPTuple func(*testing.T) (*sdkmocks.MockControlPlaneSDK, *sdkmocks.MockControlPlaneGroupSDK, *konnectv1alpha2.KonnectGatewayControlPlane)
		expectedErr bool
		expectedID  string
	}{
		{
			name: "success",
			mockCPTuple: func(t *testing.T) (*sdkmocks.MockControlPlaneSDK, *sdkmocks.MockControlPlaneGroupSDK, *konnectv1alpha2.KonnectGatewayControlPlane) {
				sdk := sdkmocks.NewMockControlPlaneSDK(t)
				sdkGroups := sdkmocks.NewMockControlPlaneGroupSDK(t)
				cp := &konnectv1alpha2.KonnectGatewayControlPlane{
					Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: &sdkkonnectcomp.CreateControlPlaneRequest{
							Name: "cp-1",
						},
						Source: lo.ToPtr(commonv1alpha1.EntitySourceOrigin),
					},
					Status: konnectv1alpha2.KonnectGatewayControlPlaneStatus{
						KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
							ID: "12345",
						},
					},
				}
				sdk.
					EXPECT().
					UpdateControlPlane(ctx, "12345",
						sdkkonnectcomp.UpdateControlPlaneRequest{
							Name:        sdkkonnectgo.String(cp.GetKonnectName()),
							Description: cp.GetKonnectDescription(),
							AuthType:    (*sdkkonnectcomp.UpdateControlPlaneRequestAuthType)(cp.GetKonnectAuthType()),
							ProxyUrls:   cp.GetKonnectProxyURLs(),
							Labels:      WithKubernetesMetadataLabels(cp, cp.GetKonnectLabels()),
						},
					).
					Return(
						&sdkkonnectops.UpdateControlPlaneResponse{
							ControlPlane: &sdkkonnectcomp.ControlPlane{
								ID: "12345",
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
			mockCPTuple: func(t *testing.T) (*sdkmocks.MockControlPlaneSDK, *sdkmocks.MockControlPlaneGroupSDK, *konnectv1alpha2.KonnectGatewayControlPlane) {
				sdk := sdkmocks.NewMockControlPlaneSDK(t)
				sdkGroups := sdkmocks.NewMockControlPlaneGroupSDK(t)
				cp := &konnectv1alpha2.KonnectGatewayControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cp-1",
						Namespace: "default",
					},
					Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: &sdkkonnectcomp.CreateControlPlaneRequest{
							Name: "cp-1",
						},
						Source: lo.ToPtr(commonv1alpha1.EntitySourceOrigin),
					},
					Status: konnectv1alpha2.KonnectGatewayControlPlaneStatus{
						KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
							ID: "12345",
						},
					},
				}

				sdk.
					EXPECT().
					UpdateControlPlane(ctx, "12345",
						sdkkonnectcomp.UpdateControlPlaneRequest{
							Name:        sdkkonnectgo.String(cp.GetKonnectName()),
							Description: cp.GetKonnectDescription(),
							AuthType:    (*sdkkonnectcomp.UpdateControlPlaneRequestAuthType)(cp.GetKonnectAuthType()),
							ProxyUrls:   cp.GetKonnectProxyURLs(),
							Labels:      WithKubernetesMetadataLabels(cp, cp.GetKonnectLabels()),
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
			expectedErr: true,
		},
		{
			name: "when not found then try to create",
			mockCPTuple: func(t *testing.T) (*sdkmocks.MockControlPlaneSDK, *sdkmocks.MockControlPlaneGroupSDK, *konnectv1alpha2.KonnectGatewayControlPlane) {
				sdk := sdkmocks.NewMockControlPlaneSDK(t)
				sdkGroups := sdkmocks.NewMockControlPlaneGroupSDK(t)
				cp := &konnectv1alpha2.KonnectGatewayControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cp-1",
						Namespace: "default",
					},
					Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: &sdkkonnectcomp.CreateControlPlaneRequest{
							Name: "cp-1",
						},
						Source: lo.ToPtr(commonv1alpha1.EntitySourceOrigin),
					},
					Status: konnectv1alpha2.KonnectGatewayControlPlaneStatus{
						KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
							ID: "12345",
						},
					},
				}

				sdk.
					EXPECT().
					UpdateControlPlane(ctx, "12345",
						sdkkonnectcomp.UpdateControlPlaneRequest{
							Name:        sdkkonnectgo.String(cp.GetKonnectName()),
							Description: cp.GetKonnectDescription(),
							AuthType:    (*sdkkonnectcomp.UpdateControlPlaneRequestAuthType)(cp.GetKonnectAuthType()),
							ProxyUrls:   cp.GetKonnectProxyURLs(),
							Labels:      WithKubernetesMetadataLabels(cp, cp.GetKonnectLabels()),
						},
					).
					Return(
						nil,
						&sdkkonnecterrs.NotFoundError{
							Status: 404,
							Detail: "not found",
						},
					)

				expectedRequest := sdkkonnectcomp.CreateControlPlaneRequest{ // Use the correct struct type
					Name:   cp.GetKonnectName(),
					Labels: WithKubernetesMetadataLabels(cp, cp.GetKonnectLabels()),
				}
				sdk.
					EXPECT().
					CreateControlPlane(ctx, expectedRequest).
					Return(
						&sdkkonnectops.CreateControlPlaneResponse{
							ControlPlane: &sdkkonnectcomp.ControlPlane{
								ID: "12345",
							},
						},
						nil,
					)

				return sdk, sdkGroups, cp
			},
			expectedID: "12345",
		},
		// TODO: add test case for group membership success/failure scenarios
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sdk, sdkGroups, cp := tc.mockCPTuple(t)
			fakeClient := fake.NewClientBuilder().Build()

			err := updateControlPlane(ctx, sdk, sdkGroups, fakeClient, cp)
			if tc.expectedErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.expectedID, cp.Status.ID)
		})
	}
}

func TestCreateAndUpdateControlPlane_KubernetesMetadataConsistency(t *testing.T) {
	var (
		ctx = t.Context()
		cp  = &konnectv1alpha2.KonnectGatewayControlPlane{
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
			Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
				CreateControlPlaneRequest: &sdkkonnectcomp.CreateControlPlaneRequest{
					Name: "cp-1",
				},
				Source: lo.ToPtr(commonv1alpha1.EntitySourceOrigin),
			},
		}
		sdk              = sdkmocks.NewMockSDKFactory(t)
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
				ID: "12345",
			},
		}, nil)
	_, err := Create(ctx, sdk.SDK, fakeClient, &metricsmocks.MockRecorder{}, cp)
	require.NoError(t, err)

	t.Log("Triggering UpdateControlPlane with expected labels")
	sdkControlPlanes.EXPECT().
		UpdateControlPlane(ctx, "12345", sdkkonnectcomp.UpdateControlPlaneRequest{
			Name:   lo.ToPtr("cp-1"),
			Labels: expectedLabels,
		}).
		Return(&sdkkonnectops.UpdateControlPlaneResponse{
			ControlPlane: &sdkkonnectcomp.ControlPlane{
				ID: "12345",
			},
		}, nil)
	_, err = Update(ctx, sdk.SDK, 0, fakeClient, &metricsmocks.MockRecorder{}, cp)
	require.NoError(t, err)
}

func TestSetGroupMembers(t *testing.T) {
	testcases := []struct {
		name                    string
		group                   *konnectv1alpha2.KonnectGatewayControlPlane
		cps                     []client.Object
		sdk                     func(t *testing.T) *sdkmocks.MockControlPlaneGroupSDK
		expectedErr             bool
		memberRefResolvedStatus metav1.ConditionStatus
		memberRefResolvedReason kcfgconsts.ConditionReason
	}{
		{
			name: "no members",
			group: &konnectv1alpha2.KonnectGatewayControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cp-group",
					Namespace: "default",
				},
				Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
					CreateControlPlaneRequest: &sdkkonnectcomp.CreateControlPlaneRequest{
						ClusterType: lo.ToPtr(sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlaneGroup),
						Name:        "cp-group",
					},
					Source: lo.ToPtr(commonv1alpha1.EntitySourceOrigin),
				},
			},
			sdk: func(t *testing.T) *sdkmocks.MockControlPlaneGroupSDK {
				sdk := sdkmocks.NewMockControlPlaneGroupSDK(t)
				sdk.EXPECT().
					PutControlPlanesIDGroupMemberships(
						mock.Anything,
						"cpg-12345",
						&sdkkonnectcomp.GroupMembership{
							Members: []sdkkonnectcomp.Members{},
						},
					).
					Return(&sdkkonnectops.PutControlPlanesIDGroupMembershipsResponse{}, nil)
				return sdk
			},
			memberRefResolvedStatus: metav1.ConditionTrue,
			memberRefResolvedReason: ControlPlaneGroupMembersReferenceResolvedReasonResolved,
		},
		{
			name: "1 member with Konnect Status ID",
			group: &konnectv1alpha2.KonnectGatewayControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cp-group",
					Namespace: "default",
				},
				Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
					CreateControlPlaneRequest: &sdkkonnectcomp.CreateControlPlaneRequest{
						ClusterType: lo.ToPtr(sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlaneGroup),
						Name:        "cp-group",
					},
					Source: lo.ToPtr(commonv1alpha1.EntitySourceOrigin),
					Members: []corev1.LocalObjectReference{
						{
							Name: "cp-1",
						},
					},
				},
			},
			cps: []client.Object{
				&konnectv1alpha2.KonnectGatewayControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cp-1",
						Namespace: "default",
					},
					Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: &sdkkonnectcomp.CreateControlPlaneRequest{
							Name: "cp-1",
						},
						Source: lo.ToPtr(commonv1alpha1.EntitySourceOrigin),
					},
					Status: konnectv1alpha2.KonnectGatewayControlPlaneStatus{
						KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
							ID: "cp-12345",
						},
					},
				},
			},
			sdk: func(t *testing.T) *sdkmocks.MockControlPlaneGroupSDK {
				sdk := sdkmocks.NewMockControlPlaneGroupSDK(t)
				sdk.EXPECT().
					PutControlPlanesIDGroupMemberships(
						mock.Anything,
						"cpg-12345",
						&sdkkonnectcomp.GroupMembership{
							Members: []sdkkonnectcomp.Members{
								{
									ID: "cp-12345",
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
			memberRefResolvedStatus: metav1.ConditionTrue,
			memberRefResolvedReason: ControlPlaneGroupMembersReferenceResolvedReasonResolved,
		},
		{
			name: "1 member without Konnect Status ID",
			group: &konnectv1alpha2.KonnectGatewayControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cp-group",
					Namespace: "default",
				},
				Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
					CreateControlPlaneRequest: &sdkkonnectcomp.CreateControlPlaneRequest{
						ClusterType: lo.ToPtr(sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlaneGroup),
						Name:        "cp-group",
					},
					Source: lo.ToPtr(commonv1alpha1.EntitySourceOrigin),
					Members: []corev1.LocalObjectReference{
						{
							Name: "cp-1",
						},
					},
				},
			},
			cps: []client.Object{
				&konnectv1alpha2.KonnectGatewayControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cp-1",
						Namespace: "default",
					},
					Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: &sdkkonnectcomp.CreateControlPlaneRequest{
							Name: "cp-1",
						},
						Source: lo.ToPtr(commonv1alpha1.EntitySourceOrigin),
					},
					Status: konnectv1alpha2.KonnectGatewayControlPlaneStatus{},
				},
			},
			sdk: func(t *testing.T) *sdkmocks.MockControlPlaneGroupSDK {
				sdk := sdkmocks.NewMockControlPlaneGroupSDK(t)
				return sdk
			},
			expectedErr:             true,
			memberRefResolvedStatus: metav1.ConditionFalse,
			memberRefResolvedReason: ControlPlaneGroupMembersReferenceResolvedReasonPartialNotResolved,
		},
		{
			name: "2 member with Konnect Status IDs",
			group: &konnectv1alpha2.KonnectGatewayControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cp-group",
					Namespace: "default",
				},
				Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
					CreateControlPlaneRequest: &sdkkonnectcomp.CreateControlPlaneRequest{
						ClusterType: lo.ToPtr(sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlaneGroup),
						Name:        "cp-group",
					},
					Source: lo.ToPtr(commonv1alpha1.EntitySourceOrigin),
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
				&konnectv1alpha2.KonnectGatewayControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cp-1",
						Namespace: "default",
					},
					Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: &sdkkonnectcomp.CreateControlPlaneRequest{
							Name: "cp-1",
						},
						Source: lo.ToPtr(commonv1alpha1.EntitySourceOrigin),
					},
					Status: konnectv1alpha2.KonnectGatewayControlPlaneStatus{
						KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
							ID: "cp-12345",
						},
					},
				},
				&konnectv1alpha2.KonnectGatewayControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cp-2",
						Namespace: "default",
					},
					Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: &sdkkonnectcomp.CreateControlPlaneRequest{
							Name: "cp-2",
						},
						Source: lo.ToPtr(commonv1alpha1.EntitySourceOrigin),
					},
					Status: konnectv1alpha2.KonnectGatewayControlPlaneStatus{
						KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
							ID: "cp-12346",
						},
					},
				},
			},
			sdk: func(t *testing.T) *sdkmocks.MockControlPlaneGroupSDK {
				sdk := sdkmocks.NewMockControlPlaneGroupSDK(t)
				sdk.EXPECT().
					PutControlPlanesIDGroupMemberships(
						mock.Anything,
						"cpg-12345",
						&sdkkonnectcomp.GroupMembership{
							Members: []sdkkonnectcomp.Members{
								{
									ID: "cp-12345",
								},
								{
									ID: "cp-12346",
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
			memberRefResolvedStatus: metav1.ConditionTrue,
			memberRefResolvedReason: ControlPlaneGroupMembersReferenceResolvedReasonResolved,
		},
		{
			name: "2 member, 1 with Konnect Status IDs, 1 without it",
			group: &konnectv1alpha2.KonnectGatewayControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cp-group",
					Namespace: "default",
				},
				Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
					CreateControlPlaneRequest: &sdkkonnectcomp.CreateControlPlaneRequest{
						ClusterType: lo.ToPtr(sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlaneGroup),
						Name:        "cp-group",
					},
					Source: lo.ToPtr(commonv1alpha1.EntitySourceOrigin),
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
				&konnectv1alpha2.KonnectGatewayControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cp-1",
						Namespace: "default",
					},
					Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: &sdkkonnectcomp.CreateControlPlaneRequest{
							Name: "cp-1",
						},
						Source: lo.ToPtr(commonv1alpha1.EntitySourceOrigin),
					},
					Status: konnectv1alpha2.KonnectGatewayControlPlaneStatus{
						KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
							ID: "cp-12345",
						},
					},
				},
				&konnectv1alpha2.KonnectGatewayControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cp-2",
						Namespace: "default",
					},
					Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: &sdkkonnectcomp.CreateControlPlaneRequest{
							Name: "cp-2",
						},
						Source: lo.ToPtr(commonv1alpha1.EntitySourceOrigin),
					},
				},
			},
			sdk: func(t *testing.T) *sdkmocks.MockControlPlaneGroupSDK {
				sdk := sdkmocks.NewMockControlPlaneGroupSDK(t)
				return sdk
			},
			expectedErr:             true,
			memberRefResolvedStatus: metav1.ConditionFalse,
			memberRefResolvedReason: ControlPlaneGroupMembersReferenceResolvedReasonPartialNotResolved,
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
			err := setGroupMembers(t.Context(), fakeClient, tc.group, "cpg-12345", sdk)
			if tc.expectedErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.True(t, sdk.AssertExpectations(t))

			membersRefResolvedCondition, conditionFound := lo.Find(tc.group.Status.Conditions, func(c metav1.Condition) bool {
				return c.Type == ControlPlaneGroupMembersReferenceResolvedConditionType
			})
			assert.True(t, conditionFound, "Should find MembersReferenceResolved condition")
			assert.Equal(t, tc.memberRefResolvedStatus, membersRefResolvedCondition.Status, "Should have expected MembersReferenceResolved status")
			assert.Equal(t, string(tc.memberRefResolvedReason), membersRefResolvedCondition.Reason, "Should have expected MembersReferenceResolved reason")
		})
	}
}
