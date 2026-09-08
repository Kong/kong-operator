package gateway

import (
	"testing"

	"github.com/stretchr/testify/assert"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func TestProtocolPortsFromListeners(t *testing.T) {
	newSectionName := func(s string) *gatewayv1.SectionName {
		sn := gatewayv1.SectionName(s)
		return &sn
	}
	newPort := func(p gatewayv1.PortNumber) *gatewayv1.PortNumber {
		return &p
	}

	gw := &gatewayv1.Gateway{
		Name: "gw", Namespace: "ns",
		Spec: gatewayv1.GatewaySpec{
			Listeners: []gatewayv1.Listener{
				{Name: "tcp-1", Protocol: gatewayv1.TCPProtocolType, Port: 8898},
				{Name: "tcp-2", Protocol: gatewayv1.TCPProtocolType, Port: 9000},
				{Name: "http", Protocol: gatewayv1.HTTPProtocolType, Port: 80},
				{Name: "udp-1", Protocol: gatewayv1.UDPProtocolType, Port: 8898},
				{Name: "udp-2", Protocol: gatewayv1.UDPProtocolType, Port: 9000},
				{Name: "http", Protocol: gatewayv1.HTTPProtocolType, Port: 80},
				{Name: "http-additional", Protocol: gatewayv1.HTTPProtocolType, Port: 5000},
				{Name: "https", Protocol: gatewayv1.HTTPSProtocolType, Port: 443},
				{Name: "tcp", Protocol: gatewayv1.TCPProtocolType, Port: 9000},
			},
		},
	}

	tests := []struct {
		name        string
		protocols   []gatewayv1.ProtocolType
		gateway     *gatewayv1.Gateway
		sectionName *gatewayv1.SectionName
		port        *gatewayv1.PortNumber
		expected    []int32
	}{
		{
			name:      "all TCP listeners when unfiltered",
			protocols: []gatewayv1.ProtocolType{gatewayv1.TCPProtocolType},
			gateway:   gw,
			expected:  []int32{8898, 9000},
		},
		{
			name:        "filtered by sectionName",
			protocols:   []gatewayv1.ProtocolType{gatewayv1.TCPProtocolType},
			gateway:     gw,
			sectionName: newSectionName("tcp-2"),
			expected:    []int32{9000},
		},
		{
			name:      "filtered by port",
			protocols: []gatewayv1.ProtocolType{gatewayv1.TCPProtocolType},
			gateway:   gw,
			port:      newPort(8898),
			expected:  []int32{8898},
		},
		{
			name:      "non-TCP listeners are ignored",
			protocols: []gatewayv1.ProtocolType{gatewayv1.TCPProtocolType},
			gateway:   gw,
			port:      newPort(80),
			expected:  nil,
		},
		{
			name:      "no listeners returns nil",
			protocols: []gatewayv1.ProtocolType{gatewayv1.TCPProtocolType},
			gateway: &gatewayv1.Gateway{
				Name: "gw", Namespace: "ns",
			},
			expected: nil,
		},
		{
			name:      "all UDP listeners when unfiltered",
			protocols: []gatewayv1.ProtocolType{gatewayv1.UDPProtocolType},
			gateway:   gw,
			expected:  []int32{8898, 9000},
		},
		{
			name:        "filtered by sectionName",
			protocols:   []gatewayv1.ProtocolType{gatewayv1.UDPProtocolType},
			gateway:     gw,
			sectionName: newSectionName("udp-2"),
			expected:    []int32{9000},
		},
		{
			name:      "filtered by port",
			protocols: []gatewayv1.ProtocolType{gatewayv1.UDPProtocolType},
			gateway:   gw,
			port:      newPort(8898),
			expected:  []int32{8898},
		},
		{
			name:      "non-UDP listeners are ignored",
			protocols: []gatewayv1.ProtocolType{gatewayv1.UDPProtocolType},
			gateway:   gw,
			port:      newPort(80),
			expected:  nil,
		},
		{
			name:      "no listeners returns nil",
			protocols: []gatewayv1.ProtocolType{gatewayv1.UDPProtocolType},
			gateway: &gatewayv1.Gateway{
				Name: "gw", Namespace: "ns",
			},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ProtocolPortsFromListeners(tt.gateway, tt.sectionName, tt.port, tt.protocols...)
			assert.Equal(t, tt.expected, got)
		})
	}
}
