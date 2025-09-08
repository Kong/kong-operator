/*
Copyright 2024 Kong, Inc.

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

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/common/v1alpha1"
	konnectv1alpha2 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha2"
)

// KongKeySet is the schema for KongKeySet API which defines a KongKeySet entity.
//
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:resource:categories=kong
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Programmed",description="The Resource is Programmed on Konnect",type=string,JSONPath=`.status.conditions[?(@.type=='Programmed')].status`
// +kubebuilder:validation:XValidation:rule="(!self.status.conditions.exists(c, c.type == 'Programmed' && c.status == 'True')) ? true : oldSelf.spec.controlPlaneRef == self.spec.controlPlaneRef", message="spec.controlPlaneRef is immutable when an entity is already Programmed"
// +kubebuilder:validation:XValidation:rule="(!has(self.spec.controlPlaneRef) || !has(self.spec.controlPlaneRef.konnectNamespacedRef)) ? true : !has(self.spec.controlPlaneRef.konnectNamespacedRef.__namespace__)", message="spec.controlPlaneRef cannot specify namespace for namespaced resource"
// +apireference:kgo:include
// +kong:channels=gateway-operator
type KongKeySet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec KongKeySetSpec `json:"spec"`

	// +kubebuilder:default={conditions: {{type: "Programmed", status: "Unknown", reason:"Pending", message:"Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}}
	Status KongKeySetStatus `json:"status,omitempty"`
}

// KongKeySetSpec defines the spec for a KongKeySet.
// +kubebuilder:validation:XValidation:rule="!has(self.controlPlaneRef) ? true : self.controlPlaneRef.type != 'kic'", message="KIC is not supported as control plane"
// +apireference:kgo:include
type KongKeySetSpec struct {
	// ControlPlaneRef is a reference to a Konnect ControlPlane with which KongKeySet is associated.
	// +kubebuilder:validation:XValidation:message="'konnectID' type is not supported", rule="self.type != 'konnectID'"
	// +required
	ControlPlaneRef *commonv1alpha1.ControlPlaneRef `json:"controlPlaneRef"`

	// KongKeySetAPISpec are the attributes of the KongKeySet itself.
	KongKeySetAPISpec `json:",inline"`
}

// KongKeySetAPISpec defines the attributes of a Kong KeySet.
// +apireference:kgo:include
type KongKeySetAPISpec struct {
	// Name is a name of the KeySet.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Tags is an optional set of strings associated with the KeySet for grouping and filtering.
	// +optional
	Tags commonv1alpha1.Tags `json:"tags,omitempty"`
}

// KongKeySetStatus defines the status for a KongKeySet.
// +apireference:kgo:include
type KongKeySetStatus struct {
	// Konnect contains the Konnect entity status.
	// +optional
	Konnect *konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef `json:"konnect,omitempty"`

	// Conditions describe the status of the Konnect entity.
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=8
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// KongKeySetList contains a list of Kong Keys.
// +kubebuilder:object:root=true
// +apireference:kgo:include
type KongKeySetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KongKeySet `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KongKeySet{}, &KongKeySetList{})
}
