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

	commonv1alpha1 "github.com/kong/kubernetes-configuration/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

// KongDataPlaneClientCertificate is the schema for KongDataPlaneClientCertificate API which defines a KongDataPlaneClientCertificate entity.
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
// +kubebuilder:validation:XValidation:rule="(!has(self.spec.controlPlaneRef) || !has(self.spec.controlPlaneRef.konnectNamespacedRef)) ? true : !has(self.spec.controlPlaneRef.konnectNamespacedRef.__namespace__)", message="spec.controlPlaneRef cannot specify namespace for namespaced resource"
// +apireference:kgo:include
// +kong:channels=gateway-operator
type KongDataPlaneClientCertificate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec KongDataPlaneClientCertificateSpec `json:"spec"`

	// +kubebuilder:default={conditions: {{type: "Programmed", status: "Unknown", reason:"Pending", message:"Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}}
	Status KongDataPlaneClientCertificateStatus `json:"status,omitempty"`
}

// KongDataPlaneClientCertificateSpec defines the spec for a KongDataPlaneClientCertificate.
// +kubebuilder:validation:XValidation:rule="!has(self.controlPlaneRef) ? true : self.controlPlaneRef.type != 'kic'", message="KIC is not supported as control plane"
// +apireference:kgo:include
type KongDataPlaneClientCertificateSpec struct {
	// ControlPlaneRef is a reference to a Konnect ControlPlane this KongDataPlaneClientCertificate is associated with.
	// +kubebuilder:validation:XValidation:message="'konnectID' type is not supported", rule="self.type != 'konnectID'"
	// +kubebuilder:validation:Required
	ControlPlaneRef *commonv1alpha1.ControlPlaneRef `json:"controlPlaneRef"`

	// KongDataPlaneClientCertificateAPISpec are the attributes of the KongDataPlaneClientCertificate itself.
	KongDataPlaneClientCertificateAPISpec `json:",inline"`
}

// KongDataPlaneClientCertificateAPISpec defines the attributes of a Kong DP certificate.
// +apireference:kgo:include
type KongDataPlaneClientCertificateAPISpec struct {
	// Cert is the certificate in PEM format. Once the certificate gets programmed this field becomes immutable.
	// +kubebuilder:validation:MinLength=1
	Cert string `json:"cert"`
}

// KongDataPlaneClientCertificateStatus defines the status for a KongDataPlaneClientCertificate.
// +apireference:kgo:include
type KongDataPlaneClientCertificateStatus struct {
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

// KongDataPlaneClientCertificateList contains a list of KongDataPlaneClientCertificate.
// +kubebuilder:object:root=true
// +apireference:kgo:include
type KongDataPlaneClientCertificateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KongDataPlaneClientCertificate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KongDataPlaneClientCertificate{}, &KongDataPlaneClientCertificateList{})
}
