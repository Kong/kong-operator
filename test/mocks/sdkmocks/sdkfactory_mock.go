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
	CloudGatewaysSDK            *mocks.MockCloudGatewaysSDK
	ControlPlaneGroupSDK        *mocks.MockControlPlaneGroupsSDK
	ServicesSDK                 *mocks.MockServicesSDK
	RoutesSDK                   *mocks.MockRoutesSDK
	ConsumersSDK                *mocks.MockConsumersSDK
	ConsumerGroupSDK            *mocks.MockConsumerGroupsSDK
	PluginSDK                   *mocks.MockPluginsSDK
	UpstreamsSDK                *mocks.MockUpstreamsSDK
	TargetsSDK                  *mocks.MockTargetsSDK
	MeSDK                       *mocks.MockMeSDK
	KongCredentialsBasicAuthSDK *mocks.MockBasicAuthCredentialsSDK
	KongCredentialsAPIKeySDK    *mocks.MockAPIKeysSDK
	KongCredentialsACLSDK       *mocks.MockACLsSDK
	KongCredentialsJWTSDK       *mocks.MockJWTsSDK
	KongCredentialsHMACSDK      *mocks.MockHMACAuthCredentialsSDK
	CACertificatesSDK           *mocks.MockCACertificatesSDK
	CertificatesSDK             *mocks.MockCertificatesSDK
	VaultSDK                    *mocks.MockVaultsSDK
	KeysSDK                     *mocks.MockKeysSDK
	KeySetsSDK                  *mocks.MockKeySetsSDK
	SNIsSDK                     *mocks.MockSNIsSDK
	DataPlaneCertificatesSDK    *mocks.MockDPCertificatesSDK
	server                      server.Server
}

var _ sdkops.SDKWrapper = MockSDKWrapper{}

func NewMockSDKWrapperWithT(t *testing.T) *MockSDKWrapper {
	return &MockSDKWrapper{
		ControlPlaneSDK:             mocks.NewMockControlPlanesSDK(t),
		ControlPlaneGroupSDK:        mocks.NewMockControlPlaneGroupsSDK(t),
		CloudGatewaysSDK:            mocks.NewMockCloudGatewaysSDK(t),
		ServicesSDK:                 mocks.NewMockServicesSDK(t),
		RoutesSDK:                   mocks.NewMockRoutesSDK(t),
		ConsumersSDK:                mocks.NewMockConsumersSDK(t),
		ConsumerGroupSDK:            mocks.NewMockConsumerGroupsSDK(t),
		PluginSDK:                   mocks.NewMockPluginsSDK(t),
		UpstreamsSDK:                mocks.NewMockUpstreamsSDK(t),
		TargetsSDK:                  mocks.NewMockTargetsSDK(t),
		MeSDK:                       mocks.NewMockMeSDK(t),
		KongCredentialsBasicAuthSDK: mocks.NewMockBasicAuthCredentialsSDK(t),
		KongCredentialsAPIKeySDK:    mocks.NewMockAPIKeysSDK(t),
		KongCredentialsACLSDK:       mocks.NewMockACLsSDK(t),
		KongCredentialsJWTSDK:       mocks.NewMockJWTsSDK(t),
		KongCredentialsHMACSDK:      mocks.NewMockHMACAuthCredentialsSDK(t),
		CACertificatesSDK:           mocks.NewMockCACertificatesSDK(t),
		CertificatesSDK:             mocks.NewMockCertificatesSDK(t),
		VaultSDK:                    mocks.NewMockVaultsSDK(t),
		KeysSDK:                     mocks.NewMockKeysSDK(t),
		KeySetsSDK:                  mocks.NewMockKeySetsSDK(t),
		SNIsSDK:                     mocks.NewMockSNIsSDK(t),
		DataPlaneCertificatesSDK:    mocks.NewMockDPCertificatesSDK(t),

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

func (m MockSDKWrapper) GetRoutesSDK() sdkkonnectgo.RoutesSDK {
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

func (m MockSDKWrapper) GetUpstreamsSDK() sdkkonnectgo.UpstreamsSDK {
	return m.UpstreamsSDK
}

func (m MockSDKWrapper) GetBasicAuthCredentialsSDK() sdkkonnectgo.BasicAuthCredentialsSDK {
	return m.KongCredentialsBasicAuthSDK
}

func (m MockSDKWrapper) GetAPIKeyCredentialsSDK() sdkkonnectgo.APIKeysSDK {
	return m.KongCredentialsAPIKeySDK
}

func (m MockSDKWrapper) GetACLCredentialsSDK() sdkkonnectgo.ACLsSDK {
	return m.KongCredentialsACLSDK
}

func (m MockSDKWrapper) GetJWTCredentialsSDK() sdkkonnectgo.JWTsSDK {
	return m.KongCredentialsJWTSDK
}

func (m MockSDKWrapper) GetHMACCredentialsSDK() sdkkonnectgo.HMACAuthCredentialsSDK {
	return m.KongCredentialsHMACSDK
}

func (m MockSDKWrapper) GetTargetsSDK() sdkkonnectgo.TargetsSDK {
	return m.TargetsSDK
}

func (m MockSDKWrapper) GetVaultSDK() sdkkonnectgo.VaultsSDK {
	return m.VaultSDK
}

func (m MockSDKWrapper) GetMeSDK() sdkkonnectgo.MeSDK {
	return m.MeSDK
}

func (m MockSDKWrapper) GetCACertificatesSDK() sdkkonnectgo.CACertificatesSDK {
	return m.CACertificatesSDK
}

func (m MockSDKWrapper) GetCertificatesSDK() sdkkonnectgo.CertificatesSDK {
	return m.CertificatesSDK
}

func (m MockSDKWrapper) GetKeysSDK() sdkkonnectgo.KeysSDK {
	return m.KeysSDK
}

func (m MockSDKWrapper) GetKeySetsSDK() sdkkonnectgo.KeySetsSDK {
	return m.KeySetsSDK
}

func (m MockSDKWrapper) GetSNIsSDK() sdkkonnectgo.SNIsSDK {
	return m.SNIsSDK
}

func (m MockSDKWrapper) GetDataPlaneCertificatesSDK() sdkkonnectgo.DPCertificatesSDK {
	return m.DataPlaneCertificatesSDK
}

func (m MockSDKWrapper) GetCloudGatewaysSDK() sdkkonnectgo.CloudGatewaysSDK {
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
