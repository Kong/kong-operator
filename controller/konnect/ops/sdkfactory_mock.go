package ops

type MockSDKWrapper struct {
	ControlPlaneSDK  *MockControlPlaneSDK
	ServicesSDK      *MockServicesSDK
	RoutesSDK        *MockRoutesSDK
	ConsumersSDK     *MockConsumersSDK
	ConsumerGroupSDK *MockConsumerGroupSDK
	PluginSDK        *MockPluginSDK
	MeSDK            *MockMeSDK
}

var _ SDKWrapper = MockSDKWrapper{}

func NewMockSDKWrapper() *MockSDKWrapper {
	return &MockSDKWrapper{
		ControlPlaneSDK:  &MockControlPlaneSDK{},
		ServicesSDK:      &MockServicesSDK{},
		RoutesSDK:        &MockRoutesSDK{},
		ConsumersSDK:     &MockConsumersSDK{},
		ConsumerGroupSDK: &MockConsumerGroupSDK{},
		PluginSDK:        &MockPluginSDK{},
		MeSDK:            &MockMeSDK{},
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

func (m MockSDKWrapper) GetMeSDK() MeSDK {
	return m.MeSDK
}

type MockSDKFactory struct {
	Wapper *MockSDKWrapper
}

var _ SDKFactory = MockSDKFactory{}

func (m MockSDKFactory) NewKonnectSDK(_ string, _ SDKToken) SDKWrapper {
	if m.Wapper != nil {
		return *m.Wapper
	}
	return MockSDKWrapper{
		ControlPlaneSDK:  &MockControlPlaneSDK{},
		ServicesSDK:      &MockServicesSDK{},
		RoutesSDK:        &MockRoutesSDK{},
		ConsumersSDK:     &MockConsumersSDK{},
		ConsumerGroupSDK: &MockConsumerGroupSDK{},
		PluginSDK:        &MockPluginSDK{},
		MeSDK:            &MockMeSDK{},
	}
}
