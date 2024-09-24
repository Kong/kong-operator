package ops

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type MockSDKWrapper struct {
	ControlPlaneSDK      *MockControlPlaneSDK
	ServicesSDK          *MockServicesSDK
	RoutesSDK            *MockRoutesSDK
	ConsumersSDK         *MockConsumersSDK
	ConsumerGroupSDK     *MockConsumerGroupSDK
	PluginSDK            *MockPluginSDK
	UpstreamsSDK         *MockUpstreamsSDK
	TargetsSDK           *MockTargetsSDK
	MeSDK                *MockMeSDK
	BasicAuthCredentials *MockKongCredentialBasicAuthSDK
	CACertificatesSDK    *MockCACertificatesSDK
}

var _ SDKWrapper = MockSDKWrapper{}

func NewMockSDKWrapperWithT(t *testing.T) *MockSDKWrapper {
	return &MockSDKWrapper{
		ControlPlaneSDK:      NewMockControlPlaneSDK(t),
		ServicesSDK:          NewMockServicesSDK(t),
		RoutesSDK:            NewMockRoutesSDK(t),
		ConsumersSDK:         NewMockConsumersSDK(t),
		ConsumerGroupSDK:     NewMockConsumerGroupSDK(t),
		PluginSDK:            NewMockPluginSDK(t),
		UpstreamsSDK:         NewMockUpstreamsSDK(t),
		TargetsSDK:           NewMockTargetsSDK(t),
		MeSDK:                NewMockMeSDK(t),
		BasicAuthCredentials: NewMockKongCredentialBasicAuthSDK(t),
		CACertificatesSDK:    NewMockCACertificatesSDK(t),
	}
}

func (m MockSDKWrapper) GetControlPlaneSDK() ControlPlaneSDK {
	return m.ControlPlaneSDK
}

func (m MockSDKWrapper) GetServicesSDK() ServicesSDK {
	return m.ServicesSDK
}

func (m MockSDKWrapper) GetRoutesSDK() RoutesSDK {
	return m.RoutesSDK
}

func (m MockSDKWrapper) GetConsumersSDK() ConsumersSDK {
	return m.ConsumersSDK
}

func (m MockSDKWrapper) GetConsumerGroupsSDK() ConsumerGroupSDK {
	return m.ConsumerGroupSDK
}

func (m MockSDKWrapper) GetPluginSDK() PluginSDK {
	return m.PluginSDK
}

func (m MockSDKWrapper) GetUpstreamsSDK() UpstreamsSDK {
	return m.UpstreamsSDK
}

func (m MockSDKWrapper) GetBasicAuthCredentials() KongCredentialBasicAuthSDK {
	return m.BasicAuthCredentials
}

func (m MockSDKWrapper) GetTargetsSDK() TargetsSDK {
	return m.TargetsSDK
}

func (m MockSDKWrapper) GetMeSDK() MeSDK {
	return m.MeSDK
}

func (m MockSDKWrapper) GetCACertificatesSDK() CACertificatesSDK {
	return m.CACertificatesSDK
}

type MockSDKFactory struct {
	t   *testing.T
	SDK *MockSDKWrapper
}

var _ SDKFactory = MockSDKFactory{}

func NewMockSDKFactory(t *testing.T) *MockSDKFactory {
	return &MockSDKFactory{
		t:   t,
		SDK: NewMockSDKWrapperWithT(t),
	}
}

func (m MockSDKFactory) NewKonnectSDK(_ string, _ SDKToken) SDKWrapper {
	require.NotNil(m.t, m.SDK)
	return *m.SDK
}
