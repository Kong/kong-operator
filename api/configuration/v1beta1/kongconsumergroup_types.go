/*
Copyright 2023 Kong, Inc.

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

package v1beta1

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
// +kubebuilder:resource:shortName=kcg,categories=kong-ingress-controller
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`,description="Age"
// +kubebuilder:printcolumn:name="Programmed",type=string,JSONPath=`.status.conditions[?(@.type=="Programmed")].status`
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.spec.controlPlaneRef) || has(self.spec.controlPlaneRef)", message="controlPlaneRef is required once set"
// +kubebuilder:validation:XValidation:rule="(!has(self.status) || !self.status.conditions.exists(c, c.type == 'Programmed' && c.status == 'True')) ? true : oldSelf.spec.controlPlaneRef == self.spec.controlPlaneRef", message="spec.controlPlaneRef is immutable when entity is already Programmed."

// KongConsumerGroup is the Schema for the kongconsumergroups API.
type KongConsumerGroup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec KongConsumerGroupSpec `json:"spec,omitempty"`

	// Status represents the current status of the KongConsumerGroup resource.
	// +kubebuilder:default={conditions: {{type: "Programmed", status: "Unknown", reason:"Pending", message:"Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}}
	Status KongConsumerGroupStatus `json:"status,omitempty"`
}

type KongConsumerGroupSpec struct {
	// Name is the name of the ConsumerGroup in Kong.
	Name *string `json:"name,omitempty"`

	// ControlPlaneRef is a reference to a ControlPlane this ConsumerGroup is associated with.
	// +optional
	ControlPlaneRef *configurationv1alpha1.ControlPlaneRef `json:"controlPlaneRef,omitempty"`
}

func (c *KongConsumerGroup) initKonnectStatus() {
	c.Status.Konnect = &konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef{}
}

// GetControlPlaneID returns the Konnect Control Plane ID of the KongConsumerGroup.
func (c *KongConsumerGroup) GetControlPlaneID() string {
	if c.Status.Konnect == nil {
		return ""
	}
	return c.Status.Konnect.ControlPlaneID
}

// SetControlPlaneID sets the Konnect Control Plane ID in the KongConsumerGroup status.
func (c *KongConsumerGroup) SetControlPlaneID(id string) {
	if c.Status.Konnect == nil {
		c.initKonnectStatus()
	}
	c.Status.Konnect.ControlPlaneID = id
}

// GetKonnectStatus returns the Konnect status contained in the KongConsumerGroup status.
func (c *KongConsumerGroup) GetKonnectStatus() *konnectv1alpha1.KonnectEntityStatus {
	if c.Status.Konnect == nil {
		return nil
	}
	return &c.Status.Konnect.KonnectEntityStatus
}

// SetKonnectID sets the Konnect ID in the KongConsumerGroup status.
func (c *KongConsumerGroup) SetKonnectID(id string) {
	if c.Status.Konnect == nil {
		c.initKonnectStatus()
	}
	c.Status.Konnect.ID = id
}

// GetTypeName returns the KongConsumerGroup Kind name
func (c KongConsumerGroup) GetTypeName() string {
	return "KongConsumerGroup"
}

// GetConditions returns the Status Conditions
func (c *KongConsumerGroup) GetConditions() []metav1.Condition {
	return c.Status.Conditions
}

// SetConditions sets the Status Conditions
func (c *KongConsumerGroup) SetConditions(conditions []metav1.Condition) {
	c.Status.Conditions = conditions
}

// +kubebuilder:object:root=true

// KongConsumerGroupList contains a list of KongConsumerGroups.
type KongConsumerGroupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KongConsumerGroup `json:"items"`
}

// KongConsumerGroupStatus represents the current status of the KongConsumerGroup resource.
type KongConsumerGroupStatus struct {
	// Konnect contains the Konnect entity status.
	// +optional
	Konnect *konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef `json:"konnect,omitempty"`

	// Conditions describe the current conditions of the KongConsumerGroup.
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
	SchemeBuilder.Register(&KongConsumerGroup{}, &KongConsumerGroupList{})
}
