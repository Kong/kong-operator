package builder

import (
	"errors"
	"fmt"
	"maps"

	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/controller/hybridgateway/metadata"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/modules/manager/scheme"
)

// KongRouteBuilder is a builder for configurationv1alpha1.KongRoute resources.
type KongRouteBuilder struct {
	route  configurationv1alpha1.KongRoute
	errors []error
}

// NewKongRoute creates and returns a new KongRouteBuilder instance.
func NewKongRoute() *KongRouteBuilder {
	return &KongRouteBuilder{
		route:  configurationv1alpha1.KongRoute{},
		errors: make([]error, 0),
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
func (b *KongRouteBuilder) WithKongService(name string) *KongRouteBuilder {
	if name != "" {
		b.route.Spec.ServiceRef = &configurationv1alpha1.ServiceRef{
			Type: configurationv1alpha1.ServiceRefNamespacedRef,
			NamespacedRef: &commonv1alpha1.NameRef{
				Name: name,
			},
		}
	}
	return b
}

// WithSpecName sets the name field in the KongRoute spec.
func (b *KongRouteBuilder) WithSpecName(name string) *KongRouteBuilder {
	b.route.Spec.Name = &name
	return b
}

// WithStripPath sets the strip path option for the KongRoute.
func (b *KongRouteBuilder) WithStripPath(stripPath bool) *KongRouteBuilder {
	b.route.Spec.StripPath = &stripPath
	return b
}

// WithOwner sets the owner reference for the KongRoute to the given HTTPRoute.
func (b *KongRouteBuilder) WithOwner(owner *gwtypes.HTTPRoute) *KongRouteBuilder {
	if owner == nil {
		b.errors = append(b.errors, errors.New("owner cannot be nil"))
		return b
	}

	err := controllerutil.SetControllerReference(owner, &b.route, scheme.Get(), controllerutil.WithBlockOwnerDeletion(true))
	if err != nil {
		b.errors = append(b.errors, fmt.Errorf("failed to set owner reference: %w", err))
	}
	return b
}

// WithName sets the name field of the KongRoute resource.
func (b *KongRouteBuilder) WithName(name string) *KongRouteBuilder {
	b.route.Name = name
	return b
}

// WithNamespace sets the namespace field of the KongRoute resource.
func (b *KongRouteBuilder) WithNamespace(namespace string) *KongRouteBuilder {
	b.route.Namespace = namespace
	return b
}

// WithLabels sets the labels for the KongRoute resource based on the given HTTPRoute.
func (b *KongRouteBuilder) WithLabels(route *gwtypes.HTTPRoute) *KongRouteBuilder {
	labels := metadata.BuildLabels(route)
	if b.route.Labels == nil {
		b.route.Labels = make(map[string]string)
	}
	maps.Copy(b.route.Labels, labels)
	return b
}

// WithAnnotations sets the annotations for the KongRoute resource based on the given HTTPRoute and parent reference.
func (b *KongRouteBuilder) WithAnnotations(route *gwtypes.HTTPRoute, parentRef *gwtypes.ParentReference) *KongRouteBuilder {
	annotations := metadata.BuildAnnotations(route, parentRef)
	if b.route.Annotations == nil {
		b.route.Annotations = make(map[string]string)
	}
	maps.Copy(b.route.Annotations, annotations)
	return b
}

// Build returns the constructed KongRoute resource and any accumulated errors.
func (b *KongRouteBuilder) Build() (configurationv1alpha1.KongRoute, error) {
	if len(b.errors) > 0 {
		return configurationv1alpha1.KongRoute{}, errors.Join(b.errors...)
	}
	return b.route, nil
}

// MustBuild returns the constructed KongRoute resource, panicking on any errors.
// Useful for tests or when you're certain the build will succeed.
func (b *KongRouteBuilder) MustBuild() configurationv1alpha1.KongRoute {
	route, err := b.Build()
	if err != nil {
		panic(fmt.Errorf("failed to build KongRoute: %w", err))
	}
	return route
}
