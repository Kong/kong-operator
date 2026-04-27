package ops

import (
	"context"
	"fmt"
	"slices"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
)

// getIdentityProviderRequestForUID adopts a portal-scoped identity provider by
// matching the desired spec. Konnect does not expose Kubernetes UID labels for
// this entity type, so list results must be compared against the requested
// configuration instead.
func getIdentityProviderRequestForUID(
	ctx context.Context,
	sdk sdkkonnectgo.PortalAuthSettingsSDK,
	obj *konnectv1alpha1.IdentityProviderRequest,
) (string, error) {
	parentID := obj.GetPortalID()
	if parentID == "" {
		return "", CantPerformOperationWithoutParentIDError{Entity: obj, Parent: "Portal", Op: GetOp}
	}

	req, err := obj.Spec.APISpec.ToCreateIdentityProvider()
	if err != nil {
		return "", fmt.Errorf("failed creating %s SDK request: %w", obj.GetTypeName(), err)
	}

	var filter *sdkkonnectops.GetPortalIdentityProvidersQueryParamFilter
	if req.GetType() != nil {
		typ := string(*req.GetType())
		filter = &sdkkonnectops.GetPortalIdentityProvidersQueryParamFilter{
			Type: &sdkkonnectcomp.StringFieldEqualsFilter{
				Eq: &typ,
			},
		}
	}

	resp, err := sdk.GetPortalIdentityProviders(ctx, parentID, filter)
	if err != nil {
		return "", fmt.Errorf("failed listing %s: %w", obj.GetTypeName(), err)
	}
	if resp == nil {
		return "", fmt.Errorf("failed listing %s: %w", obj.GetTypeName(), ErrNilResponse)
	}

	for _, existing := range resp.GetIdentityProviders() {
		if !identityProviderMatchesCreateRequest(existing, *req) {
			continue
		}
		if id := existing.GetID(); id != nil && *id != "" {
			return *id, nil
		}
	}

	return "", EntityWithMatchingUIDNotFoundError{Entity: obj}
}

func identityProviderMatchesCreateRequest(
	existing sdkkonnectcomp.IdentityProvider,
	desired sdkkonnectcomp.CreateIdentityProvider,
) bool {
	if !stringPtrEqual(existing.GetType(), desired.GetType()) {
		return false
	}
	if !boolPtrEqual(existing.GetEnabled(), desired.GetEnabled()) {
		return false
	}
	if !stringPtrEqual(existing.GetLoginPath(), desired.GetLoginPath()) {
		return false
	}

	return identityProviderConfigMatchesCreateRequest(existing.GetConfig(), desired.GetConfig())
}

func identityProviderConfigMatchesCreateRequest(
	existing *sdkkonnectcomp.IdentityProviderConfig,
	desired *sdkkonnectcomp.CreateIdentityProviderConfig,
) bool {
	switch {
	case desired == nil:
		return existing == nil
	case existing == nil:
		return false
	case desired.OIDCIdentityProviderConfig != nil:
		return existing.OIDCIdentityProviderConfigOutput != nil &&
			oidcIdentityProviderConfigMatchesCreateRequest(
				*existing.OIDCIdentityProviderConfigOutput,
				*desired.OIDCIdentityProviderConfig,
			)
	case desired.SAMLIdentityProviderConfigInput != nil:
		return existing.SAMLIdentityProviderConfig != nil &&
			samlIdentityProviderConfigMatchesCreateRequest(
				*existing.SAMLIdentityProviderConfig,
				*desired.SAMLIdentityProviderConfigInput,
			)
	default:
		return false
	}
}

func oidcIdentityProviderConfigMatchesCreateRequest(
	existing sdkkonnectcomp.OIDCIdentityProviderConfigOutput,
	desired sdkkonnectcomp.OIDCIdentityProviderConfig,
) bool {
	if existing.GetIssuerURL() != desired.GetIssuerURL() {
		return false
	}
	if existing.GetClientID() != desired.GetClientID() {
		return false
	}
	if !slices.Equal(existing.GetScopes(), desired.GetScopes()) {
		return false
	}
	return oidcClaimMappingsEqual(existing.GetClaimMappings(), desired.GetClaimMappings())
}

func samlIdentityProviderConfigMatchesCreateRequest(
	existing sdkkonnectcomp.SAMLIdentityProviderConfig,
	desired sdkkonnectcomp.SAMLIdentityProviderConfigInput,
) bool {
	return stringPtrEqual(existing.GetIdpMetadataURL(), desired.GetIdpMetadataURL()) &&
		stringPtrEqual(existing.GetIdpMetadataXML(), desired.GetIdpMetadataXML())
}

func oidcClaimMappingsEqual(
	existing *sdkkonnectcomp.OIDCIdentityProviderClaimMappings,
	desired *sdkkonnectcomp.OIDCIdentityProviderClaimMappings,
) bool {
	switch {
	case desired == nil:
		return existing == nil
	case existing == nil:
		return false
	default:
		return stringPtrEqual(existing.GetName(), desired.GetName()) &&
			stringPtrEqual(existing.GetEmail(), desired.GetEmail()) &&
			stringPtrEqual(existing.GetGroups(), desired.GetGroups())
	}
}

func stringPtrEqual[T ~string](a, b *T) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a == nil || b == nil:
		return false
	default:
		return *a == *b
	}
}

func boolPtrEqual(a, b *bool) bool {
	return boolValue(a) == boolValue(b)
}

func boolValue(v *bool) bool {
	return v != nil && *v
}
