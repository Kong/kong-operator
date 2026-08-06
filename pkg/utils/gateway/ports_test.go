package gateway

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func TestTCPPortsFromListeners(t *testing.T) {
	newSectionName := func(s string) *gatewayv1.SectionName {
		sn := gatewayv1.SectionName(s)
		return &sn
	}
	newPort := func(p gatewayv1.PortNumber) *gatewayv1.PortNumber {
		return &p
	}

	gw := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "gw", Namespace: "ns"},
		Spec: gatewayv1.GatewaySpec{
			Listeners: []gatewayv1.Listener{
				{Name: "tcp-1", Protocol: gatewayv1.TCPProtocolType, Port: 8898},
				{Name: "tcp-2", Protocol: gatewayv1.TCPProtocolType, Port: 9000},
				{Name: "http", Protocol: gatewayv1.HTTPProtocolType, Port: 80},
			},
		},
	}

	tests := []struct {
		name        string
		gateway     *gatewayv1.Gateway
		sectionName *gatewayv1.SectionName
		port        *gatewayv1.PortNumber
		expected    []int32
	}{
		{
			name:     "all TCP listeners when unfiltered",
			gateway:  gw,
			expected: []int32{8898, 9000},
		},
		{
			name:        "filtered by sectionName",
			gateway:     gw,
			sectionName: newSectionName("tcp-2"),
			expected:    []int32{9000},
		},
		{
			name:     "filtered by port",
			gateway:  gw,
			port:     newPort(8898),
			expected: []int32{8898},
		},
		{
			name:     "non-TCP listeners are ignored",
			gateway:  gw,
			port:     newPort(80),
			expected: nil,
		},
		{
			name: "no listeners returns nil",
			gateway: &gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{Name: "gw", Namespace: "ns"},
			},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TCPPortsFromListeners(tt.gateway, tt.sectionName, tt.port)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestUDPPortsFromListeners(t *testing.T) {
	newSectionName := func(s string) *gatewayv1.SectionName {
		sn := gatewayv1.SectionName(s)
		return &sn
	}
	newPort := func(p gatewayv1.PortNumber) *gatewayv1.PortNumber {
		return &p
	}

	gw := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "gw", Namespace: "ns"},
		Spec: gatewayv1.GatewaySpec{
			Listeners: []gatewayv1.Listener{
				{Name: "udp-1", Protocol: gatewayv1.UDPProtocolType, Port: 8898},
				{Name: "udp-2", Protocol: gatewayv1.UDPProtocolType, Port: 9000},
				{Name: "http", Protocol: gatewayv1.HTTPProtocolType, Port: 80},
			},
		},
	}

	tests := []struct {
		name        string
		gateway     *gatewayv1.Gateway
		sectionName *gatewayv1.SectionName
		port        *gatewayv1.PortNumber
		expected    []int32
	}{
		{
			name:     "all UDP listeners when unfiltered",
			gateway:  gw,
			expected: []int32{8898, 9000},
		},
		{
			name:        "filtered by sectionName",
			gateway:     gw,
			sectionName: newSectionName("udp-2"),
			expected:    []int32{9000},
		},
		{
			name:     "filtered by port",
			gateway:  gw,
			port:     newPort(8898),
			expected: []int32{8898},
		},
		{
			name:     "non-UDP listeners are ignored",
			gateway:  gw,
			port:     newPort(80),
			expected: nil,
		},
		{
			name: "no listeners returns nil",
			gateway: &gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{Name: "gw", Namespace: "ns"},
			},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := UDPPortsFromListeners(tt.gateway, tt.sectionName, tt.port)
			assert.Equal(t, tt.expected, got)
		})
	}
}
