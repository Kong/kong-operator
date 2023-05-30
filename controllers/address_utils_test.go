package controllers

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
)

func Test_addressesFromService(t *testing.T) {
	tests := []struct {
		name    string
		service *corev1.Service
		want    []operatorv1alpha1.Address
	}{
		{
			name: "1 load balancer IP address",
			service: &corev1.Service{
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{
							{
								IP: "1.1.1.1",
							},
						},
					},
				},
			},
			want: []operatorv1alpha1.Address{
				{
					Type:       addressOf(operatorv1alpha1.IPAddressType),
					Value:      "1.1.1.1",
					SourceType: operatorv1alpha1.PublicLoadBalancerAddressSourceType,
				},
			},
		},
		{
			name: "1 load balancer IP address and 1 hostname",
			service: &corev1.Service{
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{
							{
								IP: "1.1.1.1",
							},
							{
								Hostname: "myhostname.com",
							},
						},
					},
				},
			},
			want: []operatorv1alpha1.Address{
				{
					Type:       addressOf(operatorv1alpha1.IPAddressType),
					Value:      "1.1.1.1",
					SourceType: operatorv1alpha1.PublicLoadBalancerAddressSourceType,
				},
				{
					Type:       addressOf(operatorv1alpha1.HostnameAddressType),
					Value:      "myhostname.com",
					SourceType: operatorv1alpha1.PublicLoadBalancerAddressSourceType,
				},
			},
		},
		{
			name: "2 load balancer IP addresses and 2 hostnames",
			service: &corev1.Service{
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{
							{
								IP: "1.1.1.1",
							},
							{
								Hostname: "myhostname.com",
							},
							{
								IP: "2.2.2.2",
							},
							{
								Hostname: "myhostname2.com",
							},
						},
					},
				},
			},
			want: []operatorv1alpha1.Address{
				{
					Type:       addressOf(operatorv1alpha1.IPAddressType),
					Value:      "1.1.1.1",
					SourceType: operatorv1alpha1.PublicLoadBalancerAddressSourceType,
				},
				{
					Type:       addressOf(operatorv1alpha1.HostnameAddressType),
					Value:      "myhostname.com",
					SourceType: operatorv1alpha1.PublicLoadBalancerAddressSourceType,
				},
				{
					Type:       addressOf(operatorv1alpha1.IPAddressType),
					Value:      "2.2.2.2",
					SourceType: operatorv1alpha1.PublicLoadBalancerAddressSourceType,
				},
				{
					Type:       addressOf(operatorv1alpha1.HostnameAddressType),
					Value:      "myhostname2.com",
					SourceType: operatorv1alpha1.PublicLoadBalancerAddressSourceType,
				},
			},
		},
		{
			name: "2 load balancer IP addresses and 2 hostnames in 2 entries",
			service: &corev1.Service{
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{
							{
								IP:       "1.1.1.1",
								Hostname: "myhostname.com",
							},
							{
								IP:       "2.2.2.2",
								Hostname: "myhostname2.com",
							},
						},
					},
				},
			},
			want: []operatorv1alpha1.Address{
				{
					Type:       addressOf(operatorv1alpha1.IPAddressType),
					Value:      "1.1.1.1",
					SourceType: operatorv1alpha1.PublicLoadBalancerAddressSourceType,
				},
				{
					Type:       addressOf(operatorv1alpha1.HostnameAddressType),
					Value:      "myhostname.com",
					SourceType: operatorv1alpha1.PublicLoadBalancerAddressSourceType,
				},
				{
					Type:       addressOf(operatorv1alpha1.IPAddressType),
					Value:      "2.2.2.2",
					SourceType: operatorv1alpha1.PublicLoadBalancerAddressSourceType,
				},
				{
					Type:       addressOf(operatorv1alpha1.HostnameAddressType),
					Value:      "myhostname2.com",
					SourceType: operatorv1alpha1.PublicLoadBalancerAddressSourceType,
				},
			},
		},
		{
			name: "1 load balancer IP address and 1 hostname and 2 ClusterIPs",
			service: &corev1.Service{
				Spec: corev1.ServiceSpec{
					ClusterIP: "10.0.0.1",
					ClusterIPs: []string{
						"10.0.0.1",
						"2001:0db8::1428:57ab",
					},
				},
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{
							{
								IP: "1.1.1.1",
							},
							{
								Hostname: "myhostname.com",
							},
							{
								IP: "2.2.2.2",
							},
							{
								Hostname: "myhostname2.com",
							},
						},
					},
				},
			},
			want: []operatorv1alpha1.Address{
				{
					Type:       addressOf(operatorv1alpha1.IPAddressType),
					Value:      "1.1.1.1",
					SourceType: operatorv1alpha1.PublicLoadBalancerAddressSourceType,
				},
				{
					Type:       addressOf(operatorv1alpha1.HostnameAddressType),
					Value:      "myhostname.com",
					SourceType: operatorv1alpha1.PublicLoadBalancerAddressSourceType,
				},
				{
					Type:       addressOf(operatorv1alpha1.IPAddressType),
					Value:      "2.2.2.2",
					SourceType: operatorv1alpha1.PublicLoadBalancerAddressSourceType,
				},
				{
					Type:       addressOf(operatorv1alpha1.HostnameAddressType),
					Value:      "myhostname2.com",
					SourceType: operatorv1alpha1.PublicLoadBalancerAddressSourceType,
				},
				{
					Type:       addressOf(operatorv1alpha1.IPAddressType),
					Value:      "10.0.0.1",
					SourceType: operatorv1alpha1.PrivateIPAddressSourceType,
				},
				{
					Type:       addressOf(operatorv1alpha1.IPAddressType),
					Value:      "2001:0db8::1428:57ab",
					SourceType: operatorv1alpha1.PrivateIPAddressSourceType,
				},
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			actual, err := addressesFromService(tt.service)
			require.NoError(t, err)
			require.Equal(t, tt.want, actual)
		})
	}
}
