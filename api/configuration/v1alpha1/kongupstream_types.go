/*
Copyright 2024 Kong, Inc.

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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

// KongUpstream is the schema for Upstream API which defines a Kong Upstream.
//
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Programmed",description="The Resource is Programmed on Konnect",type=string,JSONPath=`.status.conditions[?(@.type=='Programmed')].status`
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.spec.controlPlaneRef) || has(self.spec.controlPlaneRef)", message="controlPlaneRef is required once set"
// +kubebuilder:validation:XValidation:rule="!has(self.spec.controlPlaneRef.konnectNamespacedRef) ? true : !has(self.spec.controlPlaneRef.konnectNamespacedRef.__namespace__)", message="spec.controlPlaneRef cannot specify namespace for namespaced resource"
// +kubebuilder:validation:XValidation:rule="(!self.status.conditions.exists(c, c.type == 'Programmed' && c.status == 'True')) ? true : oldSelf.spec.controlPlaneRef == self.spec.controlPlaneRef", message="spec.controlPlaneRef is immutable when an entity is already Programmed"
// +apireference:kgo:include
type KongUpstream struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec KongUpstreamSpec `json:"spec"`

	// +kubebuilder:default={conditions: {{type: "Programmed", status: "Unknown", reason:"Pending", message:"Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}}
	Status KongUpstreamStatus `json:"status,omitempty"`
}

// KongUpstreamSpec defines specification of a Kong Upstream.
// +apireference:kgo:include
type KongUpstreamSpec struct {
	// ControlPlaneRef is a reference to a ControlPlane this KongUpstream is associated with.
	// +optional
	ControlPlaneRef *ControlPlaneRef `json:"controlPlaneRef,omitempty"`

	KongUpstreamAPISpec `json:",inline"`
}

// KongUpstreamAPISpec defines specification of a Kong Upstream.
//
// +kubebuilder:validation:XValidation:rule="!has(self.hash_fallback) || (self.hash_fallback != 'header' || has(self.hash_fallback_header))", message="hash_fallback_header is required when `hash_fallback` is set to `header`."
// +kubebuilder:validation:XValidation:rule="!has(self.hash_fallback) || (self.hash_fallback != 'query_arg' || has(self.hash_fallback_query_arg))", message="hash_fallback_query_arg is required when `hash_fallback` is set to `query_arg`."
// +kubebuilder:validation:XValidation:rule="!has(self.hash_fallback) || (self.hash_fallback != 'uri_capture' || has(self.hash_fallback_uri_capture))", message="hash_fallback_uri_capture is required when `hash_fallback` is set to `uri_capture`."
// +kubebuilder:validation:XValidation:rule="!has(self.hash_fallback) || (self.hash_fallback != 'cookie' || has(self.hash_on_cookie))", message="hash_on_cookie is required when hash_fallback is set to `cookie`."
// +kubebuilder:validation:XValidation:rule="!has(self.hash_on) || (self.hash_on != 'cookie' || has(self.hash_on_cookie))", message="hash_on_cookie is required when hash_on is set to `cookie`."
// +kubebuilder:validation:XValidation:rule="!has(self.hash_fallback) || (self.hash_fallback != 'cookie' || has(self.hash_on_cookie_path))", message="hash_on_cookie_path is required when hash_fallback is set to `cookie`."
// +kubebuilder:validation:XValidation:rule="!has(self.hash_on) || (self.hash_on != 'cookie' || has(self.hash_on_cookie_path))", message="hash_on_cookie_path is required when hash_on is set to `cookie`."
// +kubebuilder:validation:XValidation:rule="!has(self.hash_on) || (self.hash_on != 'header' || has(self.hash_on_header))", message="hash_on_header is required when hash_on is set to `header`."
// +kubebuilder:validation:XValidation:rule="!has(self.hash_on) || (self.hash_on != 'query_arg' || has(self.hash_on_query_arg))", message="hash_on_query_arg is required when `hash_on` is set to `query_arg`."
// +kubebuilder:validation:XValidation:rule="!has(self.hash_on) || (self.hash_on != 'uri_capture' || has(self.hash_on_uri_capture))", message="hash_on_uri_capture is required when `hash_on` is set to `uri_capture`."
// +apireference:kgo:include
type KongUpstreamAPISpec struct {
	// Which load balancing algorithm to use.
	Algorithm *sdkkonnectcomp.UpstreamAlgorithm `default:"round-robin" json:"algorithm,omitempty"`
	// If set, the certificate to be used as client certificate while TLS handshaking to the upstream server.
	ClientCertificate *sdkkonnectcomp.UpstreamClientCertificate `json:"client_certificate,omitempty"`
	// What to use as hashing input if the primary `hash_on` does not return a hash (eg. header is missing, or no Consumer identified). Not available if `hash_on` is set to `cookie`.
	HashFallback *sdkkonnectcomp.HashFallback `default:"none" json:"hash_fallback,omitempty"`
	// The header name to take the value from as hash input. Only required when `hash_fallback` is set to `header`.
	HashFallbackHeader *string `json:"hash_fallback_header,omitempty"`
	// The name of the query string argument to take the value from as hash input. Only required when `hash_fallback` is set to `query_arg`.
	HashFallbackQueryArg *string `json:"hash_fallback_query_arg,omitempty"`
	// The name of the route URI capture to take the value from as hash input. Only required when `hash_fallback` is set to `uri_capture`.
	HashFallbackURICapture *string `json:"hash_fallback_uri_capture,omitempty"`
	// What to use as hashing input. Using `none` results in a weighted-round-robin scheme with no hashing.
	HashOn *sdkkonnectcomp.HashOn `default:"none" json:"hash_on,omitempty"`
	// The cookie name to take the value from as hash input. Only required when `hash_on` or `hash_fallback` is set to `cookie`. If the specified cookie is not in the request, Kong will generate a value and set the cookie in the response.
	HashOnCookie *string `json:"hash_on_cookie,omitempty"`
	// The cookie path to set in the response headers. Only required when `hash_on` or `hash_fallback` is set to `cookie`.
	HashOnCookiePath *string `default:"/" json:"hash_on_cookie_path,omitempty"`
	// The header name to take the value from as hash input. Only required when `hash_on` is set to `header`.
	HashOnHeader *string `json:"hash_on_header,omitempty"`
	// The name of the query string argument to take the value from as hash input. Only required when `hash_on` is set to `query_arg`.
	HashOnQueryArg *string `json:"hash_on_query_arg,omitempty"`
	// The name of the route URI capture to take the value from as hash input. Only required when `hash_on` is set to `uri_capture`.
	HashOnURICapture *string                      `json:"hash_on_uri_capture,omitempty"`
	Healthchecks     *sdkkonnectcomp.Healthchecks `json:"healthchecks,omitempty"`
	// The hostname to be used as `Host` header when proxying requests through Kong.
	HostHeader *string `json:"host_header,omitempty"`
	// This is a hostname, which must be equal to the `host` of a Service.
	Name string `json:"name,omitempty"`
	// The number of slots in the load balancer algorithm. If `algorithm` is set to `round-robin`, this setting determines the maximum number of slots. If `algorithm` is set to `consistent-hashing`, this setting determines the actual number of slots in the algorithm. Accepts an integer in the range `10`-`65536`.
	// +kubebuilder:validation:Minimum=10
	// +kubebuilder:validation:Maximum=65536
	Slots *int64 `default:"10000" json:"slots,omitempty"`
	// An optional set of strings associated with the Upstream for grouping and filtering.
	Tags []string `json:"tags,omitempty"`
	// If set, the balancer will use SRV hostname(if DNS Answer has SRV record) as the proxy upstream `Host`.
	UseSrvName *bool `default:"false" json:"use_srv_name,omitempty"`
}

// KongUpstreamStatus represents the current status of the Kong Upstream resource.
// +apireference:kgo:include
type KongUpstreamStatus struct {
	// Konnect contains the Konnect entity status.
	// +optional
	Konnect *konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef `json:"konnect,omitempty"`

	// Conditions describe the status of the Konnect entity.
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=8
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// KongUpstreamList contains a list of Kong Upstreams.
// +kubebuilder:object:root=true
// +apireference:kgo:include
type KongUpstreamList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KongUpstream `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KongUpstream{}, &KongUpstreamList{})
}
