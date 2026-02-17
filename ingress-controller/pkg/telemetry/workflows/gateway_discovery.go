package workflows

import (
	"context"
	"errors"
	"fmt"

	"github.com/kong/kubernetes-telemetry/pkg/provider"
	"github.com/kong/kubernetes-telemetry/pkg/telemetry"
	"github.com/kong/kubernetes-telemetry/pkg/types"
)

const gatewayDiscoveryWorkflowName = "gateway_discovery"

// DiscoveredGatewaysCounter is an interface that allows to count currently discovered Gateways.
type DiscoveredGatewaysCounter interface {
	GatewayClientsCount() int
}

// NewGatewayDiscoveryWorkflow creates a new telemetry workflow that reports the number of currently discovered Gateways.
func NewGatewayDiscoveryWorkflow(gatewaysCounter DiscoveredGatewaysCounter) (telemetry.Workflow, error) {
	w := telemetry.NewWorkflow(gatewayDiscoveryWorkflowName)

	discoveredGatewaysCountProvider, err := newDiscoveredGatewaysCountProvider(gatewaysCounter)
	if err != nil {
		return nil, fmt.Errorf("failed to create discovered gateways count provider: %w", err)
	}
	w.AddProvider(discoveredGatewaysCountProvider)

	return w, nil
}

// discoveredGatewaysCountProvider is a provider that reports the number of currently discovered Gateways.
type discoveredGatewaysCountProvider struct {
	counter DiscoveredGatewaysCounter
}

var _ provider.Provider = (*discoveredGatewaysCountProvider)(nil)

func newDiscoveredGatewaysCountProvider(counter DiscoveredGatewaysCounter) (*discoveredGatewaysCountProvider, error) {
	if counter == nil {
		return nil, errors.New("discovered gateways counter is required")
	}

	return &discoveredGatewaysCountProvider{counter: counter}, nil
}

const (
	discoveredGatewaysCountProviderName = "discovered_gateways_count"
	discoveredGatewaysCountProviderKind = provider.Kind(discoveredGatewaysCountProviderName)
	discoveredGatewaysCountKey          = types.ProviderReportKey(discoveredGatewaysCountProviderName)
)

// Name returns the name of the provider.
func (d *discoveredGatewaysCountProvider) Name() string {
	return discoveredGatewaysCountProviderName
}

// Kind returns the kind of the provider.
func (d *discoveredGatewaysCountProvider) Kind() provider.Kind {
	return discoveredGatewaysCountProviderKind
}

// Provide returns the number of currently discovered Gateways as a provider report.
func (d *discoveredGatewaysCountProvider) Provide(context.Context) (types.ProviderReport, error) {
	return types.ProviderReport{
		discoveredGatewaysCountKey: d.counter.GatewayClientsCount(),
	}, nil
}
