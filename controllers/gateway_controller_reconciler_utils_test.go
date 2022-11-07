package controllers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	gwtypes "github.com/kong/gateway-operator/internal/types"
)

func TestParseKongProxyListenEnv(t *testing.T) {
	testcases := []struct {
		Name            string
		KongProxyListen string
		Expected        KongListenConfig
	}{
		{
			Name:            "basic http",
			KongProxyListen: "0.0.0.0:8001 reuseport backlog=16384",
			Expected: KongListenConfig{
				Endpoint: &proxyListenEndpoint{
					Address: "0.0.0.0",
					Port:    8001,
				},
			},
		},
		{
			Name:            "basic https",
			KongProxyListen: "0.0.0.0:8443 http2 ssl reuseport backlog=16384",
			Expected: KongListenConfig{
				SSLEndpoint: &proxyListenEndpoint{
					Address: "0.0.0.0",
					Port:    8443,
				},
			},
		},
		{
			Name:            "basic http + https",
			KongProxyListen: "0.0.0.0:8001 reuseport backlog=16384, 0.0.0.0:8443 http2 ssl reuseport backlog=16384",
			Expected: KongListenConfig{
				Endpoint: &proxyListenEndpoint{
					Address: "0.0.0.0",
					Port:    8001,
				},
				SSLEndpoint: &proxyListenEndpoint{
					Address: "0.0.0.0",
					Port:    8443,
				},
			},
		},
	}

	for _, tc := range testcases {
		tc := tc

		t.Run(tc.Name, func(t *testing.T) {
			actual, err := parseKongListenEnv(tc.KongProxyListen)
			require.NoError(t, err)
			require.Equal(t, tc.Expected, actual)
		})
	}
}

func TestGatewayAddressesFromService(t *testing.T) {
	testCases := []struct {
		name      string
		svc       corev1.Service
		addresses []gwtypes.GatewayAddress
		wantErr   bool
	}{
		{
			name: "ClusterIP Service",
			svc: corev1.Service{
				Spec: corev1.ServiceSpec{
					Type:      "ClusterIP",
					ClusterIP: "198.51.100.1",
				},
			},
			addresses: []gwtypes.GatewayAddress{
				{
					Value: "198.51.100.1",
					Type:  &IPAddressType,
				},
			},
			wantErr: false,
		},
		{
			name: "ClusterIP Service without ClusterIP",
			svc: corev1.Service{
				Spec: corev1.ServiceSpec{
					Type: "ClusterIP",
				},
			},
			addresses: []gwtypes.GatewayAddress{},
			wantErr:   true,
		},
		{
			name: "LoadBalancer with IP addresses",
			svc: corev1.Service{
				Spec: corev1.ServiceSpec{
					Type:      "LoadBalancer",
					ClusterIP: "198.51.100.1",
				},
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{
							{
								IP: "203.0.113.1",
							},
							{
								IP: "203.0.113.2",
							},
						},
					},
				},
			},
			addresses: []gwtypes.GatewayAddress{
				{
					Value: "203.0.113.1",
					Type:  &IPAddressType,
				},
				{
					Value: "203.0.113.2",
					Type:  &IPAddressType,
				},
			},
			wantErr: false,
		},
		{
			name: "LoadBalancer with hostnames",
			svc: corev1.Service{
				Spec: corev1.ServiceSpec{
					Type:      "LoadBalancer",
					ClusterIP: "198.51.100.1",
				},
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{
							{
								Hostname: "one.example.net",
							},
							{
								Hostname: "two.example.net",
							},
						},
					},
				},
			},
			addresses: []gwtypes.GatewayAddress{
				{
					Value: "one.example.net",
					Type:  &HostnameAddressType,
				},
				{
					Value: "two.example.net",
					Type:  &HostnameAddressType,
				},
			},
			wantErr: false,
		},
		{
			name: "LoadBalancer with both IP and hostname in one status entry",
			svc: corev1.Service{
				Spec: corev1.ServiceSpec{
					Type:      "LoadBalancer",
					ClusterIP: "198.51.100.1",
				},
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{
							{
								IP:       "203.0.113.1",
								Hostname: "one.example.net",
							},
							{
								Hostname: "two.example.net",
							},
						},
					},
				},
			},
			addresses: []gwtypes.GatewayAddress{
				{
					Value: "203.0.113.1",
					Type:  &IPAddressType,
				},
				{
					Value: "one.example.net",
					Type:  &HostnameAddressType,
				},
				{
					Value: "two.example.net",
					Type:  &HostnameAddressType,
				},
			},
			wantErr: false,
		},
		{
			name: "LoadBalancer has status entries with neither hostname nor IP",
			svc: corev1.Service{
				Spec: corev1.ServiceSpec{
					Type:      "LoadBalancer",
					ClusterIP: "198.51.100.1",
				},
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{
							{},
						},
					},
				},
			},
			addresses: []gwtypes.GatewayAddress{},
			wantErr:   false,
		},
		{
			name: "LoadBalancer has no status entries",
			svc: corev1.Service{
				Spec: corev1.ServiceSpec{
					Type:      "LoadBalancer",
					ClusterIP: "198.51.100.1",
				},
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{},
				},
			},
			addresses: []gwtypes.GatewayAddress{},
			wantErr:   false,
		},
	}
	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			addresses, err := gatewayAddressesFromService(tc.svc)
			assert.Equal(t, tc.wantErr, err != nil)
			require.Equal(t, addresses, tc.addresses)
		})
	}
}
