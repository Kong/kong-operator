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

package v2beta1

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/common/v1alpha1"
)

func init() {
	SchemeBuilder.Register(&ControlPlane{}, &ControlPlaneList{})
}

// ControlPlane is the Schema for the controlplanes API
//
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:storageversion
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=kocp,categories=kong
// +kubebuilder:printcolumn:name="Ready",description="The Resource is ready",type=string,JSONPath=`.status.conditions[?(@.type=='Ready')].status`
// +apireference:kgo:include
// +kong:channels=gateway-operator
type ControlPlane struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec is the specification of the ControlPlane resource.
	Spec ControlPlaneSpec `json:"spec,omitempty"`

	// Status is the status of the ControlPlane resource.
	//
	// +optional
	Status ControlPlaneStatus `json:"status,omitempty"`
}

// ControlPlaneList contains a list of ControlPlane
//
// +kubebuilder:object:root=true
// +apireference:kgo:include
type ControlPlaneList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ControlPlane `json:"items"`
}

// ControlPlaneSpec defines the desired state of ControlPlane
//
// +apireference:kgo:include
// +kubebuilder:validation:XValidation:message="When dataplane target is of type 'ref' the ingressClass must be set",rule="self.dataplane.type != 'ref' || has(self.ingressClass)"
type ControlPlaneSpec struct {
	// DataPlane designates the target data plane to configure.
	//
	// It can be:
	// - a name of a DataPlane resource that is managed by the operator,
	// - a DataPlane that is managed by the owner of the ControlPlane (e.g. a Gateway resource)
	//
	// +required
	DataPlane ControlPlaneDataPlaneTarget `json:"dataplane"`

	ControlPlaneOptions `json:",inline"`

	// Extensions provide additional or replacement features for the ControlPlane
	// resources to influence or enhance functionality.
	//
	// +optional
	// +kubebuilder:validation:MaxItems=2
	// +kubebuilder:validation:XValidation:message="Extension not allowed for ControlPlane",rule="self.all(e, (e.group == 'konnect.konghq.com' && e.kind == 'KonnectExtension') || (e.group == 'gateway-operator.konghq.com' && e.kind == 'DataPlaneMetricsExtension'))"
	Extensions []commonv1alpha1.ExtensionRef `json:"extensions,omitempty"`
}

// ControlPlaneOptions indicates the specific information needed to
// deploy and connect a ControlPlane to a DataPlane object.
//
// +apireference:kgo:include
type ControlPlaneOptions struct {
	// IngressClass enables support for the Ingress resources and indicates
	// which Ingress resources this ControlPlane should be responsible for.
	//
	// If omitted, Ingress resources will not be supported by the ControlPlane.
	//
	// +optional
	IngressClass *string `json:"ingressClass,omitempty"`

	// WatchNamespaces indicates the namespaces to watch for resources.
	//
	// +optional
	// +kubebuilder:default={type: all}
	WatchNamespaces *WatchNamespaces `json:"watchNamespaces,omitempty"`

	// FeatureGates is a list of feature gates that are enabled for this ControlPlane.
	//
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=32
	FeatureGates []ControlPlaneFeatureGate `json:"featureGates,omitempty"`

	// Controllers defines the controllers that are enabled for this ControlPlane.
	//
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=32
	Controllers []ControlPlaneController `json:"controllers,omitempty"`

	// GatewayDiscovery defines the configuration for the Gateway Discovery feature.
	//
	// +optional
	GatewayDiscovery *ControlPlaneGatewayDiscovery `json:"gatewayDiscovery,omitempty"`

	// Cache defines the configuration related to the kubernetes object caches.
	//
	// +optional
	Cache *ControlPlaneK8sCache `json:"cache,omitempty"`

	// DataPlaneSync defines the configuration for syncing Kong configuration to the DataPlane.
	//
	// +optional
	DataPlaneSync *ControlPlaneDataPlaneSync `json:"dataplaneSync,omitempty"`

	// Translation defines the configuration for translating Kong configuration.
	//
	// +optional
	Translation *ControlPlaneTranslationOptions `json:"translation,omitempty"`

	// ConfigDump defines the options for dumping generated Kong configuration from a diagnostics server.
	//
	// +optional
	ConfigDump *ControlPlaneConfigDump `json:"configDump,omitempty"`

	// ObjectFilters defines the filters to limit watched objects by the controllers.
	//
	// +optional
	ObjectFilters *ControlPlaneObjectFilters `json:"objectFilters,omitempty"`

	// Konnect defines the Konnect-related configuration options for the ControlPlane.
	//
	// +optional
	Konnect *ControlPlaneKonnectOptions `json:"konnect,omitempty"`
}

// ControlPlaneTranslationOptions defines the configuration for translating
// cluster resources into Kong configuration.
type ControlPlaneTranslationOptions struct {
	// CombinedServicesFromDifferentHTTPRoutes indicates whether the ControlPlane should
	// combine services from different HTTPRoutes into a single Kong DataPlane service.
	//
	// +optional
	// +kubebuilder:default=enabled
	// +kubebuilder:validation:Enum=enabled;disabled
	CombinedServicesFromDifferentHTTPRoutes *ControlPlaneCombinedServicesFromDifferentHTTPRoutesState `json:"combinedServicesFromDifferentHTTPRoutes,omitempty"`

	// FallbackConfiguration defines the fallback configuration options for the ControlPlane.
	//
	// +optional
	FallbackConfiguration *ControlPlaneFallbackConfiguration `json:"fallbackConfiguration,omitempty"`

	// DrainSupport defines the configuration for the ControlPlane to include
	// terminating endpoints in Kong upstreams with weight=0 for graceful connection draining.
	//
	// +optional
	// +kubebuilder:default=enabled
	// +kubebuilder:validation:Enum=enabled;disabled
	DrainSupport *ControlPlaneDrainSupportState `json:"drainSupport,omitempty"`
}

// ControlPlaneDrainSupportState defines the state of the feature that allows the ControlPlane
// to include terminating endpoints in Kong upstreams with weight=0 for graceful connection draining.
type ControlPlaneDrainSupportState string

const (
	// ControlPlaneDrainSupportStateEnabled indicates that the feature is enabled.
	ControlPlaneDrainSupportStateEnabled ControlPlaneDrainSupportState = "enabled"
	// ControlPlaneDrainSupportStateDisabled indicates that the feature is disabled.
	ControlPlaneDrainSupportStateDisabled ControlPlaneDrainSupportState = "disabled"
)

// ControlPlaneCombinedServicesFromDifferentHTTPRoutesState defines the state of the
// feature that allows the ControlPlane to combine services from different HTTPRoutes.
type ControlPlaneCombinedServicesFromDifferentHTTPRoutesState string

const (
	// ControlPlaneCombinedServicesFromDifferentHTTPRoutesStateEnabled indicates that the feature is enabled.
	ControlPlaneCombinedServicesFromDifferentHTTPRoutesStateEnabled ControlPlaneCombinedServicesFromDifferentHTTPRoutesState = "enabled"
	// ControlPlaneCombinedServicesFromDifferentHTTPRoutesStateDisabled indicates that the feature is disabled.
	ControlPlaneCombinedServicesFromDifferentHTTPRoutesStateDisabled ControlPlaneCombinedServicesFromDifferentHTTPRoutesState = "disabled"
)

// ControlPlaneFallbackConfiguration defines the fallback configuration options for the ControlPlane.
type ControlPlaneFallbackConfiguration struct {
	// UseLastValidConfig indicates whether the ControlPlane should use the last valid configuration
	// when the current configuration is invalid.
	//
	// +optional
	// +kubebuilder:default=enabled
	// +kubebuilder:validation:Enum=enabled;disabled
	UseLastValidConfig *ControlPlaneFallbackConfigurationState `json:"useLastValidConfig,omitempty"`
}

// ControlPlaneFallbackConfigurationState defines the state of the fallback configuration feature.
type ControlPlaneFallbackConfigurationState string

const (
	// ControlPlaneFallbackConfigurationStateEnabled indicates that the fallback configuration is enabled.
	ControlPlaneFallbackConfigurationStateEnabled ControlPlaneFallbackConfigurationState = "enabled"
	// ControlPlaneFallbackConfigurationStateDisabled indicates that the fallback configuration is disabled.
	ControlPlaneFallbackConfigurationStateDisabled ControlPlaneFallbackConfigurationState = "disabled"
)

// ControlPlaneDataPlaneSync defines the configuration for syncing Kong configuration to the DataPlane.
//
// +apireference:kgo:include
type ControlPlaneDataPlaneSync struct {
	// ReverseSync sends configuration to DataPlane (Kong Gateway) even if
	// the configuration checksum has not changed since previous update.
	//
	// +optional
	// +kubebuilder:default=disabled
	ReverseSync *ControlPlaneReverseSyncState `json:"reverseSync,omitempty"`

	// Interval is the interval between two rounds of syncing Kong configuration with dataplanes.
	//
	// +optional
	Interval *metav1.Duration `json:"interval,omitempty"`

	// Timeout is the timeout of a single run of syncing Kong configuration with dataplanes.
	//
	// +optional
	Timeout *metav1.Duration `json:"timeout,omitempty"`
}

// ControlPlaneReverseSyncState defines the state of the reverse sync feature.
type ControlPlaneReverseSyncState string

const (
	// ControlPlaneReverseSyncStateEnabled indicates that reverse sync is enabled.
	ControlPlaneReverseSyncStateEnabled ControlPlaneReverseSyncState = "enabled"
	// ControlPlaneReverseSyncStateDisabled indicates that reverse sync is disabled.
	ControlPlaneReverseSyncStateDisabled ControlPlaneReverseSyncState = "disabled"
)

// ControlPlaneDataPlaneTarget defines the target for the DataPlane that the ControlPlane
// is responsible for configuring.
//
// +kubebuilder:validation:XValidation:message="Ref has to be provided when type is set to ref",rule="self.type != 'ref' || has(self.ref)"
// +kubebuilder:validation:XValidation:message="Ref cannot be provided when type is set to managedByOwner",rule="self.type != 'managedByOwner' || !has(self.ref)"
type ControlPlaneDataPlaneTarget struct {
	// Type indicates the type of the DataPlane target.
	//
	// +required
	// +kubebuilder:validation:Enum=ref;managedByOwner
	Type ControlPlaneDataPlaneTargetType `json:"type"`

	// Ref is the name of the DataPlane to configure.
	//
	// +optional
	Ref *ControlPlaneDataPlaneTargetRef `json:"ref,omitempty"`
}

// ControlPlaneDataPlaneTargetType defines the type of the DataPlane target
// that the ControlPlane is responsible for configuring.
type ControlPlaneDataPlaneTargetType string

const (
	// ControlPlaneDataPlaneTargetRefType indicates that the DataPlane target is a ref
	// of a DataPlane resource managed by the operator.
	// This is used for configuring DataPlanes that are managed by the operator.
	ControlPlaneDataPlaneTargetRefType ControlPlaneDataPlaneTargetType = "ref"

	// ControlPlaneDataPlaneTargetManagedByType indicates that the DataPlane target
	// is managed by the owner of the ControlPlane.
	// This is the case when using a Gateway resource to manage the DataPlane
	// and the ControlPlane is responsible for configuring it.
	ControlPlaneDataPlaneTargetManagedByType ControlPlaneDataPlaneTargetType = "managedByOwner"
)

// ControlPlaneDataPlaneTargetRef defines the reference to a DataPlane resource
// that the ControlPlane is responsible for configuring.
type ControlPlaneDataPlaneTargetRef struct {
	// Ref is the name of the DataPlane to configure.
	//
	// +required
	Name string `json:"name"`
}

// ControlPlaneGatewayDiscovery defines the configuration for the Gateway Discovery
// feature of the ControlPlane.
type ControlPlaneGatewayDiscovery struct {
	// ReadinessCheckInterval defines the interval at which the ControlPlane
	// checks the readiness of the DataPlanes it is responsible for.
	// If not specified, the default interval as defined by the operator will be used.
	//
	// +optional
	ReadinessCheckInterval *metav1.Duration `json:"readinessCheckInterval,omitempty"`

	// ReadinessCheckTimeout defines the timeout for the DataPlane readiness check.
	// If not specified, the default interval as defined by the operator will be used.
	//
	// +optional
	ReadinessCheckTimeout *metav1.Duration `json:"readinessCheckTimeout,omitempty"`
}

// ControlPlaneK8sCache defines the configuration related to Kubernetes object caches
// of the ControlPlane.
//
// +apireference:kgo:include
type ControlPlaneK8sCache struct {
	// InitSyncDuration defines the initial delay to wait for Kubernetes object caches to be synced before the initial configuration.
	// If omitted, the default value (5s) is used.
	//
	// +optional
	InitSyncDuration *metav1.Duration `json:"initSyncDuration,omitempty"`
}

// ConfigDumpState defines the state of configuration dump.
type ConfigDumpState string

const (
	// ConfigDumpStateEnabled indicates that configuration dump is enabled.
	ConfigDumpStateEnabled ConfigDumpState = "enabled"
	// ConfigDumpStateDisabled indicates that the configuration dump is disabled.
	ConfigDumpStateDisabled ConfigDumpState = "disabled"
)

// ControlPlaneConfigDump defines the options for dumping translated Kong configuration from a diagnostics server.
//
// +apireference:kgo:include
// +kubebuilder:validation:XValidation:message="Cannot enable dumpSensitive when state is disabled",rule="self.state == 'enabled' || self.dumpSensitive == 'disabled'"
type ControlPlaneConfigDump struct {
	// When State is enabled, Operator will dump the translated Kong configuration by it from a diagnostics server.
	//
	// +required
	// +kubebuilder:validation:Enum=enabled;disabled
	// +kubebuilder:default="disabled"
	State ConfigDumpState `json:"state"`

	// When DumpSensitive is enabled, the configuration will be dumped unchanged, including sensitive parts like private keys and credentials.
	// When DumpSensitive is disabled, the sensitive configuration parts like private keys and credentials are redacted.
	//
	// +required
	// +kubebuilder:validation:Enum=enabled;disabled
	// +kubebuilder:default="disabled"
	DumpSensitive ConfigDumpState `json:"dumpSensitive"`
}

// ControlPlaneObjectFilters defines filters to limit watched objects by the controllers.
type ControlPlaneObjectFilters struct {
	// Secrets defines the filters for watched secrets.
	//
	// +optional
	Secrets *ControlPlaneFilterForObjectType `json:"secrets,omitempty"`
	// ConfigMaps defines the filters for watched config maps.
	//
	// +optional
	ConfigMaps *ControlPlaneFilterForObjectType `json:"configMaps,omitempty"`
}

// ControlPlaneFilterForObjectType defines the filters for a certain type of object.
type ControlPlaneFilterForObjectType struct {
	// MatchLabels defines the labels that the object must match to get reconciled by the controller for the ControlPlane.
	// For example, if `secrets.matchLabels` is `{"label1":"val1","label2":"val2"}`,
	// only secrets with labels `label1=val1` and `label2=val2` are reconciled.
	//
	// +optional
	// +kubebuilder:validation:MaxProperties=8
	// +kubebuilder:validation:XValidation:message="Minimum length of key in matchLabels is 1",rule="self.all(key,key.size() >= 1)"
	// +kubebuilder:validation:XValidation:message="Maximum length of value in matchLabels is 63",rule="self.all(key,self[key].size() <= 63)"
	MatchLabels map[string]string `json:"matchLabels,omitempty"`
}

// DefaultControlPlaneInitialCacheSyncDelay defines the default initial delay
// to wait for the Kubernetes object caches to be synced.
const DefaultControlPlaneInitialCacheSyncDelay = 5 * time.Second

// ControllerState defines the state of a controller.
type ControllerState string

const (
	// ControllerStateEnabled indicates that the controller is enabled.
	ControllerStateEnabled ControllerState = "enabled"
	// ControllerStateDisabled indicates that the controller is disabled.
	ControllerStateDisabled ControllerState = "disabled"
)

// ControlPlaneController defines a controller state for the ControlPlane.
// It overrides the default behavior as defined in the deployed operator version.
//
// +apireference:kgo:include
type ControlPlaneController struct {
	// Name is the name of the controller.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// State indicates whether the feature gate is enabled or disabled.
	//
	// +required
	// +kubebuilder:validation:Enum=enabled;disabled
	State ControllerState `json:"state"`
}

// FeatureGateState defines the state of a feature gate.
type FeatureGateState string

const (
	// FeatureGateStateEnabled indicates that the feature gate is enabled.
	FeatureGateStateEnabled FeatureGateState = "enabled"
	// FeatureGateStateDisabled indicates that the feature gate is disabled.
	FeatureGateStateDisabled FeatureGateState = "disabled"
)

// ControlPlaneFeatureGate defines a feature gate state for the ControlPlane.
// It overrides the default behavior as defined in the deployed operator version.
//
// +apireference:kgo:include
type ControlPlaneFeatureGate struct {
	// Name is the name of the feature gate.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// State indicates whether the feature gate is enabled or disabled.
	//
	// +required
	// +kubebuilder:validation:Enum=enabled;disabled
	State FeatureGateState `json:"state"`
}

// ControlPlaneStatus defines the observed state of ControlPlane
//
// +apireference:kgo:include
type ControlPlaneStatus struct {
	// Conditions describe the current conditions of the Gateway.
	//
	// +optional
	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	// +kubebuilder:validation:MaxItems=8
	// +kubebuilder:default={{type: "Provisioned", status: "Unknown", reason:"NotReconciled", message:"Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`

	// DataPlane describes the status of the DataPlane that the ControlPlane is
	// responsible for configuring.
	//
	// +optional
	DataPlane *ControlPlaneDataPlaneStatus `json:"dataPlane,omitempty"`

	// FeatureGates is a list of effective feature gates for this ControlPlane.
	//
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=32
	FeatureGates []ControlPlaneFeatureGate `json:"featureGates,omitempty"`

	// Controllers is a list of enabled and disabled controllers for this ControlPlane.
	//
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=32
	Controllers []ControlPlaneController `json:"controllers,omitempty"`
}

// ControlPlaneDataPlaneStatus defines the status of the DataPlane that the
// ControlPlane is responsible for configuring.
type ControlPlaneDataPlaneStatus struct {
	// Name is the name of the DataPlane.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
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

// ControlPlaneKonnectOptions defines the Konnect-related configuration options for the ControlPlane.
//
// +apireference:kgo:include
type ControlPlaneKonnectOptions struct {
	// ConsumersSync indicates whether consumer synchronization with Konnect is enabled.
	//
	// +optional
	// +kubebuilder:default=enabled
	// +kubebuilder:validation:Enum=enabled;disabled
	ConsumersSync *ControlPlaneKonnectConsumersSyncState `json:"consumersSync,omitempty"`

	// Licensing defines the configuration for Konnect licensing.
	//
	// +optional
	Licensing *ControlPlaneKonnectLicensing `json:"licensing,omitempty"`

	// NodeRefreshPeriod is the period for refreshing the node information in Konnect.
	//
	// +optional
	NodeRefreshPeriod *metav1.Duration `json:"nodeRefreshPeriod,omitempty"`

	// ConfigUploadPeriod is the period for uploading configuration to Konnect.
	//
	// +optional
	ConfigUploadPeriod *metav1.Duration `json:"configUploadPeriod,omitempty"`
}

// ControlPlaneKonnectConsumersSyncState defines the state of consumer synchronization with Konnect.
type ControlPlaneKonnectConsumersSyncState string

const (
	// ControlPlaneKonnectConsumersSyncStateEnabled indicates that consumer synchronization is enabled.
	ControlPlaneKonnectConsumersSyncStateEnabled ControlPlaneKonnectConsumersSyncState = "enabled"
	// ControlPlaneKonnectConsumersSyncStateDisabled indicates that consumer synchronization is disabled.
	ControlPlaneKonnectConsumersSyncStateDisabled ControlPlaneKonnectConsumersSyncState = "disabled"
)

// ControlPlaneKonnectLicensing defines the configuration for Konnect licensing.
//
// +apireference:kgo:include
// +kubebuilder:validation:XValidation:message="initialPollingPeriod can only be set when licensing is enabled",rule="!has(self.initialPollingPeriod) || self.state == 'enabled'"
// +kubebuilder:validation:XValidation:message="pollingPeriod can only be set when licensing is enabled",rule="!has(self.pollingPeriod) || self.state == 'enabled'"
// +kubebuilder:validation:XValidation:message="storageState can only be set to enabled when licensing is enabled",rule="!has(self.storageState) || self.storageState == 'disabled' || self.state == 'enabled'"
type ControlPlaneKonnectLicensing struct {
	// State indicates whether Konnect licensing is enabled.
	//
	// +optional
	// +kubebuilder:default=disabled
	// +kubebuilder:validation:Enum=enabled;disabled
	State *ControlPlaneKonnectLicensingState `json:"state,omitempty"`

	// InitialPollingPeriod is the initial polling period for license checks.
	//
	// +optional
	InitialPollingPeriod *metav1.Duration `json:"initialPollingPeriod,omitempty"`

	// PollingPeriod is the polling period for license checks.
	//
	// +optional
	PollingPeriod *metav1.Duration `json:"pollingPeriod,omitempty"`

	// StorageState indicates whether to store licenses fetched from Konnect
	// to Secrets locally to use them later when connection to Konnect is broken.
	// Only effective when State is set to enabled.
	//
	// +optional
	// +kubebuilder:default=enabled
	// +kubebuilder:validation:Enum=enabled;disabled
	StorageState *ControlPlaneKonnectLicensingState `json:"storageState,omitempty"`
}

// ControlPlaneKonnectLicensingState defines the state of Konnect licensing.
type ControlPlaneKonnectLicensingState string

const (
	// ControlPlaneKonnectLicensingStateEnabled indicates that Konnect licensing is enabled.
	ControlPlaneKonnectLicensingStateEnabled ControlPlaneKonnectLicensingState = "enabled"
	// ControlPlaneKonnectLicensingStateDisabled indicates that Konnect licensing is disabled.
	ControlPlaneKonnectLicensingStateDisabled ControlPlaneKonnectLicensingState = "disabled"
)

// Hub marks the ControlPlane type as a hub type (storageversion) for conversion webhook.
func (c *ControlPlane) Hub() {}
