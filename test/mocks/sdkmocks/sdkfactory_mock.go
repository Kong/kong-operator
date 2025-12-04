package sdkmocks

import (
	"testing"

	sdkkonnectgo "github.com/Kong/sdk-konnect-go"
	"github.com/Kong/sdk-konnect-go/test/mocks"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	sdkops "github.com/kong/kong-operator/controller/konnect/ops/sdk"
	"github.com/kong/kong-operator/controller/konnect/server"
	gwtypes "github.com/kong/kong-operator/internal/types"
)

type MockSDKWrapper struct {
	ControlPlaneSDK             *mocks.MockControlPlanesSDK
	CloudGatewaysSDK            *MockCloudGatewaysSDK
	ControlPlaneGroupSDK        *MockControlPlaneGroupSDK
	ServicesSDK                 *mocks.MockServicesSDK
	RoutesSDK                   *MockRoutesSDK
	ConsumersSDK                *mocks.MockConsumersSDK
	ConsumerGroupSDK            *mocks.MockConsumerGroupsSDK
	PluginSDK                   *mocks.MockPluginsSDK
	UpstreamsSDK                *MockUpstreamsSDK
	TargetsSDK                  *MockTargetsSDK
	MeSDK                       *MockMeSDK
	KongCredentialsBasicAuthSDK *MockKongCredentialBasicAuthSDK
	KongCredentialsAPIKeySDK    *MockKongCredentialAPIKeySDK
	KongCredentialsACLSDK       *MockKongCredentialACLSDK
	KongCredentialsJWTSDK       *MockKongCredentialJWTSDK
	KongCredentialsHMACSDK      *MockKongCredentialHMACSDK
	CACertificatesSDK           *MockCACertificatesSDK
	CertificatesSDK             *MockCertificatesSDK
	VaultSDK                    *MockVaultSDK
	KeysSDK                     *mocks.MockKeysSDK
	KeySetsSDK                  *mocks.MockKeySetsSDK
	SNIsSDK                     *MockSNIsSDK
	DataPlaneCertificatesSDK    *MockDataPlaneClientCertificatesSDK
	server                      server.Server
}

var _ sdkops.SDKWrapper = MockSDKWrapper{}

func NewMockSDKWrapperWithT(t *testing.T) *MockSDKWrapper {
	return &MockSDKWrapper{
		ControlPlaneSDK:             mocks.NewMockControlPlanesSDK(t),
		ControlPlaneGroupSDK:        NewMockControlPlaneGroupSDK(t),
		CloudGatewaysSDK:            NewMockCloudGatewaysSDK(t),
		ServicesSDK:                 mocks.NewMockServicesSDK(t),
		RoutesSDK:                   NewMockRoutesSDK(t),
		ConsumersSDK:                mocks.NewMockConsumersSDK(t),
		ConsumerGroupSDK:            mocks.NewMockConsumerGroupsSDK(t),
		PluginSDK:                   mocks.NewMockPluginsSDK(t),
		UpstreamsSDK:                NewMockUpstreamsSDK(t),
		TargetsSDK:                  NewMockTargetsSDK(t),
		MeSDK:                       NewMockMeSDK(t),
		KongCredentialsBasicAuthSDK: NewMockKongCredentialBasicAuthSDK(t),
		KongCredentialsAPIKeySDK:    NewMockKongCredentialAPIKeySDK(t),
		KongCredentialsACLSDK:       NewMockKongCredentialACLSDK(t),
		KongCredentialsJWTSDK:       NewMockKongCredentialJWTSDK(t),
		KongCredentialsHMACSDK:      NewMockKongCredentialHMACSDK(t),
		CACertificatesSDK:           NewMockCACertificatesSDK(t),
		CertificatesSDK:             NewMockCertificatesSDK(t),
		VaultSDK:                    NewMockVaultSDK(t),
		KeysSDK:                     mocks.NewMockKeysSDK(t),
		KeySetsSDK:                  mocks.NewMockKeySetsSDK(t),
		SNIsSDK:                     NewMockSNIsSDK(t),
		DataPlaneCertificatesSDK:    NewMockDataPlaneClientCertificatesSDK(t),

		server: lo.Must(server.NewServer[*gwtypes.ControlPlane](SDKServerURL)),
	}
}

const (
	// SDKServerURL is the synthetic URL of the mock server.
	SDKServerURL = "https://us.mock.konghq.com"
)

func (m MockSDKWrapper) GetServerURL() string {
	return m.server.URL()
}

func (m MockSDKWrapper) GetServer() server.Server {
	return m.server
}

func (m MockSDKWrapper) GetControlPlaneSDK() sdkkonnectgo.ControlPlanesSDK {
	return m.ControlPlaneSDK
}

func (m MockSDKWrapper) GetControlPlaneGroupSDK() sdkops.ControlPlaneGroupSDK {
	return m.ControlPlaneGroupSDK
}

func (m MockSDKWrapper) GetServicesSDK() sdkkonnectgo.ServicesSDK {
	return m.ServicesSDK
}

func (m MockSDKWrapper) GetRoutesSDK() sdkops.RoutesSDK {
	return m.RoutesSDK
}

func (m MockSDKWrapper) GetConsumersSDK() sdkkonnectgo.ConsumersSDK {
	return m.ConsumersSDK
}

func (m MockSDKWrapper) GetConsumerGroupsSDK() sdkkonnectgo.ConsumerGroupsSDK {
	return m.ConsumerGroupSDK
}

func (m MockSDKWrapper) GetPluginSDK() sdkkonnectgo.PluginsSDK {
	return m.PluginSDK
}

func (m MockSDKWrapper) GetUpstreamsSDK() sdkops.UpstreamsSDK {
	return m.UpstreamsSDK
}

func (m MockSDKWrapper) GetBasicAuthCredentialsSDK() sdkops.KongCredentialBasicAuthSDK {
	return m.KongCredentialsBasicAuthSDK
}

func (m MockSDKWrapper) GetAPIKeyCredentialsSDK() sdkops.KongCredentialAPIKeySDK {
	return m.KongCredentialsAPIKeySDK
}

func (m MockSDKWrapper) GetACLCredentialsSDK() sdkops.KongCredentialACLSDK {
	return m.KongCredentialsACLSDK
}

func (m MockSDKWrapper) GetJWTCredentialsSDK() sdkops.KongCredentialJWTSDK {
	return m.KongCredentialsJWTSDK
}

func (m MockSDKWrapper) GetHMACCredentialsSDK() sdkops.KongCredentialHMACSDK {
	return m.KongCredentialsHMACSDK
}

func (m MockSDKWrapper) GetTargetsSDK() sdkops.TargetsSDK {
	return m.TargetsSDK
}

func (m MockSDKWrapper) GetVaultSDK() sdkops.VaultSDK {
	return m.VaultSDK
}

func (m MockSDKWrapper) GetMeSDK() sdkops.MeSDK {
	return m.MeSDK
}

func (m MockSDKWrapper) GetCACertificatesSDK() sdkops.CACertificatesSDK {
	return m.CACertificatesSDK
}

func (m MockSDKWrapper) GetCertificatesSDK() sdkops.CertificatesSDK {
	return m.CertificatesSDK
}

func (m MockSDKWrapper) GetKeysSDK() sdkkonnectgo.KeysSDK {
	return m.KeysSDK
}

func (m MockSDKWrapper) GetKeySetsSDK() sdkkonnectgo.KeySetsSDK {
	return m.KeySetsSDK
}

func (m MockSDKWrapper) GetSNIsSDK() sdkops.SNIsSDK {
	return m.SNIsSDK
}

func (m MockSDKWrapper) GetDataPlaneCertificatesSDK() sdkops.DataPlaneClientCertificatesSDK {
	return m.DataPlaneCertificatesSDK
}

func (m MockSDKWrapper) GetCloudGatewaysSDK() sdkops.CloudGatewaysSDK {
	return m.CloudGatewaysSDK
}

type MockSDKFactory struct {
	t   *testing.T
	SDK *MockSDKWrapper
}

var _ sdkops.SDKFactory = MockSDKFactory{}

func NewMockSDKFactory(t *testing.T) *MockSDKFactory {
	return &MockSDKFactory{
		t:   t,
		SDK: NewMockSDKWrapperWithT(t),
	}
}

func (m MockSDKFactory) NewKonnectSDK(_ server.Server, _ sdkops.SDKToken) sdkops.SDKWrapper {
	require.NotNil(m.t, m.SDK)
	return *m.SDK
}
