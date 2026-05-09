package ops

import (
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkmocks "github.com/Kong/sdk-konnect-go/test/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
)

func TestCreatePortalPage(t *testing.T) {
	ctx := t.Context()
	sdk := sdkmocks.NewMockPortalPagesSDK(t)
	page := testPortalPage()

	expectedRequest, err := page.Spec.APISpec.ToCreatePortalPageRequest()
	require.NoError(t, err)

	sdk.EXPECT().
		CreatePortalPage(mock.Anything, "portal-1", *expectedRequest).
		Return(&sdkkonnectops.CreatePortalPageResponse{
			PortalPageResponse: &sdkkonnectcomp.PortalPageResponse{
				ID: "page-1",
			},
		}, nil).
		Once()

	require.NoError(t, createPortalPage(ctx, sdk, page))
	assert.Equal(t, "page-1", page.GetKonnectID())
}

func TestUpdatePortalPage(t *testing.T) {
	ctx := t.Context()
	sdk := sdkmocks.NewMockPortalPagesSDK(t)
	page := testPortalPage()
	page.SetKonnectID("page-1")

	expectedRequest, err := page.Spec.APISpec.ToUpdatePortalPageRequest()
	require.NoError(t, err)

	sdk.EXPECT().
		UpdatePortalPage(mock.Anything, sdkkonnectops.UpdatePortalPageRequest{
			PortalID:                "portal-1",
			PageID:                  "page-1",
			UpdatePortalPageRequest: *expectedRequest,
		}).
		Return(&sdkkonnectops.UpdatePortalPageResponse{}, nil).
		Once()

	require.NoError(t, updatePortalPage(ctx, sdk, page))
}

func TestDeletePortalPage(t *testing.T) {
	ctx := t.Context()
	sdk := sdkmocks.NewMockPortalPagesSDK(t)
	page := testPortalPage()
	page.SetKonnectID("page-1")

	sdk.EXPECT().
		DeletePortalPage(mock.Anything, "portal-1", "page-1").
		Return(&sdkkonnectops.DeletePortalPageResponse{}, nil).
		Once()

	require.NoError(t, deletePortalPage(ctx, sdk, page))
}

func TestGetPortalPageForUID(t *testing.T) {
	ctx := t.Context()
	sdk := sdkmocks.NewMockPortalPagesSDK(t)
	page := testPortalPage()

	id, err := getPortalPageForUID(ctx, sdk, page)
	require.Empty(t, id)

	var notFoundErr EntityWithMatchingUIDNotFoundError
	require.ErrorAs(t, err, &notFoundErr)
}

func testPortalPage() *konnectv1alpha1.PortalPage {
	return &konnectv1alpha1.PortalPage{
		TypeMeta: metav1.TypeMeta{
			APIVersion: konnectv1alpha1.GroupVersion.String(),
			Kind:       "PortalPage",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "portal-page",
			Namespace:  "default",
			UID:        "portal-page-uid",
			Generation: 2,
		},
		Spec: konnectv1alpha1.PortalPageSpec{
			PortalRef: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: "portal",
				},
			},
			APISpec: konnectv1alpha1.PortalPageAPISpec{
				Content:     konnectv1alpha1.PageContent("# docs"),
				Description: konnectv1alpha1.Description("Portal page description"),
				Slug:        konnectv1alpha1.PageSlug("docs"),
				Status:      konnectv1alpha1.PublishedStatus("published"),
				Title:       konnectv1alpha1.PageTitle("Documentation"),
				Visibility:  konnectv1alpha1.PageVisibilityStatus("public"),
			},
		},
		Status: konnectv1alpha1.PortalPageStatus{
			PortalID: &konnectv1alpha1.KonnectEntityRef{
				ID: "portal-1",
			},
		},
	}
}
