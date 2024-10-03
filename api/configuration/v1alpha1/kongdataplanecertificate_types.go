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

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

// KongDataplaneCertificate is the schema for KongDataplaneCertificate API which defines a KongDataplaneCertificate entity.
//
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Programmed",description="The Resource is Programmed on Konnect",type=string,JSONPath=`.status.conditions[?(@.type=='Programmed')].status`
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.spec.controlPlaneRef) || has(self.spec.controlPlaneRef)", message="controlPlaneRef is required once set"
// +kubebuilder:validation:XValidation:rule="!has(self.spec.controlPlaneRef) ? true : (!self.status.conditions.exists(c, c.type == 'Programmed' && c.status == 'True')) ? true : oldSelf.spec.controlPlaneRef == self.spec.controlPlaneRef", message="spec.controlPlaneRef is immutable when an entity is already Programmed"
// +kubebuilder:validation:XValidation:rule="(!self.status.conditions.exists(c, c.type == 'Programmed' && c.status == 'True')) ? true : oldSelf.spec.cert == self.spec.cert", message="spec.cert is immutable when an entity is already Programmed"
// +kubebuilder:validation:XValidation:rule="!has(self.spec.controlPlaneRef) ? true : !has(self.spec.controlPlaneRef.konnectNamespacedRef) ? true : !has(self.spec.controlPlaneRef.konnectNamespacedRef.__namespace__)", message="spec.controlPlaneRef cannot specify namespace for namespaced resource - it's not supported yet"
// +apireference:kgo:include
type KongDataplaneCertificate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec KongDataplaneCertificateSpec `json:"spec"`

	// +kubebuilder:default={conditions: {{type: "Programmed", status: "Unknown", reason:"Pending", message:"Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}}
	Status KongDataplaneCertificateStatus `json:"status,omitempty"`
}

// KongDataplaneCertificateSpec defines the spec for a KongDataplaneCertificate.
// +apireference:kgo:include
type KongDataplaneCertificateSpec struct {
	// ControlPlaneRef is a reference to a Konnect ControlPlane this KongDataplaneCertificate is associated with.
	// +optional
	ControlPlaneRef *ControlPlaneRef `json:"controlPlaneRef,omitempty"`

	// KongDataplaneCertificateAPISpec are the attributes of the KongDataplaneCertificate itself.
	KongDataplaneCertificateAPISpec `json:",inline"`
}

// KongDataplaneCertificateAPISpec defines the attributes of a Kong DP certificate.
// +apireference:kgo:include
type KongDataplaneCertificateAPISpec struct {
	// Cert is the certificate in PEM format. Once the certificate gets programmed this field becomes immutable.
	// +kubebuilder:validation:MinLength=1
	Cert string `json:"cert"`
}

// KongDataplaneCertificateStatus defines the status for a KongDataplaneCertificate.
// +apireference:kgo:include
type KongDataplaneCertificateStatus struct {
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

// KongDataplaneCertificateList contains a list of Kong Keys.
// +kubebuilder:object:root=true
// +apireference:kgo:include
type KongDataplaneCertificateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KongDataplaneCertificate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KongDataplaneCertificate{}, &KongDataplaneCertificateList{})
}
