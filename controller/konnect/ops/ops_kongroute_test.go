package ops

import (
	"fmt"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/Kong/sdk-konnect-go/test/mocks"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/pkg/metadata"
)

func TestKongRouteToSDKRouteInput_Tags(t *testing.T) {
	route := &configurationv1alpha1.KongRoute{
		TypeMeta: metav1.TypeMeta{
			Kind:       "KongRoute",
			APIVersion: "configuration.konghq.com/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "route-1",
			Namespace:  "default",
			UID:        k8stypes.UID(uuid.NewString()),
			Generation: 2,
			Annotations: map[string]string{
				metadata.AnnotationKeyTags: "tag1,tag2,duplicate-tag",
			},
		},
		Spec: configurationv1alpha1.KongRouteSpec{
			ServiceRef: &configurationv1alpha1.ServiceRef{
				Type: configurationv1alpha1.ServiceRefNamespacedRef,
				NamespacedRef: &commonv1alpha1.NameRef{
					Name: "service-1",
				},
			},
			KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{
				Tags: []string{"tag3", "tag4", "duplicate-tag"},
			},
		},
		Status: configurationv1alpha1.KongRouteStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndServiceRefs{
				ControlPlaneID: "12345",
			},
		},
	}

	output := kongRouteToSDKRouteInput(route)
	expectedTags := []string{
		"k8s-kind:KongRoute",
		"k8s-name:route-1",
		"k8s-namespace:default",
		"k8s-uid:" + string(route.GetUID()),
		"k8s-version:v1alpha1",
		"k8s-group:configuration.konghq.com",
		"k8s-generation:2",
		"tag1",
		"tag2",
		"tag3",
		"tag4",
		"duplicate-tag",
	}
	require.ElementsMatch(t, expectedTags, output.RouteJSON.Tags)
}

func TestCreateKongRoute(t *testing.T) {
	ctx := t.Context()

	t.Run("existing route by UID is reused when name is missing", func(t *testing.T) {
		sdk := mocks.NewMockRoutesSDK(t)
		route := &configurationv1alpha1.KongRoute{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "route-1",
				Namespace: "default",
				UID:       k8stypes.UID("abcd-1234"),
			},
			Spec: configurationv1alpha1.KongRouteSpec{
				KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{
					Hosts: []string{"example.com"},
				},
			},
			Status: configurationv1alpha1.KongRouteStatus{
				Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndServiceRefs{
					ControlPlaneID: "123456789",
				},
			},
		}

		sdk.
			EXPECT().
			ListRoute(ctx, mock.MatchedBy(func(req sdkkonnectops.ListRouteRequest) bool {
				return req.ControlPlaneID == "123456789" && req.Tags != nil && *req.Tags == UIDLabelForObject(route)
			})).
			Return(
				&sdkkonnectops.ListRouteResponse{
					Object: &sdkkonnectops.ListRouteResponseBody{
						Data: []sdkkonnectcomp.Route{
							sdkkonnectcomp.CreateRouteRouteJSON(sdkkonnectcomp.RouteJSON{
								ID: lo.ToPtr("route-123"),
							}),
						},
					},
				},
				nil,
			)

		err := createRoute(ctx, sdk, route)
		require.NoError(t, err)
		assert.Equal(t, "route-123", route.GetKonnectStatus().GetKonnectID())
	})
}

func TestAdoptRoute(t *testing.T) {
	ctx := t.Context()
	testCases := []struct {
		name                string
		mockRoutePair       func(*testing.T) (*mocks.MockRoutesSDK, *configurationv1alpha1.KongRoute)
		assertions          func(*testing.T, *configurationv1alpha1.KongRoute)
		expectedErrContains string
		expectedErrType     error
	}{
		{
			name: "success",
			mockRoutePair: func(t *testing.T) (*mocks.MockRoutesSDK, *configurationv1alpha1.KongRoute) {
				sdk := mocks.NewMockRoutesSDK(t)
				route := &configurationv1alpha1.KongRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "route-1",
						Namespace: "default",
						UID:       k8stypes.UID("abcd-1234"),
					},
					Spec: configurationv1alpha1.KongRouteSpec{
						Adopt: &commonv1alpha1.AdoptOptions{
							From: commonv1alpha1.AdoptSourceKonnect,
							Mode: commonv1alpha1.AdoptModeOverride,
							Konnect: &commonv1alpha1.AdoptKonnectOptions{
								ID: "1234",
							},
						},
						ServiceRef: &configurationv1alpha1.ServiceRef{
							Type:          "namespacedRef",
							NamespacedRef: &commonv1alpha1.NameRef{Name: "svc-1"},
						},
						KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{
							Paths: []string{"/test-1"},
						},
					},
					Status: configurationv1alpha1.KongRouteStatus{
						Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndServiceRefs{
							ControlPlaneID: "123456789",
							ServiceID:      "12345",
						},
					},
				}
				sdk.EXPECT().GetRoute(
					mock.Anything,
					"1234",
					"123456789",
				).Return(
					&sdkkonnectops.GetRouteResponse{
						Route: &sdkkonnectcomp.Route{
							Type: sdkkonnectcomp.RouteTypeRouteJSON,
							RouteJSON: &sdkkonnectcomp.RouteJSON{
								Service: &sdkkonnectcomp.RouteJSONService{
									ID: lo.ToPtr("12345"),
								},
								Paths: []string{"/test"},
							},
						},
					}, nil,
				)

				sdk.EXPECT().UpsertRoute(
					mock.Anything,
					mock.MatchedBy(func(req sdkkonnectops.UpsertRouteRequest) bool {
						return req.RouteID == "1234"
					}),
				).Return(&sdkkonnectops.UpsertRouteResponse{}, nil)
				return sdk, route
			},
			assertions: func(t *testing.T, kr *configurationv1alpha1.KongRoute) {
				assert.Equal(t, "1234", kr.GetKonnectID())
			},
		},
		{
			name: "failed to fetch",
			mockRoutePair: func(t *testing.T) (*mocks.MockRoutesSDK, *configurationv1alpha1.KongRoute) {
				sdk := mocks.NewMockRoutesSDK(t)
				route := &configurationv1alpha1.KongRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "route-1",
						Namespace: "default",
						UID:       k8stypes.UID("abcd-1234"),
					},
					Spec: configurationv1alpha1.KongRouteSpec{
						Adopt: &commonv1alpha1.AdoptOptions{
							From: commonv1alpha1.AdoptSourceKonnect,
							Mode: commonv1alpha1.AdoptModeOverride,
							Konnect: &commonv1alpha1.AdoptKonnectOptions{
								ID: "1234",
							},
						},
						KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{
							Paths: []string{"/test-1"},
						},
					},
					Status: configurationv1alpha1.KongRouteStatus{
						Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndServiceRefs{
							ControlPlaneID: "123456789",
						},
					},
				}

				sdk.EXPECT().GetRoute(
					mock.Anything,
					"1234",
					"123456789",
				).Return(
					nil, &sdkkonnecterrs.NotFoundError{},
				)

				return sdk, route
			},
			expectedErrContains: "failed to fetch Konnect entity",
			expectedErrType:     KonnectEntityAdoptionFetchError{},
		},
		{
			name: "unsupported route type",
			mockRoutePair: func(t *testing.T) (*mocks.MockRoutesSDK, *configurationv1alpha1.KongRoute) {
				sdk := mocks.NewMockRoutesSDK(t)
				route := &configurationv1alpha1.KongRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "route-1",
						Namespace: "default",
						UID:       k8stypes.UID("abcd-1234"),
					},
					Spec: configurationv1alpha1.KongRouteSpec{
						Adopt: &commonv1alpha1.AdoptOptions{
							From: commonv1alpha1.AdoptSourceKonnect,
							Mode: commonv1alpha1.AdoptModeOverride,
							Konnect: &commonv1alpha1.AdoptKonnectOptions{
								ID: "1234",
							},
						},
						KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{
							Paths: []string{"/test-1"},
						},
					},
					Status: configurationv1alpha1.KongRouteStatus{
						Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndServiceRefs{
							ControlPlaneID: "123456789",
						},
					},
				}

				sdk.EXPECT().GetRoute(
					mock.Anything,
					"1234",
					"123456789",
				).Return(
					&sdkkonnectops.GetRouteResponse{
						Route: &sdkkonnectcomp.Route{
							Type: sdkkonnectcomp.RouteTypeRouteExpression,
						},
					}, nil,
				)

				return sdk, route
			},
			expectedErrContains: fmt.Sprintf("route type %q not supported", sdkkonnectcomp.RouteTypeRouteExpression),
		},
		{
			name: "service reference not match",
			mockRoutePair: func(t *testing.T) (*mocks.MockRoutesSDK, *configurationv1alpha1.KongRoute) {
				sdk := mocks.NewMockRoutesSDK(t)
				route := &configurationv1alpha1.KongRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "route-1",
						Namespace: "default",
						UID:       k8stypes.UID("abcd-1234"),
					},
					Spec: configurationv1alpha1.KongRouteSpec{
						Adopt: &commonv1alpha1.AdoptOptions{
							From: commonv1alpha1.AdoptSourceKonnect,
							Mode: commonv1alpha1.AdoptModeOverride,
							Konnect: &commonv1alpha1.AdoptKonnectOptions{
								ID: "1234",
							},
						},
						ServiceRef: &configurationv1alpha1.ServiceRef{
							Type:          "namespacedRef",
							NamespacedRef: &commonv1alpha1.NameRef{Name: "svc-1"},
						},
						KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{
							Paths: []string{"/test-1"},
						},
					},
					Status: configurationv1alpha1.KongRouteStatus{
						Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndServiceRefs{
							ControlPlaneID: "123456789",
							ServiceID:      "12345",
						},
					},
				}
				sdk.EXPECT().GetRoute(
					mock.Anything,
					"1234",
					"123456789",
				).Return(
					&sdkkonnectops.GetRouteResponse{
						Route: &sdkkonnectcomp.Route{
							Type: sdkkonnectcomp.RouteTypeRouteJSON,
							RouteJSON: &sdkkonnectcomp.RouteJSON{
								Service: &sdkkonnectcomp.RouteJSONService{
									ID: lo.ToPtr("123456"),
								},
								Paths: []string{"/test"},
							},
						},
					}, nil,
				)

				return sdk, route
			},
			expectedErrContains: "failed to adopt: reference service ID does not match",
		},
		{
			name: "success in match mode",
			mockRoutePair: func(t *testing.T) (*mocks.MockRoutesSDK, *configurationv1alpha1.KongRoute) {
				sdk := mocks.NewMockRoutesSDK(t)
				route := &configurationv1alpha1.KongRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "route-1",
						Namespace: "default",
						UID:       k8stypes.UID("abcd-1234"),
					},
					Spec: configurationv1alpha1.KongRouteSpec{
						Adopt: &commonv1alpha1.AdoptOptions{
							From: commonv1alpha1.AdoptSourceKonnect,
							Mode: commonv1alpha1.AdoptModeMatch,
							Konnect: &commonv1alpha1.AdoptKonnectOptions{
								ID: "1234",
							},
						},
						KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{
							Paths: []string{"/test-2", "/test"},
							Headers: map[string][]string{
								"h1": {"v1"},
							},
						},
					},
					Status: configurationv1alpha1.KongRouteStatus{
						Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndServiceRefs{
							ControlPlaneID: "123456789",
						},
					},
				}
				sdk.EXPECT().GetRoute(
					mock.Anything,
					"1234",
					"123456789",
				).Return(
					&sdkkonnectops.GetRouteResponse{
						Route: &sdkkonnectcomp.Route{
							Type: sdkkonnectcomp.RouteTypeRouteJSON,
							RouteJSON: &sdkkonnectcomp.RouteJSON{
								Paths: []string{"/test", "/test-2"},
								Headers: map[string][]string{
									"h1": {"v1"},
								},
								HTTPSRedirectStatusCode: lo.ToPtr(sdkkonnectcomp.HTTPSRedirectStatusCodeFourHundredAndTwentySix),
							},
						},
					}, nil,
				)

				return sdk, route
			},
			assertions: func(t *testing.T, kr *configurationv1alpha1.KongRoute) {
				assert.Equal(t, "1234", kr.GetKonnectID())
			},
		},
		{
			name: "failed to match in match mode",
			mockRoutePair: func(t *testing.T) (*mocks.MockRoutesSDK, *configurationv1alpha1.KongRoute) {
				sdk := mocks.NewMockRoutesSDK(t)
				route := &configurationv1alpha1.KongRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "route-1",
						Namespace: "default",
						UID:       k8stypes.UID("abcd-1234"),
					},
					Spec: configurationv1alpha1.KongRouteSpec{
						Adopt: &commonv1alpha1.AdoptOptions{
							From: commonv1alpha1.AdoptSourceKonnect,
							Mode: commonv1alpha1.AdoptModeMatch,
							Konnect: &commonv1alpha1.AdoptKonnectOptions{
								ID: "1234",
							},
						},
						KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{
							Paths: []string{"/test-2", "/test"},
							Headers: map[string][]string{
								"h1": {"v1"},
							},
							Methods: []string{"GET", "POST"},
						},
					},
					Status: configurationv1alpha1.KongRouteStatus{
						Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndServiceRefs{
							ControlPlaneID: "123456789",
						},
					},
				}
				sdk.EXPECT().GetRoute(
					mock.Anything,
					"1234",
					"123456789",
				).Return(
					&sdkkonnectops.GetRouteResponse{
						Route: &sdkkonnectcomp.Route{
							Type: sdkkonnectcomp.RouteTypeRouteJSON,
							RouteJSON: &sdkkonnectcomp.RouteJSON{
								Paths: []string{"/test", "/test-2"},
								Headers: map[string][]string{
									"h1": {"v1"},
								},
								Methods:                 []string{"GET"},
								HTTPSRedirectStatusCode: lo.ToPtr(sdkkonnectcomp.HTTPSRedirectStatusCodeFourHundredAndTwentySix),
							},
						},
					}, nil,
				)

				return sdk, route
			},
			expectedErrContains: "Konnect entity (ID: 1234) does not match the spec of the object when adopting in match mode",
			expectedErrType:     &KonnectEntityAdoptionNotMatchError{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sdk, route := tc.mockRoutePair(t)
			err := adoptRoute(ctx, sdk, route)

			if tc.assertions != nil {
				tc.assertions(t, route)
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
