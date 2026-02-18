package builder

import (
	"errors"
	"fmt"
	"maps"

	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/metadata"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

// KongPluginBindingBuilder is a builder for configurationv1alpha1.KongPluginBinding resources.
type KongPluginBindingBuilder struct {
	binding configurationv1alpha1.KongPluginBinding
	errors  []error
}

// NewKongPluginBinding creates and returns a new KongPluginBindingBuilder instance.
func NewKongPluginBinding() *KongPluginBindingBuilder {
	return &KongPluginBindingBuilder{
		binding: configurationv1alpha1.KongPluginBinding{},
		errors:  make([]error, 0),
	}
}

// WithName sets the name for the KongPluginBinding being built.
func (b *KongPluginBindingBuilder) WithName(name string) *KongPluginBindingBuilder {
	b.binding.Name = name
	return b
}

// WithNamespace sets the namespace for the KongPluginBinding being built.
func (b *KongPluginBindingBuilder) WithNamespace(namespace string) *KongPluginBindingBuilder {
	b.binding.Namespace = namespace
	return b
}

// WithLabels sets the labels for the KongPluginBinding resource based on the given HTTPRoute.
func (b *KongPluginBindingBuilder) WithLabels(route *gwtypes.HTTPRoute, parentRef *gwtypes.ParentReference) *KongPluginBindingBuilder {
	labels := metadata.BuildLabels(route, parentRef)
	if b.binding.Labels == nil {
		b.binding.Labels = make(map[string]string)
	}
	maps.Copy(b.binding.Labels, labels)
	return b
}

// WithAnnotations sets the annotations for the KongPluginBinding resource based on the given HTTPRoute and parent reference.
func (b *KongPluginBindingBuilder) WithAnnotations(route *gwtypes.HTTPRoute, parentRef *gwtypes.ParentReference) *KongPluginBindingBuilder {
	annotations := metadata.BuildAnnotations(route, parentRef)
	if b.binding.Annotations == nil {
		b.binding.Annotations = make(map[string]string)
	}
	maps.Copy(b.binding.Annotations, annotations)
	return b
}

// WithPluginRef sets the plugin reference for the KongPluginBinding.
func (b *KongPluginBindingBuilder) WithPluginRef(name string) *KongPluginBindingBuilder {
	b.binding.Spec.PluginReference.Name = name
	return b
}

// WithRouteRef sets the KongRoute reference for the KongPluginBinding.
func (b *KongPluginBindingBuilder) WithRouteRef(name string) *KongPluginBindingBuilder {
	if b.binding.Spec.Targets == nil {
		b.binding.Spec.Targets = &configurationv1alpha1.KongPluginBindingTargets{}
	}
	b.binding.Spec.Targets.RouteReference = &configurationv1alpha1.TargetRefWithGroupKind{
		Name:  name,
		Group: "configuration.konghq.com",
		Kind:  "KongRoute",
	}
	return b
}

// WithServiceRef sets the KongService reference for the KongPluginBinding.
func (b *KongPluginBindingBuilder) WithServiceRef(name string) *KongPluginBindingBuilder {
	if b.binding.Spec.Targets == nil {
		b.binding.Spec.Targets = &configurationv1alpha1.KongPluginBindingTargets{}
	}
	b.binding.Spec.Targets.ServiceReference = &configurationv1alpha1.TargetRefWithGroupKind{
		Name:  name,
		Group: "configuration.konghq.com",
		Kind:  "KongService",
	}
	return b
}

// WithControlPlaneRef sets the ControlPlaneRef for the KongPluginBinding being built.
func (b *KongPluginBindingBuilder) WithControlPlaneRef(cpr commonv1alpha1.ControlPlaneRef) *KongPluginBindingBuilder {
	b.binding.Spec.ControlPlaneRef = cpr
	return b
}

// WithOwner sets the owner reference for the KongPluginBinding to the given HTTPRoute.
func (b *KongPluginBindingBuilder) WithOwner(owner *gwtypes.HTTPRoute) *KongPluginBindingBuilder {
	if owner == nil {
		b.errors = append(b.errors, errors.New("owner cannot be nil"))
		return b
	}

	err := controllerutil.SetOwnerReference(owner, &b.binding, scheme.Get(), controllerutil.WithBlockOwnerDeletion(true))
	if err != nil {
		b.errors = append(b.errors, fmt.Errorf("failed to set owner reference: %w", err))
	}
	return b
}

// Build returns the constructed KongPluginBinding resource and any accumulated errors.
func (b *KongPluginBindingBuilder) Build() (configurationv1alpha1.KongPluginBinding, error) {
	if len(b.errors) > 0 {
		return configurationv1alpha1.KongPluginBinding{}, errors.Join(b.errors...)
	}
	return b.binding, nil
}

// MustBuild returns the constructed KongPluginBinding resource, panicking on any errors.
// Useful for tests or when you're certain the build will succeed.
func (b *KongPluginBindingBuilder) MustBuild() configurationv1alpha1.KongPluginBinding {
	binding, err := b.Build()
	if err != nil {
		panic(fmt.Errorf("failed to build KongPluginBinding: %w", err))
	}
	return binding
}
