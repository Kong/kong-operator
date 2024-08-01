/*
Copyright 2022 Kong Inc.

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

package v1alpha1

// ExtensionRef corresponds to another resource in the Kubernetes cluster which
// defines extended behavior for a resource (e.g. ControlPlane).
type ExtensionRef struct {
	// Group is the group of the extension resource.
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=gateway-operator.konghq.com
	Group string `json:"group"`

	// Kind is kind of the extension resource.
	Kind string `json:"kind"`

	// NamespacedRef is a reference to the extension resource.
	NamespacedRef `json:",inline"`
}

// NamespacedRef is a reference to a namespaced resource.
type NamespacedRef struct {
	// Name is the name of the referred resource.
	//
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name"`

	// Namespace is the namespace of the referred resource.
	//
	// For namespace-scoped resources if no Namespace is provided then the
	// namespace of the parent object MUST be used.
	//
	// This field MUST not be set when referring to cluster-scoped resources.
	//
	// +optional
	Namespace *string `json:"namespace,omitempty"`
}
