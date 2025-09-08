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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/common/v1alpha1"
	konnectv1alpha2 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha2"
)

// KongCredentialACL is the schema for ACL credentials API which defines a ACL credential for consumers.
//
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:resource:categories=kong
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Programmed",description="The Resource is Programmed on Konnect",type=string,JSONPath=`.status.conditions[?(@.type=='Programmed')].status`
// +kubebuilder:validation:XValidation:rule="(!self.status.conditions.exists(c, c.type == 'Programmed' && c.status == 'True')) ? true : oldSelf.spec.consumerRef == self.spec.consumerRef",message="spec.consumerRef is immutable when an entity is already Programmed"
// +apireference:kgo:include
// +kong:channels=gateway-operator
type KongCredentialACL struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec contains the ACL credential specification.
	Spec KongCredentialACLSpec `json:"spec"`

	// Status contains the ACL credential status.
	//
	// +kubebuilder:default={conditions: {{type: "Programmed", status: "Unknown", reason:"Pending", message:"Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}}
	Status KongCredentialACLStatus `json:"status,omitempty"`
}

// KongCredentialACLSpec defines specification of Kong ACL.
// +apireference:kgo:include
type KongCredentialACLSpec struct {
	// ConsumerRef is a reference to a Consumer this KongCredentialACL is associated with.
	//
	// +required
	ConsumerRef corev1.LocalObjectReference `json:"consumerRef"`

	KongCredentialACLAPISpec `json:",inline"`
}

// KongCredentialACLAPISpec defines specification of an ACL credential.
// +apireference:kgo:include
type KongCredentialACLAPISpec struct {
	// Group is the name for the ACL credential.
	//
	// +required
	Group string `json:"group"`

	// Tags is a list of tags for the ACL credential.
	Tags commonv1alpha1.Tags `json:"tags,omitempty"`
}

// KongCredentialACLStatus represents the current status of the ACL credential resource.
// +apireference:kgo:include
type KongCredentialACLStatus struct {
	// Konnect contains the Konnect entity status.
	// +optional
	Konnect *konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndConsumerRefs `json:"konnect,omitempty"`

	// Conditions describe the status of the Konnect entity.
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=8
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// KongCredentialACLList contains a list of ACL credentials.
// +kubebuilder:object:root=true
// +apireference:kgo:include
type KongCredentialACLList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KongCredentialACL `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KongCredentialACL{}, &KongCredentialACLList{})
}
