package konnect

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kubernetes-configuration/api/configuration/v1beta1"
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

// GetCombinations groups all the entities by ControlPlane.
// NOTE: currently only supports Konnect ControlPlane which is referenced by a konnectNamespacedRef.
func (relations *ForeignRelations) GroupByControlPlane(
	ctx context.Context,
	cl client.Client,
) (ForeignRelationsGroupedByControlPlane, error) {
	ret := make(map[types.NamespacedName]ForeignRelations)
	for _, service := range relations.Service {
		cpRef, ok := controlPlaneRefIsKonnectNamespacedRef(&service)
		if !ok {
			continue
		}
		nn := types.NamespacedName{
			// TODO: implement cross namespace references
			Namespace: service.Namespace,
			Name:      cpRef.KonnectNamespacedRef.Name,
		}
		fr := ret[nn]
		fr.Service = append(fr.Service, service)
		ret[nn] = fr
	}
	for _, route := range relations.Route {
		svcRef := route.Spec.ServiceRef
		if svcRef == nil || svcRef.NamespacedRef == nil {

			cpRef := route.Spec.ControlPlaneRef
			if cpRef == nil || cpRef.KonnectNamespacedRef == nil {
				continue
			}

			nn := types.NamespacedName{
				Namespace: route.Namespace,
				Name:      cpRef.KonnectNamespacedRef.Name,
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

		cpRef, ok := controlPlaneRefIsKonnectNamespacedRef(&svc)
		if !ok {
			continue
		}

		nn := types.NamespacedName{
			// TODO: implement cross namespace references
			Namespace: route.Namespace,
			Name:      cpRef.KonnectNamespacedRef.Name,
		}
		fr := ret[nn]
		fr.Route = append(fr.Route, route)
		ret[nn] = fr
	}
	for _, consumer := range relations.Consumer {
		cpRef, ok := controlPlaneRefIsKonnectNamespacedRef(&consumer)
		if !ok {
			continue
		}
		nn := types.NamespacedName{
			// TODO: implement cross namespace references
			Namespace: consumer.Namespace,
			Name:      cpRef.KonnectNamespacedRef.Name,
		}
		fr := ret[nn]
		fr.Consumer = append(fr.Consumer, consumer)
		ret[nn] = fr
	}
	for _, group := range relations.ConsumerGroup {
		cpRef, ok := controlPlaneRefIsKonnectNamespacedRef(&group)
		if !ok {
			continue
		}
		nn := types.NamespacedName{
			// TODO: implement cross namespace references
			Namespace: group.Namespace,
			Name:      cpRef.KonnectNamespacedRef.Name,
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
// TODO: https://github.com/Kong/gateway-operator/pull/659
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
