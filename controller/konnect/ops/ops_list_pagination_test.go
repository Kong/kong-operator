package ops

import (
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/Kong/sdk-konnect-go/test/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

// TestSetListPageSize is not parallel: it changes the page size the other
// tests' lookups request, and restores it.
func TestSetListPageSize(t *testing.T) {
	original := listPageSize
	t.Cleanup(func() { listPageSize = original })

	for _, size := range []int64{0, -1, consts.MaxKonnectListPageSize + 1} {
		require.Error(t, SetListPageSize(size), "size %d", size)
		assert.Equal(t, original, listPageSize, "an invalid size must not change the page size")
	}

	require.NoError(t, SetListPageSize(20))
	assert.Equal(t, int64(20), listPageSize)
	require.NoError(t, SetListPageSize(consts.MaxKonnectListPageSize))
	assert.Equal(t, int64(consts.MaxKonnectListPageSize), listPageSize)
}

func TestNextPageCursor(t *testing.T) {
	testCases := []struct {
		name           string
		next           *string
		seenCursors    map[string]struct{}
		expectedCursor *string
		expectedErr    bool
	}{
		{
			name: "no next page",
		},
		{
			name: "empty next page",
			next: new(""),
		},
		{
			name:           "next page",
			next:           new("/v1/event-gateways?page%5Bafter%5D=cursor-2&page%5Bsize%5D=100"),
			seenCursors:    map[string]struct{}{"cursor-1": {}},
			expectedCursor: new("cursor-2"),
		},
		{
			name:        "next page cursor already requested",
			next:        new("/v1/event-gateways?page%5Bafter%5D=cursor-1"),
			seenCursors: map[string]struct{}{"cursor-1": {}},
			expectedErr: true,
		},
		{
			name:        "next page without a cursor",
			next:        new("/v1/event-gateways?page%5Bsize%5D=100"),
			expectedErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			seenCursors := tc.seenCursors
			if seenCursors == nil {
				seenCursors = map[string]struct{}{}
			}
			cursor, err := nextPageCursor(tc.next, seenCursors)
			if tc.expectedErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expectedCursor, cursor)
			if cursor != nil {
				assert.Contains(t, seenCursors, *cursor)
			}
		})
	}
}

func TestHasNextNumberedPage(t *testing.T) {
	testCases := []struct {
		name       string
		pageNumber int64
		page       sdkkonnectcomp.PageMeta
		items      int
		expected   bool
	}{
		{
			name:       "more items after the page",
			pageNumber: 1,
			page:       sdkkonnectcomp.PageMeta{Number: 1, Size: 100, Total: 101},
			items:      100,
			expected:   true,
		},
		{
			name:       "last full page",
			pageNumber: 2,
			page:       sdkkonnectcomp.PageMeta{Number: 2, Size: 100, Total: 200},
			items:      100,
		},
		{
			name:       "last partial page",
			pageNumber: 1,
			page:       sdkkonnectcomp.PageMeta{Number: 1, Size: 100, Total: 42},
			items:      42,
		},
		{
			name:       "empty page, although the total says otherwise",
			pageNumber: 3,
			page:       sdkkonnectcomp.PageMeta{Number: 3, Size: 100, Total: 1000},
		},
		{
			name:       "no page size",
			pageNumber: 1,
			page:       sdkkonnectcomp.PageMeta{Number: 1, Total: 1000},
			items:      10,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, hasNextNumberedPage(tc.pageNumber, tc.page, tc.items))
		})
	}
}

// TestGetForUIDPagesThroughList covers lookups finding (or not) an entity
// listed after the first page, for each pagination style.
func TestGetForUIDPagesThroughList(t *testing.T) {
	const uid = "event-control-plane-uid"
	eventGateways := func(next *string, gateways ...sdkkonnectcomp.EventGatewayInfo) *sdkkonnectops.ListEventGatewaysResponse {
		return &sdkkonnectops.ListEventGatewaysResponse{
			ListEventGatewaysResponse: &sdkkonnectcomp.ListEventGatewaysResponse{
				Data: gateways,
				Meta: sdkkonnectcomp.CursorMeta{Page: sdkkonnectcomp.CursorMetaPage{Next: next}},
			},
		}
	}
	eventGateway := func(id, uid string) sdkkonnectcomp.EventGatewayInfo {
		return sdkkonnectcomp.EventGatewayInfo{ID: id, Labels: map[string]string{KubernetesUIDLabelKey: uid}}
	}
	listEventGateways := func(sdk *mocks.MockEventGatewaysSDK, after *string) *mocks.MockEventGatewaysSDK_ListEventGateways_Call {
		return sdk.EXPECT().ListEventGateways(mock.Anything, sdkkonnectops.ListEventGatewaysRequest{
			PageSize:  new(listPageSize),
			PageAfter: after,
		})
	}

	t.Run("cursor: entity on a later page is found", func(t *testing.T) {
		sdk := mocks.NewMockEventGatewaysSDK(t)
		listEventGateways(sdk, nil).
			Return(eventGateways(new("/v1/event-gateways?page%5Bafter%5D=cursor-1"), eventGateway("gateway-other", "other-uid")), nil).
			Once()
		listEventGateways(sdk, new("cursor-1")).
			Return(eventGateways(nil, eventGateway("gateway-1", uid)), nil).
			Once()

		id, err := getKonnectEventGatewayForUID(t.Context(), sdk, &konnectv1alpha1.KonnectEventGateway{UID: uid})
		require.NoError(t, err)
		assert.Equal(t, "gateway-1", id)
	})

	t.Run("cursor: not found after the last page", func(t *testing.T) {
		sdk := mocks.NewMockEventGatewaysSDK(t)
		listEventGateways(sdk, nil).
			Return(eventGateways(new("/v1/event-gateways?page%5Bafter%5D=cursor-1"), eventGateway("gateway-other", "other-uid")), nil).
			Once()
		listEventGateways(sdk, new("cursor-1")).
			Return(eventGateways(nil, eventGateway("gateway-other-2", "other-uid-2")), nil).
			Once()

		_, err := getKonnectEventGatewayForUID(t.Context(), sdk, &konnectv1alpha1.KonnectEventGateway{UID: uid})
		var notFound EntityWithMatchingUIDNotFoundError
		require.ErrorAs(t, err, &notFound)
	})

	t.Run("cursor: a repeated next page cursor is an error", func(t *testing.T) {
		sdk := mocks.NewMockEventGatewaysSDK(t)
		listEventGateways(sdk, nil).
			Return(eventGateways(new("/v1/event-gateways?page%5Bafter%5D=cursor-1")), nil).
			Once()
		listEventGateways(sdk, new("cursor-1")).
			Return(eventGateways(new("/v1/event-gateways?page%5Bafter%5D=cursor-1")), nil).
			Once()

		_, err := getKonnectEventGatewayForUID(t.Context(), sdk, &konnectv1alpha1.KonnectEventGateway{UID: uid})
		require.ErrorContains(t, err, "repeated")
		var notFound EntityWithMatchingUIDNotFoundError
		require.NotErrorAs(t, err, &notFound)
	})

	t.Run("page number: entity on a later page is found", func(t *testing.T) {
		const aiUID = "ai-gateway-uid"
		aiGateways := func(pageNumber float64, gateways ...sdkkonnectcomp.AIGateway) *sdkkonnectops.ListAiGatewaysResponse {
			return &sdkkonnectops.ListAiGatewaysResponse{
				ListAIGatewaysResponse: &sdkkonnectcomp.ListAIGatewaysResponse{
					Data: gateways,
					Meta: sdkkonnectcomp.PaginatedMeta{Page: sdkkonnectcomp.PageMeta{Number: pageNumber, Size: 1, Total: 2}},
				},
			}
		}
		sdk := mocks.NewMockAIGatewaysSDK(t)
		sdk.EXPECT().ListAiGateways(mock.Anything, new(listPageSize), new(int64(1))).
			Return(aiGateways(1, sdkkonnectcomp.AIGateway{ID: "ai-gateway-other", Labels: map[string]string{KubernetesUIDLabelKey: "other-uid"}}), nil).
			Once()
		sdk.EXPECT().ListAiGateways(mock.Anything, new(listPageSize), new(int64(2))).
			Return(aiGateways(2, sdkkonnectcomp.AIGateway{ID: "ai-gateway-1", Labels: map[string]string{KubernetesUIDLabelKey: aiUID}}), nil).
			Once()

		id, err := getKonnectAIGatewayForUID(t.Context(), sdk, &konnectv1alpha1.KonnectAIGateway{UID: aiUID})
		require.NoError(t, err)
		assert.Equal(t, "ai-gateway-1", id)
	})

	t.Run("Event Gateway data plane certificate on a later page is found", func(t *testing.T) {
		obj := testEventGatewayDataPlaneCertificate()
		certificates := func(next *string, certs ...sdkkonnectcomp.EventGatewayDataPlaneCertificate) *sdkkonnectops.ListEventGatewayDataPlaneCertificatesResponse {
			return &sdkkonnectops.ListEventGatewayDataPlaneCertificatesResponse{
				ListEventGatewayDataPlaneCertificatesResponse: &sdkkonnectcomp.ListEventGatewayDataPlaneCertificatesResponse{
					Data: certs,
					Meta: &sdkkonnectcomp.CursorMeta{Page: sdkkonnectcomp.CursorMetaPage{Next: next}},
				},
			}
		}
		sdk := mocks.NewMockEventGatewayDataPlaneCertificatesSDK(t)
		sdk.On("ListEventGatewayDataPlaneCertificates", mock.Anything, sdkkonnectops.ListEventGatewayDataPlaneCertificatesRequest{
			GatewayID: "gateway-1",
			PageSize:  new(listPageSize),
		}).
			Return(certificates(new("/v1/event-gateways/gateway-1/data-plane-certificates?page%5Bafter%5D=cursor-1"),
				sdkkonnectcomp.EventGatewayDataPlaneCertificate{ID: "cert-other", Certificate: "other-cert"},
			), nil).
			Once()
		sdk.On("ListEventGatewayDataPlaneCertificates", mock.Anything, sdkkonnectops.ListEventGatewayDataPlaneCertificatesRequest{
			GatewayID: "gateway-1",
			PageSize:  new(listPageSize),
			PageAfter: new("cursor-1"),
		}).
			Return(certificates(nil, sdkkonnectcomp.EventGatewayDataPlaneCertificate{
				ID:          "cert-1",
				Certificate: *obj.Spec.APISpec.Certificate.Value,
				Name:        new(obj.Spec.APISpec.Name),
				Description: new(obj.Spec.APISpec.Description),
			}), nil).
			Once()

		cl := fake.NewClientBuilder().WithScheme(scheme.Get()).Build()
		id, err := getEventGatewayDataPlaneCertificateForUID(t.Context(), sdk, cl, obj)
		require.NoError(t, err)
		assert.Equal(t, "cert-1", id)
	})

	t.Run("legacy AI Gateway data plane certificate on a later page is found", func(t *testing.T) {
		const certPEM = "-----BEGIN CERTIFICATE-----\nowned\n-----END CERTIFICATE-----"
		obj := &aiconfigurationv1alpha1.AIGatewayDataPlaneCertificate{
			Name:      "dp-cert",
			Namespace: "default",
			UID:       "dp-cert-uid",
			Status: aiconfigurationv1alpha1.AIGatewayDataPlaneCertificateStatus{
				GatewayID: &aiconfigurationv1alpha1.KonnectEntityRef{ID: "gateway-1"},
			},
		}
		obj.Spec.APISpec.Title = "dp-cert"
		obj.Spec.APISpec.Cert = aiconfigurationv1alpha1.SensitiveDataSource{
			Type:  aiconfigurationv1alpha1.SensitiveDataSourceTypeInline,
			Value: new(certPEM),
		}
		certificates := func(next *string, certs ...sdkkonnectcomp.AIGatewayDataPlaneClientCertificate) *sdkkonnectops.ListAiGatewayDataPlaneCertificatesResponse {
			return &sdkkonnectops.ListAiGatewayDataPlaneCertificatesResponse{
				ListAIGatewayDataPlaneCertificatesResponse: &sdkkonnectcomp.ListAIGatewayDataPlaneCertificatesResponse{
					Data: certs,
					Meta: sdkkonnectcomp.CursorMeta{Page: sdkkonnectcomp.CursorMetaPage{Next: next}},
				},
			}
		}
		sdk := mocks.NewMockAIGatewayDataPlaneCertificatesSDK(t)
		// AI Gateway sub-collections return the bare cursor as the next page.
		sdk.EXPECT().ListAiGatewayDataPlaneCertificates(mock.Anything, sdkkonnectops.ListAiGatewayDataPlaneCertificatesRequest{
			GatewayID: "gateway-1",
			PageSize:  new(listPageSize),
		}).
			Return(certificates(new("cursor-1"),
				sdkkonnectcomp.AIGatewayDataPlaneClientCertificate{ID: "cert-other", Title: "dp-cert", Cert: "other-cert"},
			), nil).
			Once()
		sdk.EXPECT().ListAiGatewayDataPlaneCertificates(mock.Anything, sdkkonnectops.ListAiGatewayDataPlaneCertificatesRequest{
			GatewayID: "gateway-1",
			PageSize:  new(listPageSize),
			PageAfter: new("cursor-1"),
		}).
			Return(certificates(nil,
				sdkkonnectcomp.AIGatewayDataPlaneClientCertificate{ID: "cert-1", Title: "dp-cert", Cert: certPEM},
			), nil).
			Once()

		cl := fake.NewClientBuilder().WithScheme(scheme.Get()).Build()
		id, err := getLegacyAIGatewayDataPlaneCertificateForUID(t.Context(), sdk, cl, obj)
		require.NoError(t, err)
		assert.Equal(t, "cert-1", id)
	})
}
