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

import (
	"fmt"

	"github.com/kong/kong-operator/v2/api/common/consts"
)

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
	// CertificateSecretRefNotFoundReason indicates the manually-referenced
	// certificate Secret does not exist (or isn't visible to the operator,
	// e.g. due to a missing secret-label-selector label).
	CertificateSecretRefNotFoundReason consts.ConditionReason = "SecretRefNotFound" //nolint:gosec
	// CertificateSecretInvalidReason indicates the manually-referenced Secret
	// does not contain a valid tls.crt/tls.key pair.
	CertificateSecretInvalidReason consts.ConditionReason = "InvalidSecret"
	// CertificateControlPlaneRefMissingReason indicates spec.certificateSecret
	// was configured but spec.controlPlaneRef is unset, so there's no
	// KonnectAIGateway to ever use the certificate against.
	CertificateControlPlaneRefMissingReason consts.ConditionReason = "ControlPlaneRefMissing"
)

// CertificateSecretRefNotFoundMessage formats the message used when the
// manually-referenced certificate Secret cannot be found.
func CertificateSecretRefNotFoundMessage(name string) string {
	return fmt.Sprintf(
		"Referenced certificate Secret %q not found (it must exist and carry the operator's secret-label-selector label to be visible)",
		name,
	)
}

// CertificateSecretInvalidMessage is the message used when the
// manually-referenced certificate Secret does not contain a valid TLS
// certificate and key.
const CertificateSecretInvalidMessage = "Referenced certificate Secret does not contain a valid tls.crt/tls.key pair"

// CertificateControlPlaneRefMissingMessage is the message used when
// spec.certificateSecret is configured but spec.controlPlaneRef is unset.
const CertificateControlPlaneRefMissingMessage = "certificateSecret is configured but controlPlaneRef is unset: there is no control plane to use the certificate against"

// -----------------------------------------------------------------------------
// DataPlane - KonnectAIGateway (controlplane) Resolved Condition Constants
// -----------------------------------------------------------------------------

const (
	// KonnectAIGatewayResolvedType indicates whether the referenced
	// KonnectAIGateway has been resolved and is Programmed.
	KonnectAIGatewayResolvedType consts.ConditionType = "KonnectAIGatewayResolved"

	// KonnectAIGatewayResolvedReason indicates the KonnectAIGateway has
	// been resolved successfully.
	KonnectAIGatewayResolvedReason consts.ConditionReason = "Resolved"
	// KonnectAIGatewayNotFoundReason indicates the referenced
	// KonnectAIGateway was not found.
	KonnectAIGatewayNotFoundReason consts.ConditionReason = "NotFound"
	// KonnectAIGatewayNotProgrammedReason indicates the referenced
	// KonnectAIGateway exists but is not yet Programmed on Konnect.
	KonnectAIGatewayNotProgrammedReason consts.ConditionReason = "NotProgrammed"
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

	// KonnectAIGatewayNotFoundMessage indicates the referenced
	// KonnectAIGateway was not found.
	KonnectAIGatewayNotFoundMessage = "Referenced KonnectAIGateway not found"
	// KonnectAIGatewayNotProgrammedMessage indicates the referenced
	// KonnectAIGateway is not yet Programmed.
	KonnectAIGatewayNotProgrammedMessage = "Referenced KonnectAIGateway is not yet Programmed on Konnect"
	// KonnectAIGatewayResolvedMessage indicates the KonnectAIGateway has
	// been resolved.
	KonnectAIGatewayResolvedMessage = "Referenced KonnectAIGateway is resolved and Programmed"
)

// -----------------------------------------------------------------------------
// DataPlane - Service Ready Condition Constants
// -----------------------------------------------------------------------------

const (
	// ServiceReadyType indicates whether the ingress Service is ready.
	// For LoadBalancer-type Services, ready means an external address has been allocated.
	// For other Service types, it is always considered ready.
	ServiceReadyType consts.ConditionType = "ServiceReady"

	// ServiceReadyReason indicates the ingress Service is ready.
	ServiceReadyReason consts.ConditionReason = "ServiceReady"
	// WaitingForAddressReason indicates the ingress Service exists but has not
	// yet been assigned an external address by the cloud load-balancer controller.
	WaitingForAddressReason consts.ConditionReason = "WaitingForAddress"
)

const (
	// ServiceReadyMessage indicates the ingress Service is ready.
	ServiceReadyMessage = "Ingress Service is ready"
	// WaitingForAddressMessage indicates the Service is waiting for an external address.
	WaitingForAddressMessage = "Waiting for ingress Service external address to be allocated"
)

// -----------------------------------------------------------------------------
// DataPlane - KonnectCertificate Registration Condition Constants
// -----------------------------------------------------------------------------

const (
	// KonnectCertificateRegisteredType indicates whether the
	// AIGatewayDataPlaneCertificate resource has been ensured for the DataPlane.
	KonnectCertificateRegisteredType consts.ConditionType = "KonnectCertificateRegistered"

	// KonnectCertificateRegisteredReason indicates the certificate resource was
	// successfully created or is already up-to-date.
	KonnectCertificateRegisteredReason consts.ConditionReason = "KonnectCertificateRegistered"
	// KonnectCertificateRegistrationFailedReason indicates the certificate resource
	// could not be ensured.
	KonnectCertificateRegistrationFailedReason consts.ConditionReason = "KonnectCertificateRegistrationFailed"
	// KonnectCertificateNotProgrammedReason indicates the
	// AIGatewayDataPlaneCertificate exists but has not yet been programmed
	// on Konnect by the Konnect controller.
	KonnectCertificateNotProgrammedReason consts.ConditionReason = "KonnectCertificateNotProgrammed"
)
