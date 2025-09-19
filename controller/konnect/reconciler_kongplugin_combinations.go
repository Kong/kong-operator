package konnect

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1 "github.com/kong/kong-operator/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kong-operator/api/configuration/v1beta1"
	"github.com/kong/kong-operator/controller/pkg/controlplane"
)

// ForeignRelations contains all the relations between Kong entities and KongPlugin.
type ForeignRelations struct {
	Consumer      []configurationv1.KongConsumer
	ConsumerGroup []configurationv1beta1.KongConsumerGroup
	Route         []configurationv1alpha1.KongRoute
	Service       []configurationv1alpha1.KongService
}

// ForeignRelationsGroupedByControlPlane is a map of ForeignRelations grouped by ControlPlane.
type ForeignRelationsGroupedByControlPlane map[types.NamespacedName]ForeignRelations

// GetCombinations returns all possible combinations of relations
// grouped by ControlPlane.
func (f ForeignRelationsGroupedByControlPlane) GetCombinations() map[types.NamespacedName][]Rel {
	ret := make(map[types.NamespacedName][]Rel, len(f))
	for nn, fr := range f {
		ret[nn] = fr.GetCombinations()
	}
	return ret
}

// GroupByControlPlane groups all the entities by ControlPlane.
func (relations *ForeignRelations) GroupByControlPlane(
	ctx context.Context,
	cl client.Client,
) (ForeignRelationsGroupedByControlPlane, error) {
	ret := make(map[types.NamespacedName]ForeignRelations)
	for _, service := range relations.Service {
		cpRef, ok := controlplane.GetControlPlaneRef(&service).Get()
		if !ok {
			continue
		}
		cp, err := controlplane.GetCPForRef(ctx, cl, cpRef, service.Namespace)
		if err != nil {
			return nil, fmt.Errorf("failed to get ControlPlane for KongService %s: %w", service.Name, err)
		}
		nn := types.NamespacedName{
			// TODO: implement cross namespace references
			Namespace: service.Namespace,
			Name:      cp.Name,
		}
		fr := ret[nn]
		fr.Service = append(fr.Service, service)
		ret[nn] = fr
	}
	for _, route := range relations.Route {
		svcRef := route.Spec.ServiceRef
		if svcRef == nil || svcRef.NamespacedRef == nil {
			cpRef, ok := controlplane.GetControlPlaneRef(&route).Get()
			if !ok {
				continue
			}
			cp, err := controlplane.GetCPForRef(ctx, cl, cpRef, route.Namespace)
			if err != nil {
				return nil, fmt.Errorf("failed to get ControlPlane for KongRoute %s: %w", route.Name, err)
			}

			nn := types.NamespacedName{
				Namespace: route.Namespace,
				Name:      cp.Name,
			}
			fr := ret[nn]
			fr.Route = append(fr.Route, route)
			ret[nn] = fr

			continue
		}

		svc := configurationv1alpha1.KongService{}
		err := cl.Get(ctx,
			types.NamespacedName{
				Namespace: route.Namespace,
				Name:      svcRef.NamespacedRef.Name,
			}, &svc,
		)
		if err != nil {
			return nil, err
		}

		cpRef, ok := controlplane.GetControlPlaneRef(&svc).Get()
		if !ok {
			continue
		}
		cp, err := controlplane.GetCPForRef(ctx, cl, cpRef, svc.Namespace)
		if err != nil {
			return nil, fmt.Errorf("failed to get ControlPlane for KongService %s: %w", svc.Name, err)
		}

		nn := types.NamespacedName{
			// TODO: implement cross namespace references
			Namespace: route.Namespace,
			Name:      cp.Name,
		}
		fr := ret[nn]
		fr.Route = append(fr.Route, route)
		ret[nn] = fr
	}
	for _, consumer := range relations.Consumer {
		cpRef, ok := controlplane.GetControlPlaneRef(&consumer).Get()
		if !ok {
			continue
		}
		cp, err := controlplane.GetCPForRef(ctx, cl, cpRef, consumer.Namespace)
		if err != nil {
			return nil, fmt.Errorf("failed to get ControlPlane for KongConsumer %s: %w", consumer.Name, err)
		}
		nn := types.NamespacedName{
			// TODO: implement cross namespace references
			Namespace: consumer.Namespace,
			Name:      cp.Name,
		}
		fr := ret[nn]
		fr.Consumer = append(fr.Consumer, consumer)
		ret[nn] = fr
	}
	for _, group := range relations.ConsumerGroup {
		cpRef, ok := controlplane.GetControlPlaneRef(&group).Get()
		if !ok {
			continue
		}
		cp, err := controlplane.GetCPForRef(ctx, cl, cpRef, group.Namespace)
		if err != nil {
			return nil, fmt.Errorf("failed to get ControlPlane for KongConsumerGroup %s: %w", group.Name, err)
		}
		nn := types.NamespacedName{
			// TODO: implement cross namespace references
			Namespace: group.Namespace,
			Name:      cp.Name,
		}
		fr := ret[nn]
		fr.ConsumerGroup = append(fr.ConsumerGroup, group)
		ret[nn] = fr
	}

	return ret, nil
}

// Rel represents a relation between Kong entities and KongPlugin.
type Rel struct {
	Consumer, ConsumerGroup, Route, Service string
}

// GetCombinations returns all possible combinations of relations.
//
// NOTE: This is heavily based on the implementation in the Kong Ingress Controller:
// https://github.com/Kong/kubernetes-ingress-controller/blob/ee797b4e84bd176526af32ab6db54f16ee9c245b/internal/util/relations_test.go
//
// TODO: https://github.com/kong/kong-operator/pull/659
// The combinations created here should be reconsidered.
// Specifically the Service + Route combination which currently creates 2 separate relations targeting
// Service and Route independently.
// This most likely should create 2 relations targeting Service and Service+Route.
func (relations *ForeignRelations) GetCombinations() []Rel {
	var (
		lConsumer      = len(relations.Consumer)
		lConsumerGroup = len(relations.ConsumerGroup)
		lRoutes        = len(relations.Route)
		lServices      = len(relations.Service)
		l              = lRoutes + lServices
	)

	var cartesianProduct []Rel

	if lConsumer > 0 { //nolint:gocritic
		if l > 0 {
			cartesianProduct = make([]Rel, 0, l*lConsumer)

			for _, consumer := range relations.Consumer {
				for _, route := range relations.Route {
					cartesianProduct = append(cartesianProduct, Rel{
						Route:    route.Name,
						Consumer: consumer.Name,
					})
				}
				for _, service := range relations.Service {
					cartesianProduct = append(cartesianProduct, Rel{
						Service:  service.Name,
						Consumer: consumer.Name,
					})
				}
			}
		} else {
			cartesianProduct = make([]Rel, 0, len(relations.Consumer))
			for _, consumer := range relations.Consumer {
				cartesianProduct = append(cartesianProduct, Rel{Consumer: consumer.Name})
			}
		}
	} else if lConsumerGroup > 0 {
		if l > 0 {
			cartesianProduct = make([]Rel, 0, l*lConsumerGroup)

			for _, group := range relations.ConsumerGroup {
				for _, route := range relations.Route {
					cartesianProduct = append(cartesianProduct, Rel{
						Route:         route.Name,
						ConsumerGroup: group.Name,
					})
				}
				for _, service := range relations.Service {
					cartesianProduct = append(cartesianProduct, Rel{
						Service:       service.Name,
						ConsumerGroup: group.Name,
					})
				}
			}

		} else {
			cartesianProduct = make([]Rel, 0, lConsumerGroup)
			for _, group := range relations.ConsumerGroup {
				cartesianProduct = append(cartesianProduct, Rel{
					ConsumerGroup: group.Name,
				})
			}
		}
	} else if l > 0 {
		cartesianProduct = make([]Rel, 0, l)
		for _, route := range relations.Route {
			cartesianProduct = append(cartesianProduct, Rel{
				Route: route.Name,
			})
		}
		for _, service := range relations.Service {
			cartesianProduct = append(cartesianProduct, Rel{
				Service: service.Name,
			})
		}
	}

	return cartesianProduct
}
