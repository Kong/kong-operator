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

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

// KongCredentialAPIKey is the schema for API key credentials API which defines a API key credential for consumers.
//
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Programmed",description="The Resource is Programmed on Konnect",type=string,JSONPath=`.status.conditions[?(@.type=='Programmed')].status`
// +kubebuilder:validation:XValidation:rule="(!self.status.conditions.exists(c, c.type == 'Programmed' && c.status == 'True')) ? true : oldSelf.spec.consumerRef == self.spec.consumerRef",message="spec.consumerRef is immutable when an entity is already Programmed"
type KongCredentialAPIKey struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec contains the API Key credential specification.
	Spec KongCredentialAPIKeySpec `json:"spec"`

	// Status contains the API Key credential status.
	//
	// +kubebuilder:default={conditions: {{type: "Programmed", status: "Unknown", reason:"Pending", message:"Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}}
	Status KongCredentialAPIKeyStatus `json:"status,omitempty"`
}

// KongCredentialAPIKeySpec defines specification of a Kong Route.
type KongCredentialAPIKeySpec struct {
	// ConsumerRef is a reference to a Consumer this KongCredentialAPIKey is associated with.
	//
	// +kubebuilder:validation:Required
	ConsumerRef corev1.LocalObjectReference `json:"consumerRef"`

	KongCredentialAPIKeyAPISpec `json:",inline"`
}

// KongCredentialAPIKeyAPISpec defines specification of a API Key credential.
type KongCredentialAPIKeyAPISpec struct {
	// Key is the key for the API Key credential.
	//
	// +kubebuilder:validation:Required
	Key string `json:"key"`

	// Tags is a list of tags for the API Key credential.
	Tags []string `json:"tags,omitempty"`
}

// KongCredentialAPIKeyStatus represents the current status of the API Key credential resource.
type KongCredentialAPIKeyStatus struct {
	// Konnect contains the Konnect entity status.
	// +optional
	Konnect *konnectv1alpha1.KonnectEntityStatusWithControlPlaneAndConsumerRefs `json:"konnect,omitempty"`

	// Conditions describe the status of the Konnect entity.
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=8
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true

// KongCredentialAPIKeyList contains a list of API Key credentials.
type KongCredentialAPIKeyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KongCredentialAPIKey `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KongCredentialAPIKey{}, &KongCredentialAPIKeyList{})
}
