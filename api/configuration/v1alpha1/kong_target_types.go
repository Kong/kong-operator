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
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// KongTarget is the schema for Target API which defines a Kong Target attached to a Kong Upstream.
//
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Programmed",description="The Resource is Programmed on Konnect",type=string,JSONPath=`.status.conditions[?(@.type=='Programmed')].status`
// +kubebuilder:validation:XValidation:rule="oldSelf.spec.upstreamRef == self.spec.upstreamRef", message="spec.upstreamRef is immutable"
type KongTarget struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec KongTargetSpec `json:"spec"`
	// +kubebuilder:default={conditions: {{type: "Programmed", status: "Unknown", reason:"Pending", message:"Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}}
	Status KongTargetStatus `json:"status,omitempty"`
}

type KongTargetSpec struct {
	// UpstreamRef is a reference to a KongUpstream this KongTarget is attached to.
	UpstreamRef TargetRef `json:"upstreamRef"`
	// KongTargetAPISpec are the attributes of the Kong Target itself.
	KongTargetAPISpec `json:",inline"`
}

type KongTargetAPISpec struct {
	// Target is the target address of the upstream.
	Target string `json:"target"`
	// Weight is the weight this target gets within the upstream loadbalancer.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=65535
	// +kubebuilder:default=100
	Weight int `json:"weight"`
	// Tags is an optional set of strings associated with the Target for grouping and filtering.
	Tags []string `json:"tags,omitempty"`
}

type KongTargetStatus struct {
	// Konnect contains the Konnect entity status.
	// +optional
	Konnect *konnectv1alpha1.KonnectEntityStatusWithControlPlaneAndUpstreamRefs `json:"konnect,omitempty"`

	// Conditions describe the status of the Konnect entity.
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=8
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true

// KongTargetList contains a list of Kong Targets.
type KongTargetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KongTarget `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KongTarget{}, &KongTargetList{})
}
