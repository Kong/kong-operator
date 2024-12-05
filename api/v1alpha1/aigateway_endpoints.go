package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// -----------------------------------------------------------------------------
// AIGateway API - Endpoints
// -----------------------------------------------------------------------------

// AIGatewayEndpoint is a network endpoint for accessing an AIGateway.
// +apireference:kgo:include
type AIGatewayEndpoint struct {
	// NetworkAccessHint is a hint to the user about what kind of network access
	// is expected for the reachability of this endpoint.
	NetworkAccessHint EndpointNetworkAccessHint `json:"network"`

	// URL is the URL to access the endpoint from the network indicated by the
	// NetworkAccessHint.
	URL string `json:"url"`

	// AvailableModels is a list of the identifiers of all the AI models that are
	// accessible from this endpoint.
	AvailableModels []string `json:"models"`

	// Consumer is a reference to the Secret that contains the credentials for
	// the Kong consumer that is allowed to access this endpoint.
	Consumer AIGatewayConsumerRef `json:"consumer"`

	// Conditions describe the current conditions of the AIGatewayEndpoint.
	//
	// Known condition types are:
	//
	//   - "Provisioning"
	//   - "EndpointReady"
	//
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:MaxItems=8
	// +kubebuilder:default={{type: "Provisioning", status: "Unknown", reason:"Pending", message:"Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// -----------------------------------------------------------------------------
// AIGateway API - Endpoints - Types
// -----------------------------------------------------------------------------

// EndpointNetworkAccessHint provides a human readable indication of what kind
// of network access is expected for a Gateway.
//
// This isn't meant to reflect knowledge of any specific network by name, which
// is why it includes "hint" in the name. It's meant to be a hint to the user
// such as "internet-accessible", "internal-only".
// +apireference:kgo:include
type EndpointNetworkAccessHint string

const (
	// NetworkInternetAccessible indicates that the endpoint is accessible from
	// the public internet.
	NetworkInternetAccessible EndpointNetworkAccessHint = "internet-accessible"
)

// AIGatewayConsumerRef indicates the Secret resource containing the credentials
// for the Kong consumer.
// +apireference:kgo:include
type AIGatewayConsumerRef struct {
	// Name is the name of the reference object.
	//
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Namespace is the namespace of the reference object.
	//
	// +kubebuilder:validation:Required
	Namespace string `json:"namespace"`
}
