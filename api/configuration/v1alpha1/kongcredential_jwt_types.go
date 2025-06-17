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

	commonv1alpha1 "github.com/kong/kubernetes-configuration/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

// KongCredentialJWT is the schema for JWT credentials API which defines a JWT credential for consumers.
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
// +kubebuilder:validation:XValidation:rule="self.spec.algorithm in [ 'RS256','RS384','RS512','ES256','ES384','ES512','PS256','PS384','PS512','EdDSA', ] ? has(self.spec.rsa_public_key) : true",message="spec.rsa_public_key is required when algorithm is RS*, ES*, PS* or EdDSA*"
// +apireference:kgo:include
// +kong:channels=gateway-operator
type KongCredentialJWT struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec contains the JWT credential specification.
	Spec KongCredentialJWTSpec `json:"spec"`

	// Status contains the JWT credential status.
	//
	// +kubebuilder:default={conditions: {{type: "Programmed", status: "Unknown", reason:"Pending", message:"Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}}
	Status KongCredentialJWTStatus `json:"status,omitempty"`
}

// KongCredentialJWTSpec defines specification of a Kong Route.
// +apireference:kgo:include
type KongCredentialJWTSpec struct {
	// ConsumerRef is a reference to a Consumer this KongCredentialJWT is associated with.
	//
	// +required
	ConsumerRef corev1.LocalObjectReference `json:"consumerRef"`

	KongCredentialJWTAPISpec `json:",inline"`
}

// KongCredentialJWTAPISpec defines specification of an JWT credential.
// +apireference:kgo:include
type KongCredentialJWTAPISpec struct {
	// Algorithm is the algorithm used to sign the JWT token.
	// +kubebuilder:default=HS256
	// +kubebuilder:validation:Enum=HS256;HS384;HS512;RS256;RS384;RS512;ES256;ES384;ES512;PS256;PS384;PS512;EdDSA
	Algorithm string `json:"algorithm,omitempty"`
	// ID is the unique identifier for the JWT credential.
	ID *string `json:"id,omitempty"`
	// Key is the key for the JWT credential.
	Key *string `json:"key,omitempty"`
	// RSA PublicKey is the RSA public key for the JWT credential.
	RSAPublicKey *string `json:"rsa_public_key,omitempty"`
	// Secret is the secret for the JWT credential.
	Secret *string `json:"secret,omitempty"`
	// Tags is a list of tags for the JWT credential.
	Tags commonv1alpha1.Tags `json:"tags,omitempty"`
}

// KongCredentialJWTStatus represents the current status of the JWT credential resource.
// +apireference:kgo:include
type KongCredentialJWTStatus struct {
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

// KongCredentialJWTList contains a list of JWT credentials.
// +kubebuilder:object:root=true
// +apireference:kgo:include
type KongCredentialJWTList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KongCredentialJWT `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KongCredentialJWT{}, &KongCredentialJWTList{})
}
