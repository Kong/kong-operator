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
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.spec.controlPlaneRef) || has(self.spec.controlPlaneRef)", message="controlPlaneRef is required once set"
// +kubebuilder:validation:XValidation:rule="(!self.status.conditions.exists(c, c.type == 'Programmed' && c.status == 'True')) ? true : oldSelf.spec.controlPlaneRef == self.spec.controlPlaneRef", message="spec.controlPlaneRef is immutable when an entity is already Programmed"
// +kubebuilder:validation:XValidation:rule="oldSelf.spec.upstreamRef == self.spec.upstreamRef", message="upstreamRef is immutable"
type KongTarget struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec KongTargetSpec `json:"spec"`
	// +kubebuilder:default={conditions: {{type: "Programmed", status: "Unknown", reason:"Pending", message:"Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}}
	Status KongTargetStatus `json:"status"`
}

func (t *KongTarget) initKonnectStatus() {
	t.Status.Konnect = &konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef{}
}

// GetKonnectStatus returns the Konnect status contained in the KongTarget status.
func (t *KongTarget) GetKonnectStatus() *konnectv1alpha1.KonnectEntityStatus {
	if t.Status.Konnect == nil {
		return nil
	}
	return &t.Status.Konnect.KonnectEntityStatus
}

// GetKonnectID returns the Konnect ID in the KongTarget status.
func (t *KongTarget) GetKonnectID() string {
	if t.Status.Konnect == nil {
		return ""
	}
	return t.Status.Konnect.ID
}

// SetKonnectID sets the Konnect ID in the KongTarget status.
func (t *KongTarget) SetKonnectID(id string) {
	if t.Status.Konnect == nil {
		t.initKonnectStatus()
	}
	t.Status.Konnect.ID = id
}

// GetControlPlaneID returns the ControlPlane ID in the KongTarget status.
func (t *KongTarget) GetControlPlaneID() string {
	if t.Status.Konnect == nil {
		return ""
	}
	return t.Status.Konnect.ControlPlaneID
}

// SetControlPlaneID sets the ControlPlane ID in the KongTarget status.
func (t *KongTarget) SetControlPlaneID(id string) {
	if t.Status.Konnect == nil {
		t.initKonnectStatus()
	}
	t.Status.Konnect.ControlPlaneID = id
}

// GetTypeName returns the KongTarget Kind name.
func (t KongTarget) GetTypeName() string {
	return "KongTarget"
}

// GetConditions returns the Status Conditions.
func (t *KongTarget) GetConditions() []metav1.Condition {
	return t.Status.Conditions
}

// SetConditions sets the Status Conditions.
func (t *KongTarget) SetConditions(conditions []metav1.Condition) {
	t.Status.Conditions = conditions
}

type KongTargetSpec struct {
	// ControlPlaneRef is a reference to a ControlPlane this KongTarget is associated with.
	// +optional
	ControlPlaneRef *ControlPlaneRef `json:"controlPlaneRef,omitempty"`
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
	Konnect *konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef `json:"konnect,omitempty"`

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
