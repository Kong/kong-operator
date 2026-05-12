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

func TestCreatePortalEmailConfig(t *testing.T) {
	ctx := t.Context()
	sdk := sdkmocks.NewMockPortalEmailsSDK(t)
	emailConfig := testPortalEmailConfig()

	expectedRequest, err := emailConfig.Spec.APISpec.ToPostPortalEmailConfig()
	require.NoError(t, err)

	sdk.EXPECT().
		CreatePortalEmailConfig(mock.Anything, "portal-1", *expectedRequest).
		Return(&sdkkonnectops.CreatePortalEmailConfigResponse{
			PortalEmailConfig: &sdkkonnectcomp.PortalEmailConfig{
				ID: "portal-email-config-1",
			},
		}, nil).
		Once()

	require.NoError(t, createPortalEmailConfig(ctx, sdk, emailConfig))
	assert.Equal(t, "portal-email-config-1", emailConfig.GetKonnectID())
}

func TestUpdatePortalEmailConfig(t *testing.T) {
	ctx := t.Context()
	sdk := sdkmocks.NewMockPortalEmailsSDK(t)
	emailConfig := testPortalEmailConfig()
	emailConfig.SetKonnectID("portal-email-config-1")

	expectedRequest, err := emailConfig.Spec.APISpec.ToPatchPortalEmailConfig()
	require.NoError(t, err)

	sdk.EXPECT().
		UpdatePortalEmailConfig(mock.Anything, "portal-1", expectedRequest).
		Return(&sdkkonnectops.UpdatePortalEmailConfigResponse{}, nil).
		Once()

	require.NoError(t, updatePortalEmailConfig(ctx, sdk, emailConfig))
}

func TestDeletePortalEmailConfig(t *testing.T) {
	ctx := t.Context()
	sdk := sdkmocks.NewMockPortalEmailsSDK(t)
	emailConfig := testPortalEmailConfig()
	emailConfig.SetKonnectID("portal-email-config-1")

	sdk.EXPECT().
		DeletePortalEmailConfig(mock.Anything, "portal-1").
		Return(&sdkkonnectops.DeletePortalEmailConfigResponse{}, nil).
		Once()

	require.NoError(t, deletePortalEmailConfig(ctx, sdk, emailConfig))
}

func TestGetPortalEmailConfigForUID(t *testing.T) {
	ctx := t.Context()
	sdk := sdkmocks.NewMockPortalEmailsSDK(t)
	emailConfig := testPortalEmailConfig()

	id, err := getPortalEmailConfigForUID(ctx, sdk, emailConfig)
	require.Empty(t, id)

	var notFoundErr EntityWithMatchingUIDNotFoundError
	require.ErrorAs(t, err, &notFoundErr)
}

func testPortalEmailConfig() *konnectv1alpha1.PortalEmailConfig {
	return &konnectv1alpha1.PortalEmailConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: konnectv1alpha1.GroupVersion.String(),
			Kind:       "PortalEmailConfig",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "portal-email-config",
			Namespace:  "default",
			UID:        "portal-email-config-uid",
			Generation: 2,
		},
		Spec: konnectv1alpha1.PortalEmailConfigSpec{
			PortalRef: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: "portal",
				},
			},
			APISpec: konnectv1alpha1.PortalEmailConfigAPISpec{
				DomainName:   new("example.com"),
				FromEmail:    new("noreply@example.com"),
				FromName:     new("Example Developer Portal"),
				ReplyToEmail: new("support@example.com"),
			},
		},
		Status: konnectv1alpha1.PortalEmailConfigStatus{
			PortalID: &konnectv1alpha1.KonnectEntityRef{
				ID: "portal-1",
			},
		},
	}
}
