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

// KongService is the schema for Services API which defines a Kong Service.
//
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Host",type=string,JSONPath=`.spec.host`,description="Host of the service"
// +kubebuilder:printcolumn:name="Protocol",type=string,JSONPath=`.spec.procol`,description="Protocol of the service"
// +kubebuilder:printcolumn:name="Programmed",description="The Resource is Programmed on Konnect",type=string,JSONPath=`.status.conditions[?(@.type=='Programmed')].status`
// +kubebuilder:printcolumn:name="ID",description="Konnect ID",type=string,JSONPath=`.status.id`
// +kubebuilder:printcolumn:name="OrgID",description="Konnect Organization ID this resource belongs to.",type=string,JSONPath=`.status.organizationID`
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.spec.konnect.authRef) || has(self.spec.konnect.authRef)", message="Konnect Configuration's API auth ref reference is required once set"
type KongService struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   KongServiceSpec   `json:"spec"`
	Status KongServiceStatus `json:"status,omitempty"`
}

func (c *KongService) GetStatus() *KonnectEntityStatus {
	return &c.Status.KonnectEntityStatus
}

func (c KongService) GetTypeName() string {
	return "KongService"
}

func (c *KongService) SetKonnectLabels(labels map[string]string) {
}

func (c *KongService) GetKonnectAPIAuthConfigurationRef() KonnectAPIAuthConfigurationRef {
	return c.Spec.KonnectConfiguration.APIAuthConfigurationRef
}

// KongServiceSpec defines specification of a Kong Route.
type KongServiceSpec struct {
	// ControlPlaneRef is a reference to a ControlPlane this Route is associated with.
	// +kubebuilder:validation:Required
	ControlPlaneRef ControlPlaneRef `json:"controlPlaneRef"`

	// +kubebuilder:validation:Required
	KonnectConfiguration KonnectConfiguration `json:"konnect,omitempty"`

	KongServiceAPISpec `json:",inline"`
}

// KongServiceAPISpec defines specification of a Kong Service.
type KongServiceAPISpec struct {
	// TODO(pmalek): client certificate implement ref
	// TODO(pmalek): ca_certificates implement ref

	// TODO(pmalek): field below are basically copy pasted from sdkkonnectgocomp.CreateService
	// The reason for this is that Service creation request contains a Konnect ID
	// reference to a client certificate. This is not what we want to expose to the user.
	// Instead we want to expose a namespaced reference to a client certificate.
	// Even if the cross namespace reference is not planned, the structured reference
	// type is preferred because it allows for easier extension in the future.
	//
	// sdkkonnectgocomp.CreateService `json:",inline"`

	// Helper field to set `protocol`, `host`, `port` and `path` using a URL. This field is write-only and is not returned in responses.
	URL *string `json:"url,omitempty"`
	// The timeout in milliseconds for establishing a connection to the upstream server.
	ConnectTimeout *int64 `json:"connect_timeout,omitempty"`
	// Whether the Service is active. If set to `false`, the proxy behavior will be as if any routes attached to it do not exist (404). Default: `true`.
	Enabled *bool `json:"enabled,omitempty"`
	// The host of the upstream server. Note that the host value is case sensitive.
	// +kubebuilder:validation:Required
	Host string `json:"host"`
	// The Service name.
	Name *string `json:"name,omitempty"`
	// The path to be used in requests to the upstream server.
	Path *string `json:"path,omitempty"`
	// The upstream server port.
	Port *int64 `json:"port,omitempty"`
	// The protocol used to communicate with the upstream.
	Protocol *sdkkonnectgocomp.Protocol `json:"protocol,omitempty"`
	// The timeout in milliseconds between two successive read operations for transmitting a request to the upstream server.
	ReadTimeout *int64 `json:"read_timeout,omitempty"`
	// The number of retries to execute upon failure to proxy.
	Retries *int64 `json:"retries,omitempty"`
	// An optional set of strings associated with the Service for grouping and filtering.
	Tags []string `json:"tags,omitempty"`
	// Whether to enable verification of upstream server TLS certificate. If set to `null`, then the Nginx default is respected.
	TLSVerify *bool `json:"tls_verify,omitempty"`
	// Maximum depth of chain while verifying Upstream server's TLS certificate. If set to `null`, then the Nginx default is respected.
	TLSVerifyDepth *int64 `json:"tls_verify_depth,omitempty"`
	// The timeout in milliseconds between two successive write operations for transmitting a request to the upstream server.
	WriteTimeout *int64 `json:"write_timeout,omitempty"`
}

// KongServiceStatus represents the current status of the Kong Service resource.
type KongServiceStatus struct {
	KonnectEntityStatus `json:",inline"`
	// ControlPlaneID is the unique identifier of the ControlPlane this Route is associated with.
	// This is currently only set for Konnect Control Planes.
	ControlPlaneID string `json:"controlPlaneID,omitempty"`
}

// +kubebuilder:object:root=true

// KongServiceList contains a list of Kong Services.
type KongServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KongService `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KongService{}, &KongServiceList{})
}
