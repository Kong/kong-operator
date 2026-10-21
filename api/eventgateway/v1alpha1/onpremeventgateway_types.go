/*
Copyright 2026 Kong, Inc.

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
	"k8s.io/apimachinery/pkg/types"
)

// OnPremEventGateway is the Schema for the on-prem Event Gateway control planes API.
// It stands for one Event Gateway whose control plane is gw-admin, running in
// the cluster, instead of Konnect: Kong Operator pushes the configuration of
// the entities that reference it to gw-admin, and the data planes pull it
// from there.
//
// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:object:generate=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=onpremegw,categories=kong
// +kubebuilder:printcolumn:name="Programmed",description="The Resource is Programmed",type=string,JSONPath=`.status.conditions[?(@.type=='Programmed')].status`
// +kubebuilder:printcolumn:name="GWAdmin",description="The GWAdmin serving the gateway",type=string,JSONPath=`.spec.gwAdminRef.name`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kong:channels=kong-operator
type OnPremEventGateway struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the desired state of OnPremEventGateway.
	//
	// +required
	Spec OnPremEventGatewaySpec `json:"spec,omitzero"`

	// Status defines the observed state of OnPremEventGateway.
	//
	// +optional
	Status OnPremEventGatewayStatus `json:"status,omitempty"`
}

// OnPremEventGatewayList contains a list of OnPremEventGateway.
//
// +kubebuilder:object:root=true
type OnPremEventGatewayList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []OnPremEventGateway `json:"items"`
}

// OnPremEventGatewaySpec defines the desired state of OnPremEventGateway.
type OnPremEventGatewaySpec struct {
	// GWAdminRef references the GWAdmin that serves this gateway. Kong
	// Operator pushes the configuration of the gateway to every gw-admin pod of
	// it, and the data planes of the gateway pull it from there. A GWAdmin in
	// another namespace is used only when a KongReferenceGrant in its
	// namespace allows OnPremEventGateways of the namespace of the gateway to
	// reference it. It can't be changed.
	// +required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="gwAdminRef is immutable"
	GWAdminRef GWAdminRef `json:"gwAdminRef,omitzero"`

	// TODO: the spec is not final. Fields are added as the implementation of
	// the on-prem Event Gateway goes on.
}

// GWAdminRef references a GWAdmin.
type GWAdminRef struct {
	// Name is the name of the GWAdmin.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	Name string `json:"name,omitempty"`

	// Namespace is the namespace of the GWAdmin. It defaults to the namespace
	// of the referencing object.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Namespace *string `json:"namespace,omitempty"`
}

// OnPremEventGatewayKind is the kind of OnPremEventGateway.
const OnPremEventGatewayKind = "OnPremEventGateway"

// GWAdminKey returns the namespace and name of the GWAdmin the gateway
// references.
func (g *OnPremEventGateway) GWAdminKey() types.NamespacedName {
	key := types.NamespacedName{Namespace: g.Namespace, Name: g.Spec.GWAdminRef.Name}
	if ns := g.Spec.GWAdminRef.Namespace; ns != nil && *ns != "" {
		key.Namespace = *ns
	}
	return key
}

// OnPremEventGatewayStatus defines the observed state of OnPremEventGateway.
type OnPremEventGatewayStatus struct {
	// Conditions describe the status of the OnPremEventGateway.
	//
	// +optional
	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	// +kubebuilder:validation:MaxItems=8
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`

	// ServerURL is the address the data planes of this gateway use to reach
	// gw-admin: the cp endpoint of its GWAdmin.
	//
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +kubebuilder:validation:XValidation:rule="isURL(self) && url(self).getScheme() == 'https' && url(self).getHostname() != ''",message="serverURL must be an https URL with a host"
	ServerURL string `json:"serverURL,omitempty"`

	// ObservedGeneration is the most recent generation observed.
	//
	// +optional
	// +kubebuilder:validation:Minimum=0
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}
