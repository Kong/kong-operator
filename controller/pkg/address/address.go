package address

import (
	"fmt"
	"net/netip"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1beta1"
)

// AddressesFromService retrieves addresses from the provided service.
//
// Currently we create the return value in a way such that:
//   - service LoadBalancer addresses are added first, one by one.
//     IPs are added first, then hostnames.
//   - next, all service's ClusterIPs are added.
//   - the result is not sorted, so the return value relies on the order in
//     in which the addresses in the service were defined.
//
// If this ends up being the desired logic and aligns with what
// has been agreed in https://github.com/kong/kong-operator/issues/281
// then no action has to be taken. Otherwise this might need to be changed.
func AddressesFromService(service *corev1.Service) ([]operatorv1beta1.Address, error) {
	addresses := make([]operatorv1beta1.Address,
		0,
		len(service.Spec.ClusterIPs)+len(service.Status.LoadBalancer.Ingress),
	)

	for _, ingress := range service.Status.LoadBalancer.Ingress {
		// TODO https://github.com/kong/kong-operator-archive/issues/482 is now resolved.
		// We should take ingress.Ports into account and format the addresses accordingly.

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
					Type:       lo.ToPtr(operatorv1beta1.IPAddressType),
					Value:      ingress.IP,
					SourceType: sourceType,
				},
			)
		}
		if ingress.Hostname != "" {
			addresses = append(addresses,
				operatorv1beta1.Address{
					Type:       lo.ToPtr(operatorv1beta1.HostnameAddressType),
					Value:      ingress.Hostname,
					SourceType: deduceAddressSourceTypeFromService(service),
				},
			)
		}
	}

	for _, address := range service.Spec.ClusterIPs {
		addresses = append(addresses,
			operatorv1beta1.Address{
				Type:       lo.ToPtr(operatorv1beta1.IPAddressType),
				Value:      address,
				SourceType: operatorv1beta1.PrivateIPAddressSourceType,
			},
		)
	}
	return addresses, nil
}

const (
	// https://kubernetes-sigs.github.io/aws-load-balancer-controller/v2.6/guide/service/annotations/#lb-scheme
	serviceAnnotationAWSLoadBalancerSchemeKey            = "service.beta.kubernetes.io/aws-load-balancer-scheme"
	serviceAnnotationAWSLoadBalancerSchemeInternal       = "internal"
	serviceAnnotationAWSLoadBalancerSchemeInternetFacing = "internet-facing"
)

// deduceAddressSourceTypeFromService tried to deduce provide Service address
// source type based on Service metadata like annotations.
// It returns the deduced address source type.
func deduceAddressSourceTypeFromService(s *corev1.Service) operatorv1beta1.AddressSourceType {
	if v, ok := s.Annotations[serviceAnnotationAWSLoadBalancerSchemeKey]; ok {
		switch v {
		case serviceAnnotationAWSLoadBalancerSchemeInternal:
			return operatorv1beta1.PrivateLoadBalancerAddressSourceType
		case serviceAnnotationAWSLoadBalancerSchemeInternetFacing:
			return operatorv1beta1.PublicLoadBalancerAddressSourceType
		}
	}

	// By default, assume that a load balancer is public.
	return operatorv1beta1.PublicLoadBalancerAddressSourceType
}
