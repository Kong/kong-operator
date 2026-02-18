package address

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
)

func Test_AddressesFromService(t *testing.T) {
	tests := []struct {
		name    string
		service *corev1.Service
		want    []operatorv1beta1.Address
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
			want: []operatorv1beta1.Address{
				{
					Type:       new(operatorv1beta1.IPAddressType),
					Value:      "1.1.1.1",
					SourceType: operatorv1beta1.PublicLoadBalancerAddressSourceType,
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
			want: []operatorv1beta1.Address{
				{
					Type:       new(operatorv1beta1.IPAddressType),
					Value:      "1.1.1.1",
					SourceType: operatorv1beta1.PublicLoadBalancerAddressSourceType,
				},
				{
					Type:       new(operatorv1beta1.HostnameAddressType),
					Value:      "myhostname.com",
					SourceType: operatorv1beta1.PublicLoadBalancerAddressSourceType,
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
			want: []operatorv1beta1.Address{
				{
					Type:       new(operatorv1beta1.IPAddressType),
					Value:      "1.1.1.1",
					SourceType: operatorv1beta1.PublicLoadBalancerAddressSourceType,
				},
				{
					Type:       new(operatorv1beta1.HostnameAddressType),
					Value:      "myhostname.com",
					SourceType: operatorv1beta1.PublicLoadBalancerAddressSourceType,
				},
				{
					Type:       new(operatorv1beta1.IPAddressType),
					Value:      "2.2.2.2",
					SourceType: operatorv1beta1.PublicLoadBalancerAddressSourceType,
				},
				{
					Type:       new(operatorv1beta1.HostnameAddressType),
					Value:      "myhostname2.com",
					SourceType: operatorv1beta1.PublicLoadBalancerAddressSourceType,
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
			want: []operatorv1beta1.Address{
				{
					Type:       new(operatorv1beta1.IPAddressType),
					Value:      "1.1.1.1",
					SourceType: operatorv1beta1.PublicLoadBalancerAddressSourceType,
				},
				{
					Type:       new(operatorv1beta1.HostnameAddressType),
					Value:      "myhostname.com",
					SourceType: operatorv1beta1.PublicLoadBalancerAddressSourceType,
				},
				{
					Type:       new(operatorv1beta1.IPAddressType),
					Value:      "2.2.2.2",
					SourceType: operatorv1beta1.PublicLoadBalancerAddressSourceType,
				},
				{
					Type:       new(operatorv1beta1.HostnameAddressType),
					Value:      "myhostname2.com",
					SourceType: operatorv1beta1.PublicLoadBalancerAddressSourceType,
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
			want: []operatorv1beta1.Address{
				{
					Type:       new(operatorv1beta1.IPAddressType),
					Value:      "1.1.1.1",
					SourceType: operatorv1beta1.PublicLoadBalancerAddressSourceType,
				},
				{
					Type:       new(operatorv1beta1.HostnameAddressType),
					Value:      "myhostname.com",
					SourceType: operatorv1beta1.PublicLoadBalancerAddressSourceType,
				},
				{
					Type:       new(operatorv1beta1.IPAddressType),
					Value:      "2.2.2.2",
					SourceType: operatorv1beta1.PublicLoadBalancerAddressSourceType,
				},
				{
					Type:       new(operatorv1beta1.HostnameAddressType),
					Value:      "myhostname2.com",
					SourceType: operatorv1beta1.PublicLoadBalancerAddressSourceType,
				},
				{
					Type:       new(operatorv1beta1.IPAddressType),
					Value:      "10.0.0.1",
					SourceType: operatorv1beta1.PrivateIPAddressSourceType,
				},
				{
					Type:       new(operatorv1beta1.IPAddressType),
					Value:      "2001:0db8::1428:57ab",
					SourceType: operatorv1beta1.PrivateIPAddressSourceType,
				},
			},
		},
		{
			name: "service having AWS LB annotation indicating internal LB scheme gets detected as private LB",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						serviceAnnotationAWSLoadBalancerSchemeKey: serviceAnnotationAWSLoadBalancerSchemeInternal,
					},
				},
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{
							{
								Hostname: "qwerty.example.com",
							},
							{
								IP: "192.168.1.1",
							},
						},
					},
				},
			},
			want: []operatorv1beta1.Address{
				{
					Type:       new(operatorv1beta1.HostnameAddressType),
					Value:      "qwerty.example.com",
					SourceType: operatorv1beta1.PrivateLoadBalancerAddressSourceType,
				},
				{
					Type:       new(operatorv1beta1.IPAddressType),
					Value:      "192.168.1.1",
					SourceType: operatorv1beta1.PrivateLoadBalancerAddressSourceType,
				},
			},
		},
		{
			name: "service having AWS LB annotation indicating internet facing LB scheme gets detected as public LB",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						serviceAnnotationAWSLoadBalancerSchemeKey: serviceAnnotationAWSLoadBalancerSchemeInternetFacing,
					},
				},
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{
							{
								Hostname: "qwerty.example.com",
							},
							{
								IP: "3.4.0.1",
							},
						},
					},
				},
			},
			want: []operatorv1beta1.Address{
				{
					Type:       new(operatorv1beta1.HostnameAddressType),
					Value:      "qwerty.example.com",
					SourceType: operatorv1beta1.PublicLoadBalancerAddressSourceType,
				},
				{
					Type:       new(operatorv1beta1.IPAddressType),
					Value:      "3.4.0.1",
					SourceType: operatorv1beta1.PublicLoadBalancerAddressSourceType,
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := AddressesFromService(tt.service)
			require.NoError(t, err)
			require.Equal(t, tt.want, actual)
		})
	}
}
