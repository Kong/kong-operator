/*
Copyright 2021 Kong, Inc.

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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +kubebuilder:resource:shortName=kc,categories=kong-ingress-controller
// +kubebuilder:printcolumn:name="Username",type=string,JSONPath=`.username`,description="Username of a Kong Consumer"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`,description="Age"
// +kubebuilder:printcolumn:name="Programmed",type=string,JSONPath=`.status.conditions[?(@.type=="Programmed")].status`
// +kubebuilder:validation:XValidation:rule="has(self.username) || has(self.custom_id)", message="Need to provide either username or custom_id"
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.spec.controlPlaneRef) || has(self.spec.controlPlaneRef)", message="controlPlaneRef is required once set"
// +kubebuilder:validation:XValidation:rule="!has(self.spec.controlPlaneRef.konnectNamespacedRef) ? true : !has(self.spec.controlPlaneRef.konnectNamespacedRef.__namespace__)", message="spec.controlPlaneRef cannot specify namespace for namespaced resource"
// +kubebuilder:validation:XValidation:rule="(!has(self.status) || !self.status.conditions.exists(c, c.type == 'Programmed' && c.status == 'True')) ? true : oldSelf.spec.controlPlaneRef == self.spec.controlPlaneRef", message="spec.controlPlaneRef is immutable when an entity is already Programmed"

// KongConsumer is the Schema for the kongconsumers API.
type KongConsumer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Username is a Kong cluster-unique username of the consumer.
	Username string `json:"username,omitempty"`

	// CustomID is a Kong cluster-unique existing ID for the consumer - useful for mapping
	// Kong with users in your existing database.
	CustomID string `json:"custom_id,omitempty"`

	// Credentials are references to secrets containing a credential to be
	// provisioned in Kong.
	// +listType=set
	Credentials []string `json:"credentials,omitempty"`

	// ConsumerGroups are references to consumer groups (that consumer wants to be part of)
	// provisioned in Kong.
	// +listType=set
	ConsumerGroups []string `json:"consumerGroups,omitempty"`

	Spec KongConsumerSpec `json:"spec,omitempty"`

	// Status represents the current status of the KongConsumer resource.
	// +kubebuilder:default={conditions: {{type: "Programmed", status: "Unknown", reason:"Pending", message:"Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}}
	Status KongConsumerStatus `json:"status,omitempty"`
}

func (c *KongConsumer) initKonnectStatus() {
	c.Status.Konnect = &konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef{}
}

// GetControlPlaneID returns the ControlPlane ID in the KongConsumer status.
func (c *KongConsumer) GetControlPlaneID() string {
	if c.Status.Konnect == nil {
		return ""
	}
	return c.Status.Konnect.ControlPlaneID
}

// SetControlPlaneID sets the ControlPlane ID in the KongConsumer status.
func (c *KongConsumer) SetControlPlaneID(id string) {
	if c.Status.Konnect == nil {
		c.initKonnectStatus()
	}
	c.Status.Konnect.ControlPlaneID = id
}

// GetKonnectStatus returns the Konnect status contained in the KongConsumer status.
func (c *KongConsumer) GetKonnectStatus() *konnectv1alpha1.KonnectEntityStatus {
	if c.Status.Konnect == nil {
		return nil
	}
	return &c.Status.Konnect.KonnectEntityStatus
}

// GetKonnectID returns the Konnect ID in the KongConsumer status.
func (c *KongConsumer) GetKonnectID() string {
	if c.Status.Konnect == nil {
		return ""
	}
	return c.Status.Konnect.ID
}

// SetKonnectID sets the Konnect ID in the KongConsumer status.
func (c *KongConsumer) SetKonnectID(id string) {
	if c.Status.Konnect == nil {
		c.initKonnectStatus()
	}
	c.Status.Konnect.ID = id
}

// GetTypeName returns the KongConsumer Kind name
func (c KongConsumer) GetTypeName() string {
	return "KongConsumer"
}

// GetConditions returns the Status Conditions
func (c *KongConsumer) GetConditions() []metav1.Condition {
	return c.Status.Conditions
}

// SetConditions sets the Status Conditions
func (c *KongConsumer) SetConditions(conditions []metav1.Condition) {
	c.Status.Conditions = conditions
}

type KongConsumerSpec struct {
	// ControlPlaneRef is a reference to a ControlPlane this Consumer is associated with.
	// +optional
	ControlPlaneRef *configurationv1alpha1.ControlPlaneRef `json:"controlPlaneRef,omitempty"`
}

// +kubebuilder:object:root=true

// KongConsumerList contains a list of KongConsumer.
type KongConsumerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KongConsumer `json:"items"`
}

// KongConsumerStatus represents the current status of the KongConsumer resource.
type KongConsumerStatus struct {
	// Konnect contains the Konnect entity status.
	// +optional
	Konnect *konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef `json:"konnect,omitempty"`

	// Conditions describe the current conditions of the KongConsumer.
	//
	// Known condition types are:
	//
	// * "Programmed"
	//
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MaxItems=8
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

func init() {
	SchemeBuilder.Register(&KongConsumer{}, &KongConsumerList{})
}
