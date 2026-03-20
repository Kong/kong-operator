package ops

import (
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/Kong/sdk-konnect-go/test/mocks"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
)

func TestCreateEventGateway(t *testing.T) {
	const egID = "eg-12345"
	ctx := t.Context()

	testCases := []struct {
		name          string
		mockPair      func(*testing.T) (*mocks.MockEventGatewaysSDK, *konnectv1alpha1.KonnectEventGateway)
		expectedError error
		expectedID    string
	}{
		{
			name: "success",
			mockPair: func(t *testing.T) (*mocks.MockEventGatewaysSDK, *konnectv1alpha1.KonnectEventGateway) {
				sdk := mocks.NewMockEventGatewaysSDK(t)
				eg := &konnectv1alpha1.KonnectEventGateway{
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						Source: new(commonv1alpha1.EntitySourceOrigin),
						CreateGatewayRequest: &konnectv1alpha1.CreateEventGatewayRequest{
							Name: "my-event-gateway",
						},
					},
				}
				sdk.EXPECT().
					CreateEventGateway(ctx, sdkkonnectcomp.CreateGatewayRequest{
						Name:   "my-event-gateway",
						Labels: WithKubernetesMetadataLabels(eg, nil),
					}).
					Return(&sdkkonnectops.CreateEventGatewayResponse{
						EventGatewayInfo: &sdkkonnectcomp.EventGatewayInfo{
							ID: egID,
						},
					}, nil)
				return sdk, eg
			},
			expectedID: egID,
		},
		{
			name: "fail",
			mockPair: func(t *testing.T) (*mocks.MockEventGatewaysSDK, *konnectv1alpha1.KonnectEventGateway) {
				sdk := mocks.NewMockEventGatewaysSDK(t)
				eg := &konnectv1alpha1.KonnectEventGateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-event-gateway",
						Namespace: "default",
					},
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						Source: new(commonv1alpha1.EntitySourceOrigin),
						CreateGatewayRequest: &konnectv1alpha1.CreateEventGatewayRequest{
							Name: "my-event-gateway",
						},
					},
				}
				sdk.EXPECT().
					CreateEventGateway(ctx, sdkkonnectcomp.CreateGatewayRequest{
						Name:   "my-event-gateway",
						Labels: WithKubernetesMetadataLabels(eg, nil),
					}).
					Return(nil, &sdkkonnecterrs.BadRequestError{
						Status: 400,
						Detail: "bad request",
					})
				return sdk, eg
			},
			expectedError: KonnectOperationFailedError{
				Op:         CreateOp,
				EntityType: "KonnectEventGateway",
				EntityKey:  "default/my-event-gateway",
				Err: &sdkkonnecterrs.BadRequestError{
					Status: 400,
					Detail: "bad request",
				},
			},
		},
		{
			name: "nil response returns error",
			mockPair: func(t *testing.T) (*mocks.MockEventGatewaysSDK, *konnectv1alpha1.KonnectEventGateway) {
				sdk := mocks.NewMockEventGatewaysSDK(t)
				eg := &konnectv1alpha1.KonnectEventGateway{
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						Source: new(commonv1alpha1.EntitySourceOrigin),
						CreateGatewayRequest: &konnectv1alpha1.CreateEventGatewayRequest{
							Name: "my-event-gateway",
						},
					},
				}
				sdk.EXPECT().
					CreateEventGateway(ctx, sdkkonnectcomp.CreateGatewayRequest{
						Name:   "my-event-gateway",
						Labels: WithKubernetesMetadataLabels(eg, nil),
					}).
					Return(nil, nil)
				return sdk, eg
			},
			expectedError: ErrNilResponse,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sdk, eg := tc.mockPair(t)

			err := createEventGateway(ctx, sdk, eg)
			require.ErrorIs(t, err, tc.expectedError)

			if tc.expectedID != "" {
				assert.Equal(t, tc.expectedID, eg.Status.ID)
			}
		})
	}
}

func TestDeleteEventGateway(t *testing.T) {
	ctx := t.Context()

	testCases := []struct {
		name        string
		mockPair    func(*testing.T) (*mocks.MockEventGatewaysSDK, *konnectv1alpha1.KonnectEventGateway)
		expectedErr bool
	}{
		{
			name: "success",
			mockPair: func(t *testing.T) (*mocks.MockEventGatewaysSDK, *konnectv1alpha1.KonnectEventGateway) {
				sdk := mocks.NewMockEventGatewaysSDK(t)
				eg := &konnectv1alpha1.KonnectEventGateway{
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						Source: new(commonv1alpha1.EntitySourceOrigin),
					},
					Status: konnectv1alpha1.KonnectEventGatewayStatus{
						KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{ID: "12345"},
					},
				}
				sdk.EXPECT().
					DeleteEventGateway(ctx, "12345").
					Return(&sdkkonnectops.DeleteEventGatewayResponse{StatusCode: 204}, nil)
				return sdk, eg
			},
		},
		{
			name: "fail",
			mockPair: func(t *testing.T) (*mocks.MockEventGatewaysSDK, *konnectv1alpha1.KonnectEventGateway) {
				sdk := mocks.NewMockEventGatewaysSDK(t)
				eg := &konnectv1alpha1.KonnectEventGateway{
					ObjectMeta: metav1.ObjectMeta{Name: "my-event-gateway", Namespace: "default"},
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						Source: new(commonv1alpha1.EntitySourceOrigin),
					},
					Status: konnectv1alpha1.KonnectEventGatewayStatus{
						KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{ID: "12345"},
					},
				}
				sdk.EXPECT().
					DeleteEventGateway(ctx, "12345").
					Return(nil, &sdkkonnecterrs.BadRequestError{
						Status: 400,
						Detail: "bad request",
					})
				return sdk, eg
			},
			expectedErr: true,
		},
		{
			name: "not found is ignored",
			mockPair: func(t *testing.T) (*mocks.MockEventGatewaysSDK, *konnectv1alpha1.KonnectEventGateway) {
				sdk := mocks.NewMockEventGatewaysSDK(t)
				eg := &konnectv1alpha1.KonnectEventGateway{
					ObjectMeta: metav1.ObjectMeta{Name: "my-event-gateway", Namespace: "default"},
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						Source: new(commonv1alpha1.EntitySourceOrigin),
					},
					Status: konnectv1alpha1.KonnectEventGatewayStatus{
						KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{ID: "12345"},
					},
				}
				sdk.EXPECT().
					DeleteEventGateway(ctx, "12345").
					Return(nil, &sdkkonnecterrs.NotFoundError{
						Status: 404,
						Detail: "not found",
					})
				return sdk, eg
			},
		},
		{
			name: "mirror source skips delete",
			mockPair: func(t *testing.T) (*mocks.MockEventGatewaysSDK, *konnectv1alpha1.KonnectEventGateway) {
				sdk := mocks.NewMockEventGatewaysSDK(t)
				eg := &konnectv1alpha1.KonnectEventGateway{
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						Source: new(commonv1alpha1.EntitySourceMirror),
						Mirror: &konnectv1alpha1.EventGatewayMirrorSpec{
							Konnect: konnectv1alpha1.EventGatewayMirrorKonnect{ID: "12345"},
						},
					},
					Status: konnectv1alpha1.KonnectEventGatewayStatus{
						KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{ID: "12345"},
					},
				}
				// No SDK call expected. Mirror gateways are never deleted.
				return sdk, eg
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sdk, eg := tc.mockPair(t)

			err := deleteEventGateway(ctx, sdk, eg)

			if tc.expectedErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestUpdateEventGateway(t *testing.T) {
	ctx := t.Context()

	testCases := []struct {
		name        string
		mockPair    func(*testing.T) (*mocks.MockEventGatewaysSDK, *konnectv1alpha1.KonnectEventGateway)
		expectedErr bool
		expectedID  string
	}{
		{
			name: "success",
			mockPair: func(t *testing.T) (*mocks.MockEventGatewaysSDK, *konnectv1alpha1.KonnectEventGateway) {
				sdk := mocks.NewMockEventGatewaysSDK(t)
				eg := &konnectv1alpha1.KonnectEventGateway{
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						Source: new(commonv1alpha1.EntitySourceOrigin),
						CreateGatewayRequest: &konnectv1alpha1.CreateEventGatewayRequest{
							Name: "my-event-gateway",
						},
					},
					Status: konnectv1alpha1.KonnectEventGatewayStatus{
						KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{ID: "12345"},
					},
				}
				sdk.EXPECT().
					UpdateEventGateway(ctx, "12345", sdkkonnectcomp.UpdateGatewayRequest{
						Name:   &eg.Spec.CreateGatewayRequest.Name,
						Labels: WithKubernetesMetadataLabels(eg, nil),
					}).
					Return(&sdkkonnectops.UpdateEventGatewayResponse{
						EventGatewayInfo: &sdkkonnectcomp.EventGatewayInfo{ID: "12345"},
					}, nil)
				return sdk, eg
			},
			expectedID: "12345",
		},
		{
			name: "fail",
			mockPair: func(t *testing.T) (*mocks.MockEventGatewaysSDK, *konnectv1alpha1.KonnectEventGateway) {
				sdk := mocks.NewMockEventGatewaysSDK(t)
				eg := &konnectv1alpha1.KonnectEventGateway{
					ObjectMeta: metav1.ObjectMeta{Name: "my-event-gateway", Namespace: "default"},
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						Source: new(commonv1alpha1.EntitySourceOrigin),
						CreateGatewayRequest: &konnectv1alpha1.CreateEventGatewayRequest{
							Name: "my-event-gateway",
						},
					},
					Status: konnectv1alpha1.KonnectEventGatewayStatus{
						KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{ID: "12345"},
					},
				}
				sdk.EXPECT().
					UpdateEventGateway(ctx, "12345", sdkkonnectcomp.UpdateGatewayRequest{
						Name:   &eg.Spec.CreateGatewayRequest.Name,
						Labels: WithKubernetesMetadataLabels(eg, nil),
					}).
					Return(nil, &sdkkonnecterrs.BadRequestError{
						Status: 400,
						Detail: "bad request",
					})
				return sdk, eg
			},
			expectedErr: true,
		},
		{
			name: "not found triggers create",
			mockPair: func(t *testing.T) (*mocks.MockEventGatewaysSDK, *konnectv1alpha1.KonnectEventGateway) {
				sdk := mocks.NewMockEventGatewaysSDK(t)
				eg := &konnectv1alpha1.KonnectEventGateway{
					ObjectMeta: metav1.ObjectMeta{Name: "my-event-gateway", Namespace: "default"},
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						Source: new(commonv1alpha1.EntitySourceOrigin),
						CreateGatewayRequest: &konnectv1alpha1.CreateEventGatewayRequest{
							Name: "my-event-gateway",
						},
					},
					Status: konnectv1alpha1.KonnectEventGatewayStatus{
						KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{ID: "12345"},
					},
				}
				sdk.EXPECT().
					UpdateEventGateway(ctx, "12345", sdkkonnectcomp.UpdateGatewayRequest{
						Name:   &eg.Spec.CreateGatewayRequest.Name,
						Labels: WithKubernetesMetadataLabels(eg, nil),
					}).
					Return(nil, &sdkkonnecterrs.NotFoundError{Status: 404, Detail: "not found"})
				sdk.EXPECT().
					CreateEventGateway(ctx, sdkkonnectcomp.CreateGatewayRequest{
						Name:   "my-event-gateway",
						Labels: WithKubernetesMetadataLabels(eg, nil),
					}).
					Return(&sdkkonnectops.CreateEventGatewayResponse{
						EventGatewayInfo: &sdkkonnectcomp.EventGatewayInfo{ID: "12345"},
					}, nil)
				return sdk, eg
			},
			expectedID: "12345",
		},
		{
			name: "nil response returns error",
			mockPair: func(t *testing.T) (*mocks.MockEventGatewaysSDK, *konnectv1alpha1.KonnectEventGateway) {
				sdk := mocks.NewMockEventGatewaysSDK(t)
				eg := &konnectv1alpha1.KonnectEventGateway{
					Spec: konnectv1alpha1.KonnectEventGatewaySpec{
						Source: new(commonv1alpha1.EntitySourceOrigin),
						CreateGatewayRequest: &konnectv1alpha1.CreateEventGatewayRequest{
							Name: "my-event-gateway",
						},
					},
					Status: konnectv1alpha1.KonnectEventGatewayStatus{
						KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{ID: "12345"},
					},
				}
				sdk.EXPECT().
					UpdateEventGateway(ctx, "12345", sdkkonnectcomp.UpdateGatewayRequest{
						Name:   &eg.Spec.CreateGatewayRequest.Name,
						Labels: WithKubernetesMetadataLabels(eg, nil),
					}).
					Return(nil, nil)
				return sdk, eg
			},
			expectedErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sdk, eg := tc.mockPair(t)

			err := updateEventGateway(ctx, sdk, eg)

			if tc.expectedErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestEnsureEventGateway_Mirror(t *testing.T) {
	const mirrorID = "mirror-eg-uuid"
	ctx := t.Context()

	t.Run("mirror success", func(t *testing.T) {
		sdk := mocks.NewMockEventGatewaysSDK(t)
		eg := &konnectv1alpha1.KonnectEventGateway{
			Spec: konnectv1alpha1.KonnectEventGatewaySpec{
				Source: new(commonv1alpha1.EntitySourceMirror),
				Mirror: &konnectv1alpha1.EventGatewayMirrorSpec{
					Konnect: konnectv1alpha1.EventGatewayMirrorKonnect{ID: mirrorID},
				},
			},
		}
		sdk.EXPECT().
			GetEventGateway(ctx, mirrorID).
			Return(&sdkkonnectops.GetEventGatewayResponse{
				EventGatewayInfo: &sdkkonnectcomp.EventGatewayInfo{ID: mirrorID},
			}, nil)

		err := ensureEventGateway(ctx, sdk, eg)
		require.NoError(t, err)
		assert.Equal(t, mirrorID, eg.Status.ID)
	})

	t.Run("mirror not found returns error", func(t *testing.T) {
		sdk := mocks.NewMockEventGatewaysSDK(t)
		eg := &konnectv1alpha1.KonnectEventGateway{
			ObjectMeta: metav1.ObjectMeta{Name: "existing-eg", Namespace: "default"},
			Spec: konnectv1alpha1.KonnectEventGatewaySpec{
				Source: new(commonv1alpha1.EntitySourceMirror),
				Mirror: &konnectv1alpha1.EventGatewayMirrorSpec{
					Konnect: konnectv1alpha1.EventGatewayMirrorKonnect{ID: mirrorID},
				},
			},
		}
		sdk.EXPECT().
			GetEventGateway(ctx, mirrorID).
			Return(nil, &sdkkonnecterrs.NotFoundError{Status: 404, Detail: "not found"})

		err := ensureEventGateway(ctx, sdk, eg)
		assert.Error(t, err)
	})

	t.Run("origin dispatches to create", func(t *testing.T) {
		sdk := mocks.NewMockEventGatewaysSDK(t)
		eg := &konnectv1alpha1.KonnectEventGateway{
			Spec: konnectv1alpha1.KonnectEventGatewaySpec{
				Source: new(commonv1alpha1.EntitySourceOrigin),
				CreateGatewayRequest: &konnectv1alpha1.CreateEventGatewayRequest{
					Name: "my-event-gateway",
				},
			},
		}
		sdk.EXPECT().
			CreateEventGateway(ctx, sdkkonnectcomp.CreateGatewayRequest{
				Name:   "my-event-gateway",
				Labels: WithKubernetesMetadataLabels(eg, nil),
			}).
			Return(&sdkkonnectops.CreateEventGatewayResponse{
				EventGatewayInfo: &sdkkonnectcomp.EventGatewayInfo{ID: mirrorID},
			}, nil)

		err := ensureEventGateway(ctx, sdk, eg)
		require.NoError(t, err)
		assert.Equal(t, mirrorID, eg.Status.ID)
	})

	t.Run("mirror nil response returns error", func(t *testing.T) {
		sdk := mocks.NewMockEventGatewaysSDK(t)
		eg := &konnectv1alpha1.KonnectEventGateway{
			Spec: konnectv1alpha1.KonnectEventGatewaySpec{
				Source: new(commonv1alpha1.EntitySourceMirror),
				Mirror: &konnectv1alpha1.EventGatewayMirrorSpec{
					Konnect: konnectv1alpha1.EventGatewayMirrorKonnect{ID: mirrorID},
				},
			},
		}
		sdk.EXPECT().
			GetEventGateway(ctx, mirrorID).
			Return(nil, nil)

		err := ensureEventGateway(ctx, sdk, eg)
		assert.ErrorIs(t, err, ErrNilResponse)
	})

	t.Run("unsupported source type returns error", func(t *testing.T) {
		sdk := mocks.NewMockEventGatewaysSDK(t)
		unknown := commonv1alpha1.EntitySource("Unknown")
		eg := &konnectv1alpha1.KonnectEventGateway{
			Spec: konnectv1alpha1.KonnectEventGatewaySpec{
				Source: &unknown,
			},
		}

		err := ensureEventGateway(ctx, sdk, eg)
		assert.Error(t, err)
	})
}

func TestGetEventGatewayForUID(t *testing.T) {
	ctx := t.Context()
	uid := k8stypes.UID(uuid.NewString())

	t.Run("found by uid label", func(t *testing.T) {
		sdk := mocks.NewMockEventGatewaysSDK(t)
		eg := &konnectv1alpha1.KonnectEventGateway{
			ObjectMeta: metav1.ObjectMeta{
				Name: "my-event-gateway",
				UID:  uid,
			},
			Spec: konnectv1alpha1.KonnectEventGatewaySpec{
				CreateGatewayRequest: &konnectv1alpha1.CreateEventGatewayRequest{
					Name: "my-event-gateway",
				},
			},
		}
		sdk.EXPECT().
			ListEventGateways(ctx, sdkkonnectops.ListEventGatewaysRequest{
				Filter: &sdkkonnectcomp.EventGatewayCommonFilter{
					Name: &sdkkonnectcomp.StringFieldContainsFilter{Contains: "my-event-gateway"},
				},
			}).
			Return(&sdkkonnectops.ListEventGatewaysResponse{
				ListEventGatewaysResponse: &sdkkonnectcomp.ListEventGatewaysResponse{
					Data: []sdkkonnectcomp.EventGatewayInfo{
						{
							ID:   "found-id",
							Name: "my-event-gateway",
							Labels: map[string]string{
								KubernetesUIDLabelKey: string(uid),
							},
						},
					},
				},
			}, nil)

		id, err := getEventGatewayForUID(ctx, sdk, eg)
		require.NoError(t, err)
		assert.Equal(t, "found-id", id)
	})

	t.Run("not found returns empty string", func(t *testing.T) {
		sdk := mocks.NewMockEventGatewaysSDK(t)
		eg := &konnectv1alpha1.KonnectEventGateway{
			ObjectMeta: metav1.ObjectMeta{
				Name: "my-event-gateway",
				UID:  uid,
			},
			Spec: konnectv1alpha1.KonnectEventGatewaySpec{
				CreateGatewayRequest: &konnectv1alpha1.CreateEventGatewayRequest{
					Name: "my-event-gateway",
				},
			},
		}
		sdk.EXPECT().
			ListEventGateways(ctx, sdkkonnectops.ListEventGatewaysRequest{
				Filter: &sdkkonnectcomp.EventGatewayCommonFilter{
					Name: &sdkkonnectcomp.StringFieldContainsFilter{Contains: "my-event-gateway"},
				},
			}).
			Return(&sdkkonnectops.ListEventGatewaysResponse{
				ListEventGatewaysResponse: &sdkkonnectcomp.ListEventGatewaysResponse{
					Data: []sdkkonnectcomp.EventGatewayInfo{
						// Different UID. should not match.
						{
							ID:   "other-id",
							Name: "my-event-gateway",
							Labels: map[string]string{
								KubernetesUIDLabelKey: uuid.NewString(),
							},
						},
					},
				},
			}, nil)

		id, err := getEventGatewayForUID(ctx, sdk, eg)
		require.NoError(t, err)
		assert.Empty(t, id)
	})

	t.Run("nil list response returns empty string", func(t *testing.T) {
		sdk := mocks.NewMockEventGatewaysSDK(t)
		eg := &konnectv1alpha1.KonnectEventGateway{
			ObjectMeta: metav1.ObjectMeta{
				Name: "my-event-gateway",
				UID:  uid,
			},
			Spec: konnectv1alpha1.KonnectEventGatewaySpec{
				CreateGatewayRequest: &konnectv1alpha1.CreateEventGatewayRequest{
					Name: "my-event-gateway",
				},
			},
		}
		sdk.EXPECT().
			ListEventGateways(ctx, sdkkonnectops.ListEventGatewaysRequest{
				Filter: &sdkkonnectcomp.EventGatewayCommonFilter{
					Name: &sdkkonnectcomp.StringFieldContainsFilter{Contains: "my-event-gateway"},
				},
			}).
			Return(nil, nil)

		id, err := getEventGatewayForUID(ctx, sdk, eg)
		require.NoError(t, err)
		assert.Empty(t, id)
	})

	t.Run("sdk error returns error", func(t *testing.T) {
		sdk := mocks.NewMockEventGatewaysSDK(t)
		eg := &konnectv1alpha1.KonnectEventGateway{
			ObjectMeta: metav1.ObjectMeta{Name: "my-event-gateway", Namespace: "default", UID: uid},
			Spec: konnectv1alpha1.KonnectEventGatewaySpec{
				CreateGatewayRequest: &konnectv1alpha1.CreateEventGatewayRequest{
					Name: "my-event-gateway",
				},
			},
		}
		sdk.EXPECT().
			ListEventGateways(ctx, sdkkonnectops.ListEventGatewaysRequest{
				Filter: &sdkkonnectcomp.EventGatewayCommonFilter{
					Name: &sdkkonnectcomp.StringFieldContainsFilter{Contains: "my-event-gateway"},
				},
			}).
			Return(nil, &sdkkonnecterrs.BadRequestError{Status: 400, Detail: "bad request"})

		id, err := getEventGatewayForUID(ctx, sdk, eg)
		assert.Error(t, err)
		assert.Empty(t, id)
	})
}
