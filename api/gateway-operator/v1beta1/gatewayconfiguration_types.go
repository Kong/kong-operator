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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/common/v1alpha1"
)

func init() {
	SchemeBuilder.Register(&GatewayConfiguration{}, &GatewayConfigurationList{})
}

// GatewayConfiguration is the Schema for the gatewayconfigurations API.
//
// +genclient
// +apireference:kgo:include
// +kong:channels=gateway-operator
// +kubebuilder:deprecatedversion:warning="GatewayConfiguration v1beta1 has been deprecated in favor of v2beta1 and it will be removed in future."
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=kogc,categories=kong
// +kubebuilder:validation:XValidation:message="Extension not allowed for GatewayConfiguration",rule="has(self.spec.extensions) ? self.spec.extensions.all(e, (e.group == 'konnect.konghq.com' && e.kind == 'KonnectExtension') || (e.group == 'gateway-operator.konghq.com' && e.kind == 'DataPlaneMetricsExtension')) : true"
type GatewayConfiguration struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GatewayConfigurationSpec   `json:"spec,omitempty"`
	Status GatewayConfigurationStatus `json:"status,omitempty"`
}

// GatewayConfigurationSpec defines the desired state of GatewayConfiguration
// +apireference:kgo:include
// +kubebuilder:validation:XValidation:message="KonnectExtension must be set at the Gateway level",rule="has(self.dataPlaneOptions) && has(self.dataPlaneOptions.extensions) ? self.dataPlaneOptions.extensions.all(e, (e.group != 'konnect.konghq.com' && e.group != 'gateway-operator.konghq.com') || e.kind != 'KonnectExtension') : true"
// +kubebuilder:validation:XValidation:message="KonnectExtension must be set at the Gateway level",rule="has(self.controlPlaneOptions) && has(self.controlPlaneOptions.extensions) ? self.controlPlaneOptions.extensions.all(e, (e.group != 'konnect.konghq.com' && e.group != 'gateway-operator.konghq.com') || e.kind != 'KonnectExtension') : true"
// +kubebuilder:validation:XValidation:message="Can only specify listener's NodePort when the type of service for dataplane to receive ingress traffic ('spec.dataPlaneOptions.network.services.ingress') is NodePort or LoadBalancer",rule="(has(self.dataPlaneOptions) && has(self.dataPlaneOptions.network) && has(self.dataPlaneOptions.network.services) &&  has(self.dataPlaneOptions.network.services.ingress) && (self.dataPlaneOptions.network.services.ingress.type == 'LoadBalancer' || self.dataPlaneOptions.network.services.ingress.type == 'NodePort')) ? true : (!has(self.listenersOptions) || self.listenersOptions.all(l,!has(l.nodePort)))"
type GatewayConfigurationSpec struct {
	// DataPlaneOptions is the specification for configuration
	// overrides for DataPlane resources that will be created for the Gateway.
	//
	// +optional
	DataPlaneOptions *GatewayConfigDataPlaneOptions `json:"dataPlaneOptions,omitempty"`

	// ControlPlaneOptions is the specification for configuration
	// overrides for ControlPlane resources that will be created for the Gateway.
	//
	// +optional
	ControlPlaneOptions *ControlPlaneOptions `json:"controlPlaneOptions,omitempty"`

	// ListenerOptions is the specification for configuration bound to specific listeners in the Gateway.
	// It will override the default configuration of control plane or data plane for the specified listener.
	// +kubebuilder:validation:MaxItems=64
	//
	// +optional
	// +kubebuilder:validation:MaxItems=64
	// +kubebuilder:validation:XValidation:message="Listener name must be unique within the Gateway",rule="self.all(l1, self.exists_one(l2, l1.name == l2.name))"
	// +kubebuilder:validation:XValidation:message="Nodeport must be unique within the Gateway if specified",rule="self.all(l1, !has(l1.nodePort) || self.exists_one(l2, l1.nodePort == l2.nodePort))"
	ListenersOptions []GatewayConfigurationListenerOptions `json:"listenersOptions,omitempty"`

	// Extensions provide additional or replacement features for the Gateway
	// resource to influence or enhance functionality.
	// NOTE: currently, there's only 1 extension that can be attached
	// at the Gateway level (KonnectExtension), so the amount of extensions
	// is limited to 1.
	//
	// +optional
	// +kubebuilder:validation:MinItems=0
	// +kubebuilder:validation:MaxItems=1
	Extensions []commonv1alpha1.ExtensionRef `json:"extensions,omitempty"`
}

// GatewayConfigDataPlaneOptions indicates the specific information needed to
// configure and deploy a DataPlane object.
// +apireference:kgo:include
// +kubebuilder:validation:XValidation:message="Extension not allowed for DataPlane",rule="has(self.extensions) ? self.extensions.all(e, (e.group == 'konnect.konghq.com' || e.group == 'gateway-operator.konghq.com') && e.kind == 'KonnectExtension') : true"
type GatewayConfigDataPlaneOptions struct {
	// +optional
	Deployment DataPlaneDeploymentOptions `json:"deployment"`

	// +optional
	Network GatewayConfigDataPlaneNetworkOptions `json:"network"`

	// +optional
	Resources *GatewayConfigDataPlaneResources `json:"resources,omitempty"`

	// Extensions provide additional or replacement features for the DataPlane
	// resources to influence or enhance functionality.
	// NOTE: since we have one extension only (KonnectExtension), we limit the amount of extensions to 1.
	//
	// +optional
	// +kubebuilder:validation:MinItems=0
	// +kubebuilder:validation:MaxItems=1
	Extensions []commonv1alpha1.ExtensionRef `json:"extensions,omitempty"`

	// PluginsToInstall is a list of KongPluginInstallation resources that
	// will be installed and available in the Gateways (DataPlanes) that
	// use this GatewayConfig.
	// +optional
	PluginsToInstall []NamespacedName `json:"pluginsToInstall,omitempty"`
}

// GatewayConfigDataPlaneNetworkOptions defines network related options for a DataPlane.
// +apireference:kgo:include
type GatewayConfigDataPlaneNetworkOptions struct {
	// Services indicates the configuration of Kubernetes Services needed for
	// the topology of various forms of traffic (including ingress, etc.) to
	// and from the DataPlane.
	Services *GatewayConfigDataPlaneServices `json:"services,omitempty"`
}

// GatewayConfigDataPlaneServices contains Services related DataPlane configuration.
// +apireference:kgo:include
type GatewayConfigDataPlaneServices struct {
	// Ingress is the Kubernetes Service that will be used to expose ingress
	// traffic for the DataPlane. Here you can determine whether the DataPlane
	// will be exposed outside the cluster (e.g. using a LoadBalancer type
	// Services) or only internally (e.g. ClusterIP), and inject any additional
	// annotations you need on the service (for instance, if you need to
	// influence a cloud provider LoadBalancer configuration).
	//
	// +optional
	Ingress *GatewayConfigServiceOptions `json:"ingress,omitempty"`
}

// GatewayConfigDataPlaneResources defines the resources that will be
// created and managed for Gateway's DataPlane.
// +apireference:kgo:include
type GatewayConfigDataPlaneResources struct {
	// PodDisruptionBudget is the configuration for the PodDisruptionBudget
	// that will be created for the DataPlane.
	//
	// +optional
	PodDisruptionBudget *PodDisruptionBudget `json:"podDisruptionBudget,omitempty"`
}

// GatewayConfigServiceOptions is used to includes options to customize the ingress service,
// such as the annotations.
// +apireference:kgo:include
type GatewayConfigServiceOptions struct {
	ServiceOptions `json:",inline"`
}

// GatewayConfigurationListenerOptions specifies configuration overrides of defaults on certain listener of the Gateway.
// The name must match the name of a listener in the Gateway
// and the options are applied to the configuration of the matching listener.
// For example, if the option for listener "http" specified the nodeport number to 30080,
// The ingress service will expose the nodeport 30080 for the "http" listener of the Gateway.
// For listeners without an item in listener options of GatewayConfiguration, default configuration is used for it.
// +apireference:kgo:include
type GatewayConfigurationListenerOptions struct {
	// Name is the name of the Listener.
	//
	// +required
	Name gatewayv1.SectionName `json:"name"`

	// The port on each node on which this service is exposed when type is
	// NodePort or LoadBalancer. Usually assigned by the system. If a value is
	// specified, in-range, and not in use it will be used, otherwise the
	// operation will fail. If not specified, a port will be allocated if this
	// Service requires one. If this field is specified when creating a
	// Service which does not need it, creation will fail. This field will be
	// wiped when updating a Service to no longer need it (e.g. changing type
	// from NodePort to ClusterIP).
	//
	// More info: https://kubernetes.io/docs/concepts/services-networking/service/#type-nodeport
	//
	// Can only be specified if type of the dataplane ingress service (specified in `spec.dataplaneOptions.network.services.ingress.type`)
	// is NodePort or LoadBalancer.
	//
	// +required
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	NodePort int32 `json:"nodePort"`
}

// GatewayConfigurationStatus defines the observed state of GatewayConfiguration
// +apireference:kgo:include
type GatewayConfigurationStatus struct {
	// Conditions describe the current conditions of the GatewayConfigurationStatus.
	//
	// +optional
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MaxItems=8
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true

// GatewayConfigurationList contains a list of GatewayConfiguration
// +apireference:kgo:include
type GatewayConfigurationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GatewayConfiguration `json:"items"`
}

// GetConditions retrieves the GatewayConfiguration Status Condition
func (g *GatewayConfiguration) GetConditions() []metav1.Condition {
	return g.Status.Conditions
}

// SetConditions sets the GatewayConfiguration Status Condition
func (g *GatewayConfiguration) SetConditions(conditions []metav1.Condition) {
	g.Status.Conditions = conditions
}

// GetExtensions retrieves the GatewayConfiguration Extensions
func (g *GatewayConfiguration) GetExtensions() []commonv1alpha1.ExtensionRef {
	return g.Spec.Extensions
}

// ConvertTo converts this GatewayConfiguration (v1beta1) to the Hub version.
func (g *GatewayConfiguration) ConvertTo(dstRaw conversion.Hub) error {
	_ = dstRaw
	panic("v1beta1 GatewayConfiguration ConvertTo(...) is not implemented yet")
}

// ConvertFrom converts from the Hub version to this version (v1beta1).
func (g *GatewayConfiguration) ConvertFrom(srcRaw conversion.Hub) error {
	_ = srcRaw
	panic("v1beta1 GatewayConfiguration ConvertFrom(...) is not implemented yet")
}
