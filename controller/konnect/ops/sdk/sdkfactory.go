package sdk

import (
	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"

	"github.com/kong/kong-operator/v2/controller/konnect/server"
)

// SDKWrapper is a wrapper of Konnect SDK to allow using mock SDKs in tests.
type SDKWrapper interface {
	GetControlPlaneSDK() sdkkonnectgo.ControlPlanesSDK
	GetControlPlaneGroupSDK() ControlPlaneGroupSDK
	GetServicesSDK() sdkkonnectgo.ServicesSDK
	GetRoutesSDK() sdkkonnectgo.RoutesSDK
	GetConsumersSDK() sdkkonnectgo.ConsumersSDK
	GetConsumerGroupsSDK() sdkkonnectgo.ConsumerGroupsSDK
	GetPluginSDK() sdkkonnectgo.PluginsSDK
	GetUpstreamsSDK() sdkkonnectgo.UpstreamsSDK
	GetTargetsSDK() sdkkonnectgo.TargetsSDK
	GetVaultSDK() sdkkonnectgo.VaultsSDK
	GetMeSDK() sdkkonnectgo.MeSDK
	GetBasicAuthCredentialsSDK() sdkkonnectgo.BasicAuthCredentialsSDK
	GetAPIKeyCredentialsSDK() sdkkonnectgo.APIKeysSDK
	GetACLCredentialsSDK() sdkkonnectgo.ACLsSDK
	GetJWTCredentialsSDK() sdkkonnectgo.JWTsSDK
	GetHMACCredentialsSDK() sdkkonnectgo.HMACAuthCredentialsSDK
	GetCACertificatesSDK() sdkkonnectgo.CACertificatesSDK
	GetCertificatesSDK() sdkkonnectgo.CertificatesSDK
	GetKeysSDK() sdkkonnectgo.KeysSDK
	GetKeySetsSDK() sdkkonnectgo.KeySetsSDK
	GetSNIsSDK() sdkkonnectgo.SNIsSDK
	GetDataPlaneCertificatesSDK() sdkkonnectgo.DPCertificatesSDK
	GetCloudGatewaysSDK() sdkkonnectgo.CloudGatewaysSDK

	// GetServerURL returns the server URL for recording metrics.
	GetServerURL() string
	GetServer() server.Server
}

type sdkWrapper struct {
	server server.Server
	sdk    *sdkkonnectgo.SDK
}

var _ SDKWrapper = sdkWrapper{}

// GetServerURL returns the Konnect server URL for recording metrics.
func (w sdkWrapper) GetServerURL() string {
	return w.server.URL()
}

// GetServer returns the Konnect server used by this SDK instance.
func (w sdkWrapper) GetServer() server.Server {
	return w.server
}

// GetControlPlaneSDK returns the SDK to operate Konnect control planes.
func (w sdkWrapper) GetControlPlaneSDK() sdkkonnectgo.ControlPlanesSDK {
	return w.sdk.ControlPlanes
}

// GetControlPlaneGroupSDK returns the SDK to operate Konnect control plane groups.
func (w sdkWrapper) GetControlPlaneGroupSDK() ControlPlaneGroupSDK {
	return w.sdk.ControlPlaneGroups
}

// GetServicesSDK returns the SDK to operate Kong services.
func (w sdkWrapper) GetServicesSDK() sdkkonnectgo.ServicesSDK {
	return w.sdk.Services
}

// GetRoutesSDK returns the SDK to operate Kong routes.
func (w sdkWrapper) GetRoutesSDK() sdkkonnectgo.RoutesSDK {
	return w.sdk.Routes
}

// GetConsumersSDK returns the SDK to operate Kong consumers.
func (w sdkWrapper) GetConsumersSDK() sdkkonnectgo.ConsumersSDK {
	return w.sdk.Consumers
}

// GetConsumerGroupsSDK returns the SDK to operate Kong consumer groups.
func (w sdkWrapper) GetConsumerGroupsSDK() sdkkonnectgo.ConsumerGroupsSDK {
	return w.sdk.ConsumerGroups
}

// GetPluginSDK returns the SDK to operate plugins.
func (w sdkWrapper) GetPluginSDK() sdkkonnectgo.PluginsSDK {
	return w.sdk.Plugins
}

// GetUpstreamsSDK returns the SDK to operate Upstreams.
func (w sdkWrapper) GetUpstreamsSDK() sdkkonnectgo.UpstreamsSDK {
	return w.sdk.Upstreams
}

// GetTargetsSDK returns the SDK to operate Targets.
func (w sdkWrapper) GetTargetsSDK() sdkkonnectgo.TargetsSDK {
	return w.sdk.Targets
}

// GetVaultSDK returns the SDK to operate Vaults.
func (w sdkWrapper) GetVaultSDK() sdkkonnectgo.VaultsSDK {
	return w.sdk.Vaults
}

// GetMeSDK returns the "me" SDK to get current organization.
func (w sdkWrapper) GetMeSDK() sdkkonnectgo.MeSDK {
	return w.sdk.Me
}

// GetCACertificatesSDK returns the SDK to operate CA certificates.
func (w sdkWrapper) GetCACertificatesSDK() sdkkonnectgo.CACertificatesSDK {
	return w.sdk.CACertificates
}

// GetCertificatesSDK returns the SDK to operate certificates.
func (w sdkWrapper) GetCertificatesSDK() sdkkonnectgo.CertificatesSDK {
	return w.sdk.Certificates
}

// GetSNIsSDK returns the SDK to operate SNIs.
func (w sdkWrapper) GetSNIsSDK() sdkkonnectgo.SNIsSDK {
	return w.sdk.SNIs
}

// GetBasicAuthCredentialsSDK returns the BasicAuthCredentials SDK to get current organization.
func (w sdkWrapper) GetBasicAuthCredentialsSDK() sdkkonnectgo.BasicAuthCredentialsSDK {
	return w.sdk.BasicAuthCredentials
}

// GetAPIKeyCredentialsSDK returns the APIAKeyCredentials SDK to get current organization.
func (w sdkWrapper) GetAPIKeyCredentialsSDK() sdkkonnectgo.APIKeysSDK {
	return w.sdk.APIKeys
}

// GetACLCredentialsSDK returns the ACLCredentials SDK to get current organization.
func (w sdkWrapper) GetACLCredentialsSDK() sdkkonnectgo.ACLsSDK {
	return w.sdk.ACLs
}

// GetJWTCredentialsSDK returns the JWTCredentials SDK to get current organization.
func (w sdkWrapper) GetJWTCredentialsSDK() sdkkonnectgo.JWTsSDK {
	return w.sdk.JWTs
}

// GetHMACCredentialsSDK returns the HMACCredentials SDK to get current organization.
func (w sdkWrapper) GetHMACCredentialsSDK() sdkkonnectgo.HMACAuthCredentialsSDK {
	return w.sdk.HMACAuthCredentials
}

// GetKeysSDK returns the SDK to operate keys.
func (w sdkWrapper) GetKeysSDK() sdkkonnectgo.KeysSDK {
	return w.sdk.Keys
}

// GetKeySetsSDK returns the SDK to operate key sets.
func (w sdkWrapper) GetKeySetsSDK() sdkkonnectgo.KeySetsSDK {
	return w.sdk.KeySets
}

// GetDataPlaneCertificatesSDK returns the SDK to operate data plane certificates.
func (w sdkWrapper) GetDataPlaneCertificatesSDK() sdkkonnectgo.DPCertificatesSDK {
	return w.sdk.DPCertificates
}

// GetCloudGatewaysSDK returns the SDK to operate Konnect Dedicated Cloud Gateways SDK.
func (w sdkWrapper) GetCloudGatewaysSDK() sdkkonnectgo.CloudGatewaysSDK {
	return w.sdk.CloudGateways
}

// SDKToken is a token used to authenticate with the Konnect SDK.
type SDKToken string

// SDKFactory is a factory for creating Konnect SDKs.
type SDKFactory interface {
	NewKonnectSDK(server server.Server, token SDKToken) SDKWrapper
}

type sdkFactory struct{}

// NewSDKFactory creates a new SDKFactory.
func NewSDKFactory() SDKFactory {
	return sdkFactory{}
}

// NewKonnectSDK creates a new Konnect SDK.
func (f sdkFactory) NewKonnectSDK(server server.Server, token SDKToken) SDKWrapper {
	return sdkWrapper{
		server: server,
		sdk: sdkkonnectgo.New(
			sdkkonnectgo.WithSecurity(
				sdkkonnectcomp.Security{
					PersonalAccessToken: new(string(token)),
				},
			),
			sdkkonnectgo.WithServerURL(server.URL()),
		),
	}
}
