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
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// AIGatewayDataPlane is the Schema for the AIGateway data planes API.
// It manages an AI Gateway binary Deployment that connects to Konnect via a
// referenced KonnectAIGateway resource.
//
// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:object:generate=true
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.deployment.replicas,statuspath=.status.replicas,selectorpath=.status.selector
// +kubebuilder:resource:shortName=aigwdp,categories=kong
// +kubebuilder:printcolumn:name="Ready",description="The Resource is ready",type=string,JSONPath=`.status.conditions[?(@.type=='Ready')].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kong:channels=kong-operator
type AIGatewayDataPlane struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the desired state of AIGatewayDataPlane.
	//
	// +required
	Spec AIGatewayDataPlaneSpec `json:"spec,omitzero"`

	// Status defines the observed state of AIGatewayDataPlane.
	//
	// +optional
	Status AIGatewayDataPlaneStatus `json:"status,omitempty"`
}

// AIGatewayDataPlaneList contains a list of AIGatewayDataPlane.
//
// +kubebuilder:object:root=true
type AIGatewayDataPlaneList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []AIGatewayDataPlane `json:"items"`
}

// AIGatewayDataPlaneSpec defines the desired state of AIGatewayDataPlane.
type AIGatewayDataPlaneSpec struct {
	// ControlPlaneRef references the control plane this AIGatewayDataPlane connects to.
	// The type field identifies which kind of control plane is being referenced.
	// Currently only konnectNamespacedRef is supported, which references a
	// KonnectAIGateway resource in the same namespace.
	//
	// +required
	ControlPlaneRef ControlPlaneRef `json:"controlPlaneRef,omitzero"`

	// Deployment configures the AI Gateway Deployment: image, replicas, resources,
	// extra env vars, volume mounts, etc.
	//
	// +optional
	Deployment *DeploymentOptions `json:"deployment,omitempty"`

	// Network configures how the AI Gateway pod is exposed to clients.
	//
	// +optional
	Network *NetworkOptions `json:"network,omitempty"`

	//TODO: add config field for AI Gateway configuration (provides optional overrides for AI Gateway runtime settings).
}

// DeploymentOptions specifies options for the Deployment managed by the AIGatewayDataPlane controller.
//
// +kubebuilder:validation:XValidation:message="Using both replicas and scaling fields is not allowed.",rule="!(has(self.scaling) && has(self.replicas))"
type DeploymentOptions struct {
	// Replicas describes the number of desired pods.
	// This is a pointer to distinguish between explicit zero and not specified.
	// This is effectively shorthand for setting a scaling minimum and maximum
	// to the same value. This field and the scaling field are mutually exclusive:
	// You can only configure one or the other.
	//
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// Scaling defines the scaling options for the deployment.
	//
	// +optional
	Scaling *Scaling `json:"scaling,omitempty"`

	// PodTemplateSpec defines PodTemplateSpec for Deployment's pods.
	// It's being applied on top of the generated Deployments using
	// [StrategicMergePatch](https://pkg.go.dev/k8s.io/apimachinery/pkg/util/strategicpatch#StrategicMergePatch).
	//
	// Note: environment variables set here take precedence over strongly-typed
	// fields in Spec.Config. Using raw env vars is discouraged and intended for
	// advanced use cases only.
	//
	// +optional
	PodTemplateSpec *corev1.PodTemplateSpec `json:"podTemplateSpec,omitempty"`
}

// Scaling defines the scaling options for the deployment.
type Scaling struct {
	// HorizontalScaling defines horizontal scaling options for the deployment.
	//
	// +optional
	HorizontalScaling *HorizontalScaling `json:"horizontal,omitempty"`
}

// HorizontalScaling defines horizontal scaling options for the deployment.
// It holds all the options from the HorizontalPodAutoscalerSpec besides the
// ScaleTargetRef which is being controlled by the Operator.
type HorizontalScaling struct {
	// minReplicas is the lower limit for the number of replicas to which the autoscaler
	// can scale down.  It defaults to 1 pod.  minReplicas is allowed to be 0 if the
	// alpha feature gate HPAScaleToZero is enabled and at least one Object or External
	// metric is configured.  Scaling is active as long as at least one metric value is
	// available.
	// +optional
	MinReplicas *int32 `json:"minReplicas,omitempty" protobuf:"varint,2,opt,name=minReplicas"`

	// maxReplicas is the upper limit for the number of replicas to which the autoscaler can scale up.
	// It cannot be less than minReplicas.
	//
	// +required
	MaxReplicas int32 `json:"maxReplicas" protobuf:"varint,3,opt,name=maxReplicas"`

	// metrics contains the specifications for which to use to calculate the
	// desired replica count (the maximum replica count across all metrics will
	// be used).  The desired replica count is calculated multiplying the
	// ratio between the target value and the current value by the current
	// number of pods.  Ergo, metrics used must decrease as the pod count is
	// increased, and vice-versa.  See the individual metric source types for
	// more information about how each type of metric must respond.
	// If not set, the default metric will be set to 80% average CPU utilization.
	// +listType=atomic
	// +kubebuilder:validation:MaxItems=32
	// +optional
	Metrics []autoscalingv2.MetricSpec `json:"metrics,omitempty" protobuf:"bytes,4,rep,name=metrics"`

	// behavior configures the scaling behavior of the target
	// in both Up and Down directions (scaleUp and scaleDown fields respectively).
	// If not set, the default HPAScalingRules for scale up and scale down are used.
	// +optional
	Behavior *autoscalingv2.HorizontalPodAutoscalerBehavior `json:"behavior,omitempty" protobuf:"bytes,5,opt,name=behavior"`
}

// NetworkOptions defines network-related options for an AIGatewayDataPlane.
type NetworkOptions struct {
	// Services configures the Kubernetes Services that expose the AI Gateway pod to clients.
	//
	// +optional
	Services *Services `json:"services,omitempty"`
}

// Services configures the Kubernetes Services created for an AI Gateway pod.
type Services struct {
	// Ingress is the Service that exposes the Ingress listener to clients.
	//
	// +optional
	Ingress *ServiceOptions `json:"ingress,omitempty"`
}

// LabelName is a label key with constraints matching Kubernetes label key requirements.
//
// +kubebuilder:validation:MinLength=1
// +kubebuilder:validation:MaxLength=316
// +kubebuilder:validation:Pattern=`^([a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?\.)*[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?\/[a-zA-Z0-9]([a-zA-Z0-9\-_.]{0,61}[a-zA-Z0-9])?$|^[a-zA-Z0-9]([a-zA-Z0-9\-_.]{0,61}[a-zA-Z0-9])?$`
type LabelName string

// LabelValue is a label value with constraints matching Kubernetes label value requirements.
//
// +kubebuilder:validation:MinLength=0
// +kubebuilder:validation:MaxLength=63
// +kubebuilder:validation:Pattern=`^(([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])?$`
type LabelValue string

// ServiceOptions contains Service configuration for the AIGatewayDataPlane.
//
// +kubebuilder:validation:XValidation:message="Cannot set NodePort when service type is not NodePort or LoadBalancer",rule="!has(self.ports) || !(self.ports.exists(p, has(p.nodePort))) ? true : has(self.type) && ['LoadBalancer', 'NodePort'].exists(t, t == self.type)"
type ServiceOptions struct {
	// Type determines how the Service is exposed.
	// Defaults to ClusterIP.
	//
	// +optional
	// +kubebuilder:default=ClusterIP
	// +kubebuilder:validation:Enum=LoadBalancer;NodePort;ClusterIP
	Type corev1.ServiceType `json:"type,omitempty"`

	// Annotations is an unstructured key value map stored with the Service resource.
	//
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`

	// Labels are propagated to the AIGatewayDataPlane's HTTP Service.
	//
	// +optional
	// +kubebuilder:validation:MaxProperties=64
	Labels map[LabelName]LabelValue `json:"labels,omitempty"`

	// ExternalTrafficPolicy describes how nodes distribute service traffic they
	// receive on one of the Service's externally-facing addresses.
	//
	// +optional
	// +kubebuilder:validation:Enum=Cluster;Local
	ExternalTrafficPolicy corev1.ServiceExternalTrafficPolicy `json:"externalTrafficPolicy,omitempty"`

	// TrafficDistribution offers a way to express preferences for how traffic is
	// distributed to Service endpoints. Implementations can use this field as a
	// hint, but are not required to guarantee strict adherence. If the field is
	// not set, the implementation will apply its default routing strategy.
	//
	// "PreferSameZone" prioritizes endpoints in the same zone as the client.
	// "PreferSameNode" prioritizes endpoints on the same node as the client.
	//
	// More info: https://kubernetes.io/docs/concepts/services-networking/service/#traffic-distribution
	//
	// +optional
	// +kubebuilder:validation:Enum=PreferSameZone;PreferSameNode
	TrafficDistribution *string `json:"trafficDistribution,omitempty"`

	// InternalTrafficPolicy describes how nodes distribute service traffic they
	// receive on the ClusterIP. If set to "Local", the proxy will assume that pods
	// only want to talk to endpoints of the service on the same node as the pod,
	// dropping the traffic if there are no local endpoints. The default value,
	// "Cluster", uses the standard behavior of routing to all endpoints evenly.
	//
	// More info: https://kubernetes.io/docs/concepts/services-networking/service/#internal-traffic-policy
	//
	// +optional
	// +kubebuilder:validation:Enum=Cluster;Local
	InternalTrafficPolicy *corev1.ServiceInternalTrafficPolicy `json:"internalTrafficPolicy,omitempty"`

	// Ports defines the list of ports that are exposed by the service.
	//
	// +kubebuilder:validation:MaxItems=64
	// +optional
	Ports []ServicePort `json:"ports,omitempty"`
}

// ServicePort contains information on a service port.
type ServicePort struct {
	// The name of this port within the service.
	//
	// +optional
	// +kubebuilder:validation:MaxLength=253
	Name *string `json:"name,omitempty"`

	// The port that will be exposed by this service.
	//
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +required
	Port int32 `json:"port,omitempty"`

	// Number or name of the port to access on the pods targeted by the service.
	//
	// +optional
	TargetPort *intstr.IntOrString `json:"targetPort,omitempty"`

	// The port on each node on which this service is exposed when type is
	// NodePort or LoadBalancer.
	//
	// +optional
	NodePort *int32 `json:"nodePort,omitempty"`
}

// AIGatewayDataPlaneStatus defines the observed state of AIGatewayDataPlane.
type AIGatewayDataPlaneStatus struct {
	// Conditions describe the status of the AIGatewayDataPlane.
	//
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MaxItems=8
	// +kubebuilder:default={{type: "Ready", status: "Unknown", reason: "Pending", message: "Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}
	// +optional
	// +patchStrategy=merge
	// +patchMergeKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`

	// ReadyReplicas indicates how many replicas have reported to be ready.
	//
	// +kubebuilder:default=0
	// +optional
	ReadyReplicas int32 `json:"readyReplicas"`

	// Replicas indicates how many replicas have been set for the AIGatewayDataPlane.
	//
	// +kubebuilder:default=0
	// +optional
	Replicas int32 `json:"replicas"`
}

// GetConditions retrieves the AIGatewayDataPlane Status Conditions.
func (a *AIGatewayDataPlane) GetConditions() []metav1.Condition {
	return a.Status.Conditions
}

// SetConditions sets the AIGatewayDataPlane Status Conditions.
func (a *AIGatewayDataPlane) SetConditions(conditions []metav1.Condition) {
	a.Status.Conditions = conditions
}
