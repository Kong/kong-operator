package metricsscraper

import (
	"cmp"
	"context"
	"slices"

	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	adminapidiscovery "github.com/kong/kong-operator/v2/internal/adminapi"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

// AdminAPIAddressProvider is an interface for providing the admin API addresses for a DataPlane.
type AdminAPIAddressProvider interface {
	AdminAddressesForDP(ctx context.Context, dataplane *operatorv1beta1.DataPlane) ([]adminapidiscovery.DiscoveredAdminAPI, error)
}

type adminAPIAddressProvider struct {
	client     client.Client
	discoverer *adminapidiscovery.Discoverer
}

// NewAdminAPIAddressProvider creates a new AdminAPIAddressProvider.
func NewAdminAPIAddressProvider(cl client.Client) (*adminAPIAddressProvider, error) {
	discoverer, err := adminapidiscovery.NewDiscoverer(sets.New(consts.DataPlaneAdminServicePortName))
	if err != nil {
		return nil, err
	}
	return &adminAPIAddressProvider{
		client:     cl,
		discoverer: discoverer,
	}, nil
}

// AdminAddressesForDP returns the admin API addresses for the given DataPlane.
func (a *adminAPIAddressProvider) AdminAddressesForDP(
	ctx context.Context,
	dataplane *operatorv1beta1.DataPlane,
) ([]adminapidiscovery.DiscoveredAdminAPI, error) {
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

	var endpointsList discoveryv1.EndpointSliceList
	if err := a.client.List(ctx, &endpointsList, &client.ListOptions{
		LabelSelector: labelselector,
		Namespace:     dataplane.Namespace,
	}); err != nil {
		return nil, err
	}

	discovered := sets.New[adminapidiscovery.DiscoveredAdminAPI]()
	for _, es := range endpointsList.Items {
		// The scraper keeps its IPv4-only behavior.
		if es.AddressType != discoveryv1.AddressTypeIPv4 {
			continue
		}
		if !hasServiceOwner(es) {
			continue
		}

		// The scraper must not use unready endpoints during a rollout.
		es.Endpoints = slices.DeleteFunc(es.Endpoints, func(endpoint discoveryv1.Endpoint) bool {
			if len(endpoint.Addresses) == 0 {
				return true
			}
			if endpoint.Conditions.Terminating != nil && *endpoint.Conditions.Terminating {
				return true
			}
			if endpoint.Conditions.Ready != nil && !*endpoint.Conditions.Ready {
				return true
			}
			return endpoint.Conditions.Serving != nil && !*endpoint.Conditions.Serving
		})

		adminAPIs, err := a.discoverer.AdminAPIsFromEndpointSlice(es)
		if err != nil {
			return nil, err
		}
		discovered = discovered.Union(adminAPIs)
	}

	if discovered.Len() == 0 {
		return nil, nil
	}

	addresses := make([]adminapidiscovery.DiscoveredAdminAPI, 0, discovered.Len())
	for endpoint := range discovered {
		addresses = append(addresses, endpoint)
	}
	slices.SortFunc(addresses, func(a, b adminapidiscovery.DiscoveredAdminAPI) int {
		return cmp.Compare(a.Address, b.Address)
	})
	return addresses, nil
}

func hasServiceOwner(endpointSlice discoveryv1.EndpointSlice) bool {
	for _, owner := range endpointSlice.OwnerReferences {
		if owner.Kind == "Service" && owner.APIVersion == "v1" && owner.Name != "" {
			return true
		}
	}
	return false
}
