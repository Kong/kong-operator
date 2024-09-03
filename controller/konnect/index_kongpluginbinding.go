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
)

// IndexOptionsForKongPluginBinding returns required Index options for KongPluginBinding reconclier.
func IndexOptionsForKongPluginBinding() []ReconciliationIndexOption {
	return []ReconciliationIndexOption{
		{
			IndexObject:  &configurationv1alpha1.KongPluginBinding{},
			IndexField:   IndexFieldKongPluginBindingKongClusterPluginReference,
			ExtractValue: kongPluginReferencesFromKongPluginBinding,
		},
		{
			IndexObject:  &configurationv1alpha1.KongPluginBinding{},
			IndexField:   IndexFieldKongPluginBindingKongClusterPluginReference,
			ExtractValue: kongClusterPluginReferencesFromKongPluginBinding,
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
