/*
Copyright 2025 Kong, Inc.

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

package consts

// -----------------------------------------------------------------------------
// Consts - DataPlane Labels
// -----------------------------------------------------------------------------

const (
	// DataPlaneManagedByLabelValue indicates that an object's lifecycle is managed
	// by the eventgatewaydataplane controller.
	DataPlaneManagedByLabelValue = "dataplane"

	// KEGDataPlaneManagedByLabelValue is the managed-by label value for KEG DataPlane owned resources.
	// Distinct from the gateway-operator DataPlaneManagedLabelValue ("dataplane") to avoid cross-query
	// collisions when listing Secrets by label selector.
	KEGDataPlaneManagedByLabelValue = "keg-dataplane"

	// KEGDataPlanePrefix is the GenerateName prefix used when creating mTLS certificate Secrets
	// for KEG DataPlane instances.
	KEGDataPlanePrefix = "keg-dp"

	// SecretKEGDataPlaneCertificateLabel marks a Secret as the mTLS certificate for a KEG DataPlane.
	SecretKEGDataPlaneCertificateLabel = "konghq.com/keg-dp-cert" //nolint:gosec
)

// -----------------------------------------------------------------------------
// Consts - DataPlane Container Parameters
// -----------------------------------------------------------------------------

const (
	// KEGContainerName is the name of the KEG container in the DataPlane Deployment.
	KEGContainerName = "keg"

	// RelatedImageKEGEnvVar is the environment variable name for the KEG container image,
	// following the operator-framework convention for related images.
	RelatedImageKEGEnvVar = "RELATED_IMAGE_KEG"

	// DefaultKEGBaseImage is the base image name for the KEG container.
	DefaultKEGBaseImage = "kong/kong-event-gateway"
	// DefaultKEGTag is the default image tag for the KEG container.
	DefaultKEGTag = "1.1.0"
	// DefaultKEGImage is the full default image reference for the KEG container.
	DefaultKEGImage = DefaultKEGBaseImage + ":" + DefaultKEGTag
)
