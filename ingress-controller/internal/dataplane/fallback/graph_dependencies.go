package fallback

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	netv1 "k8s.io/api/networking/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kong-operator/v2/api/configuration/v1beta1"
	incubatorv1alpha1 "github.com/kong/kong-operator/v2/api/incubator/v1alpha1"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/gatewayapi"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/store"
)

// ResolveDependencies resolves dependencies for a given object. Dependencies are all objects referenced by the
// given object. For example, an Ingress object might refer to an IngressClass, Services, Plugins, etc.
// Every supported object type should explicitly have a case in this function.
func ResolveDependencies(cache store.CacheStores, obj client.Object) ([]client.Object, error) {
	switch obj := obj.(type) {
	// Standard Kubernetes objects.
	case *corev1.Service:
		return resolveServiceDependencies(cache, obj), nil
	case *netv1.Ingress:
		return resolveIngressDependencies(cache, obj), nil
	// Gateway API objects.
	case *gatewayapi.HTTPRoute:
		return resolveHTTPRouteDependencies(cache, obj), nil
	case *gatewayapi.TLSRoute:
		return resolveTLSRouteDependencies(cache, obj), nil
	case *gatewayapi.TCPRoute:
		return resolveTCPRouteDependencies(cache, obj), nil
	case *gatewayapi.UDPRoute:
		return resolveUDPRouteDependencies(cache, obj), nil
	case *gatewayapi.GRPCRoute:
		return resolveGRPCRouteDependencies(cache, obj), nil
	// Kong specific objects.
	case *configurationv1.KongPlugin:
		return resolveKongPluginDependencies(cache, obj), nil
	case *configurationv1.KongClusterPlugin:
		return resolveKongClusterPluginDependencies(cache, obj), nil
	case *configurationv1.KongConsumer:
		return resolveKongConsumerDependencies(cache, obj), nil
	case *configurationv1beta1.KongConsumerGroup:
		return resolveKongConsumerGroupDependencies(cache, obj), nil
	case *incubatorv1alpha1.KongServiceFacade:
		return resolveKongServiceFacadeDependencies(cache, obj), nil
	case *configurationv1alpha1.KongCustomEntity:
		return resolveKongCustomEntityDependencies(cache, obj), nil
	// v1alpha1 types with dependencies.
	case *configurationv1alpha1.KongRoute:
		return resolveKongRouteV1Alpha1Dependencies(cache, obj), nil
	case *configurationv1alpha1.KongTarget:
		return resolveKongTargetV1Alpha1Dependencies(cache, obj), nil
	case *configurationv1alpha1.KongSNI:
		return resolveKongSNIV1Alpha1Dependencies(cache, obj), nil
	case *configurationv1alpha1.KongPluginBinding:
		return resolveKongPluginBindingV1Alpha1Dependencies(cache, obj), nil
	// Object types that have no dependencies.
	case *netv1.IngressClass,
		*corev1.Secret,
		*corev1.ConfigMap,
		*discoveryv1.EndpointSlice,
		*gatewayapi.ReferenceGrant,
		*gatewayapi.Gateway,
		*gatewayapi.BackendTLSPolicy,
		*configurationv1beta1.KongUpstreamPolicy,
		*configurationv1alpha1.IngressClassParameters,
		*configurationv1alpha1.KongVault,
		*configurationv1alpha1.KongService,
		*configurationv1alpha1.KongUpstream,
		*configurationv1alpha1.KongCertificate,
		*configurationv1alpha1.KongCACertificate:
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported object type: %T", obj)
	}
}

func resolveKongRouteV1Alpha1Dependencies(cache store.CacheStores, obj *configurationv1alpha1.KongRoute) []client.Object {
	if obj.Spec.ServiceRef == nil || obj.Spec.ServiceRef.NamespacedRef == nil {
		return nil
	}
	svc, exists, err := cache.KongServiceV1Alpha1.GetByKey(fmt.Sprintf("%s/%s", obj.Namespace, obj.Spec.ServiceRef.NamespacedRef.Name))
	if err != nil || !exists {
		return nil
	}
	kongSvc, ok := svc.(*configurationv1alpha1.KongService)
	if !ok {
		return nil
	}
	return []client.Object{kongSvc}
}

func resolveKongTargetV1Alpha1Dependencies(cache store.CacheStores, obj *configurationv1alpha1.KongTarget) []client.Object {
	upstream, exists, err := cache.KongUpstreamV1Alpha1.GetByKey(fmt.Sprintf("%s/%s", obj.Namespace, obj.Spec.UpstreamRef.Name))
	if err != nil || !exists {
		return nil
	}
	kongUpstream, ok := upstream.(*configurationv1alpha1.KongUpstream)
	if !ok {
		return nil
	}
	return []client.Object{kongUpstream}
}

func resolveKongSNIV1Alpha1Dependencies(cache store.CacheStores, obj *configurationv1alpha1.KongSNI) []client.Object {
	cert, exists, err := cache.KongCertificateV1Alpha1.GetByKey(fmt.Sprintf("%s/%s", obj.Namespace, obj.Spec.CertificateRef.Name))
	if err != nil || !exists {
		return nil
	}
	kongCert, ok := cert.(*configurationv1alpha1.KongCertificate)
	if !ok {
		return nil
	}
	return []client.Object{kongCert}
}

func resolveKongPluginBindingV1Alpha1Dependencies(cache store.CacheStores, obj *configurationv1alpha1.KongPluginBinding) []client.Object {
	var deps []client.Object
	// Resolve plugin reference.
	pluginNS := obj.Namespace
	if obj.Spec.PluginReference.Namespace != "" {
		pluginNS = obj.Spec.PluginReference.Namespace
	}
	pluginKey := fmt.Sprintf("%s/%s", pluginNS, obj.Spec.PluginReference.Name)
	if plugin, exists, err := cache.Plugin.GetByKey(pluginKey); err == nil && exists {
		if p, ok := plugin.(client.Object); ok {
			deps = append(deps, p)
		}
	}
	if obj.Spec.Targets == nil {
		return deps
	}
	// Resolve target references.
	if obj.Spec.Targets.ServiceReference != nil && obj.Spec.Targets.ServiceReference.Kind == "KongService" {
		if svc, exists, err := cache.KongServiceV1Alpha1.GetByKey(fmt.Sprintf("%s/%s", obj.Namespace, obj.Spec.Targets.ServiceReference.Name)); err == nil && exists {
			if s, ok := svc.(client.Object); ok {
				deps = append(deps, s)
			}
		}
	}
	if obj.Spec.Targets.RouteReference != nil && obj.Spec.Targets.RouteReference.Kind == "KongRoute" {
		if route, exists, err := cache.KongRouteV1Alpha1.GetByKey(fmt.Sprintf("%s/%s", obj.Namespace, obj.Spec.Targets.RouteReference.Name)); err == nil && exists {
			if r, ok := route.(client.Object); ok {
				deps = append(deps, r)
			}
		}
	}
	return deps
}
