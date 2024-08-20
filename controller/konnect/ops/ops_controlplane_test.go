package ops

import (
	"context"
	"testing"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectgocomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectgoops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnectgoerrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kong/gateway-operator/controller/konnect/conditions"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

func TestCreateControlPlane(t *testing.T) {
	ctx := context.Background()
	testCases := []struct {
		name        string
		mockCPPair  func() (*MockControlPlaneSDK, *konnectv1alpha1.KonnectControlPlane)
		expectedErr bool
		assertions  func(*testing.T, *konnectv1alpha1.KonnectControlPlane)
	}{
		{
			name: "success",
			mockCPPair: func() (*MockControlPlaneSDK, *konnectv1alpha1.KonnectControlPlane) {
				sdk := &MockControlPlaneSDK{}
				cp := &konnectv1alpha1.KonnectControlPlane{
					Spec: konnectv1alpha1.KonnectControlPlaneSpec{
						CreateControlPlaneRequest: sdkkonnectgocomp.CreateControlPlaneRequest{
							Name: "cp-1",
						},
					},
				}

				sdk.
					EXPECT().
					CreateControlPlane(ctx, cp.Spec.CreateControlPlaneRequest).
					Return(
						&sdkkonnectgoops.CreateControlPlaneResponse{
							ControlPlane: &sdkkonnectgocomp.ControlPlane{
								ID: "12345",
							},
						},
						nil,
					)

				return sdk, cp
			},
			assertions: func(t *testing.T, cp *konnectv1alpha1.KonnectControlPlane) {
				assert.Equal(t, "12345", cp.Status.GetKonnectID())
				cond, ok := k8sutils.GetCondition(conditions.KonnectEntityProgrammedConditionType, cp)
				require.True(t, ok, "Programmed condition not set on KonnectControlPlane")
				assert.Equal(t, metav1.ConditionTrue, cond.Status)
				assert.Equal(t, conditions.KonnectEntityProgrammedReasonProgrammed, cond.Reason)
				assert.Equal(t, cp.GetGeneration(), cond.ObservedGeneration)
			},
		},
		{
			name: "fail",
			mockCPPair: func() (*MockControlPlaneSDK, *konnectv1alpha1.KonnectControlPlane) {
				sdk := &MockControlPlaneSDK{}
				cp := &konnectv1alpha1.KonnectControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cp-1",
						Namespace: "default",
					},
					Spec: konnectv1alpha1.KonnectControlPlaneSpec{
						CreateControlPlaneRequest: sdkkonnectgocomp.CreateControlPlaneRequest{
							Name: "cp-1",
						},
					},
				}

				sdk.
					EXPECT().
					CreateControlPlane(ctx, cp.Spec.CreateControlPlaneRequest).
					Return(
						nil,
						&sdkkonnectgoerrs.BadRequestError{
							Status: 400,
							Detail: "bad request",
						},
					)

				return sdk, cp
			},
			assertions: func(t *testing.T, cp *konnectv1alpha1.KonnectControlPlane) {
				assert.Equal(t, "", cp.Status.GetKonnectID())
				cond, ok := k8sutils.GetCondition(conditions.KonnectEntityProgrammedConditionType, cp)
				require.True(t, ok, "Programmed condition not set on KonnectControlPlane")
				assert.Equal(t, metav1.ConditionFalse, cond.Status)
				assert.Equal(t, "FailedToCreate", cond.Reason)
				assert.Equal(t, cp.GetGeneration(), cond.ObservedGeneration)
				assert.Equal(t, "failed to create KonnectControlPlane default/cp-1: {\"status\":400,\"title\":\"\",\"instance\":\"\",\"detail\":\"bad request\",\"invalid_parameters\":null}", cond.Message)
			},
			expectedErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sdk, cp := tc.mockCPPair()

			err := createControlPlane(ctx, sdk, cp)
			t.Cleanup(func() {
				assert.True(t, sdk.AssertExpectations(t))
			})

			tc.assertions(t, cp)

			if tc.expectedErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
		})
	}
}

func TestDeleteControlPlane(t *testing.T) {
	ctx := context.Background()
	testCases := []struct {
		name        string
		mockCPPair  func() (*MockControlPlaneSDK, *konnectv1alpha1.KonnectControlPlane)
		expectedErr bool
		assertions  func(*testing.T, *konnectv1alpha1.KonnectControlPlane)
	}{
		{
			name: "success",
			mockCPPair: func() (*MockControlPlaneSDK, *konnectv1alpha1.KonnectControlPlane) {
				sdk := &MockControlPlaneSDK{}
				cp := &konnectv1alpha1.KonnectControlPlane{
					Spec: konnectv1alpha1.KonnectControlPlaneSpec{
						CreateControlPlaneRequest: sdkkonnectgocomp.CreateControlPlaneRequest{
							Name: "cp-1",
						},
					},
					Status: konnectv1alpha1.KonnectControlPlaneStatus{
						KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{
							ID: "12345",
						},
					},
				}
				sdk.
					EXPECT().
					DeleteControlPlane(ctx, "12345").
					Return(
						&sdkkonnectgoops.DeleteControlPlaneResponse{
							StatusCode: 200,
						},
						nil,
					)

				return sdk, cp
			},
		},
		{
			name: "fail",
			mockCPPair: func() (*MockControlPlaneSDK, *konnectv1alpha1.KonnectControlPlane) {
				sdk := &MockControlPlaneSDK{}
				cp := &konnectv1alpha1.KonnectControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cp-1",
						Namespace: "default",
					},
					Spec: konnectv1alpha1.KonnectControlPlaneSpec{
						CreateControlPlaneRequest: sdkkonnectgocomp.CreateControlPlaneRequest{
							Name: "cp-1",
						},
					},
					Status: konnectv1alpha1.KonnectControlPlaneStatus{
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
						&sdkkonnectgoerrs.BadRequestError{
							Status: 400,
							Detail: "bad request",
						},
					)

				return sdk, cp
			},
			expectedErr: true,
		},
		{
			name: "not found error is ignore and considered a success when trying to delete",
			mockCPPair: func() (*MockControlPlaneSDK, *konnectv1alpha1.KonnectControlPlane) {
				sdk := &MockControlPlaneSDK{}
				cp := &konnectv1alpha1.KonnectControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cp-1",
						Namespace: "default",
					},
					Spec: konnectv1alpha1.KonnectControlPlaneSpec{
						CreateControlPlaneRequest: sdkkonnectgocomp.CreateControlPlaneRequest{
							Name: "cp-1",
						},
					},
					Status: konnectv1alpha1.KonnectControlPlaneStatus{
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
						&sdkkonnectgoerrs.NotFoundError{
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
			sdk, cp := tc.mockCPPair()

			err := deleteControlPlane(ctx, sdk, cp)

			if tc.assertions != nil {
				tc.assertions(t, cp)
			}

			if tc.expectedErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.True(t, sdk.AssertExpectations(t))
		})
	}
}

func TestUpdateControlPlane(t *testing.T) {
	ctx := context.Background()
	testCases := []struct {
		name        string
		mockCPPair  func() (*MockControlPlaneSDK, *konnectv1alpha1.KonnectControlPlane)
		expectedErr bool
		assertions  func(*testing.T, *konnectv1alpha1.KonnectControlPlane)
	}{
		{
			name: "success",
			mockCPPair: func() (*MockControlPlaneSDK, *konnectv1alpha1.KonnectControlPlane) {
				sdk := &MockControlPlaneSDK{}
				cp := &konnectv1alpha1.KonnectControlPlane{
					Spec: konnectv1alpha1.KonnectControlPlaneSpec{
						CreateControlPlaneRequest: sdkkonnectgocomp.CreateControlPlaneRequest{
							Name: "cp-1",
						},
					},
					Status: konnectv1alpha1.KonnectControlPlaneStatus{
						KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{
							ID: "12345",
						},
					},
				}
				sdk.
					EXPECT().
					UpdateControlPlane(ctx, "12345",
						sdkkonnectgocomp.UpdateControlPlaneRequest{
							Name:        sdkkonnectgo.String(cp.Spec.Name),
							Description: cp.Spec.Description,
							AuthType:    (*sdkkonnectgocomp.UpdateControlPlaneRequestAuthType)(cp.Spec.AuthType),
							ProxyUrls:   cp.Spec.ProxyUrls,
							Labels:      cp.Spec.Labels,
						},
					).
					Return(
						&sdkkonnectgoops.UpdateControlPlaneResponse{
							ControlPlane: &sdkkonnectgocomp.ControlPlane{
								ID: "12345",
							},
						},
						nil,
					)

				return sdk, cp
			},
			assertions: func(t *testing.T, cp *konnectv1alpha1.KonnectControlPlane) {
				assert.Equal(t, "12345", cp.Status.GetKonnectID())
				cond, ok := k8sutils.GetCondition(conditions.KonnectEntityProgrammedConditionType, cp)
				require.True(t, ok, "Programmed condition not set on KonnectControlPlane")
				assert.Equal(t, metav1.ConditionTrue, cond.Status)
				assert.Equal(t, conditions.KonnectEntityProgrammedReasonProgrammed, cond.Reason)
				assert.Equal(t, cp.GetGeneration(), cond.ObservedGeneration)
				assert.Equal(t, "", cond.Message)
			},
		},
		{
			name: "fail",
			mockCPPair: func() (*MockControlPlaneSDK, *konnectv1alpha1.KonnectControlPlane) {
				sdk := &MockControlPlaneSDK{}
				cp := &konnectv1alpha1.KonnectControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cp-1",
						Namespace: "default",
					},
					Spec: konnectv1alpha1.KonnectControlPlaneSpec{
						CreateControlPlaneRequest: sdkkonnectgocomp.CreateControlPlaneRequest{
							Name: "cp-1",
						},
					},
					Status: konnectv1alpha1.KonnectControlPlaneStatus{
						KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{
							ID: "12345",
						},
					},
				}

				sdk.
					EXPECT().
					UpdateControlPlane(ctx, "12345",
						sdkkonnectgocomp.UpdateControlPlaneRequest{
							Name:        sdkkonnectgo.String(cp.Spec.Name),
							Description: cp.Spec.Description,
							AuthType:    (*sdkkonnectgocomp.UpdateControlPlaneRequestAuthType)(cp.Spec.AuthType),
							ProxyUrls:   cp.Spec.ProxyUrls,
							Labels:      cp.Spec.Labels,
						},
					).
					Return(
						nil,
						&sdkkonnectgoerrs.BadRequestError{
							Status: 400,
							Detail: "bad request",
						},
					)

				return sdk, cp
			},
			assertions: func(t *testing.T, cp *konnectv1alpha1.KonnectControlPlane) {
				assert.Equal(t, "12345", cp.Status.GetKonnectID())
				cond, ok := k8sutils.GetCondition(conditions.KonnectEntityProgrammedConditionType, cp)
				require.True(t, ok, "Programmed condition not set on KonnectControlPlane")
				assert.Equal(t, metav1.ConditionFalse, cond.Status)
				assert.Equal(t, "FailedToUpdate", cond.Reason)
				assert.Equal(t, cp.GetGeneration(), cond.ObservedGeneration)
				assert.Equal(t, "failed to update KonnectControlPlane default/cp-1: {\"status\":400,\"title\":\"\",\"instance\":\"\",\"detail\":\"bad request\",\"invalid_parameters\":null}", cond.Message)
			},
			expectedErr: true,
		},
		{
			name: "when not found then try to create",
			mockCPPair: func() (*MockControlPlaneSDK, *konnectv1alpha1.KonnectControlPlane) {
				sdk := &MockControlPlaneSDK{}
				cp := &konnectv1alpha1.KonnectControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cp-1",
						Namespace: "default",
					},
					Spec: konnectv1alpha1.KonnectControlPlaneSpec{
						CreateControlPlaneRequest: sdkkonnectgocomp.CreateControlPlaneRequest{
							Name: "cp-1",
						},
					},
					Status: konnectv1alpha1.KonnectControlPlaneStatus{
						KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{
							ID: "12345",
						},
					},
				}

				sdk.
					EXPECT().
					UpdateControlPlane(ctx, "12345",
						sdkkonnectgocomp.UpdateControlPlaneRequest{
							Name:        sdkkonnectgo.String(cp.Spec.Name),
							Description: cp.Spec.Description,
							AuthType:    (*sdkkonnectgocomp.UpdateControlPlaneRequestAuthType)(cp.Spec.AuthType),
							ProxyUrls:   cp.Spec.ProxyUrls,
							Labels:      cp.Spec.Labels,
						},
					).
					Return(
						nil,
						&sdkkonnectgoerrs.NotFoundError{
							Status: 404,
							Detail: "not found",
						},
					)

				sdk.
					EXPECT().
					CreateControlPlane(ctx, cp.Spec.CreateControlPlaneRequest).
					Return(
						&sdkkonnectgoops.CreateControlPlaneResponse{
							ControlPlane: &sdkkonnectgocomp.ControlPlane{
								ID: "12345",
							},
						},
						nil,
					)

				return sdk, cp
			},
			assertions: func(t *testing.T, cp *konnectv1alpha1.KonnectControlPlane) {
				assert.Equal(t, "12345", cp.Status.GetKonnectID())
				cond, ok := k8sutils.GetCondition(conditions.KonnectEntityProgrammedConditionType, cp)
				require.True(t, ok, "Programmed condition not set on KonnectControlPlane")
				assert.Equal(t, metav1.ConditionTrue, cond.Status)
				assert.Equal(t, conditions.KonnectEntityProgrammedReasonProgrammed, cond.Reason)
				assert.Equal(t, cp.GetGeneration(), cond.ObservedGeneration)
				assert.Equal(t, "", cond.Message)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sdk, cp := tc.mockCPPair()

			err := updateControlPlane(ctx, sdk, cp)

			if tc.assertions != nil {
				tc.assertions(t, cp)
			}

			if tc.expectedErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.True(t, sdk.AssertExpectations(t))
		})
	}
}
