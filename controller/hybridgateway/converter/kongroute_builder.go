package converter

import (
	routes "github.com/kong/kong-operator/controller/hybridgateway/route"
	"github.com/kong/kong-operator/controller/hybridgateway/utils"
	gwtypes "github.com/kong/kong-operator/internal/types"
	commonv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/kong/kong-operator/modules/manager/scheme"
	"github.com/kong/kong-operator/pkg/consts"
)

// KongRouteBuilder is a builder for configurationv1alpha1.KongRoute resources.
type KongRouteBuilder struct {
	route configurationv1alpha1.KongRoute
}

// NewKongRouteBuilder creates and returns a new KongRouteBuilder instance.
func NewKongRouteBuilder() *KongRouteBuilder {
	return &KongRouteBuilder{
		route: configurationv1alpha1.KongRoute{},
	}
}

// WithHosts sets the hosts for the KongRoute being built.
func (b *KongRouteBuilder) WithHosts(hosts []string) *KongRouteBuilder {
	b.route.Spec.Hosts = append(b.route.Spec.Hosts, hosts...)
	return b
}

// WithHTTPRouteMatch sets the match criteria (path, method, headers) for the KongRoute.
func (b *KongRouteBuilder) WithHTTPRouteMatch(match gwtypes.HTTPRouteMatch) *KongRouteBuilder {
	// Path.
	if match.Path != nil && match.Path.Value != nil {
		b.route.Spec.Paths = append(b.route.Spec.Paths, *match.Path.Value)
	}

	// Method
	if match.Method != nil {
		b.route.Spec.Methods = append(b.route.Spec.Methods, string(*match.Method))
	}

	// Headers
	if len(match.Headers) > 0 {
		if b.route.Spec.Headers == nil {
			b.route.Spec.Headers = make(map[string][]string)
		}
		for _, hdr := range match.Headers {
			b.route.Spec.Headers[string(hdr.Name)] = append(b.route.Spec.Headers[string(hdr.Name)], hdr.Value)
		}
	}
	// Note: QueryParams are not natively supported by KongRoute

	return b
}

// WithKongService sets the KongService reference for the KongRoute.
func (b *KongRouteBuilder) WithKongService(ks *configurationv1alpha1.KongService) *KongRouteBuilder {
	if ks != nil {
		b.route.Spec.ServiceRef = &configurationv1alpha1.ServiceRef{
			Type: configurationv1alpha1.ServiceRefNamespacedRef,
			NamespacedRef: &commonv1alpha1.NameRef{
				Name: ks.Name,
			},
		}
	}
	return b
}

// WithOwner sets the owner reference for the KongRoute to the given HTTPRoute.
func (b *KongRouteBuilder) WithOwner(owner *gwtypes.HTTPRoute) *KongRouteBuilder {
	controllerutil.SetOwnerReference(owner, &b.route, scheme.Get(), controllerutil.WithBlockOwnerDeletion(true))
	return b
}

// WithMetadata sets metadata (generateName, namespace, labels, annotations) for the KongRoute.
func (b *KongRouteBuilder) WithMetadata(route *gwtypes.HTTPRoute, parentRef *gwtypes.ParentReference, match *gwtypes.HTTPRouteMatch) *KongRouteBuilder {
	b.route.SetGenerateName(route.GetName() + "-")
	b.route.SetNamespace(route.GetNamespace())

	labels := map[string]string{
		consts.GatewayOperatorManagedByLabel:          consts.HTTPRouteManagedByLabel,
		consts.GatewayOperatorManagedByNameLabel:      route.GetName(),
		consts.GatewayOperatorManagedByNamespaceLabel: route.GetNamespace(),
		consts.GatewayOperatorHashSpecLabel:           utils.Hash64(match),
	}
	b.route.SetLabels(labels)

	annotations := map[string]string{
		consts.GatewayOperatorHybridRouteAnnotation:    routes.HTTPRouteKey + "|" + client.ObjectKeyFromObject(route).String(),
		consts.GatewayOperatorHybridGatewaysAnnotation: string(parentRef.Name),
	}
	b.route.SetAnnotations(annotations)

	return b
}

// Build returns the constructed KongRoute resource.
func (b *KongRouteBuilder) Build() configurationv1alpha1.KongRoute {
	return b.route
}
