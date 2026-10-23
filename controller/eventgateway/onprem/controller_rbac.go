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

package onprem

// -----------------------------------------------------------------------------
// OnPremEventGateway Reconciler - RBAC
// -----------------------------------------------------------------------------

// +kubebuilder:rbac:groups=eventgateway.konghq.com,resources=onpremeventgateways,verbs=get;list;watch
// +kubebuilder:rbac:groups=eventgateway.konghq.com,resources=onpremeventgateways/status,verbs=update;patch

// The GWAdmin of a gateway, the KongReferenceGrants allowing a gateway to use
// a GWAdmin in another namespace, and the endpoints of its API Service.
// +kubebuilder:rbac:groups=gateway-operator.konghq.com,resources=gwadmins,verbs=get;list;watch
// +kubebuilder:rbac:groups=configuration.konghq.com,resources=kongreferencegrants,verbs=get;list;watch
// +kubebuilder:rbac:groups=discovery.k8s.io,resources=endpointslices,verbs=get;list;watch

// The RBAC to read the entities that can belong to an OnPremEventGateway is in
// zz_generated_entities.go.
