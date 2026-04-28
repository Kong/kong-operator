package ops

import (
	"context"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/Kong/sdk-konnect-go/test/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
)

func TestCreatePortal_UsesSDKOpsConversion(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sdk := mocks.NewMockPortalsSDK(t)
	portal := testGeneratedPortal()

	expectedRequest, err := portal.Spec.APISpec.ToCreatePortal()
	require.NoError(t, err)
	expectedRequest.Labels = WithKubernetesMetadataLabelsPtr(portal, expectedRequest.Labels)

	sdk.EXPECT().
		CreatePortal(mock.Anything, *expectedRequest).
		Return(&sdkkonnectops.CreatePortalResponse{
			PortalResponse: &sdkkonnectcomp.PortalResponse{
				ID: "portal-1",
			},
		}, nil).
		Once()

	err = createPortal(ctx, sdk, portal)
	require.NoError(t, err)
	assert.Equal(t, "portal-1", portal.GetKonnectID())
}

func TestUpdatePortal_UsesSDKOpsConversion(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sdk := mocks.NewMockPortalsSDK(t)
	portal := testGeneratedPortal()
	portal.SetKonnectID("portal-1")

	expectedRequest, err := portal.Spec.APISpec.ToUpdatePortal()
	require.NoError(t, err)
	expectedRequest.Labels = WithKubernetesMetadataLabelsPtr(portal, expectedRequest.Labels)

	sdk.EXPECT().
		UpdatePortal(mock.Anything, "portal-1", *expectedRequest).
		Return(&sdkkonnectops.UpdatePortalResponse{}, nil).
		Once()

	err = updatePortal(ctx, sdk, portal)
	require.NoError(t, err)
}

func TestDeletePortal_UsesKonnectID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sdk := mocks.NewMockPortalsSDK(t)
	portal := testGeneratedPortal()
	portal.SetKonnectID("portal-1")

	sdk.EXPECT().
		DeletePortal(mock.Anything, "portal-1", (*sdkkonnectops.DeletePortalQueryParamForce)(nil)).
		Return(&sdkkonnectops.DeletePortalResponse{}, nil).
		Once()

	err := deletePortal(ctx, sdk, portal)
	require.NoError(t, err)
}

func TestCreateIdentityProviderRequest_UsesSDKOpsConversion(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sdk := mocks.NewMockPortalAuthSettingsSDK(t)
	idp := testGeneratedIdentityProviderRequest()
	idp.SetPortalID("portal-1")

	expectedRequest, err := idp.Spec.APISpec.ToCreateIdentityProvider()
	require.NoError(t, err)

	konnectID := "idp-1"
	sdk.EXPECT().
		CreatePortalIdentityProvider(mock.Anything, "portal-1", *expectedRequest).
		Return(&sdkkonnectops.CreatePortalIdentityProviderResponse{
			IdentityProvider: &sdkkonnectcomp.IdentityProvider{
				ID: &konnectID,
			},
		}, nil).
		Once()

	err = createIdentityProviderRequest(ctx, sdk, idp)
	require.NoError(t, err)
	assert.Equal(t, "idp-1", idp.GetKonnectID())
}

func TestUpdateIdentityProviderRequest_UsesSDKOpsConversion(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sdk := mocks.NewMockPortalAuthSettingsSDK(t)
	idp := testGeneratedIdentityProviderRequest()
	idp.SetPortalID("portal-1")
	idp.SetKonnectID("idp-1")

	expectedRequest, err := idp.Spec.APISpec.ToUpdateIdentityProvider()
	require.NoError(t, err)

	sdk.EXPECT().
		UpdatePortalIdentityProvider(mock.Anything, sdkkonnectops.UpdatePortalIdentityProviderRequest{
			PortalID:               "portal-1",
			ID:                     "idp-1",
			UpdateIdentityProvider: *expectedRequest,
		}).
		Return(&sdkkonnectops.UpdatePortalIdentityProviderResponse{}, nil).
		Once()

	err = updateIdentityProviderRequest(ctx, sdk, idp)
	require.NoError(t, err)
}

func TestDeleteIdentityProviderRequest_UsesParentAndKonnectID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sdk := mocks.NewMockPortalAuthSettingsSDK(t)
	idp := testGeneratedIdentityProviderRequest()
	idp.SetPortalID("portal-1")
	idp.SetKonnectID("idp-1")

	sdk.EXPECT().
		DeletePortalIdentityProvider(mock.Anything, "portal-1", "idp-1").
		Return(&sdkkonnectops.DeletePortalIdentityProviderResponse{}, nil).
		Once()

	err := deleteIdentityProviderRequest(ctx, sdk, idp)
	require.NoError(t, err)
}

func testGeneratedPortal() *konnectv1alpha1.Portal {
	description := "Developer portal"
	return &konnectv1alpha1.Portal{
		TypeMeta: metav1.TypeMeta{
			APIVersion: konnectv1alpha1.GroupVersion.String(),
			Kind:       "Portal",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "dev-portal",
			Namespace:  "default",
			UID:        "portal-uid",
			Generation: 3,
		},
		Spec: konnectv1alpha1.PortalSpec{
			KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
				APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
					Name: "test-auth",
				},
			},
			APISpec: konnectv1alpha1.PortalAPISpec{
				AuthenticationEnabled: "Enabled",
				Description:           &description,
				DisplayName:           "Developer Portal",
				Labels: konnectv1alpha1.LabelsUpdate{
					"team": "platform",
				},
				Name: "dev-portal",
			},
		},
	}
}

func testGeneratedIdentityProviderRequest() *konnectv1alpha1.IdentityProviderRequest {
	return &konnectv1alpha1.IdentityProviderRequest{
		TypeMeta: metav1.TypeMeta{
			APIVersion: konnectv1alpha1.GroupVersion.String(),
			Kind:       "IdentityProviderRequest",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "portal-idp",
			Namespace: "default",
			UID:       "portal-idp-uid",
		},
		Spec: konnectv1alpha1.IdentityProviderRequestSpec{
			APISpec: konnectv1alpha1.IdentityProviderRequestAPISpec{
				Enabled:   konnectv1alpha1.IdentityProviderEnabledEnabled,
				LoginPath: konnectv1alpha1.IdentityProviderLoginPath("/login"),
				Type:      konnectv1alpha1.IdentityProviderType("oidc"),
			},
		},
	}
}
