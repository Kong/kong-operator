package v1alpha1

import (
	"encoding/json"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPortalIdentityProviderRequest_SDKWirePayload pins the wire shape of the
// SDK create request for both config variants: the Konnect API takes the
// provider type at the request's top level and the config carries only the
// variant's own fields.
func TestPortalIdentityProviderRequest_SDKWirePayload(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		spec    PortalIdentityProviderRequestAPISpec
		reqType string
	}{
		{
			name: "oidc",
			spec: PortalIdentityProviderRequestAPISpec{
				Type: "oidc",
				Config: &PortalIdentityProviderRequestConfig{
					Type: PortalIdentityProviderRequestConfigTypeOIDC,
					OIDC: &CreateOIDCIdentityProviderConfig{
						ClientID:     "client-id",
						ClientSecret: SensitiveDataSource{Type: SensitiveDataSourceTypeInline, Value: new("client-secret")},
						IssuerURL:    "https://issuer.example.com",
					},
				},
			},
			reqType: "oidc",
		},
		{
			name: "saml",
			spec: PortalIdentityProviderRequestAPISpec{
				Type: "saml",
				Config: &PortalIdentityProviderRequestConfig{
					Type: PortalIdentityProviderRequestConfigTypeSAML,
					SAML: &PortalSAMLIdentityProviderConfig{IdpMetadataURL: "https://idp.example.com/metadata"},
				},
			},
			reqType: "saml",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := tc.spec.marshalSDKOpsPayload()
			require.NoError(t, err)

			var req sdkkonnectcomp.PortalCreateIdentityProvider
			require.NoError(t, json.Unmarshal(data, &req))
			require.NotNil(t, req.Type)
			assert.Equal(t, tc.reqType, string(*req.Type))

			require.NotNil(t, req.Config)
			cfg, err := json.Marshal(req.Config)
			require.NoError(t, err)

			var decoded map[string]any
			require.NoError(t, json.Unmarshal(cfg, &decoded))
			assert.NotContains(t, decoded, "type", "the config variant carries no discriminator on the wire")
		})
	}
}
