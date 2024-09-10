package konnect

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

const (
	// IndexFieldKongPluginBindingKongPluginReference is the index field for KongPlugin -> KongPluginBinding.
	IndexFieldKongPluginBindingKongPluginReference = "kongPluginRef"
	// IndexFieldKongPluginBindingKongClusterPluginReference is the index field for KongClusterPlugin -> KongPluginBinding.
	IndexFieldKongPluginBindingKongClusterPluginReference = "kongClusterPluginRef"
	// IndexFieldKongPluginBindingKongServiceReference is the index field for KongService -> KongPluginBinding.
	IndexFieldKongPluginBindingKongServiceReference = "kongServiceRef"
	// IndexFieldKongPluginBindingKongServiceReference is the index field for KongRoute -> KongPluginBinding.
	IndexFieldKongPluginBindingKongRouteReference = "kongRouteRef"
	// IndexFieldKongPluginBindingKongServiceReference is the index field for KongConsumer -> KongPluginBinding.
	IndexFieldKongPluginBindingKongConsumerReference = "kongConsumerRef"
	// IndexFieldKongPluginBindingKongServiceReference is the index field for KongConsumerGroup -> KongPluginBinding.
	IndexFieldKongPluginBindingKongConsumerGroupReference = "kongConsumerGroupRef"
)

// IndexOptionsForKongPluginBinding returns required Index options for KongPluginBinding reconclier.
func IndexOptionsForKongPluginBinding() []ReconciliationIndexOption {
	return []ReconciliationIndexOption{
		{
			IndexObject:  &configurationv1alpha1.KongPluginBinding{},
			IndexField:   IndexFieldKongPluginBindingKongPluginReference,
			ExtractValue: kongPluginReferencesFromKongPluginBinding,
		},
		{
			IndexObject:  &configurationv1alpha1.KongPluginBinding{},
			IndexField:   IndexFieldKongPluginBindingKongClusterPluginReference,
			ExtractValue: kongClusterPluginReferencesFromKongPluginBinding,
		},
		{
			IndexObject:  &configurationv1alpha1.KongPluginBinding{},
			IndexField:   IndexFieldKongPluginBindingKongServiceReference,
			ExtractValue: kongServiceReferencesFromKongPluginBinding,
		},
		{
			IndexObject:  &configurationv1alpha1.KongPluginBinding{},
			IndexField:   IndexFieldKongPluginBindingKongRouteReference,
			ExtractValue: kongRouteReferencesFromKongPluginBinding,
		},
		{
			IndexObject:  &configurationv1alpha1.KongPluginBinding{},
			IndexField:   IndexFieldKongPluginBindingKongConsumerReference,
			ExtractValue: kongConsumerReferencesFromKongPluginBinding,
		},
		{
			IndexObject:  &configurationv1alpha1.KongPluginBinding{},
			IndexField:   IndexFieldKongPluginBindingKongConsumerGroupReference,
			ExtractValue: kongConsumerGroupReferencesFromKongPluginBinding,
		},
	}
}

// kongPluginReferencesFromKongPluginBinding returns namespace/name of referenced KongPlugin in KongPluginBinding spec.
func kongPluginReferencesFromKongPluginBinding(obj client.Object) []string {
	binding, ok := obj.(*configurationv1alpha1.KongPluginBinding)
	if !ok {
		return nil
	}
	if binding.Spec.PluginReference.Kind != nil && *binding.Spec.PluginReference.Kind != "KongPlugin" {
		return nil
	}
	return []string{binding.Namespace + "/" + binding.Spec.PluginReference.Name}
}

// kongClusterPluginReferencesFromKongPluginBinding returns name of referenced KongClusterPlugin in KongPluginBinding spec.
func kongClusterPluginReferencesFromKongPluginBinding(obj client.Object) []string {
	binding, ok := obj.(*configurationv1alpha1.KongPluginBinding)
	if !ok {
		return nil
	}
	if binding.Spec.PluginReference.Kind == nil || *binding.Spec.PluginReference.Kind != "KongClusterPlugin" {
		return nil
	}
	return []string{binding.Spec.PluginReference.Name}
}

// kongServiceReferencesFromKongPluginBinding returns name of referenced KongService in KongPluginBinding spec.
func kongServiceReferencesFromKongPluginBinding(obj client.Object) []string {
	binding, ok := obj.(*configurationv1alpha1.KongPluginBinding)
	if !ok {
		return nil
	}
	if binding.Spec.Targets.ServiceReference == nil ||
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
	if binding.Spec.Targets.RouteReference == nil ||
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
	if binding.Spec.Targets.ConsumerReference == nil {
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
	if binding.Spec.Targets.ConsumerGroupReference == nil {
		return nil
	}
	return []string{binding.Spec.Targets.ConsumerGroupReference.Name}
}
