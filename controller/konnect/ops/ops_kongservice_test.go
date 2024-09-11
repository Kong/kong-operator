package ops

import (
	"context"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kong/gateway-operator/controller/konnect/conditions"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

func TestCreateKongService(t *testing.T) {
	ctx := context.Background()
	testCases := []struct {
		name            string
		mockServicePair func(*testing.T) (*MockServicesSDK, *configurationv1alpha1.KongService)
		expectedErr     bool
		assertions      func(*testing.T, *configurationv1alpha1.KongService)
	}{
		{
			name: "success",
			mockServicePair: func(t *testing.T) (*MockServicesSDK, *configurationv1alpha1.KongService) {
				sdk := NewMockServicesSDK(t)
				svc := &configurationv1alpha1.KongService{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc-1",
						Namespace: "default",
					},
					Spec: configurationv1alpha1.KongServiceSpec{
						KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
							Name: lo.ToPtr("svc-1"),
							Host: "example.com",
						},
					},
					Status: configurationv1alpha1.KongServiceStatus{
						Konnect: &konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef{
							ControlPlaneID: "123456789",
						},
					},
				}

				sdk.
					EXPECT().
					CreateService(ctx, "123456789", kongServiceToSDKServiceInput(svc)).
					Return(
						&sdkkonnectops.CreateServiceResponse{
							Service: &sdkkonnectcomp.Service{
								ID:   lo.ToPtr("12345"),
								Host: "example.com",
								Name: lo.ToPtr("svc-1"),
							},
						},
						nil,
					)

				return sdk, svc
			},
			assertions: func(t *testing.T, svc *configurationv1alpha1.KongService) {
				assert.Equal(t, "12345", svc.GetKonnectStatus().GetKonnectID())
				cond, ok := k8sutils.GetCondition(conditions.KonnectEntityProgrammedConditionType, svc)
				require.True(t, ok, "Programmed condition not set on KongService")
				assert.Equal(t, metav1.ConditionTrue, cond.Status)
				assert.Equal(t, conditions.KonnectEntityProgrammedReasonProgrammed, cond.Reason)
				assert.Equal(t, svc.GetGeneration(), cond.ObservedGeneration)
			},
		},
		{
			name: "fail - no control plane ID in status returns an error and does not create the Service in Konnect",
			mockServicePair: func(t *testing.T) (*MockServicesSDK, *configurationv1alpha1.KongService) {
				sdk := NewMockServicesSDK(t)
				svc := &configurationv1alpha1.KongService{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc-1",
						Namespace: "default",
					},
					Spec: configurationv1alpha1.KongServiceSpec{
						KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
							Name: lo.ToPtr("svc-1"),
							Host: "example.com",
						},
					},
				}

				return sdk, svc
			},
			assertions: func(t *testing.T, svc *configurationv1alpha1.KongService) {
				assert.Equal(t, "", svc.GetKonnectStatus().GetKonnectID())
				// TODO: we should probably set a condition when the control plane ID is missing in the status.
			},
			expectedErr: true,
		},
		{
			name: "fail",
			mockServicePair: func(t *testing.T) (*MockServicesSDK, *configurationv1alpha1.KongService) {
				sdk := NewMockServicesSDK(t)
				svc := &configurationv1alpha1.KongService{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc-1",
						Namespace: "default",
					},
					Spec: configurationv1alpha1.KongServiceSpec{
						KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
							Name: lo.ToPtr("svc-1"),
							Host: "example.com",
						},
					},
					Status: configurationv1alpha1.KongServiceStatus{
						Konnect: &konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef{
							ControlPlaneID: "123456789",
						},
					},
				}

				sdk.
					EXPECT().
					CreateService(ctx, "123456789", kongServiceToSDKServiceInput(svc)).
					Return(
						nil,
						&sdkkonnecterrs.BadRequestError{
							Status: 400,
							Detail: "bad request",
						},
					)

				return sdk, svc
			},
			assertions: func(t *testing.T, svc *configurationv1alpha1.KongService) {
				assert.Equal(t, "", svc.GetKonnectStatus().GetKonnectID())
				cond, ok := k8sutils.GetCondition(conditions.KonnectEntityProgrammedConditionType, svc)
				require.True(t, ok, "Programmed condition not set on KonnectGatewayControlPlane")
				assert.Equal(t, metav1.ConditionFalse, cond.Status)
				assert.Equal(t, "FailedToCreate", cond.Reason)
				assert.Equal(t, svc.GetGeneration(), cond.ObservedGeneration)
				assert.Equal(t, `failed to create KongService default/svc-1: {"status":400,"title":"","instance":"","detail":"bad request","invalid_parameters":null}`, cond.Message)
			},
			expectedErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sdk, svc := tc.mockServicePair(t)

			err := createService(ctx, sdk, svc)

			tc.assertions(t, svc)

			if tc.expectedErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
		})
	}
}

func TestDeleteKongService(t *testing.T) {
	ctx := context.Background()
	testCases := []struct {
		name            string
		mockServicePair func(*testing.T) (*MockServicesSDK, *configurationv1alpha1.KongService)
		expectedErr     bool
		assertions      func(*testing.T, *configurationv1alpha1.KongService)
	}{
		{
			name: "success",
			mockServicePair: func(t *testing.T) (*MockServicesSDK, *configurationv1alpha1.KongService) {
				sdk := NewMockServicesSDK(t)
				svc := &configurationv1alpha1.KongService{
					Spec: configurationv1alpha1.KongServiceSpec{
						KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
							Name: lo.ToPtr("svc-1"),
						},
					},
					Status: configurationv1alpha1.KongServiceStatus{
						Konnect: &konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef{
							ControlPlaneID: "12345",
							KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{
								ID: "123456789",
							},
						},
					},
				}
				sdk.
					EXPECT().
					DeleteService(ctx, "12345", "123456789").
					Return(
						&sdkkonnectops.DeleteServiceResponse{
							StatusCode: 200,
						},
						nil,
					)

				return sdk, svc
			},
		},
		{
			name: "fail",
			mockServicePair: func(t *testing.T) (*MockServicesSDK, *configurationv1alpha1.KongService) {
				sdk := NewMockServicesSDK(t)
				svc := &configurationv1alpha1.KongService{
					Spec: configurationv1alpha1.KongServiceSpec{
						KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
							Name: lo.ToPtr("svc-1"),
						},
					},
					Status: configurationv1alpha1.KongServiceStatus{
						Konnect: &konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef{
							ControlPlaneID: "12345",
							KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{
								ID: "123456789",
							},
						},
					},
				}
				sdk.
					EXPECT().
					DeleteService(ctx, "12345", "123456789").
					Return(
						nil,
						&sdkkonnecterrs.BadRequestError{
							Status: 400,
							Detail: "bad request",
						},
					)

				return sdk, svc
			},
			expectedErr: true,
		},
		{
			name: "not found error is ignored and considered a success when trying to delete",
			mockServicePair: func(t *testing.T) (*MockServicesSDK, *configurationv1alpha1.KongService) {
				sdk := NewMockServicesSDK(t)
				svc := &configurationv1alpha1.KongService{
					Spec: configurationv1alpha1.KongServiceSpec{
						KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
							Name: lo.ToPtr("svc-1"),
						},
					},
					Status: configurationv1alpha1.KongServiceStatus{
						Konnect: &konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef{
							ControlPlaneID: "12345",
							KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{
								ID: "123456789",
							},
						},
					},
				}
				sdk.
					EXPECT().
					DeleteService(ctx, "12345", "123456789").
					Return(
						nil,
						&sdkkonnecterrs.SDKError{
							Message:    "not found",
							StatusCode: 404,
						},
					)

				return sdk, svc
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sdk, svc := tc.mockServicePair(t)

			err := deleteService(ctx, sdk, svc)

			if tc.assertions != nil {
				tc.assertions(t, svc)
			}

			if tc.expectedErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
		})
	}
}

func TestUpdateKongService(t *testing.T) {
	ctx := context.Background()
	testCases := []struct {
		name            string
		mockServicePair func(*testing.T) (*MockServicesSDK, *configurationv1alpha1.KongService)
		expectedErr     bool
		assertions      func(*testing.T, *configurationv1alpha1.KongService)
	}{
		{
			name: "success",
			mockServicePair: func(t *testing.T) (*MockServicesSDK, *configurationv1alpha1.KongService) {
				sdk := NewMockServicesSDK(t)
				svc := &configurationv1alpha1.KongService{
					Spec: configurationv1alpha1.KongServiceSpec{
						KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
							Name: lo.ToPtr("svc-1"),
						},
					},
					Status: configurationv1alpha1.KongServiceStatus{
						Konnect: &konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef{
							ControlPlaneID: "12345",
							KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{
								ID: "123456789",
							},
						},
					},
				}
				sdk.
					EXPECT().
					UpsertService(ctx,
						sdkkonnectops.UpsertServiceRequest{
							ControlPlaneID: "12345",
							ServiceID:      "123456789",
							Service:        kongServiceToSDKServiceInput(svc),
						},
					).
					Return(
						&sdkkonnectops.UpsertServiceResponse{
							StatusCode: 200,
							Service: &sdkkonnectcomp.Service{
								ID:   lo.ToPtr("123456789"),
								Name: lo.ToPtr("svc-1"),
							},
						},
						nil,
					)

				return sdk, svc
			},
			assertions: func(t *testing.T, svc *configurationv1alpha1.KongService) {
				assert.Equal(t, "123456789", svc.GetKonnectStatus().GetKonnectID())
				cond, ok := k8sutils.GetCondition(conditions.KonnectEntityProgrammedConditionType, svc)
				require.True(t, ok, "Programmed condition not set on KonnectGatewayControlPlane")
				assert.Equal(t, metav1.ConditionTrue, cond.Status)
				assert.Equal(t, conditions.KonnectEntityProgrammedReasonProgrammed, cond.Reason)
				assert.Equal(t, svc.GetGeneration(), cond.ObservedGeneration)
				assert.Equal(t, "", cond.Message)
			},
		},
		{
			name: "fail",
			mockServicePair: func(t *testing.T) (*MockServicesSDK, *configurationv1alpha1.KongService) {
				sdk := NewMockServicesSDK(t)
				svc := &configurationv1alpha1.KongService{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc-1",
						Namespace: "default",
					},
					Spec: configurationv1alpha1.KongServiceSpec{
						KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
							Name: lo.ToPtr("svc-1"),
						},
					},
					Status: configurationv1alpha1.KongServiceStatus{
						Konnect: &konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef{
							ControlPlaneID: "12345",
							KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{
								ID: "123456789",
							},
						},
					},
				}
				sdk.
					EXPECT().
					UpsertService(ctx,
						sdkkonnectops.UpsertServiceRequest{
							ControlPlaneID: "12345",
							ServiceID:      "123456789",
							Service:        kongServiceToSDKServiceInput(svc),
						},
					).
					Return(
						nil,
						&sdkkonnecterrs.BadRequestError{
							Status: 400,
							Title:  "bad request",
						},
					)

				return sdk, svc
			},
			assertions: func(t *testing.T, svc *configurationv1alpha1.KongService) {
				// TODO: When we fail to update a KongService, do we want to clear
				// the Konnect ID from the status? Probably not.
				// assert.Equal(t, "", svc.GetKonnectStatus().GetKonnectID())
				cond, ok := k8sutils.GetCondition(conditions.KonnectEntityProgrammedConditionType, svc)
				require.True(t, ok, "Programmed condition not set on KonnectGatewayControlPlane")
				assert.Equal(t, metav1.ConditionFalse, cond.Status)
				assert.Equal(t, "FailedToUpdate", cond.Reason)
				assert.Equal(t, svc.GetGeneration(), cond.ObservedGeneration)
				assert.Equal(t, `failed to update KongService default/svc-1: {"status":400,"title":"bad request","instance":"","detail":"","invalid_parameters":null}`, cond.Message)
			},
			expectedErr: true,
		},
		{
			name: "when not found then try to create",
			mockServicePair: func(t *testing.T) (*MockServicesSDK, *configurationv1alpha1.KongService) {
				sdk := NewMockServicesSDK(t)
				svc := &configurationv1alpha1.KongService{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc-1",
						Namespace: "default",
					},
					Spec: configurationv1alpha1.KongServiceSpec{
						KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
							Name: lo.ToPtr("svc-1"),
						},
					},
					Status: configurationv1alpha1.KongServiceStatus{
						Konnect: &konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef{
							ControlPlaneID: "12345",
							KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{
								ID: "123456789",
							},
						},
					},
				}
				sdk.
					EXPECT().
					UpsertService(ctx,
						sdkkonnectops.UpsertServiceRequest{
							ControlPlaneID: "12345",
							ServiceID:      "123456789",
							Service:        kongServiceToSDKServiceInput(svc),
						},
					).
					Return(
						nil,
						&sdkkonnecterrs.SDKError{
							StatusCode: 404,
							Message:    "not found",
						},
					)

				sdk.
					EXPECT().
					CreateService(ctx, "12345", kongServiceToSDKServiceInput(svc)).
					Return(
						&sdkkonnectops.CreateServiceResponse{
							Service: &sdkkonnectcomp.Service{
								ID:   lo.ToPtr("123456789"),
								Name: lo.ToPtr("svc-1"),
							},
						},
						nil,
					)

				return sdk, svc
			},
			assertions: func(t *testing.T, svc *configurationv1alpha1.KongService) {
				assert.Equal(t, "123456789", svc.GetKonnectStatus().GetKonnectID())
				cond, ok := k8sutils.GetCondition(conditions.KonnectEntityProgrammedConditionType, svc)
				require.True(t, ok, "Programmed condition not set on KonnectGatewayControlPlane")
				assert.Equal(t, metav1.ConditionTrue, cond.Status)
				assert.Equal(t, conditions.KonnectEntityProgrammedReasonProgrammed, cond.Reason)
				assert.Equal(t, svc.GetGeneration(), cond.ObservedGeneration)
				assert.Equal(t, "", cond.Message)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sdk, svc := tc.mockServicePair(t)

			err := updateService(ctx, sdk, svc)

			if tc.assertions != nil {
				tc.assertions(t, svc)
			}

			if tc.expectedErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
		})
	}
}
