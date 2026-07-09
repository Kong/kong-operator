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

package consts

// -----------------------------------------------------------------------------
// Consts - AIGateway DataPlane Labels
// -----------------------------------------------------------------------------

const (
	// AIGatewayDataPlaneManagedByLabelValue is the managed-by label value for AI Gateway DataPlane owned resources.
	// Distinct from the gateway-operator DataPlaneManagedLabelValue ("dataplane") to avoid cross-query
	// collisions when listing Secrets by label selector.
	AIGatewayDataPlaneManagedByLabelValue = "aigateway-dataplane"

	// AIGatewayDataPlanePrefix is the GenerateName prefix used when creating mTLS certificate Secrets
	// for AI Gateway DataPlane instances.
	AIGatewayDataPlanePrefix = "aigw-dp"

	// SecretAIGatewayDataPlaneCertificateLabel marks a Secret as the mTLS certificate for an AI Gateway DataPlane.
	SecretAIGatewayDataPlaneCertificateLabel = "konghq.com/aigw-dp-cert" //nolint:gosec
)

// -----------------------------------------------------------------------------
// Consts - AIGateway DataPlane Container Parameters
// -----------------------------------------------------------------------------

const (
	// AIGatewayContainerName is the name of the AI Gateway container in the DataPlane Deployment.
	AIGatewayContainerName = "aigw"

	// RelatedImageAIGatewayEnvVar is the environment variable name for the AI Gateway container image,
	// following the operator-framework convention for related images.
	RelatedImageAIGatewayEnvVar = "RELATED_IMAGE_AIGW"

	// DefaultAIGatewayBaseImage is the base image name for the AI Gateway container.
	DefaultAIGatewayBaseImage = "kong/kong-ai-gateway"
	// DefaultAIGatewayTag is the default image tag for the AI Gateway container.
	DefaultAIGatewayTag = "2.0.0"
	// DefaultAIGatewayImage is the full default image reference for the AI Gateway container.
	DefaultAIGatewayImage = DefaultAIGatewayBaseImage + ":" + DefaultAIGatewayTag
)
