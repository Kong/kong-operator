package converter

import (
	"context"
	"fmt"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1alpha1"

	"github.com/kong/kong-operator/controller/fullhybrid/refs"
	"github.com/kong/kong-operator/controller/fullhybrid/utils"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/internal/utils/index"
	"github.com/kong/kong-operator/pkg/consts"
)

var _ APIConverter[corev1.Service] = &serviceConverter{}

// serviceConverter is a concrete implementation of the APIConverter interface.
// It can be seen as an oversimplified version of the Service converter that has the main
// goal to demonstrate the basic functionality of the converter, help with further development,
// and testing.
//
// It does the following:
// - For each HTTPRoute, it checks if the route's backend references match the service's ports.
// - If so, it converts the backend references into KongServices.
type serviceConverter struct {
	client.Client

	service     *corev1.Service
	store       serviceStore
	outputStore []configurationv1alpha1.KongService
}

type serviceStore struct {
	httpBackendRefs       []gwtypes.HTTPBackendRef
	konnectNamespacedRefs map[string]commonv1alpha1.KonnectNamespacedRef
	hostnames             map[string]any
}

// NewServiceConverter returns a new instance of serviceConverter.
func newServiceConverter(service *corev1.Service, cl client.Client) APIConverter[corev1.Service] {
	return &serviceConverter{
		Client: cl,
		store: serviceStore{
			httpBackendRefs:       []gwtypes.HTTPBackendRef{},
			konnectNamespacedRefs: map[string]commonv1alpha1.KonnectNamespacedRef{},
			hostnames:             map[string]any{},
		},
		outputStore: []configurationv1alpha1.KongService{},
		service:     service,
	}
}

// -----------------------------------------------------------------------------
// APIConverter implementation
// -----------------------------------------------------------------------------

// GetRootObject implements APIConverter.
func (d *serviceConverter) GetRootObject() corev1.Service {
	return *d.service
}

// Translate implements APIConverter.
func (d *serviceConverter) Translate() error {
	if err := d.loadInputStore(context.Background()); err != nil {
		return err
	}
	return d.translate()
}

// GetOutputStore implements APIConverter.
func (d *serviceConverter) GetOutputStore(ctx context.Context) []unstructured.Unstructured {
	objects := make([]unstructured.Unstructured, 0, len(d.outputStore))
	for _, ks := range d.outputStore {
		unstr, err := utils.ToUnstructured(&ks, d.Scheme())
		if err != nil {
			continue
		}
		objects = append(objects, unstr)
	}
	return objects
}

// Reduce implements APIConverter.
func (d *serviceConverter) Reduce(obj unstructured.Unstructured) []utils.ReduceFunc {
	switch obj.GetKind() {
	// Order here is key as the handlers are called sequentially.
	case "KongService":
		return []utils.ReduceFunc{
			utils.KeepProgrammed,
			utils.KeepYoungest,
		}
	default:
		return nil
	}
}

// ListExistingObjects implements APIConverter.
func (d *serviceConverter) ListExistingObjects(ctx context.Context) ([]unstructured.Unstructured, error) {
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
		unstr, err := utils.ToUnstructured(&item, d.Scheme())
		if err != nil {
			return nil, err
		}
		unstructuredItems = append(unstructuredItems, unstr)
	}

	return unstructuredItems, nil
}

// -----------------------------------------------------------------------------
// Private functions
// -----------------------------------------------------------------------------

// loadInputStore populates the internal store with HTTPBackendRefs from HTTPRoutes
// in the same namespace as the service. It lists HTTPRoutes, filters backend references
// that match the service name, namespace, and port, and appends them to the store.
// Returns an error if listing HTTPRoutes fails.
func (d *serviceConverter) loadInputStore(ctx context.Context) error {
	// List only the HTTPRoutes the the same namespace as the service.
	// Do not consider cross-namespace refs in the service implementation.
	httpRoutes := gwtypes.HTTPRouteList{}
	err := d.List(ctx, &httpRoutes,
		client.InNamespace(d.service.Namespace),
		client.MatchingFields{
			index.BackendServicesOnHTTPRouteIndex: d.service.Namespace + "/" + d.service.Name,
		},
	)
	if err != nil {
		return err
	}

	for _, r := range httpRoutes.Items {
		gateways := refs.GetGatewaysByHTTPRoute(ctx, d.Client, r)
		hostnames := d.HostnameIntersection(gateways, r)
		for _, h := range hostnames {
			d.store.hostnames[h] = nil
		}
		namespacedRefs, err := refs.GetNamespacedRefs(ctx, d.Client, &r)
		if err != nil {
			return err
		}
		// In case there is no ControlPlane reference, skip the resource.
		if len(namespacedRefs) == 0 {
			continue
		}
		for _, ref := range namespacedRefs {
			d.store.konnectNamespacedRefs[ref.Name+"/"+ref.Namespace] = ref
		}
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

// translate converts each HTTP backend reference in the store to a KongService resource,
// sets its metadata, and appends it to the output store.
// Returns an error if metadata setting fails.
func (d *serviceConverter) translate() error {
	for _, r := range d.store.httpBackendRefs {
		kongServices := []configurationv1alpha1.KongService{}
		for _, ref := range d.store.konnectNamespacedRefs {
			for hostname := range d.store.hostnames {
				kongService := configurationv1alpha1.KongService{
					Spec: configurationv1alpha1.KongServiceSpec{
						KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
							Name: lo.ToPtr(d.service.Name + lo.Ternary(r.Port != nil, fmt.Sprintf("-%d", *r.Port), "")),
							Port: int64(*r.Port),
							Host: hostname,
						},
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: ref.Name,
							},
						},
					},
				}

				if err := d.setMetadata(&kongService); err != nil {
					return err
				}
				kongServices = append(kongServices, kongService)
			}
		}

		d.outputStore = append(d.outputStore, kongServices...)
	}
	return nil
}

func (d *serviceConverter) setMetadata(kongService *configurationv1alpha1.KongService) error {
	hashSpec := utils.Hash(kongService.Spec)
	if err := utils.SetMetadata(d.service, kongService, hashSpec); err != nil {
		return err
	}
	return nil
}

// HostnameIntersection computes the intersection of hostnames from the provided Gateways and HTTPRoute.
func (d *serviceConverter) HostnameIntersection(gateways []gwtypes.Gateway, httpRoute gwtypes.HTTPRoute) []string {
	// TODO(mlavacca): implement proper hostname intersection logic
	return []string{"api.kong-air.com"}
}
