/*
Copyright 2026 Kong, Inc.

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

// ControlPlaneRef identifies the control plane this DataPlane connects to.
// The Type field determines which sub-field is active.
//
// +kubebuilder:object:generate=true
// +kubebuilder:validation:XValidation:rule="self.type != 'konnectNamespacedRef' || has(self.konnectNamespacedRef)",message="konnectNamespacedRef must be set when type is konnectNamespacedRef"
// +kubebuilder:validation:XValidation:rule="self.type != 'onpremNamespacedRef' || has(self.onpremNamespacedRef)",message="onpremNamespacedRef must be set when type is onpremNamespacedRef"
// +kubebuilder:validation:XValidation:rule="!(has(self.konnectNamespacedRef) && has(self.onpremNamespacedRef))",message="only one of konnectNamespacedRef or onpremNamespacedRef can be set"
type ControlPlaneRef struct {
	// Type indicates the type of the control plane being referenced.
	//
	// +required
	Type ControlPlaneRefType `json:"type,omitempty"`

	// KonnectNamespacedRef references a KonnectAIGateway (controlplane) resource in the same namespace.
	// Must be set when type is konnectNamespacedRef; validated by CEL rules on this struct.
	//
	// +optional
	KonnectNamespacedRef *NamespacedRef `json:"konnectNamespacedRef,omitempty"`

	// OnPremNamespacedRef references an OnPremAIGateway (controlplane) resource in the same namespace.
	// Must be set when type is onpremNamespacedRef; validated by CEL rules on this struct.
	//
	// +optional
	OnPremNamespacedRef *NamespacedRef `json:"onpremNamespacedRef,omitempty"`
}

// ControlPlaneRefType identifies the kind of control plane being referenced.
//
// +kubebuilder:validation:Enum=konnectNamespacedRef;onpremNamespacedRef
type ControlPlaneRefType string

const (
	// ControlPlaneRefTypeKonnectNamespacedRef references a KonnectAIGateway
	// resource in the same namespace as the DataPlane.
	ControlPlaneRefTypeKonnectNamespacedRef ControlPlaneRefType = "konnectNamespacedRef"

	// ControlPlaneRefTypeOnPremNamespacedRef references an OnPremAIGateway
	// resource in the same namespace as the DataPlane.
	ControlPlaneRefTypeOnPremNamespacedRef ControlPlaneRefType = "onpremNamespacedRef"
)

// NamespacedRef is a reference to a namespaced resource. It is shared by the
// different ControlPlaneRef types, which reference control planes in the same
// namespace as the referencing AIGatewayDataPlane.
//
// +kubebuilder:object:generate=true
type NamespacedRef struct {
	// Name is the name of the referenced resource.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name,omitempty"`
}
