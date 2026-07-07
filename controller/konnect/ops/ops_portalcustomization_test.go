package ops

import (
	"reflect"
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

func TestCreatePortalCustomization(t *testing.T) {
	ctx := t.Context()
	sdk := sdkmocks.NewMockPortalCustomizationSDK(t)
	customization := testPortalCustomization()

	expectedRequest, err := customization.Spec.APISpec.ToCreatePortalCustomization()
	require.NoError(t, err)

	sdk.EXPECT().
		ReplacePortalCustomization(
			mock.Anything,
			"portal-1",
			mock.MatchedBy(func(req *sdkkonnectcomp.PortalCustomization) bool {
				return reflect.DeepEqual(req, expectedRequest)
			}),
		).
		Return(&sdkkonnectops.ReplacePortalCustomizationResponse{
			PortalCustomization: &sdkkonnectcomp.PortalCustomization{},
		}, nil).
		Once()

	require.NoError(t, createPortalCustomization(ctx, sdk, customization))
	assert.Empty(t, customization.GetKonnectID())
}

func TestUpdatePortalCustomization(t *testing.T) {
	ctx := t.Context()
	sdk := sdkmocks.NewMockPortalCustomizationSDK(t)
	customization := testPortalCustomization()

	expectedRequest, err := customization.Spec.APISpec.ToUpdatePortalCustomization()
	require.NoError(t, err)

	sdk.EXPECT().
		ReplacePortalCustomization(
			mock.Anything,
			"portal-1",
			mock.MatchedBy(func(req *sdkkonnectcomp.PortalCustomization) bool {
				return reflect.DeepEqual(req, expectedRequest)
			}),
		).
		Return(&sdkkonnectops.ReplacePortalCustomizationResponse{}, nil).
		Once()

	require.NoError(t, updatePortalCustomization(ctx, sdk, customization))
}

func TestDeletePortalCustomization(t *testing.T) {
	ctx := t.Context()
	sdk := sdkmocks.NewMockPortalCustomizationSDK(t)
	customization := testPortalCustomization()

	sdk.EXPECT().
		ReplacePortalCustomization(
			mock.Anything,
			"portal-1",
			mock.MatchedBy(func(req *sdkkonnectcomp.PortalCustomization) bool {
				return reflect.DeepEqual(req, &sdkkonnectcomp.PortalCustomization{})
			}),
		).
		Return(&sdkkonnectops.ReplacePortalCustomizationResponse{}, nil).
		Once()

	require.NoError(t, deletePortalCustomization(ctx, sdk, customization))
}

func TestPortalCustomizationPersistsKonnectID(t *testing.T) {
	t.Parallel()

	assert.False(t, EntityPersistsKonnectID(&konnectv1alpha1.PortalCustomization{}))
	assert.True(t, EntityPersistsKonnectID(&konnectv1alpha1.Portal{}))
}

func TestGetPortalCustomizationForUID(t *testing.T) {
	ctx := t.Context()
	sdk := sdkmocks.NewMockPortalCustomizationSDK(t)
	customization := testPortalCustomization()

	id, err := getPortalCustomizationForUID(ctx, sdk, customization)
	require.Empty(t, id)

	var notFoundErr EntityWithMatchingUIDNotFoundError
	require.ErrorAs(t, err, &notFoundErr)
}

func testPortalCustomization() *konnectv1alpha1.PortalCustomization {
	return &konnectv1alpha1.PortalCustomization{
		TypeMeta: metav1.TypeMeta{
			APIVersion: konnectv1alpha1.GroupVersion.String(),
			Kind:       "PortalCustomization",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "portal-customization",
			Namespace:  "default",
			UID:        "portal-customization-uid",
			Generation: 2,
		},
		Spec: konnectv1alpha1.PortalCustomizationSpec{
			PortalRef: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: "portal",
				},
			},
			APISpec: konnectv1alpha1.PortalCustomizationAPISpec{
				Css:    new("body { background-color: #f0f0f0; }"),
				Layout: "single-column",
				Robots: new("User-agent: *\nAllow: /"),
			},
		},
		Status: konnectv1alpha1.PortalCustomizationStatus{
			PortalID: &konnectv1alpha1.KonnectEntityRef{
				ID: "portal-1",
			},
		},
	}
}
