package metricsmocks

import (
	"context"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1beta1"

	"github.com/kong/kong-operator/internal/metrics"
)

type MockAdminAPIAddressProvider struct {
	Addresses []string
}

func (m *MockAdminAPIAddressProvider) AdminAddressesForDP(ctx context.Context, dataplane *operatorv1beta1.DataPlane) ([]string, error) {
	return m.Addresses, nil
}
