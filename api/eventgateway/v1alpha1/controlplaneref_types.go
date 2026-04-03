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
// +kubebuilder:validation:XValidation:rule="self.type == 'konnectNamespacedRef' ? has(self.konnectNamespacedRef) : true",message="konnectNamespacedRef must be set when type is konnectNamespacedRef"
type ControlPlaneRef struct {
	// Type indicates the type of the control plane being referenced.
	// Currently only konnectNamespacedRef is supported.
	//
	// +required
	Type ControlPlaneRefType `json:"type,omitempty"`

	// KonnectNamespacedRef references a KonnectEventGateway resource in the same namespace.
	// Must be set when type is konnectNamespacedRef; validated by CEL rules on this struct.
	//
	// +optional
	KonnectNamespacedRef *KonnectNamespacedRef `json:"konnectNamespacedRef,omitempty"`
}

// ControlPlaneRefType identifies the kind of control plane being referenced.
//
// +kubebuilder:validation:Enum=konnectNamespacedRef
type ControlPlaneRefType string

const (
	// ControlPlaneRefTypeKonnectNamespacedRef references a KonnectEventGateway
	// resource in the same namespace as the DataPlane.
	ControlPlaneRefTypeKonnectNamespacedRef ControlPlaneRefType = "konnectNamespacedRef"
)

// KonnectNamespacedRef is a reference to a KonnectEventGateway resource in the same namespace.
//
// +kubebuilder:object:generate=true
type KonnectNamespacedRef struct {
	// Name is the name of the KonnectEventGateway resource.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name,omitempty"`
}
