package converter

import (
	"context"
	"fmt"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1alpha1"

	"github.com/kong/kong-operator/controller/fullhybrid/utils"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/pkg/consts"
)

var _ APIConverter[*corev1.Service] = &dummyConverter{}

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

	service     *corev1.Service
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

// GetRootObject implements APIConverter.
func (d *dummyConverter) GetRootObject() *corev1.Service {
	return d.service
}

// SetRootObject implements APIConverter.
func (d *dummyConverter) SetRootObject(obj *corev1.Service) {
	d.service = obj
}

// LoadStore implements APIConverter.
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

// Translate implements APIConverter.
func (d *dummyConverter) Translate() error {
	for _, r := range d.store.httpBackendRefs {
		kongService := configurationv1alpha1.KongService{
			Spec: configurationv1alpha1.KongServiceSpec{
				KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
					Name: lo.ToPtr(d.service.Name + lo.Ternary(r.Port != nil, fmt.Sprintf("-%d", *r.Port), "")),
					Port: int64(*r.Port),
				},
			},
		}
		if err := d.setMetadata(&kongService); err != nil {
			return err
		}
		d.outputStore = append(d.outputStore, kongService)
	}
	return nil
}

// EnforceState implements APIConverter.
func (d *dummyConverter) EnforceState(ctx context.Context) error {
	kongServices := configurationv1alpha1.KongServiceList{}
	if err := d.List(ctx, &kongServices, client.InNamespace(d.service.Namespace)); err != nil {
		return err
	}

	for _, ks := range d.outputStore {
		if !lo.ContainsBy(kongServices.Items, func(item configurationv1alpha1.KongService) bool {
			return item.Name == ks.Name && item.Namespace == ks.Namespace
		}) {
			// If the KongService is not found, create it.
			if err := d.Create(ctx, &ks); err != nil {
				return err
			}
		}
	}
	return nil
}

// GetStore implements APIConverter.
func (d *dummyConverter) GetStore(ctx context.Context) []unstructured.Unstructured {
	objects := make([]unstructured.Unstructured, 0, len(d.outputStore))
	for _, ks := range d.outputStore {
		unstr, err := utils.ToUnstructured(&ks)
		if err != nil {
			continue
		}
		objects = append(objects, unstr)
	}
	return objects
}

// Reduct implements APIConverter.
func (d *dummyConverter) Reduct(obj unstructured.Unstructured) []utils.ReductFunc {
	switch obj.GetKind() {
	// Order here is key as the handlers are called sequentially.
	case "KongService":
		return []utils.ReductFunc{
			utils.KeepProgrammed,
			utils.KeepYoungest,
		}
	default:
		return nil
	}
}

// ListExistingObjects implements APIConverter.
func (d *dummyConverter) ListExistingObjects(ctx context.Context) ([]unstructured.Unstructured, error) {
	if d.service == nil {
		return nil, nil
	}

	list := &configurationv1alpha1.KongServiceList{}
	labels := map[string]string{
		consts.GatewayOperatorManagedByLabel:          consts.ServiceManagedByLabel,
		consts.GatewayOperatorManagedByNameLabel:      d.service.Name,
		consts.GatewayOperatorManagedByNamespaceLabel: d.service.Namespace,
	}
	opts := []client.ListOption{
		client.InNamespace(d.service.Namespace),
		client.MatchingLabels(labels),
	}
	if err := d.List(ctx, list, opts...); err != nil {
		return nil, err
	}

	unstructuredItems := make([]unstructured.Unstructured, 0, len(list.Items))
	for _, item := range list.Items {
		unstr, err := utils.ToUnstructured(&item)
		if err != nil {
			return nil, err
		}
		unstructuredItems = append(unstructuredItems, unstr)
	}

	return unstructuredItems, nil
}

// setMetadata sets the metadata for the given KongService.
func (d *dummyConverter) setMetadata(kongService *configurationv1alpha1.KongService) error {
	kongService.SetGenerateName(d.service.Name + "-")
	kongService.SetNamespace(d.service.Namespace)

	labels := map[string]string{
		consts.GatewayOperatorManagedByLabel:          consts.ServiceManagedByLabel,
		consts.GatewayOperatorManagedByNameLabel:      d.service.Name,
		consts.GatewayOperatorManagedByNamespaceLabel: d.service.Namespace,
		consts.GatewayOperatorHashSpecLabel:           utils.Hash(kongService.Spec),
	}
	kongService.SetLabels(labels)

	return controllerutil.SetOwnerReference(d.service, kongService, d.Scheme(), controllerutil.WithBlockOwnerDeletion(true))
}
