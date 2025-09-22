package converter

import (
	"context"
	"fmt"
	"strings"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/controller/hybridgateway/refs"
	"github.com/kong/kong-operator/controller/hybridgateway/route"
	"github.com/kong/kong-operator/controller/hybridgateway/utils"
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

	service         *corev1.Service
	store           serviceStore
	outputStore     []configurationv1alpha1.KongService
	sharedStatusMap *route.SharedRouteStatusMap
}

type serviceStore struct {
	httpBackendRefs       map[string][]gwtypes.HTTPBackendRef
	konnectNamespacedRefs map[string]refs.GatewaysByNamespacedRef
	gateways              map[string][]gwtypes.Gateway
	hostnames             map[string]any
}

// NewServiceConverter returns a new instance of serviceConverter.
func newServiceConverter(service *corev1.Service, cl client.Client, sharedStatusMap *route.SharedRouteStatusMap) APIConverter[corev1.Service] {
	return &serviceConverter{
		Client: cl,
		store: serviceStore{
			httpBackendRefs:       map[string][]gwtypes.HTTPBackendRef{},
			konnectNamespacedRefs: map[string]refs.GatewaysByNamespacedRef{},
			hostnames:             map[string]any{},
		},
		outputStore:     []configurationv1alpha1.KongService{},
		sharedStatusMap: sharedStatusMap,
		service:         service,
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

// UpdateSharedRouteStatus implements APIConverter.

// UpdateSharedRouteStatus updates the shared status map with the count of "Programmed" services
// for each unique combination of route and gateway, based on the provided list of unstructured
// Kubernetes objects. It expects each object to have specific annotations indicating the route
// and associated gateways. If required annotations are missing, it returns an error. The function
// groups objects by route and gateway, filters those with a "Programmed" condition set to "True",
// and updates the shared status accordingly.
func (d *serviceConverter) UpdateSharedRouteStatus(objs []unstructured.Unstructured) error {
	mappedObjects := map[string][]unstructured.Unstructured{}
	for _, obj := range objs {
		var routeKey, gatewaysNNs string
		var gateways []string
		var ok bool

		routeKey, ok = obj.GetAnnotations()[consts.GatewayOperatorHybridRouteAnnotation]
		if !ok {
			return fmt.Errorf("missing route annotation on object %s/%s", obj.GetNamespace(), obj.GetName())
		}

		gatewaysNNs, ok = obj.GetAnnotations()[consts.GatewayOperatorHybridGatewaysAnnotation]
		if !ok {
			return fmt.Errorf("missing gateways annotation on object %s/%s", obj.GetNamespace(), obj.GetName())
		}

		gateways = strings.Split(gatewaysNNs, ",")
		for _, gw := range gateways {
			mappedObjects[routeKey+"|"+gw] = append(mappedObjects[routeKey+"|"+gw], obj)
		}
	}

	for key, groupedObjs := range mappedObjects {
		programmedObjs := lo.Filter(groupedObjs, func(obj unstructured.Unstructured, _ int) bool {
			conditions, found, err := unstructured.NestedSlice(obj.Object, "status", "conditions")
			if err != nil || !found {
				return false
			}
			for _, c := range conditions {
				condMap, ok := c.(map[string]any)
				if !ok {
					continue
				}
				if condMap["type"] == string(konnectv1alpha1.KonnectEntityProgrammedConditionType) && condMap["status"] == string(metav1.ConditionTrue) {
					return true
				}
			}
			return false
		})
		d.sharedStatusMap.UpdateProgrammedServices(*d.service, key, len(programmedObjs))
	}
	return nil
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
			d.store.konnectNamespacedRefs[ref.Ref.Name+"/"+ref.Ref.Namespace] = ref
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
				routeNN := r.Namespace + "/" + r.Name
				if _, ok := d.store.httpBackendRefs[routeNN]; !ok {
					d.store.httpBackendRefs[routeNN] = []gwtypes.HTTPBackendRef{}
				}
				d.store.httpBackendRefs[routeNN] = append(d.store.httpBackendRefs[routeNN], b)
			}
		}
	}
	return nil
}

// translate converts each HTTP backend reference in the store to a KongService resource,
// sets its metadata, and appends it to the output store.
// Returns an error if metadata setting fails.
func (d *serviceConverter) translate() error {
	for routeNN, brefs := range d.store.httpBackendRefs {
		kongServices := []configurationv1alpha1.KongService{}
		for _, brefs := range brefs {
			for _, ref := range d.store.konnectNamespacedRefs {
				for hostname := range d.store.hostnames {
					kongService := configurationv1alpha1.KongService{
						Spec: configurationv1alpha1.KongServiceSpec{
							KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
								Port: int64(*brefs.Port),
								Host: hostname,
							},
							ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
								Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
								KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
									Name: ref.Ref.Name,
								},
							},
						},
					}
					// Set all the fields that on the KongService spec, then compute the name based on the spec hash.
					kongService.Spec.Name = lo.ToPtr(d.service.Namespace + "_" + d.service.Name + "-" + utils.Hash32(kongService.Spec))

					if err := d.setMetadata(&kongService, route.HTTPRouteKey+"|"+routeNN, utils.GatewaysSliceToAnnotation(ref.Gateways)); err != nil {
						return err
					}
					kongServices = append(kongServices, kongService)
				}
			}
		}

		d.outputStore = append(d.outputStore, kongServices...)
	}
	return nil
}

func (d *serviceConverter) setMetadata(kongService *configurationv1alpha1.KongService, routeAnnotation string, gatewaysAnnotation string) error {
	hashSpec := utils.Hash64(kongService.Spec)
	if err := utils.SetMetadata(d.service, kongService, hashSpec, routeAnnotation, gatewaysAnnotation); err != nil {
		return err
	}
	return nil
}

// HostnameIntersection computes the intersection of hostnames from the provided Gateways and HTTPRoute.
func (d *serviceConverter) HostnameIntersection(gateways []gwtypes.Gateway, httpRoute gwtypes.HTTPRoute) []string {
	// TODO(mlavacca): This is a placeholder implementation, implement proper hostname intersection logic
	return []string{"api.kong-air.com"}
}
