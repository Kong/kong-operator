package ops

import (
	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
)

// SDKWrapper is a wrapper of Konnect SDK to allow using mock SDKs in tests.
type SDKWrapper interface {
	GetControlPlaneSDK() ControlPlaneSDK
	GetServicesSDK() ServicesSDK
	GetRoutesSDK() RoutesSDK
	GetConsumersSDK() ConsumersSDK
	GetConsumerGroupsSDK() ConsumerGroupSDK
	GetPluginSDK() PluginSDK
	GetUpstreamsSDK() UpstreamsSDK
	GetTargetsSDK() TargetsSDK
	GetVaultSDK() VaultSDK
	GetMeSDK() MeSDK
	GetBasicAuthCredentialsSDK() KongCredentialBasicAuthSDK
	GetAPIKeyCredentialsSDK() KongCredentialAPIKeySDK
	GetACLCredentialsSDK() KongCredentialACLSDK
	GetJWTCredentialsSDK() KongCredentialJWTSDK
	GetCACertificatesSDK() CACertificatesSDK
	GetCertificatesSDK() CertificatesSDK
	GetKeysSDK() KeysSDK
	GetKeySetsSDK() KeySetsSDK
	GetSNIsSDK() SNIsSDK
}

type sdkWrapper struct {
	sdk *sdkkonnectgo.SDK
}

var _ SDKWrapper = sdkWrapper{}

// GetControlPlaneSDK returns the SDK to operate Konenct control planes.
func (w sdkWrapper) GetControlPlaneSDK() ControlPlaneSDK {
	return w.sdk.ControlPlanes
}

// GetServicesSDK returns the SDK to operate Kong services.
func (w sdkWrapper) GetServicesSDK() ServicesSDK {
	return w.sdk.Services
}

// GetRoutesSDK returns the SDK to operate Kong routes.
func (w sdkWrapper) GetRoutesSDK() RoutesSDK {
	return w.sdk.Routes
}

// GetConsumersSDK returns the SDK to operate Kong consumers.
func (w sdkWrapper) GetConsumersSDK() ConsumersSDK {
	return w.sdk.Consumers
}

// GetConsumerGroupsSDK returns the SDK to operate Kong consumer groups.
func (w sdkWrapper) GetConsumerGroupsSDK() ConsumerGroupSDK {
	return w.sdk.ConsumerGroups
}

// GetPluginSDK returns the SDK to operate plugins.
func (w sdkWrapper) GetPluginSDK() PluginSDK {
	return w.sdk.Plugins
}

// GetUpstreamsSDK returns the SDK to operate Upstreams.
func (w sdkWrapper) GetUpstreamsSDK() UpstreamsSDK {
	return w.sdk.Upstreams
}

// GetTargetsSDK returns the SDK to operate Targets.
func (w sdkWrapper) GetTargetsSDK() TargetsSDK {
	return w.sdk.Targets
}

// GetVaultSDK returns the SDK to operate Vaults.
func (w sdkWrapper) GetVaultSDK() VaultSDK {
	return w.sdk.Vaults
}

// GetMeSDK returns the "me" SDK to get current organization.
func (w sdkWrapper) GetMeSDK() MeSDK {
	return w.sdk.Me
}

// GetCACertificatesSDK returns the SDK to operate CA certificates.
func (w sdkWrapper) GetCACertificatesSDK() CACertificatesSDK {
	return w.sdk.CACertificates
}

// GetCertificatesSDK returns the SDK to operate certificates.
func (w sdkWrapper) GetCertificatesSDK() CertificatesSDK {
	return w.sdk.Certificates
}

// GetSNIsSDK returns the SDK to operate SNIs.
func (w sdkWrapper) GetSNIsSDK() SNIsSDK {
	return w.sdk.SNIs
}

// GetBasicAuthCredentialsSDK returns the BasicAuthCredentials SDK to get current organization.
func (w sdkWrapper) GetBasicAuthCredentialsSDK() KongCredentialBasicAuthSDK {
	return w.sdk.BasicAuthCredentials
}

// GetAPIKeyCredentialsSDK returns the APIAKeyCredentials SDK to get current organization.
func (w sdkWrapper) GetAPIKeyCredentialsSDK() KongCredentialAPIKeySDK {
	return w.sdk.APIKeys
}

// GetACLCredentialsSDK returns the ACLCredentials SDK to get current organization.
func (w sdkWrapper) GetACLCredentialsSDK() KongCredentialACLSDK {
	return w.sdk.ACLs
}

// GetJWTCredentialsSDK returns the JWTCredentials SDK to get current organization.
func (w sdkWrapper) GetJWTCredentialsSDK() KongCredentialJWTSDK {
	return w.sdk.JWTs
}

// GetKeysSDK returns the SDK to operate keys.
func (w sdkWrapper) GetKeysSDK() KeysSDK {
	return w.sdk.Keys
}

// GetKeySetsSDK returns the SDK to operate key sets.
func (w sdkWrapper) GetKeySetsSDK() KeySetsSDK {
	return w.sdk.KeySets
}

// SDKToken is a token used to authenticate with the Konnect SDK.
type SDKToken string

// SDKFactory is a factory for creating Konnect SDKs.
type SDKFactory interface {
	NewKonnectSDK(serverURL string, token SDKToken) SDKWrapper
}

type sdkFactory struct{}

// NewSDKFactory creates a new SDKFactory.
func NewSDKFactory() SDKFactory {
	return sdkFactory{}
}

// NewKonnectSDK creates a new Konnect SDK.
func (f sdkFactory) NewKonnectSDK(serverURL string, token SDKToken) SDKWrapper {
	return sdkWrapper{
		sdk: sdkkonnectgo.New(
			sdkkonnectgo.WithSecurity(
				sdkkonnectcomp.Security{
					PersonalAccessToken: sdkkonnectgo.String(string(token)),
				},
			),
			sdkkonnectgo.WithServerURL(serverURL),
		),
	}
}
