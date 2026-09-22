package ops

import (
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkmocks "github.com/Kong/sdk-konnect-go/test/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
)

func testPortalIdentityProviderRequestOIDC() *konnectv1alpha1.PortalIdentityProviderRequest {
	return &konnectv1alpha1.PortalIdentityProviderRequest{
		APIVersion: konnectv1alpha1.GroupVersion.String(),
		Kind:       "PortalIdentityProviderRequest",
		Name:       "idp",
		Namespace:  "default",
		UID:        "idp-uid",
		Spec: konnectv1alpha1.PortalIdentityProviderRequestSpec{
			APISpec: konnectv1alpha1.PortalIdentityProviderRequestAPISpec{
				Type: "oidc",
				Config: &konnectv1alpha1.PortalIdentityProviderRequestConfig{
					Type: konnectv1alpha1.PortalIdentityProviderRequestConfigTypeOIDC,
					OIDC: &konnectv1alpha1.OIDCIdentityProviderConfig{
						IssuerURL: "https://idp.example.com",
						ClientID:  "client-1",
					},
				},
			},
		},
	}
}

func TestGetPortalIdentityProviderRequestForUID(t *testing.T) {
	t.Run("matches OIDC provider by issuer URL and client ID", func(t *testing.T) {
		ctx := t.Context()
		sdk := sdkmocks.NewMockPortalAuthSettingsSDK(t)
		obj := testPortalIdentityProviderRequestOIDC()
		obj.SetPortalID("portal-1")

		sdk.EXPECT().
			GetPortalIdentityProviders(mock.Anything, "portal-1", (*sdkkonnectops.GetPortalIdentityProvidersQueryParamFilter)(nil)).
			Return(&sdkkonnectops.GetPortalIdentityProvidersResponse{
				PortalIdentityProviders: []sdkkonnectcomp.PortalIdentityProvider{
					{
						ID:   new("idp-other"),
						Type: sdkkonnectcomp.IdentityProviderTypeOidc.ToPointer(),
						Config: &sdkkonnectcomp.PortalIdentityProviderConfig{
							OIDCIdentityProviderConfigOutput: &sdkkonnectcomp.OIDCIdentityProviderConfigOutput{
								IssuerURL: "https://other.example.com",
								ClientID:  "client-2",
							},
						},
					},
					{
						ID:   new("idp-1"),
						Type: sdkkonnectcomp.IdentityProviderTypeOidc.ToPointer(),
						Config: &sdkkonnectcomp.PortalIdentityProviderConfig{
							OIDCIdentityProviderConfigOutput: &sdkkonnectcomp.OIDCIdentityProviderConfigOutput{
								IssuerURL: "https://idp.example.com",
								ClientID:  "client-1",
							},
						},
					},
				},
			}, nil).
			Once()

		id, err := getPortalIdentityProviderRequestForUID(ctx, sdk, obj)
		require.NoError(t, err)
		assert.Equal(t, "idp-1", id)
	})

	t.Run("skips entries of a different provider type", func(t *testing.T) {
		ctx := t.Context()
		sdk := sdkmocks.NewMockPortalAuthSettingsSDK(t)
		obj := testPortalIdentityProviderRequestOIDC()
		obj.SetPortalID("portal-1")

		sdk.EXPECT().
			GetPortalIdentityProviders(mock.Anything, "portal-1", (*sdkkonnectops.GetPortalIdentityProvidersQueryParamFilter)(nil)).
			Return(&sdkkonnectops.GetPortalIdentityProvidersResponse{
				PortalIdentityProviders: []sdkkonnectcomp.PortalIdentityProvider{
					{
						ID:   new("idp-saml"),
						Type: sdkkonnectcomp.IdentityProviderTypeSaml.ToPointer(),
						Config: &sdkkonnectcomp.PortalIdentityProviderConfig{
							PortalSAMLIdentityProviderConfig: &sdkkonnectcomp.PortalSAMLIdentityProviderConfig{
								IdpMetadataURL: new("https://idp.example.com/metadata"),
							},
						},
					},
				},
			}, nil).
			Once()

		id, err := getPortalIdentityProviderRequestForUID(ctx, sdk, obj)
		require.Empty(t, id)

		var notFoundErr EntityWithMatchingUIDNotFoundError
		require.ErrorAs(t, err, &notFoundErr)
	})

	t.Run("skips entries with nil config or nil variant", func(t *testing.T) {
		ctx := t.Context()
		sdk := sdkmocks.NewMockPortalAuthSettingsSDK(t)
		obj := testPortalIdentityProviderRequestOIDC()
		obj.SetPortalID("portal-1")

		sdk.EXPECT().
			GetPortalIdentityProviders(mock.Anything, "portal-1", (*sdkkonnectops.GetPortalIdentityProvidersQueryParamFilter)(nil)).
			Return(&sdkkonnectops.GetPortalIdentityProvidersResponse{
				PortalIdentityProviders: []sdkkonnectcomp.PortalIdentityProvider{
					{
						ID:   new("idp-no-config"),
						Type: sdkkonnectcomp.IdentityProviderTypeOidc.ToPointer(),
					},
					{
						ID:     new("idp-wrong-variant"),
						Type:   sdkkonnectcomp.IdentityProviderTypeOidc.ToPointer(),
						Config: &sdkkonnectcomp.PortalIdentityProviderConfig{},
					},
				},
			}, nil).
			Once()

		id, err := getPortalIdentityProviderRequestForUID(ctx, sdk, obj)
		require.Empty(t, id)

		var notFoundErr EntityWithMatchingUIDNotFoundError
		require.ErrorAs(t, err, &notFoundErr)
	})

	t.Run("matches SAML provider by metadata URL and XML", func(t *testing.T) {
		ctx := t.Context()
		sdk := sdkmocks.NewMockPortalAuthSettingsSDK(t)
		obj := testPortalIdentityProviderRequestOIDC()
		obj.SetPortalID("portal-1")
		obj.Spec.APISpec.Type = "saml"
		obj.Spec.APISpec.Config = &konnectv1alpha1.PortalIdentityProviderRequestConfig{
			Type: konnectv1alpha1.PortalIdentityProviderRequestConfigTypePortalSAML,
			PortalSAML: &konnectv1alpha1.PortalSAMLIdentityProviderConfig{
				IdpMetadataURL: "https://idp.example.com/metadata",
			},
		}

		sdk.EXPECT().
			GetPortalIdentityProviders(mock.Anything, "portal-1", (*sdkkonnectops.GetPortalIdentityProvidersQueryParamFilter)(nil)).
			Return(&sdkkonnectops.GetPortalIdentityProvidersResponse{
				PortalIdentityProviders: []sdkkonnectcomp.PortalIdentityProvider{
					{
						ID:   new("idp-saml-1"),
						Type: sdkkonnectcomp.IdentityProviderTypeSaml.ToPointer(),
						Config: &sdkkonnectcomp.PortalIdentityProviderConfig{
							PortalSAMLIdentityProviderConfig: &sdkkonnectcomp.PortalSAMLIdentityProviderConfig{
								IdpMetadataURL: new("https://idp.example.com/metadata"),
							},
						},
					},
				},
			}, nil).
			Once()

		id, err := getPortalIdentityProviderRequestForUID(ctx, sdk, obj)
		require.NoError(t, err)
		assert.Equal(t, "idp-saml-1", id)
	})

	t.Run("matches SAML provider when the remote entry stores metadata XML the spec omits", func(t *testing.T) {
		ctx := t.Context()
		sdk := sdkmocks.NewMockPortalAuthSettingsSDK(t)
		obj := testPortalIdentityProviderRequestOIDC()
		obj.SetPortalID("portal-1")
		obj.Spec.APISpec.Type = "saml"
		obj.Spec.APISpec.Config = &konnectv1alpha1.PortalIdentityProviderRequestConfig{
			Type: konnectv1alpha1.PortalIdentityProviderRequestConfigTypePortalSAML,
			PortalSAML: &konnectv1alpha1.PortalSAMLIdentityProviderConfig{
				IdpMetadataURL: "https://idp.example.com/metadata",
			},
		}

		sdk.EXPECT().
			GetPortalIdentityProviders(mock.Anything, "portal-1", (*sdkkonnectops.GetPortalIdentityProvidersQueryParamFilter)(nil)).
			Return(&sdkkonnectops.GetPortalIdentityProvidersResponse{
				PortalIdentityProviders: []sdkkonnectcomp.PortalIdentityProvider{
					{
						ID:   new("idp-saml-1"),
						Type: sdkkonnectcomp.IdentityProviderTypeSaml.ToPointer(),
						Config: &sdkkonnectcomp.PortalIdentityProviderConfig{
							PortalSAMLIdentityProviderConfig: &sdkkonnectcomp.PortalSAMLIdentityProviderConfig{
								IdpMetadataURL: new("https://idp.example.com/metadata"),
								// Konnect resolved the metadata from the URL and echoes
								// it back even though the spec never set it.
								IdpMetadataXML: new("<EntityDescriptor/>"),
							},
						},
					},
				},
			}, nil).
			Once()

		id, err := getPortalIdentityProviderRequestForUID(ctx, sdk, obj)
		require.NoError(t, err)
		assert.Equal(t, "idp-saml-1", id)
	})

	t.Run("does not match a SAML provider with a different metadata XML", func(t *testing.T) {
		ctx := t.Context()
		sdk := sdkmocks.NewMockPortalAuthSettingsSDK(t)
		obj := testPortalIdentityProviderRequestOIDC()
		obj.SetPortalID("portal-1")
		obj.Spec.APISpec.Type = "saml"
		obj.Spec.APISpec.Config = &konnectv1alpha1.PortalIdentityProviderRequestConfig{
			Type: konnectv1alpha1.PortalIdentityProviderRequestConfigTypePortalSAML,
			PortalSAML: &konnectv1alpha1.PortalSAMLIdentityProviderConfig{
				IdpMetadataXML: "<EntityDescriptor id=\"ours\"/>",
			},
		}

		sdk.EXPECT().
			GetPortalIdentityProviders(mock.Anything, "portal-1", (*sdkkonnectops.GetPortalIdentityProvidersQueryParamFilter)(nil)).
			Return(&sdkkonnectops.GetPortalIdentityProvidersResponse{
				PortalIdentityProviders: []sdkkonnectcomp.PortalIdentityProvider{
					{
						ID:   new("idp-saml-other"),
						Type: sdkkonnectcomp.IdentityProviderTypeSaml.ToPointer(),
						Config: &sdkkonnectcomp.PortalIdentityProviderConfig{
							PortalSAMLIdentityProviderConfig: &sdkkonnectcomp.PortalSAMLIdentityProviderConfig{
								IdpMetadataXML: new("<EntityDescriptor id=\"theirs\"/>"),
							},
						},
					},
				},
			}, nil).
			Once()

		id, err := getPortalIdentityProviderRequestForUID(ctx, sdk, obj)
		require.Empty(t, id)

		var notFoundErr EntityWithMatchingUIDNotFoundError
		require.ErrorAs(t, err, &notFoundErr)
	})

	t.Run("returns not found when the SAML variant sets neither metadata field", func(t *testing.T) {
		ctx := t.Context()
		sdk := sdkmocks.NewMockPortalAuthSettingsSDK(t)
		obj := testPortalIdentityProviderRequestOIDC()
		obj.SetPortalID("portal-1")
		obj.Spec.APISpec.Type = "saml"
		obj.Spec.APISpec.Config = &konnectv1alpha1.PortalIdentityProviderRequestConfig{
			Type:       konnectv1alpha1.PortalIdentityProviderRequestConfigTypePortalSAML,
			PortalSAML: &konnectv1alpha1.PortalSAMLIdentityProviderConfig{},
		}

		sdk.EXPECT().
			GetPortalIdentityProviders(mock.Anything, "portal-1", (*sdkkonnectops.GetPortalIdentityProvidersQueryParamFilter)(nil)).
			Return(&sdkkonnectops.GetPortalIdentityProvidersResponse{
				PortalIdentityProviders: []sdkkonnectcomp.PortalIdentityProvider{
					{
						ID:   new("idp-saml-unrelated"),
						Type: sdkkonnectcomp.IdentityProviderTypeSaml.ToPointer(),
						Config: &sdkkonnectcomp.PortalIdentityProviderConfig{
							PortalSAMLIdentityProviderConfig: &sdkkonnectcomp.PortalSAMLIdentityProviderConfig{
								IdpMetadataURL: new("https://someone-else.example.com/metadata"),
							},
						},
					},
				},
			}, nil).
			Once()

		// Both SAML fields are optional, so with nothing set there is no
		// evidence the remote entry belongs to this object: it must not be
		// adopted.
		id, err := getPortalIdentityProviderRequestForUID(ctx, sdk, obj)
		require.Empty(t, id)

		var notFoundErr EntityWithMatchingUIDNotFoundError
		require.ErrorAs(t, err, &notFoundErr)
	})

	t.Run("returns not found when the spec has no config", func(t *testing.T) {
		ctx := t.Context()
		sdk := sdkmocks.NewMockPortalAuthSettingsSDK(t)
		obj := testPortalIdentityProviderRequestOIDC()
		obj.SetPortalID("portal-1")
		obj.Spec.APISpec.Config = nil

		sdk.EXPECT().
			GetPortalIdentityProviders(mock.Anything, "portal-1", (*sdkkonnectops.GetPortalIdentityProvidersQueryParamFilter)(nil)).
			Return(&sdkkonnectops.GetPortalIdentityProvidersResponse{}, nil).
			Once()

		id, err := getPortalIdentityProviderRequestForUID(ctx, sdk, obj)
		require.Empty(t, id)

		var notFoundErr EntityWithMatchingUIDNotFoundError
		require.ErrorAs(t, err, &notFoundErr)
	})

	t.Run("returns error when parent ID is missing", func(t *testing.T) {
		ctx := t.Context()
		sdk := sdkmocks.NewMockPortalAuthSettingsSDK(t)
		obj := testPortalIdentityProviderRequestOIDC()

		id, err := getPortalIdentityProviderRequestForUID(ctx, sdk, obj)
		require.Empty(t, id)

		var parentErr CantPerformOperationWithoutParentIDError
		require.ErrorAs(t, err, &parentErr)
	})

	t.Run("returns error on nil response", func(t *testing.T) {
		ctx := t.Context()
		sdk := sdkmocks.NewMockPortalAuthSettingsSDK(t)
		obj := testPortalIdentityProviderRequestOIDC()
		obj.SetPortalID("portal-1")

		sdk.EXPECT().
			GetPortalIdentityProviders(mock.Anything, "portal-1", (*sdkkonnectops.GetPortalIdentityProvidersQueryParamFilter)(nil)).
			Return(nil, nil).
			Once()

		id, err := getPortalIdentityProviderRequestForUID(ctx, sdk, obj)
		require.Empty(t, id)
		require.ErrorIs(t, err, ErrNilResponse)
	})
}
