package converter

import (
	"context"
	"fmt"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1alpha1"

	gwtypes "github.com/kong/kong-operator/internal/types"
)

var _ APIConverter[corev1.Service] = &dummyConverter{}

// dummyConverter is a concrete implementation of the APIConverter interface.
// It can be seen as an oversimplified version of the Service converter that has the main
// goal to demonstrate the basic functionality of the converter, help with further development,
// and testing.
//
// It does the following:
// - For each HTTPRoute, it checks if the route's backend references match the service's ports.
// - If so, it converts the backend references into KongServices.
type dummyConverter struct {
	client.Client

	service     corev1.Service
	store       dummyStore
	outputStore []configurationv1alpha1.KongService
}

type dummyStore struct {
	httpBackendRefs []gwtypes.HTTPBackendRef
}

// NewDummyConverter returns a new instance of dummyConverter.
func NewDummyConverter(cl client.Client) *dummyConverter {
	return &dummyConverter{
		Client: cl,
		store: dummyStore{
			httpBackendRefs: []gwtypes.HTTPBackendRef{},
		},
		outputStore: []configurationv1alpha1.KongService{},
	}
}

// SetRootObject implements ObjectConverter.
func (d *dummyConverter) SetRootObject(obj corev1.Service) {
	d.service = obj
}

// LoadStore implements ObjectConverter.
func (d *dummyConverter) LoadStore(ctx context.Context) error {
	// List only the HTTPRoutes the the same namespace as the service.
	// Do not consider cross-namespace refs in the dummy implementation.
	httpRoutes := gwtypes.HTTPRouteList{}
	err := d.List(ctx, &httpRoutes, client.InNamespace(d.service.Namespace))
	if err != nil {
		return err
	}

	for _, r := range httpRoutes.Items {
		for _, rule := range r.Spec.Rules {
			if b, found := lo.Find(rule.BackendRefs, func(b gwtypes.HTTPBackendRef) bool {
				namespace := d.service.Namespace
				containsPort := lo.ContainsBy(d.service.Spec.Ports, func(p corev1.ServicePort) bool {
					if b.Port == nil {
						return false
					}
					return int32(*b.Port) == p.Port
				})
				return string(b.Name) == d.service.Name &&
					(b.Namespace == nil || string(*b.Namespace) == namespace) &&
					containsPort

			}); found {
				d.store.httpBackendRefs = append(d.store.httpBackendRefs, b)
			}
		}
	}
	return nil
}

// Translate implements ObjectConverter.
func (d *dummyConverter) Translate() error {
	for _, r := range d.store.httpBackendRefs {
		serviceName := fmt.Sprintf("%s-%d", d.service.Name, *r.Port)
		d.outputStore = append(d.outputStore, configurationv1alpha1.KongService{
			ObjectMeta: metav1.ObjectMeta{
				Name:      serviceName,
				Namespace: d.service.Namespace,
			},
			Spec: configurationv1alpha1.KongServiceSpec{
				KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
					Name: lo.ToPtr(serviceName),
					Port: int64(*r.Port),
				},
			},
		})
	}
	return nil
}

// DumpOutputStore is an utility function to allow testing the dummy converter in isolation, without exposing internal state.
func (d *dummyConverter) DumpOutputStore() []configurationv1alpha1.KongService {
	return d.outputStore
}
