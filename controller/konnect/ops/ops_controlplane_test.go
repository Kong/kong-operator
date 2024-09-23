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
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"

	"github.com/kong/gateway-operator/controller/konnect/conditions"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

func TestCreateControlPlane(t *testing.T) {
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

				return sdk, cp
			},
			assertions: func(t *testing.T, cp *konnectv1alpha1.KonnectGatewayControlPlane) {
				assert.Equal(t, "12345", cp.GetKonnectStatus().GetKonnectID())
				cond, ok := k8sutils.GetCondition(conditions.KonnectEntityProgrammedConditionType, cp)
				require.True(t, ok, "Programmed condition not set on KonnectGatewayControlPlane")
				assert.Equal(t, metav1.ConditionTrue, cond.Status)
				assert.Equal(t, conditions.KonnectEntityProgrammedReasonProgrammed, cond.Reason)
				assert.Equal(t, cp.GetGeneration(), cond.ObservedGeneration)
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

				return sdk, cp
			},
			assertions: func(t *testing.T, cp *konnectv1alpha1.KonnectGatewayControlPlane) {
				assert.Equal(t, "", cp.Status.GetKonnectID())
				cond, ok := k8sutils.GetCondition(conditions.KonnectEntityProgrammedConditionType, cp)
				require.True(t, ok, "Programmed condition not set on KonnectGatewayControlPlane")
				assert.Equal(t, metav1.ConditionFalse, cond.Status)
				assert.Equal(t, "FailedToCreate", cond.Reason)
				assert.Equal(t, cp.GetGeneration(), cond.ObservedGeneration)
				assert.Equal(t, `failed to create KonnectGatewayControlPlane default/cp-1: {"status":400,"title":"","instance":"","detail":"bad request","invalid_parameters":null}`, cond.Message)
			},
			expectedErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sdk, cp := tc.mockCPPair(t)

			err := createControlPlane(ctx, sdk, cp)
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

				return sdk, cp
			},
			assertions: func(t *testing.T, cp *konnectv1alpha1.KonnectGatewayControlPlane) {
				assert.Equal(t, "12345", cp.Status.GetKonnectID())
				cond, ok := k8sutils.GetCondition(conditions.KonnectEntityProgrammedConditionType, cp)
				require.True(t, ok, "Programmed condition not set on KonnectGatewayControlPlane")
				assert.Equal(t, metav1.ConditionTrue, cond.Status)
				assert.Equal(t, conditions.KonnectEntityProgrammedReasonProgrammed, cond.Reason)
				assert.Equal(t, cp.GetGeneration(), cond.ObservedGeneration)
				assert.Equal(t, "", cond.Message)
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

				return sdk, cp
			},
			assertions: func(t *testing.T, cp *konnectv1alpha1.KonnectGatewayControlPlane) {
				assert.Equal(t, "12345", cp.Status.GetKonnectID())
				cond, ok := k8sutils.GetCondition(conditions.KonnectEntityProgrammedConditionType, cp)
				require.True(t, ok, "Programmed condition not set on KonnectGatewayControlPlane")
				assert.Equal(t, metav1.ConditionFalse, cond.Status)
				assert.Equal(t, "FailedToUpdate", cond.Reason)
				assert.Equal(t, cp.GetGeneration(), cond.ObservedGeneration)
				assert.Equal(t, `failed to update KonnectGatewayControlPlane default/cp-1: {"status":400,"title":"","instance":"","detail":"bad request","invalid_parameters":null}`, cond.Message)
			},
			expectedErr: true,
		},
		{
			name: "when not found then try to create",
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

				return sdk, cp
			},
			assertions: func(t *testing.T, cp *konnectv1alpha1.KonnectGatewayControlPlane) {
				assert.Equal(t, "12345", cp.Status.GetKonnectID())
				cond, ok := k8sutils.GetCondition(conditions.KonnectEntityProgrammedConditionType, cp)
				require.True(t, ok, "Programmed condition not set on KonnectGatewayControlPlane")
				assert.Equal(t, metav1.ConditionTrue, cond.Status)
				assert.Equal(t, conditions.KonnectEntityProgrammedReasonProgrammed, cond.Reason)
				assert.Equal(t, cp.GetGeneration(), cond.ObservedGeneration)
				assert.Equal(t, "", cond.Message)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sdk, cp := tc.mockCPPair(t)

			err := updateControlPlane(ctx, sdk, cp)

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
		sdk = NewMockControlPlaneSDK(t)
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
	sdk.EXPECT().
		CreateControlPlane(ctx, sdkkonnectcomp.CreateControlPlaneRequest{
			Name:   "cp-1",
			Labels: expectedLabels,
		}).
		Return(&sdkkonnectops.CreateControlPlaneResponse{
			ControlPlane: &sdkkonnectcomp.ControlPlane{
				ID: lo.ToPtr("12345"),
			},
		}, nil)
	require.NoError(t, createControlPlane(ctx, sdk, cp))

	t.Log("Triggering UpdateControlPlane with expected labels")
	sdk.EXPECT().
		UpdateControlPlane(ctx, "12345", sdkkonnectcomp.UpdateControlPlaneRequest{
			Name:   lo.ToPtr("cp-1"),
			Labels: expectedLabels,
		}).
		Return(&sdkkonnectops.UpdateControlPlaneResponse{
			ControlPlane: &sdkkonnectcomp.ControlPlane{
				ID: lo.ToPtr("12345"),
			},
		}, nil)
	require.NoError(t, updateControlPlane(ctx, sdk, cp))
}
