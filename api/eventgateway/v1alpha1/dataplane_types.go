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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
)

func init() {
	SchemeBuilder.Register(&DataPlane{}, &DataPlaneList{})
}

// DataPlane is the Schema for the EventGateway data planes API.
// It manages a keg binary Deployment that connects to Konnect via a
// referenced KonnectEventGateway resource.
//
// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:object:generate=true
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.deployment.replicas,statuspath=.status.replicas,selectorpath=.status.selector
// +kubebuilder:resource:shortName=egdp,categories=kong,plural=dataplanes
// +kubebuilder:printcolumn:name="Ready",description="The Resource is ready",type=string,JSONPath=`.status.conditions[?(@.type=='Ready')].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kong:channels=kong-operator
type DataPlane struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the desired state of DataPlane.
	//
	// +required
	Spec DataPlaneSpec `json:"spec"`

	// Status defines the observed state of DataPlane.
	//
	// +optional
	Status DataPlaneStatus `json:"status,omitempty"`
}

// DataPlaneList contains a list of DataPlane.
//
// +kubebuilder:object:root=true
type DataPlaneList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []DataPlane `json:"items"`
}

// DataPlaneSpec defines the desired state of DataPlane.
type DataPlaneSpec struct {
	// KonnectEventGatewayRef references the KonnectEventGateway that this
	// data plane connects to. The controller reads its status to obtain the
	// Konnect gateway ID and region.
	//
	// +required
	KonnectEventGatewayRef corev1.LocalObjectReference `json:"konnectEventGatewayRef"`

	// Deployment configures the keg Deployment: image, replicas, resources,
	// extra env vars, volume mounts, etc.
	//
	// +optional
	Deployment *DeploymentOptions `json:"deployment,omitempty"`

	// Network configures how the keg pod is exposed to Kafka clients.
	//
	// +optional
	Network *NetworkOptions `json:"network,omitempty"`

	// Config provides optional overrides for keg runtime settings.
	// When a field is omitted the keg default applies.
	//
	// +optional
	Config *Config `json:"config,omitempty"`
}

// DeploymentOptions specifies options for the Deployment managed
// by the DataPlane controller.
type DeploymentOptions struct {
	operatorv1beta1.DeploymentOptions `json:",inline"`
}

// NetworkOptions defines network-related options for an DataPlane.
type NetworkOptions struct {
	// Services configures the Kubernetes Services that expose the keg pod to
	// Kafka clients.
	//
	// +optional
	Services *Services `json:"services,omitempty"`
}

// Services configures the Kubernetes Services created for a keg pod.
//
// keg exposes a single TCP port for Kafka client traffic. In production the
// recommended approach is SNI mapping, one port (default 9092), multiple backend
// clusters via distinct TLS hostnames. For external access the Service type must
// be LoadBalancer (or a Gateway API TLSRoute passthrough can be used).
type Services struct {
	// Kafka is the Service that exposes the Kafka protocol listener to clients.
	//
	// In SNI mapping mode (production) this is a single port that defaults to 9092.
	// Konnect Listeners configure which hostnames keg advertises to clients;
	// those hostnames must resolve to this Service's external address.
	//
	// Set type to LoadBalancer for external access, or use a TLSRoute (Gateway
	// API passthrough) to route to this Service from a shared ingress Gateway.
	//
	// +optional
	Kafka *ServiceOptions `json:"kafka,omitempty"`
}

// ServiceOptions contains Service configuration for the DataPlane.
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

	// Labels are propagated to the DataPlane's Kafka Service.
	//
	// +optional
	// +kubebuilder:validation:MaxProperties=64
	Labels map[operatorv1beta1.LabelName]operatorv1beta1.LabelValue `json:"labels,omitempty"`

	// ExternalTrafficPolicy describes how nodes distribute service traffic they
	// receive on one of the Service's externally-facing addresses.
	//
	// +optional
	// +kubebuilder:validation:Enum=Cluster;Local
	ExternalTrafficPolicy corev1.ServiceExternalTrafficPolicy `json:"externalTrafficPolicy,omitempty"`

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
	Name string `json:"name,omitempty"`

	// The port that will be exposed by this service.
	Port int32 `json:"port"`

	// Number or name of the port to access on the pods targeted by the service.
	//
	// +optional
	TargetPort intstr.IntOrString `json:"targetPort,omitempty"`

	// The port on each node on which this service is exposed when type is
	// NodePort or LoadBalancer.
	//
	// +optional
	NodePort int32 `json:"nodePort,omitempty"`
}

// Config provides optional overrides for keg runtime settings.
// All fields map 1-to-1 to keg configuration variables.
type Config struct {
	// Konnect provides optional overrides for the keg → Konnect connection
	// parameters. All other connection values (region, gateway_cluster_id,
	// cert paths) are derived automatically and cannot be overridden here.
	//
	// +optional
	Konnect *KonnectConfig `json:"konnect,omitempty"`

	// ConfigPollInterval overrides how often keg polls Konnect for config changes.
	// Corresponds to config_poll_interval / KEG__CONFIG_POLL_INTERVAL. Default: 5s.
	//
	// +optional
	ConfigPollInterval *metav1.Duration `json:"configPollInterval,omitempty"`

	// EnableDebugEndpoints enables the /debug/pprof/allocs endpoint.
	// Corresponds to enable_debug_endpoints / KEG__ENABLE_DEBUG_ENDPOINTS. Default: false.
	//
	// +optional
	EnableDebugEndpoints *bool `json:"enableDebugEndpoints,omitempty"`

	// Observability configures logging, metrics, and tracing.
	//
	// +optional
	Observability *ObservabilityConfig `json:"observability,omitempty"`

	// Runtime configures graceful shutdown and health endpoint behaviour.
	//
	// +optional
	Runtime *RuntimeOptions `json:"runtime,omitempty"`
}

// KonnectConfig exposes the small subset of konnect.* config keys
// that are user-tunable (all others are set automatically by the controller).
type KonnectConfig struct {
	// Domain overrides the Konnect domain (default: konghq.com).
	// Corresponds to konnect.domain / KONG_KONNECT_DOMAIN.
	//
	// +optional
	Domain *string `json:"domain,omitempty"`

	// APIRequestTimeout overrides the Konnect API request timeout (default: 5s).
	// Corresponds to konnect.api_request_timeout / KONG_KONNECT_API_REQUEST_TIMEOUT.
	//
	// +optional
	APIRequestTimeout *metav1.Duration `json:"apiRequestTimeout,omitempty"`

	// InsecureSkipVerify disables TLS verification for the Konnect connection.
	// For testing only, do not use in production.
	// Corresponds to konnect.insecure_skip_verify / KONG_KONNECT_INSECURE_SKIP_VERIFY.
	//
	// +optional
	InsecureSkipVerify *bool `json:"insecureSkipVerify,omitempty"`
}

// ObservabilityConfig configures logging, metrics, and tracing for KEG.
type ObservabilityConfig struct {
	// LogFlags sets the log level. Accepted values: trace, debug, info, warn, error.
	// Corresponds to observability.log_flags / KEG__OBSERVABILITY__LOG_FLAGS. Default: info.
	//
	// +optional
	LogFlags *string `json:"logFlags,omitempty"`

	// LogFormat sets the log output format.
	// Corresponds to observability.log_format / KEG__OBSERVABILITY__LOG_FORMAT.
	//
	// +kubebuilder:validation:Enum=logfmt;json
	// +optional
	LogFormat *string `json:"logFormat,omitempty"`

	// MetricsRollupAllowMap prevents high-cardinality metrics by collapsing
	// unmatched label values to "other".
	// Corresponds to observability.metrics_rollup_allow_map /
	// KEG__OBSERVABILITY__METRICS_ROLLUP_ALLOW_MAP. Default: "messaging.operation.name=produce,fetch".
	//
	// +optional
	MetricsRollupAllowMap *string `json:"metricsRollupAllowMap,omitempty"`

	// PolicyErrorsInfoLogInterval sets the interval for INFO-level logging of policy errors.
	// Set to 0s to disable. Default: 1s.
	// Corresponds to observability.policy_errors_info_log_interval /
	// KEG__OBSERVABILITY__POLICY_ERRORS_INFO_LOG_INTERVAL.
	//
	// +optional
	PolicyErrorsInfoLogInterval *metav1.Duration `json:"policyErrorsInfoLogInterval,omitempty"`
}

// RuntimeOptions configures graceful shutdown and health endpoint behaviour for keg.
type RuntimeOptions struct {
	// HealthListenerAddressPort sets the address:port for the health endpoint.
	// Corresponds to runtime.health_listener_address_port /
	// KEG__RUNTIME__HEALTH_LISTENER_ADDRESS_PORT. Default: 0.0.0.0:8080.
	//
	// +optional
	HealthListenerAddressPort *string `json:"healthListenerAddressPort,omitempty"`

	// DrainDuration sets how long keg drains existing connections on shutdown.
	// Corresponds to runtime.drain_duration / KEG__RUNTIME__DRAIN_DURATION. Default: 5s.
	//
	// +optional
	DrainDuration *metav1.Duration `json:"drainDuration,omitempty"`

	// ShutdownTimeout sets the graceful shutdown timeout.
	// Corresponds to runtime.shutdown_timeout / KEG__RUNTIME__SHUTDOWN_TIMEOUT. Default: 10s.
	//
	// +optional
	ShutdownTimeout *metav1.Duration `json:"shutdownTimeout,omitempty"`
}

// DataPlaneStatus defines the observed state of DataPlane.
type DataPlaneStatus struct {
	// Conditions describe the status of the DataPlane.
	//
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MaxItems=8
	// +kubebuilder:default={{type: "Ready", status: "Unknown", reason: "Pending", message: "Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ReadyReplicas indicates how many replicas have reported to be ready.
	//
	// +kubebuilder:default=0
	ReadyReplicas int32 `json:"readyReplicas"`

	// Replicas indicates how many replicas have been set for the DataPlane.
	//
	// +kubebuilder:default=0
	Replicas int32 `json:"replicas"`
}

// GetConditions retrieves the DataPlane Status Conditions.
func (e *DataPlane) GetConditions() []metav1.Condition {
	return e.Status.Conditions
}

// SetConditions sets the DataPlane Status Conditions.
func (e *DataPlane) SetConditions(conditions []metav1.Condition) {
	e.Status.Conditions = conditions
}
