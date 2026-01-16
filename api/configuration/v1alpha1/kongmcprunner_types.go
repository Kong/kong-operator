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

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
)

// KongMCPRunner is the schema for MCP Runners API which defines an MCP Runner.
//
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:resource:categories=kong
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Programmed",description="The Resource is Programmed on Konnect",type=string,JSONPath=`.status.conditions[?(@.type=='Programmed')].status`
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.spec.controlPlaneRef) || has(self.spec.controlPlaneRef)", message="controlPlaneRef is required once set"
// +kubebuilder:validation:XValidation:rule="(!has(self.spec.controlPlaneRef)) ? true : (!has(self.status) || !self.status.conditions.exists(c, c.type == 'Programmed' && c.status == 'True')) ? true : oldSelf.spec.controlPlaneRef == self.spec.controlPlaneRef", message="spec.controlPlaneRef is immutable when an entity is already Programmed"
// +apireference:kgo:include
// +kong:channels=kong-operator
type KongMCPRunner struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec KongMCPRunnerSpec `json:"spec"`

	// +kubebuilder:default={conditions: {{type: "Programmed", status: "Unknown", reason:"Pending", message:"Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}}
	Status KongMCPRunnerStatus `json:"status,omitempty"`
}

// KongMCPRunnerList contains a list of MCP Runners.
// +kubebuilder:object:root=true
// +apireference:kgo:include
type KongMCPRunnerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KongMCPRunner `json:"items"`
}

// KongMCPRunnerSpec defines specification of an MCP Runner.
// +kubebuilder:validation:XValidation:rule="!has(self.controlPlaneRef) ? true : self.controlPlaneRef.type != 'kic'", message="KIC is not supported as control plane"
// +apireference:kgo:include
type KongMCPRunnerSpec struct {
	// ControlPlaneRef is a reference to a ControlPlane this MCPRunner is associated with.
	// +kubebuilder:validation:XValidation:message="'konnectID' type is not supported", rule="self.type != 'konnectID'"
	// +required
	ControlPlaneRef *commonv1alpha1.ControlPlaneRef `json:"controlPlaneRef"`

	// Mirror is the Konnect Mirror configuration.
	// It is only applicable for MCPRunners that are created as Mirrors.
	// TODO: evaluate whether to keep this field required or not.
	//
	// +required
	Mirror *MirrorSpec `json:"mirror,omitempty"`
}

// KongMCPRunnerStatus represents the current status of the MCP Runner resource.
// +apireference:kgo:include
type KongMCPRunnerStatus struct {
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

// MirrorSpec contains the Konnect Mirror configuration.
type MirrorSpec struct {
	// Konnect contains the KonnectID of the MCPRunner that
	// is mirrored.
	//
	// +required
	Konnect MirrorKonnect `json:"konnect"`
}

// MirrorKonnect contains the Konnect Mirror configuration.
type MirrorKonnect struct {
	// ID is the ID of the Konnect entity. It can be set only in case
	// the MCPRunner type is Mirror.
	//
	// +required
	ID commonv1alpha1.KonnectIDType `json:"id"`
}

func init() {
	SchemeBuilder.Register(&KongMCPRunner{}, &KongMCPRunnerList{})
}
