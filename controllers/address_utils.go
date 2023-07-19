package controllers

import (
	"fmt"
	"net/netip"

	corev1 "k8s.io/api/core/v1"

	operatorv1beta1 "github.com/kong/gateway-operator/apis/v1beta1"
)

// addressesFromService retrieves addreses from the provided service.
//
// Currently we create the return value in a way such that:
//   - service LoadBalancer addresses are added first, one by one.
//     IPs are added first, then hostnames.
//   - next, all service's ClusterIPs are added.
//   - the result is not sorted, so the return value relies on the order in
//     in which the addresses in the service were defined.
//
// If this ends up being the desired logic and aligns with what
// has been agreed in https://github.com/Kong/gateway-operator/issues/281
// then no action has to be taken. Otherwise this might need to be changed.
func addressesFromService(service *corev1.Service) ([]operatorv1beta1.Address, error) {
	addresses := make([]operatorv1beta1.Address,
		0,
		len(service.Spec.ClusterIPs)+len(service.Status.LoadBalancer.Ingress),
	)

	for _, ingress := range service.Status.LoadBalancer.Ingress {
		// TODO Since currently we don't support Gateway listeners spec:
		// https://github.com/Kong/gateway-operator/issues/482, we don't take into
		// account Ingress.Ports.
		// When #482 gets implemented we should take those into account and format
		// the addresses accordingly.

		if ingress.IP != "" {
			ip, err := netip.ParseAddr(ingress.IP)
			if err != nil {
				return nil, fmt.Errorf("failed parsing IP (%v) for ingress: %w", ingress.IP, err)
			}

			var sourceType operatorv1beta1.AddressSourceType

			// We check to see if the IP address is public or private. Private IP addresses
			// have limited utility today: they more or less indicate a need for special
			// knowledge of the network to do anything useful. In the future we may expand
			// private IP related functionality as needed.
			if ip.IsPrivate() {
				sourceType = operatorv1beta1.PrivateLoadBalancerAddressSourceType
			} else {
				sourceType = operatorv1beta1.PublicLoadBalancerAddressSourceType
			}

			addresses = append(addresses,
				operatorv1beta1.Address{
					Type:       addressOf(operatorv1beta1.IPAddressType),
					Value:      ingress.IP,
					SourceType: sourceType,
				},
			)
		}
		if ingress.Hostname != "" {
			addresses = append(addresses,
				operatorv1beta1.Address{
					Type:  addressOf(operatorv1beta1.HostnameAddressType),
					Value: ingress.Hostname,

					// Assume that a load balancer with a hostname is public for now. Currently
					// the operator only creates external load balancers. To determine whether a
					// hostname is for an external or internal load balancer will require additional metadata.
					SourceType: operatorv1beta1.PublicLoadBalancerAddressSourceType,
				},
			)
		}
	}

	for _, address := range service.Spec.ClusterIPs {
		addresses = append(addresses,
			operatorv1beta1.Address{
				Type:       addressOf(operatorv1beta1.IPAddressType),
				Value:      address,
				SourceType: operatorv1beta1.PrivateIPAddressSourceType,
			},
		)
	}
	return addresses, nil
}
