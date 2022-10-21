package controllers

import (
	corev1 "k8s.io/api/core/v1"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
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
func addressesFromService(service *corev1.Service) []operatorv1alpha1.Address {
	addresses := make([]operatorv1alpha1.Address,
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
			addresses = append(addresses,
				operatorv1alpha1.Address{
					Type:  addressOf(operatorv1alpha1.IPAddressType),
					Value: ingress.IP,
				},
			)
		}
		if ingress.Hostname != "" {
			addresses = append(addresses,
				operatorv1alpha1.Address{
					Type:  addressOf(operatorv1alpha1.HostnameAddressType),
					Value: ingress.Hostname,
				},
			)
		}
	}

	for _, address := range service.Spec.ClusterIPs {
		addresses = append(addresses,
			operatorv1alpha1.Address{
				Type:  addressOf(operatorv1alpha1.IPAddressType),
				Value: address,
			},
		)
	}
	return addresses
}
