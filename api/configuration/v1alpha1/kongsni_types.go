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
	"github.com/kong/kubernetes-configuration/api/configuration/common"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

// KongSNI is the schema for SNI API which defines a Kong SNI.
//
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Programmed",description="The Resource is Programmed on Konnect",type=string,JSONPath=`.status.conditions[?(@.type=='Programmed')].status`
// +kubebuilder:validation:XValidation:rule="(!self.status.conditions.exists(c, c.type == 'Programmed' && c.status == 'True')) ? true : oldSelf.spec.certificateRef == self.spec.certificateRef", message="spec.certificateRef is immutable when programmed"
// +apireference:kgo:include
type KongSNI struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec KongSNISpec `json:"spec"`

	// +kubebuilder:default={conditions: {{type: "Programmed", status: "Unknown", reason:"Pending", message:"Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}}
	Status KongSNIStatus `json:"status,omitempty"`
}

// KongSNIAPISpec defines specification of an SNI.
// +apireference:kgo:include
type KongSNIAPISpec struct {
	// Name is the name of the SNI. Required and must be a host or wildcard host.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Tags is an optional set of strings associated with the SNI for grouping and filtering.
	Tags common.Tags `json:"tags,omitempty"`
}

// KongSNISpec defines specification of a Kong SNI.
// +apireference:kgo:include
type KongSNISpec struct {
	// CertificateRef is the reference to the certificate to which the KongSNI is attached.
	CertificateRef KongObjectRef `json:"certificateRef"`
	// KongSNIAPISpec are the attributes of the Kong SNI itself.
	KongSNIAPISpec `json:",inline"`
}

// KongSNIStatus defines the status for a KongSNI.
// +apireference:kgo:include
type KongSNIStatus struct {
	// Konnect contains the Konnect entity status.
	// +optional
	Konnect *konnectv1alpha1.KonnectEntityStatusWithControlPlaneAndCertificateRefs `json:"konnect,omitempty"`

	// Conditions describe the status of the Konnect entity.
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=8
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// KongSNIList contains a list of Kong SNIs.
// +kubebuilder:object:root=true
// +apireference:kgo:include
type KongSNIList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KongSNI `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KongSNI{}, &KongSNIList{})
}
