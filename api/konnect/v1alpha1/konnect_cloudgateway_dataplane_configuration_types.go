package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/common/v1alpha1"
	konnectv1alpha2 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha2"
)

func init() {
	SchemeBuilder.Register(&KonnectCloudGatewayDataPlaneGroupConfiguration{}, &KonnectCloudGatewayDataPlaneGroupConfigurationList{})
}

// KonnectCloudGatewayDataPlaneGroupConfiguration is the Schema for the Konnect Network API.
//
// +genclient
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:resource:categories=kong;konnect
// +kubebuilder:object:root=true
// +kubebuilder:object:generate=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Programmed",description="The Resource is Programmed on Konnect",type=string,JSONPath=`.status.conditions[?(@.type=='Programmed')].status`
// +kubebuilder:printcolumn:name="ID",description="Konnect ID",type=string,JSONPath=`.status.id`
// +kubebuilder:printcolumn:name="ControlPlaneID",description="ControlPlane ID",type=string,JSONPath=`.status.controlPlaneID`
// +kubebuilder:printcolumn:name="OrgID",description="Konnect Organization ID this resource belongs to.",type=string,JSONPath=`.status.organizationID`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`,description="Age"
// +apireference:kgo:include
// +kong:channels=gateway-operator
type KonnectCloudGatewayDataPlaneGroupConfiguration struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the desired state of KonnectCloudGatewayDataPlaneGroupConfiguration.
	Spec KonnectCloudGatewayDataPlaneGroupConfigurationSpec `json:"spec"`

	// Status defines the observed state of KonnectCloudGatewayDataPlaneGroupConfiguration.
	//
	// +optional
	Status KonnectCloudGatewayDataPlaneGroupConfigurationStatus `json:"status,omitempty"`
}

// KonnectCloudGatewayDataPlaneGroupConfigurationSpec defines the desired state of KonnectCloudGatewayDataPlaneGroupConfiguration.
//
// +apireference:kgo:include
type KonnectCloudGatewayDataPlaneGroupConfigurationSpec struct {
	// Version specifies the desired Kong Gateway version.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	Version string `json:"version"`

	// DataplaneGroups is a list of desired data-plane groups that describe where
	// to deploy instances, along with how many instances.
	//
	// +optional
	DataplaneGroups []KonnectConfigurationDataPlaneGroup `json:"dataplane_groups"`

	// APIAccess is the desired type of API access for data-plane groups.
	//
	// +optional
	// +kubebuilder:default=private+public
	// +kubebuilder:validation:Enum=private;public;private+public
	APIAccess *sdkkonnectcomp.APIAccess `json:"api_access"`

	// ControlPlaneRef is a reference to a ControlPlane which DataPlanes from this
	// configuration will connect to.
	//
	// +required
	ControlPlaneRef commonv1alpha1.ControlPlaneRef `json:"controlPlaneRef"`
}

// APIAccess defines the API access type for data-plane groups.
type APIAccess string

const (
	// APIAccessPrivate is the API access type for private data-plane groups.
	APIAccessPrivate APIAccess = "private"
	// APIAccessPublic is the API access type for public data-plane groups.
	APIAccessPublic APIAccess = "public"
	// APIAccessPrivatePublic is the API access type for private and public data-plane groups.
	APIAccessPrivatePublic APIAccess = "private+public"
)

// KonnectConfigurationDataPlaneGroup is the schema for the KonnectConfiguration type.
type KonnectConfigurationDataPlaneGroup struct {
	// Name of cloud provider.
	//
	// +required
	Provider sdkkonnectcomp.ProviderName `json:"provider"`

	// Region for cloud provider region.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	Region string `json:"region"`

	// NetworkRef is the reference to the network that this data-plane group will be deployed on.
	//
	// Cross namespace references are not supported for networkRef of type namespacedRef.
	// This will be enforced in the future but currently (due to limitation in CEL validation
	// in Kubernetes 1.31 and older) it is not.
	//
	// +required
	NetworkRef commonv1alpha1.ObjectRef `json:"networkRef"`

	// Autoscale configuration for the data-plane group.
	//
	// +required
	Autoscale ConfigurationDataPlaneGroupAutoscale `json:"autoscale"`

	// Array of environment variables to set for a data-plane group.
	//
	// +optional
	Environment []ConfigurationDataPlaneGroupEnvironmentField `json:"environment,omitempty"`
}

// ConfigurationDataPlaneGroupEnvironmentField specifies an environment variable field for the data-plane group.
type ConfigurationDataPlaneGroupEnvironmentField struct {
	// Name of the environment variable field to set for the data-plane group. Must be prefixed by KONG_.
	//
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +required
	// +kubebuilder:validation:Pattern="^KONG_."
	Name string `json:"name"`
	// Value assigned to the environment variable field for the data-plane group.
	//
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +required
	Value string `json:"value"`
}

// ConfigurationDataPlaneGroupAutoscaleType is the type of autoscale configuration for the data-plane group.
type ConfigurationDataPlaneGroupAutoscaleType string

const (
	// ConfigurationDataPlaneGroupAutoscaleTypeStatic is the autoscale type for static configuration.
	ConfigurationDataPlaneGroupAutoscaleTypeStatic ConfigurationDataPlaneGroupAutoscaleType = "static"

	// ConfigurationDataPlaneGroupAutoscaleTypeAutopilot is the autoscale type for autopilot configuration.
	ConfigurationDataPlaneGroupAutoscaleTypeAutopilot ConfigurationDataPlaneGroupAutoscaleType = "autopilot"
)

// ConfigurationDataPlaneGroupAutoscale specifies the autoscale configuration for the data-plane group.
//
// +kubebuilder:validation:XValidation:rule="!(has(self.autopilot) && has(self.static))",message="can't provide both autopilot and static configuration"
// +kubebuilder:validation:XValidation:rule="self.type == 'static' ? has(self.static) : true",message="static is required when type is static"
// +kubebuilder:validation:XValidation:rule="self.type == 'autopilot' ? has(self.autopilot) : true",message="autopilot is required when type is autopilot"
type ConfigurationDataPlaneGroupAutoscale struct {
	// Static specifies the static configuration for the data-plane group.
	//
	// +optional
	Static *ConfigurationDataPlaneGroupAutoscaleStatic `json:"static,omitempty"`

	// Autopilot specifies the autoscale configuration for the data-plane group.
	//
	// +optional
	Autopilot *ConfigurationDataPlaneGroupAutoscaleAutopilot `json:"autopilot,omitempty"`

	// Type of autoscaling to use.
	//
	// +required
	// +kubebuilder:validation:Enum=static;autopilot
	Type ConfigurationDataPlaneGroupAutoscaleType `json:"type"`
}

// ConfigurationDataPlaneGroupAutoscaleAutopilot specifies the autoscale configuration for the data-plane group.
type ConfigurationDataPlaneGroupAutoscaleAutopilot struct {
	// Base number of requests per second that the deployment target should support.
	//
	// +required
	BaseRps int64 `json:"base_rps"`

	// Max number of requests per second that the deployment target should support. If not set, this defaults to 10x base_rps.
	//
	// +optional
	MaxRps *int64 `json:"max_rps,omitempty"`
}

// ConfigurationDataPlaneGroupAutoscaleStatic specifies the static configuration for the data-plane group.
type ConfigurationDataPlaneGroupAutoscaleStatic struct {
	// Instance type name to indicate capacity.
	// Currently supported values are small, medium, large but this list might be
	// expanded in the future.
	// For all the allowed values, please refer to the Konnect API documentation
	// at https://docs.konghq.com/konnect/api/cloud-gateways/latest/#/Data-Plane%20Group%20Configurations/create-configuration.
	//
	// +required
	InstanceType sdkkonnectcomp.InstanceTypeName `json:"instance_type"`

	// Number of data-planes the deployment target will contain.
	//
	// +required
	RequestedInstances int64 `json:"requested_instances"`
}

// KonnectCloudGatewayDataPlaneGroupConfigurationStatus defines the observed state of KonnectCloudGatewayDataPlaneGroupConfiguration.
// +apireference:kgo:include
type KonnectCloudGatewayDataPlaneGroupConfigurationStatus struct {
	// Conditions describe the current conditions of the KonnectCloudGatewayDataPlaneGroupConfiguration.
	//
	// Known condition types are:
	//
	// * "Programmed"
	//
	// +optional
	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	// +kubebuilder:validation:MaxItems=8
	// +kubebuilder:default={{type: "Programmed", status: "Unknown", reason:"Pending", message:"Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`

	// DataPlaneGroups is a list of deployed data-plane groups.
	//
	// +optional
	DataPlaneGroups []KonnectCloudGatewayDataPlaneGroupConfigurationStatusGroup `json:"dataplane_groups,omitempty"`

	konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef `json:",inline"`
}

// KonnectCloudGatewayDataPlaneGroupConfigurationStatusGroup defines the observed state of a deployed data-plane group.
type KonnectCloudGatewayDataPlaneGroupConfigurationStatusGroup struct {
	// ID is the ID of the deployed data-plane group.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	ID string `json:"id"`

	// CloudGatewayNetworkID is the ID of the cloud gateway network.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	CloudGatewayNetworkID string `json:"cloud_gateway_network_id"`

	// Name of cloud provider.
	//
	// +required
	Provider sdkkonnectcomp.ProviderName `json:"provider"`

	// Region ID for cloud provider region.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	Region string `json:"region"`

	// PrivateIPAddresses is a list of private IP addresses of the internal load balancer that proxies traffic to this data-plane group.
	//
	// +optional
	PrivateIPAddresses []string `json:"private_ip_addresses,omitempty"`

	// EgressIPAddresses is a list of egress IP addresses for the network that this data-plane group runs on.
	//
	// +optional
	EgressIPAddresses []string `json:"egress_ip_addresses,omitempty"`

	// State is the current state of the data plane group. Can be e.g. initializing, ready, terminating.
	//
	// +required
	// +kubebuilder:validation:MinLength=1
	State string `json:"state"`
}

// KonnectCloudGatewayDataPlaneGroupConfigurationList contains a list of KonnectCloudGatewayDataPlaneGroupConfiguration.
// +kubebuilder:object:root=true
// +apireference:kgo:include
type KonnectCloudGatewayDataPlaneGroupConfigurationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []KonnectCloudGatewayDataPlaneGroupConfiguration `json:"items"`
}
