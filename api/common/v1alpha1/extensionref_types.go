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
// +apireference:kgo:include
type ExtensionRef struct {
	// Group is the group of the extension resource.
	// +optional
	// +kubebuilder:default=gateway-operator.konghq.com
	Group string `json:"group"`

	// Kind is kind of the extension resource.
	Kind string `json:"kind"`

	// NamespacedRef is a reference to the extension resource.
	NamespacedRef `json:",inline"`
}
