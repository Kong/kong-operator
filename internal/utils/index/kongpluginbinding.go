package index

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
)

const (
	// IndexFieldKongPluginBindingKongPluginReference is the index field for KongPluginBinding -> KongPlugin.
	IndexFieldKongPluginBindingKongPluginReference = "kongPluginBindingPluginRef"
	// IndexFieldKongPluginBindingKongServiceReference is the index field for KongPluginBinding -> KongService.
	IndexFieldKongPluginBindingKongServiceReference = "kongPluginBindingServiceRef"
	// IndexFieldKongPluginBindingKongRouteReference is the index field for KongPluginBinding -> KongRoute.
	IndexFieldKongPluginBindingKongRouteReference = "kongPluginBindingRouteRef"
	// IndexFieldKongPluginBindingKongConsumerReference is the index field for KongPluginBinding -> KongConsumer.
	IndexFieldKongPluginBindingKongConsumerReference = "kongPluginBindingConsumerRef"
	// IndexFieldKongPluginBindingKongConsumerGroupReference is the index field for KongPluginBinding -> KongConsumerGroup.
	IndexFieldKongPluginBindingKongConsumerGroupReference = "kongPluginBindingConsumerGroupRef"
	// IndexFieldKongPluginBindingKonnectGatewayControlPlane is the index field for KongPluginBinding -> KonnectGatewayControlPlane.
	IndexFieldKongPluginBindingKonnectGatewayControlPlane = "kongPluginBindingKonnectGatewayControlPlaneRef"
)

// OptionsForKongPluginBinding returns required Index options for KongPluginBinding reconciler.
func OptionsForKongPluginBinding() []Option {
	return []Option{
		{
			Object:         &configurationv1alpha1.KongPluginBinding{},
			Field:          IndexFieldKongPluginBindingKongPluginReference,
			ExtractValueFn: kongPluginReferencesFromKongPluginBinding,
		},
		{
			Object:         &configurationv1alpha1.KongPluginBinding{},
			Field:          IndexFieldKongPluginBindingKongServiceReference,
			ExtractValueFn: kongServiceReferencesFromKongPluginBinding,
		},
		{
			Object:         &configurationv1alpha1.KongPluginBinding{},
			Field:          IndexFieldKongPluginBindingKongRouteReference,
			ExtractValueFn: kongRouteReferencesFromKongPluginBinding,
		},
		{
			Object:         &configurationv1alpha1.KongPluginBinding{},
			Field:          IndexFieldKongPluginBindingKongConsumerReference,
			ExtractValueFn: kongConsumerReferencesFromKongPluginBinding,
		},
		{
			Object:         &configurationv1alpha1.KongPluginBinding{},
			Field:          IndexFieldKongPluginBindingKongConsumerGroupReference,
			ExtractValueFn: kongConsumerGroupReferencesFromKongPluginBinding,
		},
		{
			Object:         &configurationv1alpha1.KongPluginBinding{},
			Field:          IndexFieldKongPluginBindingKonnectGatewayControlPlane,
			ExtractValueFn: kongPluginBindingReferencesKonnectGatewayControlPlane,
		},
	}
}

// kongPluginReferencesFromKongPluginBinding returns namespace/name of referenced KongPlugin in KongPluginBinding spec.
func kongPluginReferencesFromKongPluginBinding(obj client.Object) []string {
	binding, ok := obj.(*configurationv1alpha1.KongPluginBinding)
	if !ok {
		return nil
	}
	pluginRef := binding.Spec.PluginReference

	if pluginRef.Kind != nil && *pluginRef.Kind != "KongPlugin" {
		return nil
	}
	if pluginRef.Namespace != "" && pluginRef.Namespace != binding.Namespace {
		return []string{pluginRef.Namespace + "/" + pluginRef.Name}
	}

	return []string{binding.Namespace + "/" + pluginRef.Name}
}

// kongServiceReferencesFromKongPluginBinding returns name of referenced KongService in KongPluginBinding spec.
func kongServiceReferencesFromKongPluginBinding(obj client.Object) []string {
	binding, ok := obj.(*configurationv1alpha1.KongPluginBinding)
	if !ok {
		return nil
	}
	if binding.Spec.Targets == nil ||
		binding.Spec.Targets.ServiceReference == nil ||
		binding.Spec.Targets.ServiceReference.Group != configurationv1alpha1.GroupVersion.Group ||
		binding.Spec.Targets.ServiceReference.Kind != "KongService" {
		return nil
	}
	return []string{binding.Spec.Targets.ServiceReference.Name}
}

// kongRouteReferencesFromKongPluginBinding returns name of referenced KongRoute in KongPluginBinding spec.
func kongRouteReferencesFromKongPluginBinding(obj client.Object) []string {
	binding, ok := obj.(*configurationv1alpha1.KongPluginBinding)
	if !ok {
		return nil
	}
	if binding.Spec.Targets == nil ||
		binding.Spec.Targets.RouteReference == nil ||
		binding.Spec.Targets.RouteReference.Group != configurationv1alpha1.GroupVersion.Group ||
		binding.Spec.Targets.RouteReference.Kind != "KongRoute" {
		return nil
	}
	return []string{binding.Spec.Targets.RouteReference.Name}
}

// kongConsumerReferencesFromKongPluginBinding returns name of referenced KongConsumer in KongPluginBinding spec.
func kongConsumerReferencesFromKongPluginBinding(obj client.Object) []string {
	binding, ok := obj.(*configurationv1alpha1.KongPluginBinding)
	if !ok {
		return nil
	}
	if binding.Spec.Targets == nil || binding.Spec.Targets.ConsumerReference == nil {
		return nil
	}
	return []string{binding.Spec.Targets.ConsumerReference.Name}
}

// kongConsumerGroupReferencesFromKongPluginBinding returns name of referenced KongConsumerGroup in KongPluginBinding spec.
func kongConsumerGroupReferencesFromKongPluginBinding(obj client.Object) []string {
	binding, ok := obj.(*configurationv1alpha1.KongPluginBinding)
	if !ok {
		return nil
	}
	if binding.Spec.Targets == nil || binding.Spec.Targets.ConsumerGroupReference == nil {
		return nil
	}
	return []string{binding.Spec.Targets.ConsumerGroupReference.Name}
}

// kongPluginBindingReferencesKonnectGatewayControlPlane returns name of referenced KonnectGatewayControlPlane in KongPluginBinding spec.
func kongPluginBindingReferencesKonnectGatewayControlPlane(obj client.Object) []string {
	binding, ok := obj.(*configurationv1alpha1.KongPluginBinding)
	if !ok {
		return nil
	}

	cpRef, ok := controlPlaneRefIsKonnectNamespacedRef(binding)
	if !ok {
		return nil
	}

	// NOTE: This provides support for setting the namespace of the KonnectGatewayControlPlane ref
	// but CRDs have validation rules in place which will disallow this until
	// cross namespace refs are allowed.
	namespace := binding.Namespace
	if cpRef.KonnectNamespacedRef.Namespace != "" {
		namespace = cpRef.KonnectNamespacedRef.Namespace
	}

	return []string{namespace + "/" + cpRef.KonnectNamespacedRef.Name}
}
