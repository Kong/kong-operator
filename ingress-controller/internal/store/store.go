/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package store

import (
	"fmt"
	"sort"
	"strings"

	"github.com/go-logr/logr"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	netv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayv1alpha3 "sigs.k8s.io/gateway-api/apis/v1alpha3"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
	"sigs.k8s.io/yaml"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kubernetes-configuration/api/configuration/v1beta1"
	incubatorv1alpha1 "github.com/kong/kubernetes-configuration/api/incubator/v1alpha1"

	"github.com/kong/kong-operator/ingress-controller/internal/annotations"
	ctrlutils "github.com/kong/kong-operator/ingress-controller/internal/controllers/utils"
	"github.com/kong/kong-operator/ingress-controller/internal/gatewayapi"
	"github.com/kong/kong-operator/ingress-controller/internal/logging"
)

const (
	caCertKey = "konghq.com/ca-cert"
	// IngressClassKongController is the string used for the Controller field of a recognized IngressClass.
	IngressClassKongController = "ingress-controllers.konghq.com/kong"
)

// Storer is the interface that wraps the required methods to gather information
// about ingresses, services, secrets and ingress annotations.
type Storer interface {
	UpdateCache(cs CacheStores)

	// Core Kubernetes resources.
	GetSecret(namespace, name string) (*corev1.Secret, error)
	GetConfigMap(namespace, name string) (*corev1.ConfigMap, error)
	GetService(namespace, name string) (*corev1.Service, error)
	GetEndpointSlicesForService(namespace, name string) ([]*discoveryv1.EndpointSlice, error)
	ListCACerts() ([]*corev1.Secret, []*corev1.ConfigMap, error)

	// Kong resources.
	GetKongIngress(namespace, name string) (*configurationv1.KongIngress, error)
	GetKongPlugin(namespace, name string) (*configurationv1.KongPlugin, error)
	GetKongClusterPlugin(name string) (*configurationv1.KongClusterPlugin, error)
	GetKongConsumer(namespace, name string) (*configurationv1.KongConsumer, error)
	GetKongConsumerGroup(namespace, name string) (*configurationv1beta1.KongConsumerGroup, error)
	GetIngressClassParametersV1Alpha1(ingressClass *netv1.IngressClass) (*configurationv1alpha1.IngressClassParameters, error)
	GetKongUpstreamPolicy(namespace, name string) (*configurationv1beta1.KongUpstreamPolicy, error)
	GetKongServiceFacade(namespace, name string) (*incubatorv1alpha1.KongServiceFacade, error)
	GetKongVault(name string) (*configurationv1alpha1.KongVault, error)
	GetKongCustomEntity(namespace, name string) (*configurationv1alpha1.KongCustomEntity, error)
	ListIngressClassParametersV1Alpha1() []*configurationv1alpha1.IngressClassParameters
	ListTCPIngresses() ([]*configurationv1beta1.TCPIngress, error)
	ListUDPIngresses() ([]*configurationv1beta1.UDPIngress, error)
	ListGlobalKongClusterPlugins() ([]*configurationv1.KongClusterPlugin, error)
	ListKongPlugins() []*configurationv1.KongPlugin
	ListKongClusterPlugins() []*configurationv1.KongClusterPlugin
	ListKongConsumers() []*configurationv1.KongConsumer
	ListKongConsumerGroups() []*configurationv1beta1.KongConsumerGroup
	ListKongVaults() []*configurationv1alpha1.KongVault
	ListKongCustomEntities() []*configurationv1alpha1.KongCustomEntity

	// Ingress API resources.
	GetIngressClassName() string
	GetIngressClassV1(name string) (*netv1.IngressClass, error)
	ListIngressesV1() []*netv1.Ingress
	ListIngressClassesV1() []*netv1.IngressClass

	// Gateway API resources.
	GetGateway(namespace string, name string) (*gatewayapi.Gateway, error)
	ListHTTPRoutes() ([]*gatewayapi.HTTPRoute, error)
	ListUDPRoutes() ([]*gatewayapi.UDPRoute, error)
	ListTCPRoutes() ([]*gatewayapi.TCPRoute, error)
	ListTLSRoutes() ([]*gatewayapi.TLSRoute, error)
	ListGRPCRoutes() ([]*gatewayapi.GRPCRoute, error)
	ListReferenceGrants() ([]*gatewayapi.ReferenceGrant, error)
	ListGateways() ([]*gatewayapi.Gateway, error)
	ListBackendTLSPoliciesByTargetService(service k8stypes.NamespacedName) ([]*gatewayapi.BackendTLSPolicy, error)
}

// Store implements Storer and can be used to list Ingress, Services
// and other resources from k8s APIserver. The backing stores should
// be synced and updated by the caller.
// It is ingressClass filter aware.
type Store struct {
	stores CacheStores

	ingressClass         string
	ingressClassMatching annotations.ClassMatching

	isValidIngressClass   func(objectMeta *metav1.ObjectMeta, annotation string, handling annotations.ClassMatching) bool
	isValidIngressV1Class func(ingress *netv1.Ingress, handling annotations.ClassMatching) bool

	logger logr.Logger
}

var _ Storer = &Store{}

// New creates a new object store to be used in the ingress controller.
func New(cs CacheStores, ingressClass string, logger logr.Logger) *Store {
	return &Store{
		stores:                cs,
		ingressClass:          ingressClass,
		ingressClassMatching:  annotations.ExactClassMatch,
		isValidIngressClass:   annotations.IngressClassValidatorFuncFromObjectMeta(ingressClass),
		isValidIngressV1Class: annotations.IngressClassValidatorFuncFromV1Ingress(ingressClass),
		logger:                logger,
	}
}

// UpdateCache updates the cache stores in the Store.
func (s *Store) UpdateCache(cs CacheStores) {
	s.stores = cs
}

// GetSecret returns a Secret using the namespace and name as key.
func (s Store) GetSecret(namespace, name string) (*corev1.Secret, error) {
	key := fmt.Sprintf("%v/%v", namespace, name)
	secret, exists, err := s.stores.Secret.GetByKey(key)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, NotFoundError{fmt.Sprintf("Secret %v not found", key)}
	}
	return secret.(*corev1.Secret), nil
}

// GetConfigMap returns a ConfigMap using the namespace and name as key.
func (s Store) GetConfigMap(namespace, name string) (*corev1.ConfigMap, error) {
	key := fmt.Sprintf("%s/%s", namespace, name)
	configMap, exists, err := s.stores.ConfigMap.GetByKey(key)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, NotFoundError{fmt.Sprintf("ConfigMap %s not found", key)}
	}
	return configMap.(*corev1.ConfigMap), nil
}

// GetService returns a Service using the namespace and name as key.
func (s Store) GetService(namespace, name string) (*corev1.Service, error) {
	key := fmt.Sprintf("%v/%v", namespace, name)
	service, exists, err := s.stores.Service.GetByKey(key)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, NotFoundError{fmt.Sprintf("Service %v not found", key)}
	}
	return service.(*corev1.Service), nil
}

// ListIngressesV1 returns the list of Ingresses in the Ingress v1 store.
func (s Store) ListIngressesV1() []*netv1.Ingress {
	// filter ingress rules
	var (
		list      = s.stores.IngressV1.List()
		ingresses = make([]*netv1.Ingress, 0, len(list))
	)
	for _, item := range list {
		ing, ok := item.(*netv1.Ingress)
		if !ok {
			s.logger.Error(nil, "ListIngressesV1: dropping object of unexpected type", "type", fmt.Sprintf("%T", item))
			continue
		}
		switch {
		case ing.GetAnnotations()[annotations.IngressClassKey] != "":
			if !s.isValidIngressClass(&ing.ObjectMeta, annotations.IngressClassKey, s.ingressClassMatching) {
				continue
			}
		case ing.Spec.IngressClassName != nil:
			if !s.isValidIngressV1Class(ing, s.ingressClassMatching) {
				continue
			}
		default:
			class, err := s.GetIngressClassV1(s.ingressClass)
			if err != nil {
				s.logger.V(logging.DebugLevel).Info("IngressClass not found", "class", s.ingressClass)
				continue
			}
			if !ctrlutils.IsDefaultIngressClass(class) {
				continue
			}
		}
		ingresses = append(ingresses, ing)
	}

	sort.SliceStable(ingresses, func(i, j int) bool {
		return strings.Compare(fmt.Sprintf("%s/%s", ingresses[i].Namespace, ingresses[i].Name),
			fmt.Sprintf("%s/%s", ingresses[j].Namespace, ingresses[j].Name)) < 0
	})

	return ingresses
}

// ListIngressClassesV1 returns the list of Ingresses in the Ingress v1 store.
func (s Store) ListIngressClassesV1() []*netv1.IngressClass {
	// filter ingress rules
	var (
		list    = s.stores.IngressClassV1.List()
		classes = make([]*netv1.IngressClass, 0, len(list))
	)
	for _, item := range list {
		class, ok := item.(*netv1.IngressClass)
		if !ok {
			s.logger.Error(nil, "ListIngressClassesV1: dropping object of unexpected type", "type", fmt.Sprintf("%T", item))
			continue
		}
		if class.Spec.Controller != IngressClassKongController {
			continue
		}
		classes = append(classes, class)
	}

	sort.SliceStable(classes, func(i, j int) bool {
		return strings.Compare(classes[i].Name, classes[j].Name) < 0
	})

	return classes
}

// ListIngressClassParametersV1Alpha1 returns the list of IngressClassParameters in the Ingress v1alpha1 store.
func (s Store) ListIngressClassParametersV1Alpha1() []*configurationv1alpha1.IngressClassParameters {
	var classParams []*configurationv1alpha1.IngressClassParameters //nolint:prealloc
	for _, item := range s.stores.IngressClassParametersV1alpha1.List() {
		classParam, ok := item.(*configurationv1alpha1.IngressClassParameters)
		if !ok {
			s.logger.Error(nil, "ListIngressClassParametersV1alpha1: dropping object of unexpected type", "type", fmt.Sprintf("%T", item))
			continue
		}
		classParams = append(classParams, classParam)
	}

	sort.SliceStable(classParams, func(i, j int) bool {
		return strings.Compare(
			fmt.Sprintf("%s/%s", classParams[i].Namespace, classParams[i].Name),
			fmt.Sprintf("%s/%s", classParams[j].Namespace, classParams[j].Name),
		) < 0
	})

	return classParams
}

// List lists all objects of given type from the provided cache stores.
func List[T client.Object](cs CacheStores) ([]T, error) {
	s, err := GetTypedStore[T](cs)
	if err != nil {
		return nil, err
	}

	objs := s.List()
	ret := make([]T, 0, len(objs))
	for _, obj := range objs {
		httproute, ok := obj.(T)
		if !ok {
			return nil, fmt.Errorf("%T is not a supported cache object type", obj)
		}
		ret = append(ret, httproute)
	}

	return ret, nil
}

// GetTypedStore returns a cache.Store for the given type.
func GetTypedStore[T client.Object](cs CacheStores) (cache.Store, error) {
	var obj T
	switch any(obj).(type) {
	case *gatewayapi.HTTPRoute:
		return cs.HTTPRoute, nil
	case *gatewayapi.TCPRoute:
		return cs.TCPRoute, nil
	case *gatewayapi.UDPRoute:
		return cs.UDPRoute, nil
	case *gatewayapi.TLSRoute:
		return cs.TLSRoute, nil
	case *gatewayapi.GRPCRoute:
		return cs.GRPCRoute, nil
	case *gatewayapi.ReferenceGrant:
		return cs.ReferenceGrant, nil
	case *gatewayapi.Gateway:
		return cs.Gateway, nil
	case *gatewayapi.BackendTLSPolicy:
		return cs.BackendTLSPolicy, nil
	case *configurationv1.KongPlugin:
		return cs.Plugin, nil
	default:
	}
	return nil, fmt.Errorf("unsupported type %T", obj)
}

// ListHTTPRoutes returns the list of HTTPRoutes in the HTTPRoute cache store.
func (s Store) ListHTTPRoutes() ([]*gatewayapi.HTTPRoute, error) {
	return List[*gatewayapi.HTTPRoute](s.stores)
}

// ListUDPRoutes returns the list of UDPRoutes in the UDPRoute cache store.
func (s Store) ListUDPRoutes() ([]*gatewayapi.UDPRoute, error) {
	return List[*gatewayapi.UDPRoute](s.stores)
}

// ListTCPRoutes returns the list of TCPRoutes in the TCPRoute cache store.
func (s Store) ListTCPRoutes() ([]*gatewayapi.TCPRoute, error) {
	return List[*gatewayapi.TCPRoute](s.stores)
}

// ListTLSRoutes returns the list of TLSRoutes in the TLSRoute cache store.
func (s Store) ListTLSRoutes() ([]*gatewayapi.TLSRoute, error) {
	return List[*gatewayapi.TLSRoute](s.stores)
}

// ListGRPCRoutes returns the list of GRPCRoutes in the GRPCRoute cache store.
func (s Store) ListGRPCRoutes() ([]*gatewayapi.GRPCRoute, error) {
	return List[*gatewayapi.GRPCRoute](s.stores)
}

// ListReferenceGrants returns the list of ReferenceGrants in the ReferenceGrant cache store.
func (s Store) ListReferenceGrants() ([]*gatewayapi.ReferenceGrant, error) {
	return List[*gatewayapi.ReferenceGrant](s.stores)
}

// ListGateways returns the list of Gateways in the Gateway cache store.
func (s Store) ListGateways() ([]*gatewayapi.Gateway, error) {
	return List[*gatewayapi.Gateway](s.stores)
}

// ListBackendTLSPoliciesByTargetService returns the list of BackendTLSPolicies in the BackendTLSPolicy cache store.
// The policies are filtered by the target service.
func (s Store) ListBackendTLSPoliciesByTargetService(service k8stypes.NamespacedName) ([]*gatewayapi.BackendTLSPolicy, error) {
	policiesToReturn := []*gatewayapi.BackendTLSPolicy{}
	policies, err := List[*gatewayapi.BackendTLSPolicy](s.stores)
	if err != nil {
		return nil, err
	}

	for _, policy := range policies {
		if lo.ContainsBy(policy.Spec.TargetRefs, func(ref gatewayapi.LocalPolicyTargetReferenceWithSectionName) bool {
			return (ref.Group == "" || ref.Group == "core") &&
				ref.Kind == "Service" &&
				(policy.Namespace == service.Namespace && string(ref.Name) == service.Name)
		}) {
			policiesToReturn = append(policiesToReturn, policy)
		}
	}

	return policiesToReturn, nil
}

// ListTCPIngresses returns the list of TCP Ingresses from
// configuration.konghq.com group.
func (s Store) ListTCPIngresses() ([]*configurationv1beta1.TCPIngress, error) {
	var ingresses []*configurationv1beta1.TCPIngress
	err := cache.ListAll(s.stores.TCPIngress, labels.NewSelector(),
		func(ob any) {
			ing, ok := ob.(*configurationv1beta1.TCPIngress)
			if ok && s.isValidIngressClass(&ing.ObjectMeta, annotations.IngressClassKey, s.getIngressClassHandling()) {
				ingresses = append(ingresses, ing)
			}
		})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(ingresses, func(i, j int) bool {
		return strings.Compare(fmt.Sprintf("%s/%s", ingresses[i].Namespace, ingresses[i].Name),
			fmt.Sprintf("%s/%s", ingresses[j].Namespace, ingresses[j].Name)) < 0
	})
	return ingresses, nil
}

// ListUDPIngresses returns the list of UDP Ingresses.
func (s Store) ListUDPIngresses() ([]*configurationv1beta1.UDPIngress, error) {
	ingresses := []*configurationv1beta1.UDPIngress{}
	if s.stores.UDPIngress == nil {
		// older versions of the KIC do not support UDPIngress so short circuit to maintain support with them
		return ingresses, nil
	}

	err := cache.ListAll(s.stores.UDPIngress, labels.NewSelector(),
		func(ob any) {
			ing, ok := ob.(*configurationv1beta1.UDPIngress)
			if ok && s.isValidIngressClass(&ing.ObjectMeta, annotations.IngressClassKey, s.getIngressClassHandling()) {
				ingresses = append(ingresses, ing)
			}
		})
	sort.SliceStable(ingresses, func(i, j int) bool {
		return strings.Compare(fmt.Sprintf("%s/%s", ingresses[i].Namespace, ingresses[i].Name),
			fmt.Sprintf("%s/%s", ingresses[j].Namespace, ingresses[j].Name)) < 0
	})
	return ingresses, err
}

// GetEndpointSlicesForService returns all EndpointSlices for service
// 'namespace/name' inside K8s.
func (s Store) GetEndpointSlicesForService(namespace, name string) ([]*discoveryv1.EndpointSlice, error) {
	// EndpointSlices are tied to a Service via a label.
	req, err := labels.NewRequirement(discoveryv1.LabelServiceName, selection.Equals, []string{name})
	if err != nil {
		return nil, err
	}
	var endpointSlices []*discoveryv1.EndpointSlice
	if err := cache.ListAll(
		s.stores.EndpointSlice, labels.NewSelector().Add(*req),
		func(obj any) {
			// Ensure the EndpointSlice is for the Service from the requested namespace.
			if eps, ok := obj.(*discoveryv1.EndpointSlice); ok && eps.Namespace == namespace {
				endpointSlices = append(endpointSlices, eps)
			}
		},
	); err != nil {
		return nil, err
	}
	if len(endpointSlices) == 0 {
		return nil, NotFoundError{fmt.Sprintf("EndpointSlices for Service %s/%s not found", namespace, name)}
	}
	return endpointSlices, nil
}

// GetKongPlugin returns the 'name' KongPlugin resource in namespace.
func (s Store) GetKongPlugin(namespace, name string) (*configurationv1.KongPlugin, error) {
	key := fmt.Sprintf("%v/%v", namespace, name)
	p, exists, err := s.stores.Plugin.GetByKey(key)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, NotFoundError{fmt.Sprintf("KongPlugin %v not found", key)}
	}
	return p.(*configurationv1.KongPlugin), nil
}

// GetKongClusterPlugin returns the 'name' KongClusterPlugin resource.
func (s Store) GetKongClusterPlugin(name string) (*configurationv1.KongClusterPlugin, error) {
	p, exists, err := s.stores.ClusterPlugin.GetByKey(name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, NotFoundError{fmt.Sprintf("KongClusterPlugin %v not found", name)}
	}
	return p.(*configurationv1.KongClusterPlugin), nil
}

// GetKongIngress returns the 'name' KongIngress resource in namespace.
func (s Store) GetKongIngress(namespace, name string) (*configurationv1.KongIngress, error) {
	key := fmt.Sprintf("%v/%v", namespace, name)
	p, exists, err := s.stores.KongIngress.GetByKey(key)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, NotFoundError{fmt.Sprintf("KongIngress %v not found", name)}
	}
	return p.(*configurationv1.KongIngress), nil
}

// GetKongConsumer returns the 'name' KongConsumer resource in namespace.
func (s Store) GetKongConsumer(namespace, name string) (*configurationv1.KongConsumer, error) {
	key := fmt.Sprintf("%v/%v", namespace, name)
	p, exists, err := s.stores.Consumer.GetByKey(key)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, NotFoundError{fmt.Sprintf("KongConsumer %v not found", key)}
	}
	return p.(*configurationv1.KongConsumer), nil
}

// GetKongConsumerGroup returns the 'name' KongConsumerGroup resource in namespace.
func (s Store) GetKongConsumerGroup(namespace, name string) (*configurationv1beta1.KongConsumerGroup, error) {
	key := fmt.Sprintf("%v/%v", namespace, name)
	p, exists, err := s.stores.ConsumerGroup.GetByKey(key)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, NotFoundError{fmt.Sprintf("KongConsumerGroup %v not found", key)}
	}
	return p.(*configurationv1beta1.KongConsumerGroup), nil
}

func (s Store) GetIngressClassName() string {
	return s.ingressClass
}

// GetIngressClassV1 returns the 'name' IngressClass resource.
func (s Store) GetIngressClassV1(name string) (*netv1.IngressClass, error) {
	p, exists, err := s.stores.IngressClassV1.GetByKey(name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, NotFoundError{fmt.Sprintf("IngressClass %v not found", name)}
	}
	return p.(*netv1.IngressClass), nil
}

func (s Store) GetKongUpstreamPolicy(namespace, name string) (*configurationv1beta1.KongUpstreamPolicy, error) {
	key := fmt.Sprintf("%v/%v", namespace, name)
	p, exists, err := s.stores.KongUpstreamPolicy.GetByKey(key)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, NotFoundError{fmt.Sprintf("KongUpstreamPolicy %v not found", key)}
	}
	return p.(*configurationv1beta1.KongUpstreamPolicy), nil
}

func (s Store) GetKongServiceFacade(namespace, name string) (*incubatorv1alpha1.KongServiceFacade, error) {
	key := fmt.Sprintf("%v/%v", namespace, name)
	p, exists, err := s.stores.KongServiceFacade.GetByKey(key)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, NotFoundError{fmt.Sprintf("KongServiceFacade %v not found", key)}
	}
	return p.(*incubatorv1alpha1.KongServiceFacade), nil
}

// GetIngressClassParametersV1Alpha1 returns IngressClassParameters for provided
// IngressClass.
func (s Store) GetIngressClassParametersV1Alpha1(ingressClass *netv1.IngressClass) (*configurationv1alpha1.IngressClassParameters, error) {
	if ingressClass == nil {
		return nil, fmt.Errorf("provided IngressClass is nil")
	}

	if ingressClass.Spec.Parameters == nil {
		return &configurationv1alpha1.IngressClassParameters{}, nil
	}

	if ingressClass.Spec.Parameters.APIGroup == nil ||
		*ingressClass.Spec.Parameters.APIGroup != configurationv1alpha1.GroupVersion.Group {
		return nil, fmt.Errorf(
			"IngressClass %s should reference parameters in apiGroup:%s",
			ingressClass.Name,
			configurationv1alpha1.GroupVersion.Group,
		)
	}

	if ingressClass.Spec.Parameters.Kind != configurationv1alpha1.IngressClassParametersKind {
		return nil, fmt.Errorf(
			"IngressClass %s should reference parameters with kind:%s",
			ingressClass.Name,
			configurationv1alpha1.IngressClassParametersKind,
		)
	}

	if ingressClass.Spec.Parameters.Scope == nil || ingressClass.Spec.Parameters.Namespace == nil {
		return nil, fmt.Errorf("IngressClass %s should reference namespaced parameters", ingressClass.Name)
	}

	key := fmt.Sprintf("%v/%v", *ingressClass.Spec.Parameters.Namespace, ingressClass.Spec.Parameters.Name)
	params, exists, err := s.stores.IngressClassParametersV1alpha1.GetByKey(key)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, NotFoundError{fmt.Sprintf("IngressClassParameters %v not found", ingressClass.Spec.Parameters.Name)}
	}
	return params.(*configurationv1alpha1.IngressClassParameters), nil
}

// GetGateway returns gateway resource having specified namespace and name.
func (s Store) GetGateway(namespace string, name string) (*gatewayapi.Gateway, error) {
	key := fmt.Sprintf("%v/%v", namespace, name)
	obj, exists, err := s.stores.Gateway.GetByKey(key)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, NotFoundError{fmt.Sprintf("Gateway %v not found", name)}
	}
	return obj.(*gatewayapi.Gateway), nil
}

// GetKongVault returns kongvault resource having specified name.
func (s Store) GetKongVault(name string) (*configurationv1alpha1.KongVault, error) {
	p, exists, err := s.stores.KongVault.GetByKey(name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, NotFoundError{fmt.Sprintf("KongVault %v not found", name)}
	}
	return p.(*configurationv1alpha1.KongVault), nil
}

func (s Store) GetKongCustomEntity(namespace, name string) (*configurationv1alpha1.KongCustomEntity, error) {
	key := fmt.Sprintf("%v/%v", namespace, name)
	e, exists, err := s.stores.KongCustomEntity.GetByKey(key)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, NotFoundError{fmt.Sprintf("KongCustomEntity %s/%s not found", namespace, name)}
	}
	return e.(*configurationv1alpha1.KongCustomEntity), nil
}

// ListKongConsumers returns all KongConsumers filtered by the ingress.class
// annotation.
func (s Store) ListKongConsumers() []*configurationv1.KongConsumer {
	var consumers []*configurationv1.KongConsumer
	for _, item := range s.stores.Consumer.List() {
		c, ok := item.(*configurationv1.KongConsumer)
		if ok && s.isValidIngressClass(&c.ObjectMeta, annotations.IngressClassKey, s.getIngressClassHandling()) {
			consumers = append(consumers, c)
		}
	}

	return consumers
}

// ListKongConsumerGroups returns all KongConsumerGroups filtered by the ingress.class
// annotation.
func (s Store) ListKongConsumerGroups() []*configurationv1beta1.KongConsumerGroup {
	var consumerGroups []*configurationv1beta1.KongConsumerGroup
	for _, item := range s.stores.ConsumerGroup.List() {
		c, ok := item.(*configurationv1beta1.KongConsumerGroup)
		if ok && s.isValidIngressClass(&c.ObjectMeta, annotations.IngressClassKey, s.getIngressClassHandling()) {
			consumerGroups = append(consumerGroups, c)
		}
	}

	return consumerGroups
}

// ListGlobalKongClusterPlugins returns all KongClusterPlugin resources
// filtered by the ingress.class annotation and with the
// label global:"true".
func (s Store) ListGlobalKongClusterPlugins() ([]*configurationv1.KongClusterPlugin, error) {
	var plugins []*configurationv1.KongClusterPlugin

	req, err := labels.NewRequirement("global", selection.Equals, []string{"true"})
	if err != nil {
		return nil, err
	}
	err = cache.ListAll(s.stores.ClusterPlugin,
		labels.NewSelector().Add(*req),
		func(ob any) {
			p, ok := ob.(*configurationv1.KongClusterPlugin)
			if ok && s.isValidIngressClass(&p.ObjectMeta, annotations.IngressClassKey, s.getIngressClassHandling()) {
				plugins = append(plugins, p)
			}
		})
	if err != nil {
		return nil, err
	}
	return plugins, nil
}

// ListKongClusterPlugins lists all KongClusterPlugins that match expected ingress.class annotation.
func (s Store) ListKongClusterPlugins() []*configurationv1.KongClusterPlugin {
	var plugins []*configurationv1.KongClusterPlugin
	for _, item := range s.stores.ClusterPlugin.List() {
		p, ok := item.(*configurationv1.KongClusterPlugin)
		if ok && s.isValidIngressClass(&p.ObjectMeta, annotations.IngressClassKey, s.getIngressClassHandling()) {
			plugins = append(plugins, p)
		}
	}
	return plugins
}

// ListKongPlugins lists all KongPlugins.
func (s Store) ListKongPlugins() []*configurationv1.KongPlugin {
	plugins, err := List[*configurationv1.KongPlugin](s.stores)
	if err != nil {
		s.logger.Error(err, "failed to list KongPlugins")
		return nil
	}
	return plugins
}

// ListCACerts returns all Secrets and ConfigMaps containing the label
// "konghq.com/ca-cert"="true".
func (s Store) ListCACerts() ([]*corev1.Secret, []*corev1.ConfigMap, error) {
	var secrets []*corev1.Secret
	var configMaps []*corev1.ConfigMap

	req, err := labels.NewRequirement(caCertKey,
		selection.Equals, []string{"true"})
	if err != nil {
		return nil, nil, err
	}
	err = cache.ListAll(s.stores.Secret,
		labels.NewSelector().Add(*req),
		func(ob any) {
			if p, ok := ob.(*corev1.Secret); ok {
				secrets = append(secrets, p)
			}
		})
	if err != nil {
		return nil, nil, err
	}
	err = cache.ListAll(s.stores.ConfigMap,
		labels.NewSelector().Add(*req),
		func(ob any) {
			if p, ok := ob.(*corev1.ConfigMap); ok {
				configMaps = append(configMaps, p)
			}
		})
	if err != nil {
		return nil, nil, err
	}

	return secrets, configMaps, nil
}

func (s Store) ListKongVaults() []*configurationv1alpha1.KongVault {
	var kongVaults []*configurationv1alpha1.KongVault
	for _, obj := range s.stores.KongVault.List() {
		kongVault, ok := obj.(*configurationv1alpha1.KongVault)
		if ok && s.isValidIngressClass(&kongVault.ObjectMeta, annotations.IngressClassKey, s.getIngressClassHandling()) {
			kongVaults = append(kongVaults, kongVault)
		}
	}
	return kongVaults
}

func (s Store) ListKongCustomEntities() []*configurationv1alpha1.KongCustomEntity {
	var kongCustomEntities []*configurationv1alpha1.KongCustomEntity
	for _, obj := range s.stores.KongCustomEntity.List() {
		kongCustomEntity, ok := obj.(*configurationv1alpha1.KongCustomEntity)
		if ok && s.isValidIngressClass(
			&metav1.ObjectMeta{
				Annotations: map[string]string{annotations.IngressClassKey: kongCustomEntity.Spec.ControllerName},
			}, annotations.IngressClassKey, s.getIngressClassHandling(),
		) {
			kongCustomEntities = append(kongCustomEntities, kongCustomEntity)
		}
	}
	return kongCustomEntities
}

// getIngressClassHandling returns annotations.ExactOrEmptyClassMatch if an IngressClass is the default class, or
// annotations.ExactClassMatch if the IngressClass is not default or does not exist.
func (s Store) getIngressClassHandling() annotations.ClassMatching {
	class, err := s.GetIngressClassV1(s.ingressClass)
	if err != nil {
		s.logger.V(logging.DebugLevel).Info("IngressClass not found", "class", s.ingressClass)
		return annotations.ExactClassMatch
	}
	if ctrlutils.IsDefaultIngressClass(class) {
		return annotations.ExactOrEmptyClassMatch
	}
	return annotations.ExactClassMatch
}

// convUnstructuredObj is a convenience function to quickly convert any runtime.Object where the underlying type
// is an *unstructured.Unstructured (client-go's dynamic client type) and convert that object to a runtime.Object
// which is backed by the API type it represents. You can use the GVK of the runtime.Object to determine what type
// you want to convert to. This function is meant so that storer implementations can optionally work with YAML files
// for caller convenience when initializing new CacheStores objects.
//
// TODO: upon some searching I didn't find an analog to this over in client-go (https://github.com/kubernetes/client-go)
// however I could have just missed it. We should switch if we find something better, OR we should contribute
// this functionality upstream.
func convUnstructuredObj(from, to runtime.Object) error {
	b, err := yaml.Marshal(from)
	if err != nil {
		return fmt.Errorf("failed to convert object %s to yaml: %w", from.GetObjectKind().GroupVersionKind(), err)
	}
	return yaml.Unmarshal(b, to)
}

// mkObjFromGVK is a factory function that returns a concrete implementation runtime.Object
// for the given GVK. Callers can then use `convert()` to convert an unstructured
// runtime.Object into a concrete one.
func mkObjFromGVK(gvk schema.GroupVersionKind) (runtime.Object, error) {
	switch gvk {
	// ----------------------------------------------------------------------------
	// Kubernetes Core APIs
	// ----------------------------------------------------------------------------
	case netv1.SchemeGroupVersion.WithKind("Ingress"):
		return &netv1.Ingress{}, nil
	case netv1.SchemeGroupVersion.WithKind("IngressClass"):
		return &netv1.IngressClass{}, nil
	case corev1.SchemeGroupVersion.WithKind("Service"):
		return &corev1.Service{}, nil
	case corev1.SchemeGroupVersion.WithKind("Secret"):
		return &corev1.Secret{}, nil
	case corev1.SchemeGroupVersion.WithKind("ConfigMap"):
		return &corev1.ConfigMap{}, nil
	// ----------------------------------------------------------------------------
	// Kubernetes Discovery APIs
	// ----------------------------------------------------------------------------
	case discoveryv1.SchemeGroupVersion.WithKind("EndpointSlice"):
		return &discoveryv1.EndpointSlice{}, nil
	// ----------------------------------------------------------------------------
	// Kubernetes Gateway APIs
	// ----------------------------------------------------------------------------
	case gatewayv1.SchemeGroupVersion.WithKind("Gateway"):
		return &gatewayapi.Gateway{}, nil
	case gatewayv1.SchemeGroupVersion.WithKind("HTTPRoute"):
		return &gatewayapi.HTTPRoute{}, nil
	case gatewayv1.SchemeGroupVersion.WithKind("GRPCRoute"):
		return &gatewayapi.GRPCRoute{}, nil
	case gatewayv1alpha2.SchemeGroupVersion.WithKind("TCPRoute"):
		return &gatewayapi.TCPRoute{}, nil
	case gatewayv1alpha2.SchemeGroupVersion.WithKind("UDPRoute"):
		return &gatewayapi.UDPRoute{}, nil
	case gatewayv1alpha2.SchemeGroupVersion.WithKind("TLSRoute"):
		return &gatewayapi.TLSRoute{}, nil
	case gatewayv1beta1.SchemeGroupVersion.WithKind("ReferenceGrant"):
		return &gatewayapi.ReferenceGrant{}, nil
	case gatewayv1alpha3.SchemeGroupVersion.WithKind("BackendTLSPolicy"):
		return &gatewayapi.BackendTLSPolicy{}, nil
	// ----------------------------------------------------------------------------
	// Kong APIs
	// ----------------------------------------------------------------------------
	case configurationv1.SchemeGroupVersion.WithKind("KongIngress"):
		return &configurationv1.KongIngress{}, nil
	case configurationv1beta1.SchemeGroupVersion.WithKind("UDPIngress"):
		return &configurationv1beta1.UDPIngress{}, nil
	case configurationv1beta1.SchemeGroupVersion.WithKind("TCPIngress"):
		return &configurationv1beta1.TCPIngress{}, nil
	case configurationv1.SchemeGroupVersion.WithKind("KongPlugin"):
		return &configurationv1.KongPlugin{}, nil
	case configurationv1.SchemeGroupVersion.WithKind("KongClusterPlugin"):
		return &configurationv1.KongClusterPlugin{}, nil
	case configurationv1.SchemeGroupVersion.WithKind("KongConsumer"):
		return &configurationv1.KongConsumer{}, nil
	case configurationv1beta1.SchemeGroupVersion.WithKind("KongConsumerGroup"):
		return &configurationv1beta1.KongConsumerGroup{}, nil
	case configurationv1alpha1.SchemeGroupVersion.WithKind("IngressClassParameters"):
		return &configurationv1alpha1.IngressClassParameters{}, nil
	case configurationv1beta1.SchemeGroupVersion.WithKind("KongUpstreamPolicy"):
		return &configurationv1beta1.KongUpstreamPolicy{}, nil
	case incubatorv1alpha1.SchemeGroupVersion.WithKind("KongServiceFacade"):
		return &incubatorv1alpha1.KongServiceFacade{}, nil
	case configurationv1alpha1.GroupVersion.WithKind(configurationv1alpha1.KongCustomEntityKind):
		return &configurationv1alpha1.KongCustomEntity{}, nil
	case configurationv1alpha1.GroupVersion.WithKind("KongVault"):
		return &configurationv1alpha1.KongVault{}, nil
	case configurationv1alpha1.GroupVersion.WithKind("KongCustomEntity"):
		return &configurationv1alpha1.KongCustomEntity{}, nil
	default:
		return nil, fmt.Errorf("%s is not a supported runtime.Object", gvk)
	}
}
