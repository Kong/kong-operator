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
// +kubebuilder:validation:XValidation:rule="self.type != 'onpremNamespacedRef' || has(self.onpremNamespacedRef)",message="onpremNamespacedRef must be set when type is onpremNamespacedRef"
// +kubebuilder:validation:XValidation:rule="!(has(self.konnectNamespacedRef) && has(self.onpremNamespacedRef))",message="only one of konnectNamespacedRef or onpremNamespacedRef can be set"
// +kubebuilder:validation:XValidation:rule="self.type == oldSelf.type",message="type is immutable"
// +kubebuilder:validation:XValidation:rule="self.type != 'onpremNamespacedRef' || !has(oldSelf.onpremNamespacedRef) || self.onpremNamespacedRef == oldSelf.onpremNamespacedRef",message="onpremNamespacedRef is immutable"
type ControlPlaneRef struct {
	// Type indicates the type of the control plane being referenced: a
	// KonnectEventGateway (konnectNamespacedRef) or an OnPremEventGateway
	// (onpremNamespacedRef). It can't be changed, and neither can an
	// onpremNamespacedRef.
	//
	// +required
	Type ControlPlaneRefType `json:"type,omitempty"`

	// KonnectNamespacedRef references a KonnectEventGateway resource in the same namespace.
	// Must be set when type is konnectNamespacedRef; validated by CEL rules on this struct.
	//
	// +optional
	KonnectNamespacedRef *KonnectNamespacedRef `json:"konnectNamespacedRef,omitempty"`

	// OnPremNamespacedRef references an OnPremEventGateway resource in the
	// same namespace. The data plane pulls its configuration from the gw-admin
	// pods of the GWAdmin of that gateway, and authenticates with a client
	// certificate trusted by the cp client CA of the GWAdmin.
	// Must be set when type is onpremNamespacedRef; validated by CEL rules on this struct.
	//
	// +optional
	OnPremNamespacedRef *OnPremNamespacedRef `json:"onpremNamespacedRef,omitempty"`
}

// ControlPlaneRefType identifies the kind of control plane being referenced.
//
// +kubebuilder:validation:Enum=konnectNamespacedRef;onpremNamespacedRef
type ControlPlaneRefType string

const (
	// ControlPlaneRefTypeKonnectNamespacedRef references a KonnectEventGateway
	// resource in the same namespace as the DataPlane.
	ControlPlaneRefTypeKonnectNamespacedRef ControlPlaneRefType = "konnectNamespacedRef"

	// ControlPlaneRefTypeOnPremNamespacedRef references an OnPremEventGateway
	// resource in the same namespace as the DataPlane.
	ControlPlaneRefTypeOnPremNamespacedRef ControlPlaneRefType = "onpremNamespacedRef"
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

// OnPremNamespacedRef is a reference to an OnPremEventGateway resource in the same namespace.
//
// +kubebuilder:object:generate=true
type OnPremNamespacedRef struct {
	// Name is the name of the OnPremEventGateway resource.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name,omitempty"`
}
