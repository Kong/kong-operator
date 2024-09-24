package konnect

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

// ForeignRelations contains all the relations between Kong entities and KongPlugin.
type ForeignRelations struct {
	Consumer, ConsumerGroup []string
	Route                   []configurationv1alpha1.KongRoute
	Service                 []configurationv1alpha1.KongService
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
		cpRef := service.Spec.ControlPlaneRef
		if cpRef == nil ||
			cpRef.KonnectNamespacedRef == nil ||
			cpRef.Type != configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef {
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

		cpRef := svc.Spec.ControlPlaneRef
		if cpRef == nil || cpRef.KonnectNamespacedRef == nil {
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

	// TODO consumers and consumer groups

	return ret, nil
}

// Rel represents a relation between Kong entities and KongPlugin.
type Rel struct {
	Consumer, ConsumerGroup, Route, Service string
}

// GetCombinations returns all possible combinations of relations.
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
			servicesForRoutes := sets.NewString()

			for _, consumer := range relations.Consumer {
				for _, route := range relations.Route {

					var serviceForRouteFound bool
					for _, service := range relations.Service {
						if route.Spec.ServiceRef.NamespacedRef.Name == service.Name {
							serviceForRouteFound = true
							servicesForRoutes.Insert(service.Name)
							cartesianProduct = append(cartesianProduct, Rel{
								Service:  service.Name,
								Route:    route.Name,
								Consumer: consumer,
							})
						} else {
							cartesianProduct = append(cartesianProduct, Rel{
								Service:  service.Name,
								Consumer: consumer,
							})
						}
					}
					if !serviceForRouteFound {
						cartesianProduct = append(cartesianProduct, Rel{
							Route:    route.Name,
							Consumer: consumer,
						})
					}
				}

				for _, service := range relations.Service {
					if !servicesForRoutes.Has(service.Name) {
						cartesianProduct = append(cartesianProduct, Rel{
							Service:  service.Name,
							Consumer: consumer,
						})
					}
				}
			}

		} else {
			cartesianProduct = make([]Rel, 0, len(relations.Consumer))
			for _, consumer := range relations.Consumer {
				cartesianProduct = append(cartesianProduct, Rel{Consumer: consumer})
			}
		}
	} else if lConsumerGroup > 0 {
		if l > 0 {
			cartesianProduct = make([]Rel, 0, l*lConsumerGroup)
			servicesForRoutes := sets.NewString()

			for _, group := range relations.ConsumerGroup {
				for _, route := range relations.Route {

					var serviceForRouteFound bool
					for _, service := range relations.Service {
						serviceRef := route.Spec.ServiceRef
						if serviceRef.Type != configurationv1alpha1.ServiceRefNamespacedRef {
							continue
						}

						if serviceRef.NamespacedRef.Name == service.Name {
							serviceForRouteFound = true
							servicesForRoutes.Insert(service.Name)
							cartesianProduct = append(cartesianProduct, Rel{
								Service:       service.Name,
								Route:         route.Name,
								ConsumerGroup: group,
							})
						} else {
							cartesianProduct = append(cartesianProduct, Rel{
								Service:       service.Name,
								ConsumerGroup: group,
							})
						}
					}
					if !serviceForRouteFound {
						cartesianProduct = append(cartesianProduct, Rel{
							Route:         route.Name,
							ConsumerGroup: group,
						})
					}
				}

				for _, service := range relations.Service {
					if !servicesForRoutes.Has(service.Name) {
						cartesianProduct = append(cartesianProduct, Rel{
							Service:       service.Name,
							ConsumerGroup: group,
						})
					}
				}
			}

		} else {
			cartesianProduct = make([]Rel, 0, lConsumerGroup)
			for _, group := range relations.ConsumerGroup {
				cartesianProduct = append(cartesianProduct, Rel{ConsumerGroup: group})
			}
		}
	} else if l > 0 {
		cartesianProduct = make([]Rel, 0, l)
		servicesForRoutes := sets.NewString()
		for _, route := range relations.Route {
			var serviceForRouteFound bool
			for _, service := range relations.Service {
				serviceRef := route.Spec.ServiceRef
				if serviceRef.Type != configurationv1alpha1.ServiceRefNamespacedRef {
					continue
				}

				if serviceRef.NamespacedRef.Name == service.Name {
					serviceForRouteFound = true
					servicesForRoutes.Insert(service.Name)
					cartesianProduct = append(cartesianProduct, Rel{
						Service: service.Name,
						Route:   route.Name,
					})
				} else {
					cartesianProduct = append(cartesianProduct, Rel{
						Service: service.Name,
					})
				}
			}
			if !serviceForRouteFound {
				cartesianProduct = append(cartesianProduct, Rel{
					Route: route.Name,
				})
			}
		}
		for _, service := range relations.Service {
			if !servicesForRoutes.Has(service.Name) {
				cartesianProduct = append(cartesianProduct, Rel{
					Service: service.Name,
				})
			}
		}
	}

	return cartesianProduct
}
