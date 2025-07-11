package ops

import (
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"

	sdkmocks "github.com/kong/kong-operator/controller/konnect/ops/sdk/mocks"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha1"
	"github.com/kong/kubernetes-configuration/v2/pkg/metadata"
)

func TestCreateKongService(t *testing.T) {
	ctx := t.Context()
	testCases := []struct {
		name                string
		mockServicePair     func(*testing.T) (*sdkmocks.MockServicesSDK, *configurationv1alpha1.KongService)
		assertions          func(*testing.T, *configurationv1alpha1.KongService)
		expectedErrContains string
		expectedErrType     error
	}{
		{
			name: "success",
			mockServicePair: func(t *testing.T) (*sdkmocks.MockServicesSDK, *configurationv1alpha1.KongService) {
				sdk := sdkmocks.NewMockServicesSDK(t)
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
							Service: &sdkkonnectcomp.ServiceOutput{
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
			},
		},
		{
			name: "fail - no control plane ID in status returns an error and does not create the Service in Konnect",
			mockServicePair: func(t *testing.T) (*sdkmocks.MockServicesSDK, *configurationv1alpha1.KongService) {
				sdk := sdkmocks.NewMockServicesSDK(t)
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
				assert.Empty(t, svc.GetKonnectStatus().GetKonnectID())
			},
			expectedErrContains: "can't create KongService default/svc-1 without a Konnect ControlPlane ID",
		},
		{
			name: "fail",
			mockServicePair: func(t *testing.T) (*sdkmocks.MockServicesSDK, *configurationv1alpha1.KongService) {
				sdk := sdkmocks.NewMockServicesSDK(t)
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
				assert.Empty(t, svc.GetKonnectStatus().GetKonnectID())
			},
			expectedErrContains: "failed to create KongService default/svc-1: {\"status\":400,\"title\":\"\",\"instance\":\"\",\"detail\":\"bad request\",\"invalid_parameters\":null}",
		},
		{
			name: "409 Conflict causes a list to find a matching (by UID) service and update it instead of creating a new one",
			mockServicePair: func(t *testing.T) (*sdkmocks.MockServicesSDK, *configurationv1alpha1.KongService) {
				sdk := sdkmocks.NewMockServicesSDK(t)
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
						&sdkkonnecterrs.ConflictError{
							Status: 409,
							Detail: "Conflict",
						},
					)

				return sdk, svc
			},
			expectedErrType:     &sdkkonnecterrs.ConflictError{},
			expectedErrContains: "failed to create KongService default/svc-1: {\"status\":409,\"title\":null,\"instance\":null,\"detail\":\"Conflict\"}",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sdk, svc := tc.mockServicePair(t)

			err := createService(ctx, sdk, svc)

			if tc.assertions != nil {
				tc.assertions(t, svc)
			}

			if tc.expectedErrContains != "" {
				assert.ErrorContains(t, err, tc.expectedErrContains)
				if tc.expectedErrType != nil {
					require.ErrorAs(t, err, &tc.expectedErrType)
				}
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestDeleteKongService(t *testing.T) {
	ctx := t.Context()
	testCases := []struct {
		name            string
		mockServicePair func(*testing.T) (*sdkmocks.MockServicesSDK, *configurationv1alpha1.KongService)
		expectedErr     bool
		assertions      func(*testing.T, *configurationv1alpha1.KongService)
	}{
		{
			name: "success",
			mockServicePair: func(t *testing.T) (*sdkmocks.MockServicesSDK, *configurationv1alpha1.KongService) {
				sdk := sdkmocks.NewMockServicesSDK(t)
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
			mockServicePair: func(t *testing.T) (*sdkmocks.MockServicesSDK, *configurationv1alpha1.KongService) {
				sdk := sdkmocks.NewMockServicesSDK(t)
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
			mockServicePair: func(t *testing.T) (*sdkmocks.MockServicesSDK, *configurationv1alpha1.KongService) {
				sdk := sdkmocks.NewMockServicesSDK(t)
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

			require.NoError(t, err)
		})
	}
}

func TestUpdateKongService(t *testing.T) {
	ctx := t.Context()
	testCases := []struct {
		name            string
		mockServicePair func(*testing.T) (*sdkmocks.MockServicesSDK, *configurationv1alpha1.KongService)
		expectedErr     bool
		assertions      func(*testing.T, *configurationv1alpha1.KongService)
	}{
		{
			name: "success",
			mockServicePair: func(t *testing.T) (*sdkmocks.MockServicesSDK, *configurationv1alpha1.KongService) {
				sdk := sdkmocks.NewMockServicesSDK(t)
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
							Service: &sdkkonnectcomp.ServiceOutput{
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
			},
		},
		{
			name: "fail",
			mockServicePair: func(t *testing.T) (*sdkmocks.MockServicesSDK, *configurationv1alpha1.KongService) {
				sdk := sdkmocks.NewMockServicesSDK(t)
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
				assert.Equal(t, "123456789", svc.GetKonnectStatus().GetKonnectID())
			},
			expectedErr: true,
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

			require.NoError(t, err)
		})
	}
}

func TestCreateAndUpdateKongService_KubernetesMetadataConsistency(t *testing.T) {
	svc := &configurationv1alpha1.KongService{
		TypeMeta: metav1.TypeMeta{
			Kind:       "KongService",
			APIVersion: "configuration.konghq.com/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "svc-1",
			Namespace:  "default",
			UID:        k8stypes.UID(uuid.NewString()),
			Generation: 2,
			Annotations: map[string]string{
				metadata.AnnotationKeyTags: "tag1,tag2,duplicate-tag",
			},
		},
		Status: configurationv1alpha1.KongServiceStatus{
			Konnect: &konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef{
				ControlPlaneID: uuid.NewString(),
			},
		},
		Spec: configurationv1alpha1.KongServiceSpec{
			KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
				Tags: []string{"tag3", "tag4", "duplicate-tag"},
			},
		},
	}
	output := kongServiceToSDKServiceInput(svc)
	expectedTags := []string{
		"k8s-kind:KongService",
		"k8s-name:svc-1",
		"k8s-uid:" + string(svc.GetUID()),
		"k8s-version:v1alpha1",
		"k8s-group:configuration.konghq.com",
		"k8s-namespace:default",
		"k8s-generation:2",
		"tag1",
		"tag2",
		"tag3",
		"tag4",
		"duplicate-tag",
	}
	require.ElementsMatch(t, expectedTags, output.Tags)
}
