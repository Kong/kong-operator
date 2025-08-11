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

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1alpha1"
	konnectv1alpha2 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha2"
	"github.com/kong/kubernetes-configuration/v2/pkg/metadata"

	sdkmocks "github.com/kong/kong-operator/controller/konnect/ops/sdk/mocks"
)

func TestCreateKongUpstream(t *testing.T) {
	ctx := t.Context()
	testCases := []struct {
		name             string
		mockUpstreamPair func(*testing.T) (*sdkmocks.MockUpstreamsSDK, *configurationv1alpha1.KongUpstream)
		expectedErr      bool
		assertions       func(*testing.T, *configurationv1alpha1.KongUpstream)
	}{
		{
			name: "success",
			mockUpstreamPair: func(t *testing.T) (*sdkmocks.MockUpstreamsSDK, *configurationv1alpha1.KongUpstream) {
				sdk := sdkmocks.NewMockUpstreamsSDK(t)
				svc := &configurationv1alpha1.KongUpstream{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc-1",
						Namespace: "default",
					},
					Spec: configurationv1alpha1.KongUpstreamSpec{
						KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
							Name: "svc-1",
						},
					},
					Status: configurationv1alpha1.KongUpstreamStatus{
						Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
							ControlPlaneID: "123456789",
						},
					},
				}

				sdk.
					EXPECT().
					CreateUpstream(ctx, "123456789", kongUpstreamToSDKUpstreamInput(svc)).
					Return(
						&sdkkonnectops.CreateUpstreamResponse{
							Upstream: &sdkkonnectcomp.Upstream{
								ID:   lo.ToPtr("12345"),
								Name: "svc-1",
							},
						},
						nil,
					)

				return sdk, svc
			},
			assertions: func(t *testing.T, svc *configurationv1alpha1.KongUpstream) {
				assert.Equal(t, "12345", svc.GetKonnectStatus().GetKonnectID())
			},
		},
		{
			name: "fail - no control plane ID in status returns an error and does not create the Upstream in Konnect",
			mockUpstreamPair: func(t *testing.T) (*sdkmocks.MockUpstreamsSDK, *configurationv1alpha1.KongUpstream) {
				sdk := sdkmocks.NewMockUpstreamsSDK(t)
				svc := &configurationv1alpha1.KongUpstream{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc-1",
						Namespace: "default",
					},
					Spec: configurationv1alpha1.KongUpstreamSpec{
						KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
							Name: "svc-1",
						},
					},
				}

				return sdk, svc
			},
			assertions: func(t *testing.T, svc *configurationv1alpha1.KongUpstream) {
				assert.Empty(t, svc.GetKonnectStatus().GetKonnectID())
				// TODO: we should probably set a condition when the control plane ID is missing in the status.
			},
			expectedErr: true,
		},
		{
			name: "fail",
			mockUpstreamPair: func(t *testing.T) (*sdkmocks.MockUpstreamsSDK, *configurationv1alpha1.KongUpstream) {
				sdk := sdkmocks.NewMockUpstreamsSDK(t)
				svc := &configurationv1alpha1.KongUpstream{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc-1",
						Namespace: "default",
					},
					Spec: configurationv1alpha1.KongUpstreamSpec{
						KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
							Name: "svc-1",
						},
					},
					Status: configurationv1alpha1.KongUpstreamStatus{
						Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
							ControlPlaneID: "123456789",
						},
					},
				}

				sdk.
					EXPECT().
					CreateUpstream(ctx, "123456789", kongUpstreamToSDKUpstreamInput(svc)).
					Return(
						nil,
						&sdkkonnecterrs.BadRequestError{
							Status: 400,
							Detail: "bad request",
						},
					)

				return sdk, svc
			},
			assertions: func(t *testing.T, svc *configurationv1alpha1.KongUpstream) {
				assert.Empty(t, svc.GetKonnectStatus().GetKonnectID())
			},
			expectedErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sdk, svc := tc.mockUpstreamPair(t)

			err := createUpstream(ctx, sdk, svc)

			tc.assertions(t, svc)

			if tc.expectedErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestDeleteKongUpstream(t *testing.T) {
	ctx := t.Context()
	testCases := []struct {
		name             string
		mockUpstreamPair func(*testing.T) (*sdkmocks.MockUpstreamsSDK, *configurationv1alpha1.KongUpstream)
		expectedErr      bool
		assertions       func(*testing.T, *configurationv1alpha1.KongUpstream)
	}{
		{
			name: "success",
			mockUpstreamPair: func(t *testing.T) (*sdkmocks.MockUpstreamsSDK, *configurationv1alpha1.KongUpstream) {
				sdk := sdkmocks.NewMockUpstreamsSDK(t)
				svc := &configurationv1alpha1.KongUpstream{
					Spec: configurationv1alpha1.KongUpstreamSpec{
						KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
							Name: "svc-1",
						},
					},
					Status: configurationv1alpha1.KongUpstreamStatus{
						Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
							ControlPlaneID: "12345",
							KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
								ID: "123456789",
							},
						},
					},
				}
				sdk.
					EXPECT().
					DeleteUpstream(ctx, "12345", "123456789").
					Return(
						&sdkkonnectops.DeleteUpstreamResponse{
							StatusCode: 200,
						},
						nil,
					)

				return sdk, svc
			},
		},
		{
			name: "fail",
			mockUpstreamPair: func(t *testing.T) (*sdkmocks.MockUpstreamsSDK, *configurationv1alpha1.KongUpstream) {
				sdk := sdkmocks.NewMockUpstreamsSDK(t)
				svc := &configurationv1alpha1.KongUpstream{
					Spec: configurationv1alpha1.KongUpstreamSpec{
						KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
							Name: "svc-1",
						},
					},
					Status: configurationv1alpha1.KongUpstreamStatus{
						Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
							ControlPlaneID: "12345",
							KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
								ID: "123456789",
							},
						},
					},
				}
				sdk.
					EXPECT().
					DeleteUpstream(ctx, "12345", "123456789").
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
			mockUpstreamPair: func(t *testing.T) (*sdkmocks.MockUpstreamsSDK, *configurationv1alpha1.KongUpstream) {
				sdk := sdkmocks.NewMockUpstreamsSDK(t)
				svc := &configurationv1alpha1.KongUpstream{
					Spec: configurationv1alpha1.KongUpstreamSpec{
						KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
							Name: "svc-1",
						},
					},
					Status: configurationv1alpha1.KongUpstreamStatus{
						Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
							ControlPlaneID: "12345",
							KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
								ID: "123456789",
							},
						},
					},
				}
				sdk.
					EXPECT().
					DeleteUpstream(ctx, "12345", "123456789").
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
			sdk, svc := tc.mockUpstreamPair(t)

			err := deleteUpstream(ctx, sdk, svc)

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

func TestUpdateKongUpstream(t *testing.T) {
	ctx := t.Context()
	testCases := []struct {
		name             string
		mockUpstreamPair func(*testing.T) (*sdkmocks.MockUpstreamsSDK, *configurationv1alpha1.KongUpstream)
		expectedErr      bool
		assertions       func(*testing.T, *configurationv1alpha1.KongUpstream)
	}{
		{
			name: "success",
			mockUpstreamPair: func(t *testing.T) (*sdkmocks.MockUpstreamsSDK, *configurationv1alpha1.KongUpstream) {
				sdk := sdkmocks.NewMockUpstreamsSDK(t)
				svc := &configurationv1alpha1.KongUpstream{
					Spec: configurationv1alpha1.KongUpstreamSpec{
						KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
							Name: "svc-1",
						},
					},
					Status: configurationv1alpha1.KongUpstreamStatus{
						Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
							ControlPlaneID: "12345",
							KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
								ID: "123456789",
							},
						},
					},
				}
				sdk.
					EXPECT().
					UpsertUpstream(ctx,
						sdkkonnectops.UpsertUpstreamRequest{
							ControlPlaneID: "12345",
							UpstreamID:     "123456789",
							Upstream:       kongUpstreamToSDKUpstreamInput(svc),
						},
					).
					Return(
						&sdkkonnectops.UpsertUpstreamResponse{
							StatusCode: 200,
							Upstream: &sdkkonnectcomp.Upstream{
								ID:   lo.ToPtr("123456789"),
								Name: "svc-1",
							},
						},
						nil,
					)

				return sdk, svc
			},
			assertions: func(t *testing.T, svc *configurationv1alpha1.KongUpstream) {
				assert.Equal(t, "123456789", svc.GetKonnectStatus().GetKonnectID())
			},
		},
		{
			name: "fail",
			mockUpstreamPair: func(t *testing.T) (*sdkmocks.MockUpstreamsSDK, *configurationv1alpha1.KongUpstream) {
				sdk := sdkmocks.NewMockUpstreamsSDK(t)
				svc := &configurationv1alpha1.KongUpstream{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc-1",
						Namespace: "default",
					},
					Spec: configurationv1alpha1.KongUpstreamSpec{
						KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
							Name: "svc-1",
						},
					},
					Status: configurationv1alpha1.KongUpstreamStatus{
						Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
							ControlPlaneID: "12345",
							KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
								ID: "123456789",
							},
						},
					},
				}
				sdk.
					EXPECT().
					UpsertUpstream(ctx,
						sdkkonnectops.UpsertUpstreamRequest{
							ControlPlaneID: "12345",
							UpstreamID:     "123456789",
							Upstream:       kongUpstreamToSDKUpstreamInput(svc),
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
			assertions: func(t *testing.T, svc *configurationv1alpha1.KongUpstream) {
				assert.Equal(t, "123456789", svc.GetKonnectStatus().GetKonnectID(),
					"Konnect ID should be retained after a failed update",
				)
			},
			expectedErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sdk, svc := tc.mockUpstreamPair(t)

			err := updateUpstream(ctx, sdk, svc)

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

func TestCreateAndUpdateKongUpstream_KubernetesMetadataConsistency(t *testing.T) {
	svc := &configurationv1alpha1.KongUpstream{
		TypeMeta: metav1.TypeMeta{
			Kind:       "KongUpstream",
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
		Status: configurationv1alpha1.KongUpstreamStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
				ControlPlaneID: uuid.NewString(),
			},
		},
		Spec: configurationv1alpha1.KongUpstreamSpec{
			KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
				Tags: []string{"tag3", "tag4", "duplicate-tag"},
			},
		},
	}
	output := kongUpstreamToSDKUpstreamInput(svc)
	expectedTags := []string{
		"k8s-kind:KongUpstream",
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
