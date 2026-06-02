package admission

import (
	"github.com/kong/go-kong/kong"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/adminapi"
)

// GatewayClientsProvider returns the most recent set of Gateway Admin API clients.
type GatewayClientsProvider interface {
	GatewayClients() []*adminapi.Client
}

// DefaultAdminAPIServicesProvider allows getting Admin API services that require having at least one Gateway discovered.
// In the case there's no Gateways, it will return `false` from every method, signalling there's no Gateway available.
type DefaultAdminAPIServicesProvider struct {
	gatewayClientsProvider GatewayClientsProvider
}

func NewDefaultAdminAPIServicesProvider(gatewaysProvider GatewayClientsProvider) *DefaultAdminAPIServicesProvider {
	return &DefaultAdminAPIServicesProvider{gatewayClientsProvider: gatewaysProvider}
}

func (p DefaultAdminAPIServicesProvider) GetConsumersService() (kong.AbstractConsumerService, bool) {
	c, ok := p.designatedAdminAPIClient()
	if !ok {
		return nil, ok
	}
	return c.Consumers, true
}

func (p DefaultAdminAPIServicesProvider) GetPluginsService() (kong.AbstractPluginService, bool) {
	c, ok := p.designatedAdminAPIClient()
	if !ok {
		return nil, ok
	}
	return c.Plugins, true
}

func (p DefaultAdminAPIServicesProvider) GetPluginsServices() []kong.AbstractPluginService {
	gwClients := p.gatewayClientsProvider.GatewayClients()
	services := make([]kong.AbstractPluginService, 0, len(gwClients))
	for _, c := range gwClients {
		services = append(services, c.AdminAPIClient().Plugins)
	}
	return services
}

func (p DefaultAdminAPIServicesProvider) GetConsumerGroupsService() (kong.AbstractConsumerGroupService, bool) {
	c, ok := p.designatedAdminAPIClient()
	if !ok {
		return nil, ok
	}
	return c.ConsumerGroups, true
}

func (p DefaultAdminAPIServicesProvider) GetInfoService() (kong.AbstractInfoService, bool) {
	c, ok := p.designatedAdminAPIClient()
	if !ok {
		return nil, ok
	}
	return c.Info, true
}

func (p DefaultAdminAPIServicesProvider) GetRoutesService() (kong.AbstractRouteService, bool) {
	c, ok := p.designatedAdminAPIClient()
	if !ok {
		return nil, ok
	}
	return c.Routes, true
}

func (p DefaultAdminAPIServicesProvider) GetVaultsService() (kong.AbstractVaultService, bool) {
	c, ok := p.designatedAdminAPIClient()
	if !ok {
		return nil, ok
	}
	return c.Vaults, true
}

func (p DefaultAdminAPIServicesProvider) GetSchemasService() (kong.AbstractSchemaService, bool) {
	c, ok := p.designatedAdminAPIClient()
	if !ok {
		return nil, ok
	}
	return c.Schemas, true
}

func (p DefaultAdminAPIServicesProvider) designatedAdminAPIClient() (*kong.Client, bool) {
	gwClients := p.gatewayClientsProvider.GatewayClients()
	if len(gwClients) == 0 {
		return nil, false
	}

	// Uses the first discovered gateway for Admin API calls (consumers, routes, etc.).
	// KongPlugin admission can opt into multi-gateway plugin service selection through
	// the optional GetPluginsServices() extension.
	return gwClients[0].AdminAPIClient(), true
}
