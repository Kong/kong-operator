package sdkmocks

import (
	"context"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/stretchr/testify/mock"

	sdkops "github.com/kong/kong-operator/v2/controller/konnect/ops/sdk"
)

type MockEventGatewayDataPlaneCertificatesSDK struct {
	mock.Mock
}

var _ sdkops.EventGatewayDataPlaneCertificatesSDK = (*MockEventGatewayDataPlaneCertificatesSDK)(nil)

func NewMockEventGatewayDataPlaneCertificatesSDK(t *testing.T) *MockEventGatewayDataPlaneCertificatesSDK {
	m := &MockEventGatewayDataPlaneCertificatesSDK{}
	m.Test(t)
	t.Cleanup(func() {
		m.AssertExpectations(t)
	})
	return m
}

func (m *MockEventGatewayDataPlaneCertificatesSDK) ListEventGatewayDataPlaneCertificates(
	ctx context.Context,
	request sdkkonnectops.ListEventGatewayDataPlaneCertificatesRequest,
	_ ...sdkkonnectops.Option,
) (*sdkkonnectops.ListEventGatewayDataPlaneCertificatesResponse, error) {
	args := m.Called(ctx, request)
	resp, _ := args.Get(0).(*sdkkonnectops.ListEventGatewayDataPlaneCertificatesResponse)
	return resp, args.Error(1)
}

func (m *MockEventGatewayDataPlaneCertificatesSDK) CreateEventGatewayDataPlaneCertificate(
	ctx context.Context,
	gatewayID string,
	request *sdkkonnectcomp.CreateEventGatewayDataPlaneCertificateRequest,
	_ ...sdkkonnectops.Option,
) (*sdkkonnectops.CreateEventGatewayDataPlaneCertificateResponse, error) {
	args := m.Called(ctx, gatewayID, request)
	resp, _ := args.Get(0).(*sdkkonnectops.CreateEventGatewayDataPlaneCertificateResponse)
	return resp, args.Error(1)
}

func (m *MockEventGatewayDataPlaneCertificatesSDK) UpdateEventGatewayDataPlaneCertificate(
	ctx context.Context,
	request sdkkonnectops.UpdateEventGatewayDataPlaneCertificateRequest,
	_ ...sdkkonnectops.Option,
) (*sdkkonnectops.UpdateEventGatewayDataPlaneCertificateResponse, error) {
	args := m.Called(ctx, request)
	resp, _ := args.Get(0).(*sdkkonnectops.UpdateEventGatewayDataPlaneCertificateResponse)
	return resp, args.Error(1)
}

func (m *MockEventGatewayDataPlaneCertificatesSDK) DeleteEventGatewayDataPlaneCertificate(
	ctx context.Context,
	gatewayID string,
	certificateID string,
	_ ...sdkkonnectops.Option,
) (*sdkkonnectops.DeleteEventGatewayDataPlaneCertificateResponse, error) {
	args := m.Called(ctx, gatewayID, certificateID)
	resp, _ := args.Get(0).(*sdkkonnectops.DeleteEventGatewayDataPlaneCertificateResponse)
	return resp, args.Error(1)
}
