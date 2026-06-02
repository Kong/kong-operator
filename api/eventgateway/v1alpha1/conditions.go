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

import "github.com/kong/kong-operator/v2/api/common/consts"

// -----------------------------------------------------------------------------
// DataPlane - Ready Condition Constants
// -----------------------------------------------------------------------------

const (
	// ReadyType indicates if the DataPlane has all dependent
	// conditions Ready.
	ReadyType consts.ConditionType = "Ready"

	// ResourceReadyReason indicates the resource is ready.
	ResourceReadyReason consts.ConditionReason = "Ready"
	// DependenciesNotReadyReason indicates other conditions are not true.
	DependenciesNotReadyReason consts.ConditionReason = "DependenciesNotReady"
	// WaitingToBecomeReadyReason is a generic reason for dependent resources
	// waiting to be ready.
	WaitingToBecomeReadyReason consts.ConditionReason = "WaitingToBecomeReady"
	// ResourceCreatedOrUpdatedReason is a generic reason for missing or
	// outdated resources.
	ResourceCreatedOrUpdatedReason consts.ConditionReason = "ResourceCreatedOrUpdated"
	// UnableToProvisionReason is a generic reason for unexpected errors.
	UnableToProvisionReason consts.ConditionReason = "UnableToProvision"
)

// -----------------------------------------------------------------------------
// DataPlane - Certificate Condition Constants
// -----------------------------------------------------------------------------

const (
	// CertificateProvisionedType indicates whether the mTLS certificate Secret
	// has been provisioned for the DataPlane.
	CertificateProvisionedType consts.ConditionType = "CertificateProvisioned"

	// CertificateProvisionedReason indicates the certificate Secret has been provisioned successfully.
	CertificateProvisionedReason consts.ConditionReason = "CertificateProvisioned"
	// CertificateProvisioningReason indicates the certificate Secret is being provisioned.
	CertificateProvisioningReason consts.ConditionReason = "CertificateProvisioning"
)

// -----------------------------------------------------------------------------
// DataPlane - KonnectEventGateway Resolved Condition Constants
// -----------------------------------------------------------------------------

const (
	// KonnectEventGatewayResolvedType indicates whether the referenced
	// KonnectEventGateway has been resolved and is Programmed.
	KonnectEventGatewayResolvedType consts.ConditionType = "KonnectEventGatewayResolved"

	// KonnectEventGatewayResolvedReason indicates the KonnectEventGateway has
	// been resolved successfully.
	KonnectEventGatewayResolvedReason consts.ConditionReason = "Resolved"
	// KonnectEventGatewayNotFoundReason indicates the referenced
	// KonnectEventGateway was not found.
	KonnectEventGatewayNotFoundReason consts.ConditionReason = "NotFound"
	// KonnectEventGatewayNotProgrammedReason indicates the referenced
	// KonnectEventGateway exists but is not yet Programmed on Konnect.
	KonnectEventGatewayNotProgrammedReason consts.ConditionReason = "NotProgrammed"
)

// -----------------------------------------------------------------------------
// DataPlane - Condition Messages
// -----------------------------------------------------------------------------

const (
	// DependenciesNotReadyMessage indicates other conditions are not yet ready.
	DependenciesNotReadyMessage = "There are other conditions that are not yet ready"
	// WaitingToBecomeReadyMessage indicates the target resource is not ready.
	WaitingToBecomeReadyMessage = "Waiting for the resource to become ready"
	// ResourceCreatedMessage indicates a missing resource was provisioned.
	ResourceCreatedMessage = "Resource has been created"
	// ResourceUpdatedMessage indicates a resource was updated.
	ResourceUpdatedMessage = "Resource has been updated"

	// KonnectEventGatewayNotFoundMessage indicates the referenced
	// KonnectEventGateway was not found.
	KonnectEventGatewayNotFoundMessage = "Referenced KonnectEventGateway not found"
	// KonnectEventGatewayNotProgrammedMessage indicates the referenced
	// KonnectEventGateway is not yet Programmed.
	KonnectEventGatewayNotProgrammedMessage = "Referenced KonnectEventGateway is not yet Programmed on Konnect"
	// KonnectEventGatewayResolvedMessage indicates the KonnectEventGateway has
	// been resolved.
	KonnectEventGatewayResolvedMessage = "Referenced KonnectEventGateway is resolved and Programmed"
)

// -----------------------------------------------------------------------------
// DataPlane - KonnectCertificate Registration Condition Constants
// -----------------------------------------------------------------------------

const (
	// KonnectCertificateRegisteredType indicates whether the
	// EventGatewayDataPlaneCertificate resource has been ensured for the DataPlane.
	KonnectCertificateRegisteredType consts.ConditionType = "KonnectCertificateRegistered"

	// KonnectCertificateRegisteredReason indicates the certificate resource was
	// successfully created or is already up-to-date.
	KonnectCertificateRegisteredReason consts.ConditionReason = "KonnectCertificateRegistered"
	// KonnectCertificateRegistrationFailedReason indicates the certificate resource
	// could not be ensured.
	KonnectCertificateRegistrationFailedReason consts.ConditionReason = "KonnectCertificateRegistrationFailed"
	// KonnectCertificateNotProgrammedReason indicates the
	// EventGatewayDataPlaneCertificate exists but has not yet been programmed
	// on Konnect by the Konnect controller.
	KonnectCertificateNotProgrammedReason consts.ConditionReason = "KonnectCertificateNotProgrammed"
)
