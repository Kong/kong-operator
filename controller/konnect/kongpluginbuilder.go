package konnect

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	commonv1alpha1 "github.com/kong/kong-operator/apis/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/apis/configuration/v1alpha1"
)

// KongPluginBindingBuilder helps to build KongPluginBinding objects.
type KongPluginBindingBuilder struct {
	binding *configurationv1alpha1.KongPluginBinding
}

// NewKongPluginBindingBuilder creates a new KongPluginBindingBuilder.
func NewKongPluginBindingBuilder() *KongPluginBindingBuilder {
	return &KongPluginBindingBuilder{
		binding: &configurationv1alpha1.KongPluginBinding{},
	}
}

// WithName sets the name of the KongPluginBinding.
func (b *KongPluginBindingBuilder) WithName(name string) *KongPluginBindingBuilder {
	b.binding.Name = name
	return b
}

// WithGenerateName sets the generate name of the KongPluginBinding.
func (b *KongPluginBindingBuilder) WithGenerateName(name string) *KongPluginBindingBuilder {
	b.binding.GenerateName = name
	return b
}

// WithNamespace sets the namespace of the KongPluginBinding.
func (b *KongPluginBindingBuilder) WithNamespace(namespace string) *KongPluginBindingBuilder {
	b.binding.Namespace = namespace
	return b
}

// WithPluginRef sets the plugin reference of the KongPluginBinding.
func (b *KongPluginBindingBuilder) WithPluginRef(pluginName string) *KongPluginBindingBuilder {
	b.binding.Spec.PluginReference.Name = pluginName
	return b
}

// WithControlPlaneRef sets the control plane reference of the KongPluginBinding.
// NOTE: Users have to ensure that the ControlPlaneRef that's set here
// is the same across all the KongPluginBinding targets.
func (b *KongPluginBindingBuilder) WithControlPlaneRef(ref commonv1alpha1.ControlPlaneRef) *KongPluginBindingBuilder {
	b.binding.Spec.ControlPlaneRef = ref
	return b
}

// WithControlPlaneRefKonnectNamespaced sets the control plane reference of the KongPluginBinding.
// NOTE: Users have to ensure that the ControlPlaneRef that's set here
// is the same across all the KongPluginBinding targets.
func (b *KongPluginBindingBuilder) WithControlPlaneRefKonnectNamespaced(name string) *KongPluginBindingBuilder {
	b.binding.Spec.ControlPlaneRef = commonv1alpha1.ControlPlaneRef{
		Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
		KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
			Name: name,
		},
	}

	return b
}

// WithConsumerTarget sets the consumer target of the KongPluginBinding.
func (b *KongPluginBindingBuilder) WithConsumerTarget(consumer string) *KongPluginBindingBuilder {
	if b.binding.Spec.Targets == nil {
		b.binding.Spec.Targets = &configurationv1alpha1.KongPluginBindingTargets{}
	}
	b.binding.Spec.Targets.ConsumerReference = &configurationv1alpha1.TargetRef{
		Name: consumer,
	}
	return b
}

// WithConsumerGroupTarget sets the consumer group target of the KongPluginBinding.
func (b *KongPluginBindingBuilder) WithConsumerGroupTarget(cg string) *KongPluginBindingBuilder {
	if b.binding.Spec.Targets == nil {
		b.binding.Spec.Targets = &configurationv1alpha1.KongPluginBindingTargets{}
	}
	b.binding.Spec.Targets.ConsumerGroupReference = &configurationv1alpha1.TargetRef{
		Name: cg,
	}
	return b
}

// WithServiceTarget sets the service target of the KongPluginBinding.
func (b *KongPluginBindingBuilder) WithServiceTarget(serviceName string) *KongPluginBindingBuilder {
	if b.binding.Spec.Targets == nil {
		b.binding.Spec.Targets = &configurationv1alpha1.KongPluginBindingTargets{}
	}
	b.binding.Spec.Targets.ServiceReference = &configurationv1alpha1.TargetRefWithGroupKind{
		Group: configurationv1alpha1.GroupVersion.Group,
		Kind:  "KongService",
		Name:  serviceName,
	}
	return b
}

// WithRouteTarget sets the route target of the KongPluginBinding.
func (b *KongPluginBindingBuilder) WithRouteTarget(routeName string) *KongPluginBindingBuilder {
	if b.binding.Spec.Targets == nil {
		b.binding.Spec.Targets = &configurationv1alpha1.KongPluginBindingTargets{}
	}
	b.binding.Spec.Targets.RouteReference = &configurationv1alpha1.TargetRefWithGroupKind{
		Group: configurationv1alpha1.GroupVersion.Group,
		Kind:  "KongRoute",
		Name:  routeName,
	}
	return b
}

// WithOwnerReference sets the owner reference of the KongPluginBinding.
func (b *KongPluginBindingBuilder) WithOwnerReference(owner client.Object, scheme *runtime.Scheme) (*KongPluginBindingBuilder, error) {
	opts := []controllerutil.OwnerReferenceOption{
		controllerutil.WithBlockOwnerDeletion(true),
	}
	if err := controllerutil.SetOwnerReference(owner, b.binding, scheme, opts...); err != nil {
		return nil, fmt.Errorf("failed to set owner reference: %w", err)
	}

	return b, nil
}

// WithScope sets the scope of the KongPluginBinding.
func (b *KongPluginBindingBuilder) WithScope(scope configurationv1alpha1.KongPluginBindingScope) *KongPluginBindingBuilder {
	b.binding.Spec.Scope = scope
	return b
}

// Build returns the KongPluginBinding.
func (b *KongPluginBindingBuilder) Build() *configurationv1alpha1.KongPluginBinding {
	return b.binding
}
