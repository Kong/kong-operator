/*
Copyright 2023 Kong, Inc.

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
	sdkkonnectgocomp "github.com/Kong/sdk-konnect-go/models/components"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TODO(pmalek): expression field is not in OpenAPI spec but error message references it:
//   when protocols has 'http', at least one of 'hosts', 'methods', 'paths', 'headers' or 'expression' must be set

// KongRoute is the schema for Routes API which defines a Kong Route.
//
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Programmed",description="The Resource is Programmed on Konnect",type=string,JSONPath=`.status.conditions[?(@.type=='Programmed')].status`
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.spec.konnect.authRef) || has(self.spec.konnect.authRef)", message="Konnect Configuration's API auth ref reference is required once set"
// +kubebuilder:validation:XValidation:rule="self.spec.protocols.exists(p, p == 'http') ? (has(self.spec.hosts) || has(self.spec.methods) || has(self.spec.paths) || has(self.spec.paths) || has(self.spec.paths) || has(self.spec.headers) ) : true", message="If protocols has 'http', at least one of 'hosts', 'methods', 'paths' or 'headers' must be set"
type KongRoute struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   KongRouteSpec   `json:"spec"`
	Status KongRouteStatus `json:"status,omitempty"`
}

func (c *KongRoute) GetKonnectStatus() *KonnectEntityStatus {
	return &c.Status.Konnect.KonnectEntityStatus
}

func (c KongRoute) GetTypeName() string {
	return "KongRoute"
}

func (c *KongRoute) GetKonnectAPIAuthConfigurationRef() KonnectAPIAuthConfigurationRef {
	return c.Spec.KonnectConfiguration.APIAuthConfigurationRef
}

// GetConditions returns the Status Conditions
func (c *KongRoute) GetConditions() []metav1.Condition {
	return c.Status.Conditions
}

// SetConditions sets the Status Conditions
func (c *KongRoute) SetConditions(conditions []metav1.Condition) {
	c.Status.Conditions = conditions
}

// KongRouteSpec defines specification of a Kong Route.
type KongRouteSpec struct {
	// ControlPlaneRef is a reference to a ControlPlane this Route is associated with.
	// +kubebuilder:validation:Required
	ControlPlaneRef ControlPlaneRef `json:"controlPlaneRef"`

	// ServiceRef is a reference to a Service this Route is associated with.
	// +optional
	ServiceRef ServiceRef `json:"serviceRef,omitempty"`

	// KonnectConfiguration holds the Konnect configuration like authentication configuration.
	// +kubebuilder:validation:Required
	KonnectConfiguration KonnectConfiguration `json:"konnect,omitempty"`

	KongRouteAPISpec `json:",inline"`
}

// KongRouteAPISpec represents the configuration of a Route in Kong as defined by the Konnect API.
//
// These fields are mostly copied from sdk-konnect-go but some modifications have been made
// to make the code generation required for Kubernetes CRDs work.
type KongRouteAPISpec struct {
	// A list of IP destinations of incoming connections that match this Route when using stream routing. Each entry is an object with fields "ip" (optionally in CIDR range notation) and/or "port".
	Destinations []sdkkonnectgocomp.CreateRouteDestinations `json:"destinations,omitempty"`
	// One or more lists of values indexed by header name that will cause this Route to match if present in the request. The `Host` header cannot be used with this attribute: hosts should be specified using the `hosts` attribute. When `headers` contains only one value and that value starts with the special prefix `~*`, the value is interpreted as a regular expression.
	Headers map[string]string `json:"headers,omitempty"`
	// A list of domain names that match this Route. Note that the hosts value is case sensitive.
	Hosts []string `json:"hosts,omitempty"`
	// The status code Kong responds with when all properties of a Route match except the protocol i.e. if the protocol of the request is `HTTP` instead of `HTTPS`. `Location` header is injected by Kong if the field is set to 301, 302, 307 or 308. Note: This config applies only if the Route is configured to only accept the `https` protocol.
	HTTPSRedirectStatusCode *sdkkonnectgocomp.CreateRouteHTTPSRedirectStatusCode `json:"https_redirect_status_code,omitempty"`
	// A list of HTTP methods that match this Route.
	Methods []string `json:"methods,omitempty"`
	// The name of the Route. Route names must be unique, and they are case sensitive. For example, there can be two different Routes named "test" and "Test".
	Name *string `json:"name,omitempty"`
	// Controls how the Service path, Route path and requested path are combined when sending a request to the upstream. See above for a detailed description of each behavior.
	PathHandling *sdkkonnectgocomp.CreateRoutePathHandling `json:"path_handling,omitempty"`
	// A list of paths that match this Route.
	Paths []string `json:"paths,omitempty"`
	// When matching a Route via one of the `hosts` domain names, use the request `Host` header in the upstream request headers. If set to `false`, the upstream `Host` header will be that of the Service's `host`.
	PreserveHost *bool `json:"preserve_host,omitempty"`
	// An array of the protocols this Route should allow. See the [Route Object](#route-object) section for a list of accepted protocols. When set to only `"https"`, HTTP requests are answered with an upgrade error. When set to only `"http"`, HTTPS requests are answered with an error.
	Protocols []sdkkonnectgocomp.CreateRouteProtocols `json:"protocols,omitempty"`
	// A number used to choose which route resolves a given request when several routes match it using regexes simultaneously. When two routes match the path and have the same `regex_priority`, the older one (lowest `created_at`) is used. Note that the priority for non-regex routes is different (longer non-regex routes are matched before shorter ones).
	RegexPriority *int64 `json:"regex_priority,omitempty"`
	// Whether to enable request body buffering or not. With HTTP 1.1, it may make sense to turn this off on services that receive data with chunked transfer encoding.
	RequestBuffering *bool `json:"request_buffering,omitempty"`
	// Whether to enable response body buffering or not. With HTTP 1.1, it may make sense to turn this off on services that send data with chunked transfer encoding.
	ResponseBuffering *bool `json:"response_buffering,omitempty"`
	// A list of SNIs that match this Route when using stream routing.
	Snis []string `json:"snis,omitempty"`
	// A list of IP sources of incoming connections that match this Route when using stream routing. Each entry is an object with fields "ip" (optionally in CIDR range notation) and/or "port".
	Sources []sdkkonnectgocomp.CreateRouteSources `json:"sources,omitempty"`
	// When matching a Route via one of the `paths`, strip the matching prefix from the upstream request URL.
	StripPath *bool `json:"strip_path,omitempty"`
	// An optional set of strings associated with the Route for grouping and filtering.
	Tags []string `json:"tags,omitempty"`
}

// KongRouteStatus represents the current status of the Kong Route resource.
type KongRouteStatus struct {
	// Konnect contains the Konnect entity status.
	Konnect KonnectEntityStatusWithControlPlaneAndServiceRefs `json:"konnect,omitempty"`

	// Conditions describe the status of the Konnect entity.
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=8
	// +kubebuilder:default={{type: "Programmed", status: "Unknown", reason:"Pending", message:"Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true

// KongRouteList contains a list of Kong Routes.
type KongRouteList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KongRoute `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KongRoute{}, &KongRouteList{})
}
