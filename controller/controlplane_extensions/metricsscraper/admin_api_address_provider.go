package metricsscraper

import (
	"context"
	"fmt"
	"strings"

	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1beta1"

	"github.com/kong/kong-operator/pkg/consts"
)

// AdminAPIAddressProvider is an interface for providing the admin API addresses for a DataPlane.
type AdminAPIAddressProvider interface {
	AdminAddressesForDP(ctx context.Context, dataplane *operatorv1beta1.DataPlane) ([]string, error)
}

type adminAPIAddressProvider struct {
	client client.Client
}

// NewAdminAPIAddressProvider creates a new AdminAPIAddressProvider.
func NewAdminAPIAddressProvider(cl client.Client) *adminAPIAddressProvider {
	return &adminAPIAddressProvider{
		client: cl,
	}
}

// AdminAddressesForDP returns the admin API addresses for the given DataPlane.
func (a *adminAPIAddressProvider) AdminAddressesForDP(ctx context.Context, dataplane *operatorv1beta1.DataPlane) ([]string, error) {
	labelReqs := []struct {
		labelName string
		selector  selection.Operator
		values    []string
	}{
		{
			labelName: consts.DataPlaneServiceStateLabel,
			selector:  selection.Equals,
			values:    []string{consts.DataPlaneStateLabelValueLive},
		},
		{
			labelName: consts.DataPlaneServiceTypeLabel,
			selector:  selection.Equals,
			values:    []string{string(consts.DataPlaneAdminServiceLabelValue)},
		},
		{
			labelName: consts.GatewayOperatorManagedByLabel,
			selector:  selection.Equals,
			values:    []string{consts.DataPlaneManagedLabelValue},
		},
		{
			labelName: "app",
			selector:  selection.Equals,
			values:    []string{dataplane.Name},
		},
	}

	labelselector := labels.NewSelector()
	for _, req := range labelReqs {
		labelReq, err := labels.NewRequirement(req.labelName, req.selector, req.values)
		if err != nil {
			return nil, err
		}
		labelselector = labelselector.Add(*labelReq)
	}

	var (
		endpointsList discoveryv1.EndpointSliceList
		urls          []string
	)
	if err := a.client.List(ctx, &endpointsList, &client.ListOptions{
		LabelSelector: labelselector,
		Namespace:     dataplane.Namespace,
	}); err != nil {
		return nil, err
	}

	for _, es := range endpointsList.Items {
		var serviceName string
		for _, or := range es.OwnerReferences {
			if or.Kind == "Service" && or.APIVersion == "v1" {
				serviceName = or.Name
				break
			}
		}
		if serviceName == "" {
			continue
		}

		for _, port := range es.Ports {
			if port.Port == nil {
				continue
			}

			for _, endpoint := range es.Endpoints {
				if len(endpoint.Addresses) == 0 {
					continue
				}
				if endpoint.Conditions.Terminating != nil && *endpoint.Conditions.Terminating {
					continue
				}
				if endpoint.Conditions.Ready != nil && !*endpoint.Conditions.Ready {
					continue
				}
				if endpoint.Conditions.Serving != nil && !*endpoint.Conditions.Serving {
					continue
				}

				svc := k8stypes.NamespacedName{
					Name:      serviceName,
					Namespace: es.Namespace,
				}

				// TODO: support IPv6
				if es.AddressType != discoveryv1.AddressTypeIPv4 {
					continue
				}

				url, err := adminAPIFromEndpoint(endpoint, port, svc)
				if err != nil {
					return nil, err
				}

				urls = append(urls, url.Address)
			}
		}
	}

	return urls, nil
}

// DiscoveredAdminAPI represents an Admin API discovered from a Kubernetes Service.
type DiscoveredAdminAPI struct {
	// Address is the Admin API's URL reachable from within the cluster.
	Address string
}

func adminAPIFromEndpoint(
	endpoint discoveryv1.Endpoint,
	port discoveryv1.EndpointPort,
	service k8stypes.NamespacedName,
) (DiscoveredAdminAPI, error) {
	// NOTE: Endpoint's addresses are assumed to be fungible, therefore we pick
	// only the first one.
	// For the context please see the `Endpoint.Addresses` godoc.
	eAddress := endpoint.Addresses[0]

	// NOTE: We assume https below because the referenced Admin API
	// server will live in another Pod/elsewhere so allowing http would
	// not be considered best practice.

	if service.Name == "" {
		return DiscoveredAdminAPI{}, fmt.Errorf(
			"service name is empty for an endpoint with TargetRef %s/%s",
			endpoint.TargetRef.Namespace, endpoint.TargetRef.Name,
		)
	}

	// NOTE: This uses the service scoped Pod DNS strategy similar to the one
	// used by KIC but in here that's the only one available because when the
	// operator generates the DataPlane certifcates it uses only the DNS names
	// and not IPs (primarily because the IPs are not known at the time of creation
	// and it would be infeasible to reissue the certificates every time the IPs
	// change).
	// Alternatively this could use the namespace scope Pod DNS strategy but that
	// would be less secure (only namespace scoped, not bound to DataPlane Service)
	// and it would require change in the DataPlane certificate generation logic.
	// Hence for now we only allow service scoped DNS names.

	ipAddr := strings.ReplaceAll(eAddress, ".", "-")
	address := fmt.Sprintf("%s.%s.%s.svc", ipAddr, service.Name, service.Namespace)

	return DiscoveredAdminAPI{
		Address: fmt.Sprintf("https://%s:%d", address, *port.Port),
	}, nil
}
