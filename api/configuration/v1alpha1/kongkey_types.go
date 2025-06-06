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

// KongKey is the schema for KongKey API which defines a KongKey entity.
//
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:resource:categories=kong
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Programmed",description="The Resource is Programmed on Konnect",type=string,JSONPath=`.status.conditions[?(@.type=='Programmed')].status`
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.spec.controlPlaneRef) || has(self.spec.controlPlaneRef)", message="controlPlaneRef is required once set"
// +kubebuilder:validation:XValidation:rule="(!self.status.conditions.exists(c, c.type == 'Programmed' && c.status == 'True')) ? true : oldSelf.spec.controlPlaneRef == self.spec.controlPlaneRef", message="spec.controlPlaneRef is immutable when an entity is already Programmed"
// +kubebuilder:validation:XValidation:rule="(!has(self.spec.controlPlaneRef) || !has(self.spec.controlPlaneRef.konnectNamespacedRef)) ? true : !has(self.spec.controlPlaneRef.konnectNamespacedRef.__namespace__)", message="spec.controlPlaneRef cannot specify namespace for namespaced resource"
// +apireference:kgo:include
// +kong:channels=gateway-operator
type KongKey struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec KongKeySpec `json:"spec"`

	// +kubebuilder:default={conditions: {{type: "Programmed", status: "Unknown", reason:"Pending", message:"Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}}
	Status KongKeyStatus `json:"status,omitempty"`
}

// KongKeySpec defines the spec for a KongKey.
// +kubebuilder:validation:XValidation:rule="!has(self.controlPlaneRef) ? true : self.controlPlaneRef.type != 'kic'", message="KIC is not supported as control plane"
// +apireference:kgo:include
type KongKeySpec struct {
	// ControlPlaneRef is a reference to a Konnect ControlPlane this KongKey is associated with.
	// +optional
	ControlPlaneRef *commonv1alpha1.ControlPlaneRef `json:"controlPlaneRef,omitempty"`

	// KeySetRef is a reference to a KongKeySet this KongKey is attached to.
	// ControlPlane referenced by a KongKeySet must be the same as the ControlPlane referenced by the KongKey.
	// +optional
	KeySetRef *KeySetRef `json:"keySetRef,omitempty"`

	// KongKeyAPISpec are the attributes of the KongKey itself.
	KongKeyAPISpec `json:",inline"`
}

// KongKeyAPISpec defines the attributes of a Kong Key.
// +kubebuilder:validation:XValidation:rule="has(self.jwk) || has(self.pem)", message="Either 'jwk' or 'pem' must be set"
// +apireference:kgo:include
type KongKeyAPISpec struct {
	// KID is a unique identifier for a key.
	// When JWK is provided, KID has to match the KID in the JWK.
	// +kubebuilder:validation:MinLength=1
	KID string `json:"kid"`

	// Name is an optional name to associate with the given key.
	// +optional
	Name *string `json:"name,omitempty"`

	// JWK is a JSON Web Key represented as a string.
	// The JWK must contain a KID field that matches the KID in the KongKey.
	// Either JWK or PEM must be set.
	// +optional
	JWK *string `json:"jwk,omitempty"`

	// PEM is a keypair in PEM format.
	// Either JWK or PEM must be set.
	// +optional
	PEM *PEMKeyPair `json:"pem,omitempty"`

	// Tags is an optional set of strings associated with the Key for grouping and filtering.
	// +optional
	Tags commonv1alpha1.Tags `json:"tags,omitempty"`
}

// PEMKeyPair defines a keypair in PEM format.
// +apireference:kgo:include
type PEMKeyPair struct {
	// The private key in PEM format.
	// +kubebuilder:validation:MinLength=1
	PrivateKey string `json:"private_key"`

	// The public key in PEM format.
	// +kubebuilder:validation:MinLength=1
	PublicKey string `json:"public_key"`
}

// KongKeyStatus defines the status for a KongKey.
// +apireference:kgo:include
type KongKeyStatus struct {
	// Konnect contains the Konnect entity status.
	// +optional
	Konnect *konnectv1alpha1.KonnectEntityStatusWithControlPlaneAndKeySetRef `json:"konnect,omitempty"`

	// Conditions describe the status of the Konnect entity.
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=8
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// KongKeyList contains a list of Kong Keys.
// +kubebuilder:object:root=true
// +apireference:kgo:include
type KongKeyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KongKey `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KongKey{}, &KongKeyList{})
}
