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

// OnPremAIGatewayWatchNamespaces defines the namespaces watched for the
// configuration entities of an OnPremAIGateway.
//
// It mirrors the ControlPlane's spec.watchNamespaces.
//
// +kubebuilder:validation:XValidation:message="list is required when type is 'list'",rule="self.type == 'list' ? has(self.list) : true"
// +kubebuilder:validation:XValidation:message="list must not be specified when type is not 'list'",rule="self.type != 'list' ? !has(self.list) : true"
type OnPremAIGatewayWatchNamespaces struct {
	// Type indicates the type of namespace watching to be done.
	//
	// +required
	Type OnPremAIGatewayWatchNamespacesType `json:"type,omitempty"`

	// List is a list of namespaces to watch for resources.
	// Only used when Type is set to List.
	//
	// +optional
	// +kubebuilder:validation:MaxItems=64
	// +kubebuilder:validation:items:MaxLength=64
	List []string `json:"list,omitempty"`
}

// OnPremAIGatewayWatchNamespacesType indicates the type of namespace watching to
// be done.
//
// +kubebuilder:validation:Enum=all;list;own
type OnPremAIGatewayWatchNamespacesType string

const (
	// OnPremAIGatewayWatchNamespacesTypeAll indicates that all namespaces should
	// be watched for resources.
	OnPremAIGatewayWatchNamespacesTypeAll OnPremAIGatewayWatchNamespacesType = "all"

	// OnPremAIGatewayWatchNamespacesTypeList indicates that only the namespaces
	// listed in the List field should be watched for resources.
	// All the namespaces enumerated in the list are watched in addition to the
	// namespace of the OnPremAIGateway.
	OnPremAIGatewayWatchNamespacesTypeList OnPremAIGatewayWatchNamespacesType = "list"

	// OnPremAIGatewayWatchNamespacesTypeOwn indicates that only the namespace of
	// the OnPremAIGateway should be watched for resources.
	OnPremAIGatewayWatchNamespacesTypeOwn OnPremAIGatewayWatchNamespacesType = "own"
)
