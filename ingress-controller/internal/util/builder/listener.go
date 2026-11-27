package builder

import (
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

// ListenerBuilder is a builder for gateway api Listener.
// Will set default values, as specified in the gateway API, for fields that are not set.
// Primarily used for testing.
type ListenerBuilder struct {
	listener gwtypes.Listener
}

func NewListener(name string) *ListenerBuilder {
	return &ListenerBuilder{
		listener: gwtypes.Listener{
			Name: gwtypes.SectionName(name),
		},
	}
}

// Build returns the configured Listener.
func (b *ListenerBuilder) Build() gwtypes.Listener {
	return b.listener
}

// IntoSlice returns the configured Listener in a slice.
func (b *ListenerBuilder) IntoSlice() []gwtypes.Listener {
	return []gwtypes.Listener{b.listener}
}

func (b *ListenerBuilder) WithPort(port int) *ListenerBuilder {
	b.listener.Port = gwtypes.PortNumber(port)
	return b
}

func (b *ListenerBuilder) HTTP() *ListenerBuilder {
	b.listener.Protocol = gwtypes.HTTPProtocolType
	return b
}

func (b *ListenerBuilder) HTTPS() *ListenerBuilder {
	b.listener.Protocol = gwtypes.HTTPSProtocolType
	return b
}

func (b *ListenerBuilder) TLS() *ListenerBuilder {
	b.listener.Protocol = gwtypes.TLSProtocolType
	return b
}

func (b *ListenerBuilder) TCP() *ListenerBuilder {
	b.listener.Protocol = gwtypes.TCPProtocolType
	return b
}

func (b *ListenerBuilder) UDP() *ListenerBuilder {
	b.listener.Protocol = gwtypes.UDPProtocolType
	return b
}

func (b *ListenerBuilder) WithHostname(hostname string) *ListenerBuilder {
	b.listener.Hostname = new(gwtypes.Hostname(hostname))
	return b
}

func (b *ListenerBuilder) WithAllowedRoutes(routes *gwtypes.AllowedRoutes) *ListenerBuilder {
	b.listener.AllowedRoutes = routes
	return b
}

func (b *ListenerBuilder) WithTLSConfig(tlsConfig *gwtypes.GatewayTLSConfig) *ListenerBuilder {
	b.listener.TLS = tlsConfig
	return b
}
