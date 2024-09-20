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

// KongCredentialBasicAuth is the schema for BasicAuth credentials API which defines a BasicAuth credential for consumers.
//
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Programmed",description="The Resource is Programmed on Konnect",type=string,JSONPath=`.status.conditions[?(@.type=='Programmed')].status`
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.spec.consumerRef) || has(self.spec.consumerRef)",message="consumerRef is required once set"
// +kubebuilder:validation:XValidation:rule="(!self.status.conditions.exists(c, c.type == 'Programmed' && c.status == 'True')) ? true : oldSelf.spec.consumerRef == self.spec.consumerRef",message="spec.consumerRef is immutable when an entity is already Programmed"
type KongCredentialBasicAuth struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec contains the BasicAuth credential specification.
	Spec KongCredentialBasicAuthSpec `json:"spec"`

	// Status contains the BasicAuth credential status.
	//
	// +kubebuilder:default={conditions: {{type: "Programmed", status: "Unknown", reason:"Pending", message:"Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}}
	Status KongCredentialBasicAuthStatus `json:"status,omitempty"`
}

func (s *KongCredentialBasicAuth) initKonnectStatus() {
	s.Status.Konnect = &konnectv1alpha1.KonnectEntityStatusWithControlPlaneAndConsumerRefs{}
}

// GetKonnectStatus returns the Konnect status contained in the KongCredentialBasicAuth status.
func (s *KongCredentialBasicAuth) GetKonnectStatus() *konnectv1alpha1.KonnectEntityStatus {
	if s.Status.Konnect == nil {
		return nil
	}
	return &s.Status.Konnect.KonnectEntityStatus
}

// GetKonnectID returns the Konnect ID in the KongCredentialBasicAuth status.
func (s *KongCredentialBasicAuth) GetKonnectID() string {
	if s.Status.Konnect == nil {
		return ""
	}
	return s.Status.Konnect.ID
}

// SetKonnectID sets the Konnect ID in the KongCredentialBasicAuth status.
func (s *KongCredentialBasicAuth) SetKonnectID(id string) {
	if s.Status.Konnect == nil {
		s.initKonnectStatus()
	}
	s.Status.Konnect.ID = id
}

// GetControlPlaneID returns the ControlPlane ID in the KongCredentialBasicAuth status.
func (s *KongCredentialBasicAuth) GetControlPlaneID() string {
	if s.Status.Konnect == nil {
		return ""
	}
	return s.Status.Konnect.ControlPlaneID
}

// SetControlPlaneID sets the ControlPlane ID in the KongCredentialBasicAuth status.
func (s *KongCredentialBasicAuth) SetControlPlaneID(id string) {
	if s.Status.Konnect == nil {
		s.initKonnectStatus()
	}
	s.Status.Konnect.ControlPlaneID = id
}

// GetTypeName returns the KongCredentialBasicAuth Kind name
func (s KongCredentialBasicAuth) GetTypeName() string {
	return "KongCredentialBasicAuth"
}

// GetConditions returns the Status Conditions
func (s *KongCredentialBasicAuth) GetConditions() []metav1.Condition {
	return s.Status.Conditions
}

// SetConditions sets the Status Conditions
func (s *KongCredentialBasicAuth) SetConditions(conditions []metav1.Condition) {
	s.Status.Conditions = conditions
}

// KongCredentialBasicAuthSpec defines specification of a Kong Route.
type KongCredentialBasicAuthSpec struct {
	// ConsumerRef is a reference to a Consumer this KongCredentialBasicAuth is associated with.
	//
	// +kubebuilder:validation:Required
	ConsumerRef corev1.LocalObjectReference `json:"consumerRef"`

	KongCredentialBasicAuthAPISpec `json:",inline"`
}

// KongCredentialBasicAuthAPISpec defines specification of a BasicAuth credential.
type KongCredentialBasicAuthAPISpec struct {
	// Password is the password for the BasicAuth credential.
	//
	// +kubebuilder:validation:Required
	Password string `json:"password"`

	// Tags is a list of tags for the BasicAuth credential.
	Tags []string `json:"tags,omitempty"`

	// Username is the username for the BasicAuth credential.
	//
	// +kubebuilder:validation:Required
	Username string `json:"username"`
}

// KongCredentialBasicAuthStatus represents the current status of the BasicAuth credential resource.
type KongCredentialBasicAuthStatus struct {
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

// KongCredentialBasicAuthList contains a list of BasicAuth credentials.
type KongCredentialBasicAuthList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KongCredentialBasicAuth `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KongCredentialBasicAuth{}, &KongCredentialBasicAuthList{})
}
