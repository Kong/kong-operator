package v1alpha1

// ConfigureOIDCIdentityProviderConfig The identity provider that contains
// configuration data for the OIDC authentication integration.
type ConfigureOIDCIdentityProviderConfig struct {
	ClaimMappings OIDCIdentityProviderClaimMappings `json:"claim_mappings,omitempty"`
	ClientID      OIDCIdentityProviderClientId      `json:"client_id,omitempty"`
	ClientSecret  OIDCIdentityProviderClientSecret  `json:"client_secret,omitempty"`
	IssuerURL     OIDCIdentityProviderIssuer        `json:"issuer_url,omitempty"`
	Scopes        OIDCIdentityProviderScopes        `json:"scopes,omitempty"`
}

// CreateDcrConfigAuth0InRequest Payload to create an Auth0 DCR provider.
type CreateDcrConfigAuth0InRequest struct {
	InitialClientAudience     DcrConfigPropertyInitialClientAudience `json:"initial_client_audience,omitempty"`
	InitialClientID           DcrConfigPropertyInitialClientId       `json:"initial_client_id,omitempty"`
	InitialClientSecret       DcrConfigPropertyInitialClientSecret   `json:"initial_client_secret,omitempty"`
	UseDeveloperManagedScopes bool                                   `json:"use_developer_managed_scopes,omitempty"`
}

// CreateDcrConfigAzureAdInRequest Payload to create an Azure AD DCR provider.
type CreateDcrConfigAzureAdInRequest struct {
	InitialClientID     DcrConfigPropertyInitialClientId     `json:"initial_client_id,omitempty"`
	InitialClientSecret DcrConfigPropertyInitialClientSecret `json:"initial_client_secret,omitempty"`
}

// CreateDcrConfigCurityInRequest Payload to create a Curity DCR provider.
type CreateDcrConfigCurityInRequest struct {
	InitialClientID     DcrConfigPropertyInitialClientId     `json:"initial_client_id,omitempty"`
	InitialClientSecret DcrConfigPropertyInitialClientSecret `json:"initial_client_secret,omitempty"`
}

// CreateDcrConfigHttpInRequest Payload to create an HTTP DCR provider.
type CreateDcrConfigHttpInRequest struct {
	APIKey               DcrConfigPropertyApiKey               `json:"api_key,omitempty"`
	DcrBaseURL           DcrBaseUrl                            `json:"dcr_base_url,omitempty"`
	DisableEventHooks    DcrConfigPropertyDisableEventHooks    `json:"disable_event_hooks,omitempty"`
	DisableRefreshSecret DcrConfigPropertyDisableRefreshSecret `json:"disable_refresh_secret,omitempty"`
}

// CreateDcrConfigOktaInRequest Payload to create an Okta DCR provider.
type CreateDcrConfigOktaInRequest struct {
	DcrToken DcrConfigPropertyDcrToken `json:"dcr_token,omitempty"`
}

// CreateDcrProviderRequestAuth0 Request body for creating an Auth0 DCR
// provider.
type CreateDcrProviderRequestAuth0 struct {
	DcrConfig    CreateDcrConfigAuth0InRequest `json:"dcr_config,omitempty"`
	DisplayName  DcrProviderDisplayName        `json:"display_name,omitempty"`
	Issuer       string                        `json:"issuer,omitempty"`
	Labels       Labels                        `json:"labels,omitempty"`
	Name         DcrProviderName               `json:"name,omitempty"`
	ProviderType string                        `json:"provider_type,omitempty"`
}

// CreateDcrProviderRequestAzureAd Request body for creating an Azure AD DCR
// provider.
type CreateDcrProviderRequestAzureAd struct {
	DcrConfig    CreateDcrConfigAzureAdInRequest `json:"dcr_config,omitempty"`
	DisplayName  DcrProviderDisplayName          `json:"display_name,omitempty"`
	Issuer       string                          `json:"issuer,omitempty"`
	Labels       Labels                          `json:"labels,omitempty"`
	Name         DcrProviderName                 `json:"name,omitempty"`
	ProviderType string                          `json:"provider_type,omitempty"`
}

// CreateDcrProviderRequestCurity Request body for creating a Curity DCR
// provider.
type CreateDcrProviderRequestCurity struct {
	DcrConfig    CreateDcrConfigCurityInRequest `json:"dcr_config,omitempty"`
	DisplayName  DcrProviderDisplayName         `json:"display_name,omitempty"`
	Issuer       string                         `json:"issuer,omitempty"`
	Labels       Labels                         `json:"labels,omitempty"`
	Name         DcrProviderName                `json:"name,omitempty"`
	ProviderType string                         `json:"provider_type,omitempty"`
}

// CreateDcrProviderRequestHttp Request body for creating an HTTP DCR provider.
type CreateDcrProviderRequestHttp struct {
	DcrConfig    CreateDcrConfigHttpInRequest `json:"dcr_config,omitempty"`
	DisplayName  DcrProviderDisplayName       `json:"display_name,omitempty"`
	Issuer       string                       `json:"issuer,omitempty"`
	Labels       Labels                       `json:"labels,omitempty"`
	Name         DcrProviderName              `json:"name,omitempty"`
	ProviderType string                       `json:"provider_type,omitempty"`
}

// CreateDcrProviderRequestOkta Request body for creating an Okta DCR provider.
type CreateDcrProviderRequestOkta struct {
	DcrConfig    CreateDcrConfigOktaInRequest `json:"dcr_config,omitempty"`
	DisplayName  DcrProviderDisplayName       `json:"display_name,omitempty"`
	Issuer       string                       `json:"issuer,omitempty"`
	Labels       Labels                       `json:"labels,omitempty"`
	Name         DcrProviderName              `json:"name,omitempty"`
	ProviderType string                       `json:"provider_type,omitempty"`
}

// CreatePortalCustomDomainSSL is a type alias.
type CreatePortalCustomDomainSSL map[string]string

// CreatePortalCustomDomainSSLStandard is a type alias.
type CreatePortalCustomDomainSSLStandard struct {
	DomainVerificationMethod string `json:"domain_verification_method,omitempty"`
}

// CreatePortalCustomDomainSSLWithCustomCertificate is a type alias.
type CreatePortalCustomDomainSSLWithCustomCertificate struct {
	CustomCertificate        string `json:"custom_certificate,omitempty"`
	CustomPrivateKey         string `json:"custom_private_key,omitempty"`
	DomainVerificationMethod string `json:"domain_verification_method,omitempty"`
	SkipCaCheck              bool   `json:"skip_ca_check,omitempty"`
}

// DcrBaseUrl The base URL of the DCR server.
// This is the URL that will be used to make the HTTP requests from Konnect to
// the DCR provider.
// This URL must be accessible from the Konnect service.
type DcrBaseUrl map[string]string

// DcrConfigPropertyApiKey This is the API Key that will be sent with each HTTP
// request to the custom DCR server.
// It can be
// verified on the server to ensure that incoming requests are coming from
// Konnect.
type DcrConfigPropertyApiKey map[string]string

// DcrConfigPropertyDcrToken This secret should be copied from your identity
// provider's settings after you create a client
// and assign it as the management client for DCR for this developer portal
type DcrConfigPropertyDcrToken map[string]string

// DcrConfigPropertyDisableEventHooks This flag disables all the event-hooks on
// the application flow for the DCR provider.
type DcrConfigPropertyDisableEventHooks map[string]string

// DcrConfigPropertyDisableRefreshSecret This flag disable the refresh-secret
// endpoint on the application flow for the DCR provider.
type DcrConfigPropertyDisableRefreshSecret map[string]string

// DcrConfigPropertyInitialClientAudience This is the audience value used for
// the initial client.
// If using a custom domain on Auth0, this must be set as to the Auth0
// Management API audience value.
// If left blank, the issuer will be used instead.
type DcrConfigPropertyInitialClientAudience map[string]string

// DcrConfigPropertyInitialClientId This ID should be copied from your identity
// provider's settings after you create a client
// and assign it as the management client for DCR for this developer portal
type DcrConfigPropertyInitialClientId map[string]string

// DcrConfigPropertyInitialClientSecret This secret should be copied from your
// identity provider's settings after you create a client
// and assign it as the management client for DCR for this developer portal
type DcrConfigPropertyInitialClientSecret map[string]string

// DcrProviderDisplayName The display name of the DCR provider.
// This is used to identify the DCR provider in the Portal UI.
type DcrProviderDisplayName map[string]string

// DcrProviderName The name of the DCR provider.
// This is used to identify the DCR provider in the Konnect UI.
type DcrProviderName map[string]string

// IdentityProviderEnabled Indicates whether the identity provider is enabled.
// Only one identity provider can be active at a time, such as SAML or OIDC.
type IdentityProviderEnabled map[string]string

// IdentityProviderLoginPath The path used for initiating login requests with
// the identity provider.
type IdentityProviderLoginPath map[string]string

// IdentityProviderType Specifies the type of identity provider.
type IdentityProviderType map[string]string

// Labels Labels store metadata of an entity that can be used for filtering an
// entity list or for searching across entity types.
//
// Keys must be of length 1-63 characters, and cannot start with "kong",
// "konnect", "mesh", "kic", or "_".
type Labels map[string]string

// LabelsUpdate Labels store metadata of an entity that can be used for
// filtering an entity list or for searching across entity types.
//
// Labels are intended to store **INTERNAL** metadata.
//
// Keys must be of length 1-63 characters, and cannot start with "kong",
// "konnect", "mesh", "kic", or "_".
type LabelsUpdate map[string]string

// OIDCIdentityProviderClaimMappings Defines the mappings between OpenID Connect
// (OIDC) claims and local claims used by your application for
// authentication.
type OIDCIdentityProviderClaimMappings struct {
	Email  string `json:"email,omitempty"`
	Groups string `json:"groups,omitempty"`
	Name   string `json:"name,omitempty"`
}

// OIDCIdentityProviderClientId The client ID assigned to your application by
// the identity provider.
type OIDCIdentityProviderClientId map[string]string

// OIDCIdentityProviderClientSecret The Client Secret assigned to your
// application by the identity provider.
type OIDCIdentityProviderClientSecret map[string]string

// OIDCIdentityProviderIssuer The issuer URI of the identity provider.
// This is the URL where the provider's metadata can be obtained.
type OIDCIdentityProviderIssuer map[string]string

// OIDCIdentityProviderScopes The scopes requested by your application when
// authenticating with the identity provider.
type OIDCIdentityProviderScopes map[string]string

// SAMLIdentityProviderConfig The identity provider that contains configuration
// data for the SAML authentication integration.
type SAMLIdentityProviderConfig struct {
	IdpMetadataURL SAMLIdentityProviderMetadataURL `json:"idp_metadata_url,omitempty"`
	IdpMetadataXML SAMLIdentityProviderMetadata    `json:"idp_metadata_xml,omitempty"`
}

// SAMLIdentityProviderMetadata The identity provider's SAML metadata.
// If the identity provider supports a metadata URL, you can use the
// `idp_metadata_url` field instead.
type SAMLIdentityProviderMetadata map[string]string

// SAMLIdentityProviderMetadataURL The identity provider's metadata URL where
// the identity provider's metadata can be obtained.
type SAMLIdentityProviderMetadataURL map[string]string
