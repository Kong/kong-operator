/*
Copyright 2025 Kong, Inc.

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

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// KongReferenceGrant identifies kinds of resources in other namespaces that are
// trusted to reference the specified kinds of resources in the same namespace
// as the policy.
//
// Each KongReferenceGrant can be used to represent a unique trust relationship.
// Additional Reference Grants can be used to add to the set of trusted
// sources of inbound references for the namespace they are defined within.
//
// All cross-namespace references in Kong APIs require a KongReferenceGrant.
//
// KongReferenceGrant is a form of runtime verification allowing users to assert
// which cross-namespace object references are permitted.
//
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:resource:categories=kong
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kong:channels=kong-operator
// +apireference:kgo:include
type KongReferenceGrant struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the desired state of KongReferenceGrant.
	// +optional
	Spec KongReferenceGrantSpec `json:"spec,omitempty"`

	// Note that `Status` sub-resource has been excluded at the
	// moment as it was difficult to work out the design.
	// `Status` sub-resource may be added in future.
}

// KongReferenceGrantList contains a list of KongReferenceGrant.
//
// +kubebuilder:object:root=true
type KongReferenceGrantList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KongReferenceGrant `json:"items"`
}

// KongReferenceGrantSpec identifies a cross namespace relationship that is trusted
// for Kong APIs.
type KongReferenceGrantSpec struct {
	// From describes the trusted namespaces and kinds that can reference the
	// resources described in "To". Each entry in this list MUST be considered
	// to be an additional place that references can be valid from, or to put
	// this another way, entries MUST be combined using OR.
	//
	// +required
	// +listType=atomic
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=16
	From []ReferenceGrantFrom `json:"from"`

	// To describes the resources that may be referenced by the resources
	// described in "From". Each entry in this list MUST be considered to be an
	// additional place that references can be valid to, or to put this another
	// way, entries MUST be combined using OR.
	//
	// +required
	// +listType=atomic
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=16
	To []ReferenceGrantTo `json:"to"`
}

// ReferenceGrantFrom describes trusted namespaces and kinds.
//
// +kubebuilder:validation:XValidation:rule=".self.group != 'configuration.konghq.com' || .self.kind in ['KongService', 'KongCertificate' ]",message="Only KongCertificate and KongService kinds are supported for 'configuration.konghq.com' group"
type ReferenceGrantFrom struct {
	// Group is the group of the referent.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	Group Group `json:"group"`

	// Kind is the kind of the referent.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	Kind Kind `json:"kind"`

	// Namespace is the namespace of the referent.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	Namespace Namespace `json:"namespace"`
}

// ReferenceGrantTo describes what Kinds are allowed as targets of the
// references.
//
// +kubebuilder:validation:XValidation:rule=".self.group != 'core' || .self.kind == 'Secret'",message="Only 'Secret' kind is supported for 'core' group"
// +kubebuilder:validation:XValidation:rule=".self.group != 'konnect.konghq.com' || .self.kind in ['KonnectGatewayControlPlane', 'KonnectAPIAuthConfiguration']",message="Only 'KonnectGatewayControlPlane' and 'KonnectAPIAuthConfiguration' kinds are supported for 'konnect.konghq.com' group"
type ReferenceGrantTo struct {
	// Group is the group of the referent.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	Group Group `json:"group"`

	// Kind is the kind of the referent.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	Kind Kind `json:"kind"`

	// Name is the name of the referent. When unspecified, this policy
	// refers to all resources of the specified Group and Kind in the local
	// namespace.
	//
	// +optional
	Name *ObjectName `json:"name,omitempty"`
}

func init() {
	SchemeBuilder.Register(&KongReferenceGrant{}, &KongReferenceGrantList{})
}
