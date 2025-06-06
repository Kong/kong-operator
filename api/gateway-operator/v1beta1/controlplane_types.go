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
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/api/common/v1alpha1"
)

func init() {
	SchemeBuilder.Register(&ControlPlane{}, &ControlPlaneList{})
}

// ControlPlane is the Schema for the controlplanes API
//
// +genclient
// +kubebuilder:storageversion
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=kocp,categories=kong;all
// +kubebuilder:printcolumn:name="Ready",description="The Resource is ready",type=string,JSONPath=`.status.conditions[?(@.type=='Ready')].status`
// +kubebuilder:printcolumn:name="Provisioned",description="The Resource is provisioned",type=string,JSONPath=`.status.conditions[?(@.type=='Provisioned')].status`
// +kubebuilder:validation:XValidation:message="ControlPlane requires an image to be set on controller container",rule="has(self.spec.deployment.podTemplateSpec) && has(self.spec.deployment.podTemplateSpec.spec.containers) && self.spec.deployment.podTemplateSpec.spec.containers.exists(c, c.name == 'controller' && has(c.image))"
// +apireference:kgo:include
// +kong:channels=gateway-operator
type ControlPlane struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ControlPlaneSpec   `json:"spec,omitempty"`
	Status ControlPlaneStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ControlPlaneList contains a list of ControlPlane
// +apireference:kgo:include
type ControlPlaneList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ControlPlane `json:"items"`
}

// ControlPlaneSpec defines the desired state of ControlPlane
// +apireference:kgo:include
type ControlPlaneSpec struct {
	ControlPlaneOptions `json:",inline"`

	// GatewayClass indicates the Gateway resources which this ControlPlane
	// should be responsible for configuring routes for (e.g. HTTPRoute,
	// TCPRoute, UDPRoute, TLSRoute, e.t.c.).
	//
	// Required for the ControlPlane to have any effect: at least one Gateway
	// must be present for configuration to be pushed to the data-plane and
	// only Gateway resources can be used to identify data-plane entities.
	//
	// +optional
	GatewayClass *gatewayv1.ObjectName `json:"gatewayClass,omitempty"`

	// IngressClass enables support for the older Ingress resource and indicates
	// which Ingress resources this ControlPlane should be responsible for.
	//
	// Routing configured this way will be applied to the Gateway resources
	// indicated by GatewayClass.
	//
	// If omitted, Ingress resources will not be supported by the ControlPlane.
	//
	// +optional
	IngressClass *string `json:"ingressClass,omitempty"`
}

// ControlPlaneOptions indicates the specific information needed to
// deploy and connect a ControlPlane to a DataPlane object.
// +apireference:kgo:include
// +kubebuilder:validation:XValidation:message="Extension not allowed for ControlPlane",rule="has(self.extensions) ? self.extensions.all(e, (e.group == 'konnect.konghq.com' && e.kind == 'KonnectExtension') || (e.group == 'gateway-operator.konghq.com' && e.kind == 'DataPlaneMetricsExtension')) : true"
type ControlPlaneOptions struct {
	// +optional
	Deployment ControlPlaneDeploymentOptions `json:"deployment"`

	// DataPlanes refers to the named DataPlane objects which this ControlPlane
	// is responsible for. Currently they must be in the same namespace as the
	// DataPlane.
	//
	// +optional
	DataPlane *string `json:"dataplane,omitempty"`

	// Extensions provide additional or replacement features for the ControlPlane
	// resources to influence or enhance functionality.
	//
	// +optional
	// +kubebuilder:validation:MinItems=0
	// +kubebuilder:validation:MaxItems=2
	Extensions []commonv1alpha1.ExtensionRef `json:"extensions,omitempty"`

	// WatchNamespaces indicates the namespaces to watch for resources.
	//
	// +optional
	// +kubebuilder:default={type: all}
	WatchNamespaces *WatchNamespaces `json:"watchNamespaces,omitempty"`
}

// ControlPlaneDeploymentOptions is a shared type used on objects to indicate that their
// configuration results in a Deployment which is managed by the Operator and
// includes options for managing Deployments such as the the number of replicas
// or pod options like container image and resource requirements.
// version, as well as Env variable overrides.
// +apireference:kgo:include
type ControlPlaneDeploymentOptions struct {
	// Replicas describes the number of desired pods.
	// This is a pointer to distinguish between explicit zero and not specified.
	// This only affects the DataPlane deployments for now, for more details on
	// ControlPlane scaling please see https://github.com/Kong/gateway-operator/issues/736.
	//
	// +optional
	// +kubebuilder:default=1
	Replicas *int32 `json:"replicas,omitempty"`

	// PodTemplateSpec defines PodTemplateSpec for Deployment's pods.
	//
	// +optional
	PodTemplateSpec *corev1.PodTemplateSpec `json:"podTemplateSpec,omitempty"`
}

// ControlPlaneStatus defines the observed state of ControlPlane
// +apireference:kgo:include
type ControlPlaneStatus struct {
	// Conditions describe the current conditions of the Gateway.
	//
	// +optional
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MaxItems=8
	// +kubebuilder:default={{type: "Scheduled", status: "Unknown", reason:"NotReconciled", message:"Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// GetConditions returns the ControlPlane Status Conditions
func (c *ControlPlane) GetConditions() []metav1.Condition {
	return c.Status.Conditions
}

// SetConditions sets the ControlPlane Status Conditions
func (c *ControlPlane) SetConditions(conditions []metav1.Condition) {
	c.Status.Conditions = conditions
}

// GetExtensions retrieves the ControlPlane Extensions
func (c *ControlPlane) GetExtensions() []commonv1alpha1.ExtensionRef {
	return c.Spec.Extensions
}
