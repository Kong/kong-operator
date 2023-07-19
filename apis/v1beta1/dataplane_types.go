/*
Copyright 2022 Kong Inc.

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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func init() {
	SchemeBuilder.Register(&DataPlane{}, &DataPlaneList{})
}

//+genclient
//+k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:shortName=kdp,categories=kong;all
//+kubebuilder:printcolumn:name="Ready",description="The Resource is ready",type=string,JSONPath=`.status.conditions[?(@.type=='Ready')].status`
//+kubebuilder:printcolumn:name="Provisioned",description="The Resource is provisioned",type=string,JSONPath=`.status.conditions[?(@.type=='Provisioned')].status`

// DataPlane is the Schema for the dataplanes API
type DataPlane struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DataPlaneSpec   `json:"spec,omitempty"`
	Status DataPlaneStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// DataPlaneList contains a list of DataPlane
type DataPlaneList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DataPlane `json:"items"`
}

// DataPlaneSpec defines the desired state of DataPlane
type DataPlaneSpec struct {
	DataPlaneOptions `json:",inline"`
}

// DataPlaneOptions defines the information specifically needed to
// deploy the DataPlane.
type DataPlaneOptions struct {
	// +optional
	Deployment DataPlaneDeploymentOptions `json:"deployment"`

	// +optional
	Network DataPlaneNetworkOptions `json:"network"`
}

// DataPlaneDeploymentOptions specifies options for the Deployments (as in the Kubernetes
// resource "Deployment") which are created and managed for the DataPlane resource.
type DataPlaneDeploymentOptions struct {
	// Rollout describes a custom rollout strategy.
	//
	// +optional
	Rollout *Rollout `json:"rollout,omitempty"`

	DeploymentOptions `json:",inline"`
}

// DataPlaneNetworkOptions defines network related options for a DataPlane.
type DataPlaneNetworkOptions struct {
	// Services indicates the configuration of Kubernetes Services needed for
	// the topology of various forms of traffic (including ingress, e.t.c.) to
	// and from the DataPlane.
	Services *DataPlaneServices `json:"services,omitempty"`
}

type DataPlaneServices struct {
	// Ingress is the Kubernetes Service that will be used to expose ingress
	// traffic for the DataPlane. Here you can determine whether the DataPlane
	// will be exposed outside the cluster (e.g. using a LoadBalancer type
	// Services) or only internally (e.g. ClusterIP), and inject any additional
	// annotations you need on the service (for instance, if you need to
	// influence a cloud provider LoadBalancer configuration).
	//
	// +optional
	Ingress *ServiceOptions `json:"ingress,omitempty"`
}

// ServiceOptions is used to includes options to customize the proxy service,
// such as the annotations.
type ServiceOptions struct {
	// Type determines how the Service is exposed.
	// Defaults to LoadBalancer.
	//
	// Valid options are LoadBalancer and ClusterIP.
	//
	// "ClusterIP" allocates a cluster-internal IP address for load-balancing
	// to endpoints.
	//
	// "LoadBalancer" builds on NodePort and creates an external load-balancer
	// (if supported in the current cloud) which routes to the same endpoints
	// as the clusterIP.
	//
	// More info: https://kubernetes.io/docs/concepts/services-networking/service/#publishing-services-service-types
	//
	// +optional
	// +kubebuilder:default=LoadBalancer
	// +kubebuilder:validation:Enum=LoadBalancer;ClusterIP
	Type corev1.ServiceType `json:"type,omitempty" protobuf:"bytes,4,opt,name=type,casttype=ServiceType"`

	// Annotations is an unstructured key value map stored with a resource that may be
	// set by external tools to store and retrieve arbitrary metadata. They are not
	// queryable and should be preserved when modifying objects.
	//
	// More info: http://kubernetes.io/docs/user-guide/annotations
	//
	// +optional
	Annotations map[string]string `json:"annotations,omitempty" protobuf:"bytes,12,rep,name=annotations"`
}

// DataPlaneStatus defines the observed state of DataPlane
type DataPlaneStatus struct {
	// Conditions describe the status of the DataPlane.
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=8
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Service indicates the Service that exposes the DataPlane's configured routes
	Service string `json:"service,omitempty"`

	// Addresses lists the addresses that have actually been bound to the DataPlane.
	//
	// +optional
	Addresses []Address `json:"addresses,omitempty"`

	// Ready indicates whether the DataPlane is ready.
	// It there are multiple replicas then all have to be ready for this flag
	// to be set to true.
	//
	// +kubebuilder:default=false
	Ready bool `json:"ready"`

	// ReadyReplicas indicates how many replicas have reported to be ready.
	//
	// +kubebuilder:default=0
	ReadyReplicas int32 `json:"readyReplicas"`

	// Replicas indicates how many replicas have been set for the DataPlane.
	//
	// +kubebuilder:default=0
	Replicas int32 `json:"replicas"`
}

// Address describes an address which can be either an IP address or a hostname.
type Address struct {
	// Type of the address.
	//
	// +optional
	// +kubebuilder:default=IPAddress
	Type *AddressType `json:"type,omitempty"`

	// Value of the address. The validity of the values will depend
	// on the type and support by the controller.
	//
	// Examples: `1.2.3.4`, `128::1`, `my-ip-address`.
	//
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Value string `json:"value"`

	// Source type of the address.
	SourceType AddressSourceType `json:"sourceType"`
}

// AddressType defines how a network address is represented as a text string.
//
// +kubebuilder:validation:Pattern=`^IPAddress|Hostname$`
type AddressType string

const (
	// A textual representation of a numeric IP address. IPv4
	// addresses must be in dotted-decimal form. IPv6 addresses
	// must be in a standard IPv6 text representation
	// (see [RFC 5952](https://tools.ietf.org/html/rfc5952)).
	//
	// This type is intended for specific addresses. Address ranges are not
	// supported (e.g. you can not use a CIDR range like 127.0.0.0/24 as an
	// IPAddress).
	IPAddressType AddressType = "IPAddress"

	// A Hostname represents a DNS based ingress point. This is similar to the
	// corresponding hostname field in Kubernetes load balancer status. For
	// example, this concept may be used for cloud load balancers where a DNS
	// name is used to expose a load balancer.
	HostnameAddressType AddressType = "Hostname"
)

// AddressSourceType defines the type of source this address represents.
//
// +kubebuilder:validation:Pattern=`^PublicLoadBalancer|PrivateLoadBalancer|PublicIP|PrivateIP$`
type AddressSourceType string

const (
	// PublicLoadBalancerAddressSourceType represents an address belonging to
	// a public Load Balancer.
	PublicLoadBalancerAddressSourceType AddressSourceType = "PublicLoadBalancer"

	// PrivateLoadBalancerAddressSourceType represents an address belonging to
	// a private Load Balancer.
	PrivateLoadBalancerAddressSourceType AddressSourceType = "PrivateLoadBalancer"

	// PublicIPAddressSourceType represents an address belonging to a public IP.
	PublicIPAddressSourceType AddressSourceType = "PublicIP"

	// PrivateIPAddressSourceType  represents an address belonging to a private IP.
	PrivateIPAddressSourceType AddressSourceType = "PrivateIP"
)

// GetConditions retrieves the DataPlane Status Conditions
func (d *DataPlane) GetConditions() []metav1.Condition {
	return d.Status.Conditions
}

// SetConditions sets the DataPlane Status Conditions
func (d *DataPlane) SetConditions(conditions []metav1.Condition) {
	d.Status.Conditions = conditions
}
