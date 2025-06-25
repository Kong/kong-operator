/*
Copyright 2025 Kong Inc.

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

package v2alpha1

import (
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/api/common/v1alpha1"
	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
)

func init() {
	SchemeBuilder.Register(&GatewayConfiguration{}, &GatewayConfigurationList{})
}

// GatewayConfiguration is the Schema for the gatewayconfigurations API.
//
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:storageversion
// +apireference:kgo:include
// +kong:channels=gateway-operator
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=kogc,categories=kong;all
type GatewayConfiguration struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the desired state of GatewayConfiguration.
	Spec GatewayConfigurationSpec `json:"spec,omitempty"`

	// Status defines the observed state of GatewayConfiguration.
	//
	// +optional
	Status GatewayConfigurationStatus `json:"status,omitempty"`
}

// GatewayConfigurationSpec defines the desired state of GatewayConfiguration
//
// +apireference:kgo:include
type GatewayConfigurationSpec struct {
	// DataPlaneOptions is the specification for configuration
	// overrides for DataPlane resources that will be created for the Gateway.
	//
	// +optional
	DataPlaneOptions *GatewayConfigDataPlaneOptions `json:"dataPlaneOptions,omitempty"`

	// ControlPlaneOptions is the specification for configuration
	// overrides for ControlPlane resources that will be managed as part of the Gateway.
	//
	// +optional
	ControlPlaneOptions *GatewayConfigControlPlaneOptions `json:"controlPlaneOptions,omitempty"`

	// Extensions provide additional or replacement features for the Gateway
	// resource to influence or enhance functionality.
	// NOTE: currently, there are only 2 extensions that can be attached
	// at the Gateway level (KonnectExtension, DataPlaneMetricsExtension),
	// so the amount of extensions is limited to 2.
	//
	// +optional
	// +kubebuilder:validation:MinItems=0
	// +kubebuilder:validation:MaxItems=2
	// +kubebuilder:validation:XValidation:message="Extension not allowed for GatewayConfiguration",rule="self.all(e, (e.group == 'konnect.konghq.com' && e.kind == 'KonnectExtension') || (e.group == 'gateway-operator.konghq.com' && e.kind == 'DataPlaneMetricsExtension'))"
	Extensions []commonv1alpha1.ExtensionRef `json:"extensions,omitempty"`
}

// GatewayConfigControlPlaneOptions contains the options for configuring
// ControlPlane resources that will be managed as part of the Gateway.
type GatewayConfigControlPlaneOptions struct {
	ControlPlaneOptions `json:",inline"`
}

// GatewayConfigDataPlaneOptions indicates the specific information needed to
// configure and deploy a DataPlane object.
//
// +apireference:kgo:include
type GatewayConfigDataPlaneOptions struct {
	// +optional
	Deployment operatorv1beta1.DataPlaneDeploymentOptions `json:"deployment"`

	// +optional
	Network GatewayConfigDataPlaneNetworkOptions `json:"network"`

	// +optional
	Resources *GatewayConfigDataPlaneResources `json:"resources,omitempty"`

	// PluginsToInstall is a list of KongPluginInstallation resources that
	// will be installed and available in the Gateways (DataPlanes) that
	// use this GatewayConfig.
	//
	// +optional
	PluginsToInstall []NamespacedName `json:"pluginsToInstall,omitempty"`
}

// GatewayConfigDataPlaneNetworkOptions defines network related options for a DataPlane.
//
// +apireference:kgo:include
type GatewayConfigDataPlaneNetworkOptions struct {
	// Services indicates the configuration of Kubernetes Services needed for
	// the topology of various forms of traffic (including ingress, etc.) to
	// and from the DataPlane.
	//
	// +optional
	Services *GatewayConfigDataPlaneServices `json:"services,omitempty"`
}

// GatewayConfigDataPlaneServices contains Services related DataPlane configuration.
//
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
//
// +apireference:kgo:include
type GatewayConfigDataPlaneResources struct {
	// PodDisruptionBudget is the configuration for the PodDisruptionBudget
	// that will be created for the DataPlane.
	//
	// +optional
	PodDisruptionBudget *PodDisruptionBudget `json:"podDisruptionBudget,omitempty"`
}

// PodDisruptionBudget defines the configuration for the PodDisruptionBudget.
//
// +apireference:kgo:include
type PodDisruptionBudget struct {
	// Spec defines the specification of the PodDisruptionBudget.
	// Selector is managed by the controller and cannot be set by the user.
	Spec PodDisruptionBudgetSpec `json:"spec,omitempty"`
}

// PodDisruptionBudgetSpec defines the specification of a PodDisruptionBudget.
//
// +kubebuilder:validation:XValidation:message="You can specify only one of maxUnavailable and minAvailable in a single PodDisruptionBudgetSpec.",rule="(has(self.minAvailable) && !has(self.maxUnavailable)) || (!has(self.minAvailable) && has(self.maxUnavailable))"
// +apireference:kgo:include
type PodDisruptionBudgetSpec struct {
	// An eviction is allowed if at least "minAvailable" pods selected by
	// "selector" will still be available after the eviction, i.e. even in the
	// absence of the evicted pod.  So for example you can prevent all voluntary
	// evictions by specifying "100%".
	//
	// +optional
	MinAvailable *intstr.IntOrString `json:"minAvailable,omitempty" protobuf:"bytes,1,opt,name=minAvailable"`

	// An eviction is allowed if at most "maxUnavailable" pods selected by
	// "selector" are unavailable after the eviction, i.e. even in absence of
	// the evicted pod. For example, one can prevent all voluntary evictions
	// by specifying 0. This is a mutually exclusive setting with "minAvailable".
	//
	// +optional
	MaxUnavailable *intstr.IntOrString `json:"maxUnavailable,omitempty" protobuf:"bytes,3,opt,name=maxUnavailable"`

	// UnhealthyPodEvictionPolicy defines the criteria for when unhealthy pods
	// should be considered for eviction. Current implementation considers healthy pods,
	// as pods that have status.conditions item with type="Ready",status="True".
	//
	// Valid policies are IfHealthyBudget and AlwaysAllow.
	// If no policy is specified, the default behavior will be used,
	// which corresponds to the IfHealthyBudget policy.
	//
	// IfHealthyBudget policy means that running pods (status.phase="Running"),
	// but not yet healthy can be evicted only if the guarded application is not
	// disrupted (status.currentHealthy is at least equal to status.desiredHealthy).
	// Healthy pods will be subject to the PDB for eviction.
	//
	// AlwaysAllow policy means that all running pods (status.phase="Running"),
	// but not yet healthy are considered disrupted and can be evicted regardless
	// of whether the criteria in a PDB is met. This means perspective running
	// pods of a disrupted application might not get a chance to become healthy.
	// Healthy pods will be subject to the PDB for eviction.
	//
	// Additional policies may be added in the future.
	// Clients making eviction decisions should disallow eviction of unhealthy pods
	// if they encounter an unrecognized policy in this field.
	//
	// This field is beta-level. The eviction API uses this field when
	// the feature gate PDBUnhealthyPodEvictionPolicy is enabled (enabled by default).
	//
	// +optional
	UnhealthyPodEvictionPolicy *policyv1.UnhealthyPodEvictionPolicyType `json:"unhealthyPodEvictionPolicy,omitempty" protobuf:"bytes,4,opt,name=unhealthyPodEvictionPolicy"`
}

// GatewayConfigServiceOptions is used to includes options to customize the ingress service,
// such as the annotations.
//
// +apireference:kgo:include
type GatewayConfigServiceOptions struct {
	ServiceOptions `json:",inline"`
}

// GatewayConfigurationStatus defines the observed state of GatewayConfiguration
//
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

// GatewayConfigurationList contains a list of GatewayConfiguration
//
// +apireference:kgo:include
// +kubebuilder:object:root=true
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
