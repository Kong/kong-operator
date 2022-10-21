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
					Type:  addressOf(operatorv1alpha1.IPAddressType),
					Value: "1.1.1.1",
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
					Type:  addressOf(operatorv1alpha1.IPAddressType),
					Value: "1.1.1.1",
				},
				{
					Type:  addressOf(operatorv1alpha1.HostnameAddressType),
					Value: "myhostname.com",
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
					Type:  addressOf(operatorv1alpha1.IPAddressType),
					Value: "1.1.1.1",
				},
				{
					Type:  addressOf(operatorv1alpha1.HostnameAddressType),
					Value: "myhostname.com",
				},
				{
					Type:  addressOf(operatorv1alpha1.IPAddressType),
					Value: "2.2.2.2",
				},
				{
					Type:  addressOf(operatorv1alpha1.HostnameAddressType),
					Value: "myhostname2.com",
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
					Type:  addressOf(operatorv1alpha1.IPAddressType),
					Value: "1.1.1.1",
				},
				{
					Type:  addressOf(operatorv1alpha1.HostnameAddressType),
					Value: "myhostname.com",
				},
				{
					Type:  addressOf(operatorv1alpha1.IPAddressType),
					Value: "2.2.2.2",
				},
				{
					Type:  addressOf(operatorv1alpha1.HostnameAddressType),
					Value: "myhostname2.com",
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
					Type:  addressOf(operatorv1alpha1.IPAddressType),
					Value: "1.1.1.1",
				},
				{
					Type:  addressOf(operatorv1alpha1.HostnameAddressType),
					Value: "myhostname.com",
				},
				{
					Type:  addressOf(operatorv1alpha1.IPAddressType),
					Value: "2.2.2.2",
				},
				{
					Type:  addressOf(operatorv1alpha1.HostnameAddressType),
					Value: "myhostname2.com",
				},
				{
					Type:  addressOf(operatorv1alpha1.IPAddressType),
					Value: "10.0.0.1",
				},
				{
					Type:  addressOf(operatorv1alpha1.IPAddressType),
					Value: "2001:0db8::1428:57ab",
				},
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, addressesFromService(tt.service))
		})
	}
}
