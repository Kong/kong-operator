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
	// CertificateSecretRefOperatorManagedReason indicates the manually-referenced
	// Secret is an operator-provisioned (Automatic) certificate Secret. Such a
	// Secret is owned and garbage-collected by the operator, so it can't be
	// used as a user-owned Manual certificate.
	CertificateSecretRefOperatorManagedReason consts.ConditionReason = "SecretRefOperatorManaged" //nolint:gosec
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

// CertificateSecretRefOperatorManagedMessage is the message used when the
// manually-referenced certificate Secret is an operator-provisioned one.
const CertificateSecretRefOperatorManagedMessage = "Referenced certificate Secret is provisioned and managed by the operator; Manual provisioning requires a user-owned Secret"

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
// DataPlane - Service Ready Condition Constants
// -----------------------------------------------------------------------------

const (
	// ServiceReadyType indicates whether the Kafka Service is ready.
	// For LoadBalancer-type Services, ready means an external address has been allocated.
	// For other Service types, it is always considered ready.
	ServiceReadyType consts.ConditionType = "ServiceReady"

	// ServiceReadyReason indicates the Kafka Service is ready.
	ServiceReadyReason consts.ConditionReason = "ServiceReady"
	// WaitingForAddressReason indicates the Kafka Service exists but has not
	// yet been assigned an external address by the cloud load-balancer controller.
	WaitingForAddressReason consts.ConditionReason = "WaitingForAddress"
)

const (
	// ServiceReadyMessage indicates the Kafka Service is ready.
	ServiceReadyMessage = "Kafka Service is ready"
	// WaitingForAddressMessage indicates the Service is waiting for an external address.
	WaitingForAddressMessage = "Waiting for Kafka Service external address to be allocated"
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

// -----------------------------------------------------------------------------
// OnPremEventGateway - Condition Constants
// -----------------------------------------------------------------------------

const (
	// OnPremEventGatewayProgrammedType indicates whether the configuration of
	// the OnPremEventGateway has been applied.
	OnPremEventGatewayProgrammedType consts.ConditionType = "Programmed"

	// OnPremEventGatewayConfigurationPushedType indicates whether the current
	// configuration reached every discovered gw-admin pod.
	OnPremEventGatewayConfigurationPushedType consts.ConditionType = "ConfigurationPushed"

	// OnPremEventGatewayProgrammedReason indicates the configuration of the
	// OnPremEventGateway has been pushed to every gw-admin pod serving: those
	// not serving yet get it later.
	OnPremEventGatewayProgrammedReason consts.ConditionReason = "Programmed"
	// OnPremEventGatewayConfigurationPushedReason indicates the configuration
	// of the OnPremEventGateway reached every gw-admin pod serving: those not
	// serving yet get it later.
	OnPremEventGatewayConfigurationPushedReason consts.ConditionReason = "ConfigurationPushed"
	// OnPremEventGatewayInvalidUIDReason indicates the UID of the
	// OnPremEventGateway is not a version 4 UUID, which gw-admin requires as
	// the id of a gateway: it can't be pushed.
	OnPremEventGatewayInvalidUIDReason consts.ConditionReason = "InvalidUID"
	// OnPremEventGatewayConfigurationPushFailedReason indicates a gw-admin pod
	// could not be reached or rejected the configuration of the
	// OnPremEventGateway.
	OnPremEventGatewayConfigurationPushFailedReason consts.ConditionReason = "ConfigurationPushFailed"
	// OnPremEventGatewayGWAdminNotResolvedMessage is the message of the
	// Programmed condition while the GWAdminResolved condition is not True.
	OnPremEventGatewayGWAdminNotResolvedMessage = "The GWAdmin of the gateway is not resolved"
)

// -----------------------------------------------------------------------------
// OnPremEventGateway - GWAdminResolved Condition Constants
// -----------------------------------------------------------------------------

const (
	// OnPremEventGatewayGWAdminResolvedType indicates whether the GWAdmin of
	// the OnPremEventGateway can serve it: the gateway may use it, and it has
	// gw-admin pods.
	OnPremEventGatewayGWAdminResolvedType consts.ConditionType = "GWAdminResolved"

	// OnPremEventGatewayGWAdminResolvedReason indicates the GWAdmin can serve
	// the OnPremEventGateway.
	OnPremEventGatewayGWAdminResolvedReason consts.ConditionReason = "Resolved"
	// OnPremEventGatewayGWAdminNotFoundReason indicates the referenced GWAdmin
	// doesn't exist.
	OnPremEventGatewayGWAdminNotFoundReason consts.ConditionReason = "NotFound"
	// OnPremEventGatewayGWAdminRefNotPermittedReason indicates the referenced
	// GWAdmin is in another namespace and no KongReferenceGrant allows the
	// OnPremEventGateway to use it.
	OnPremEventGatewayGWAdminRefNotPermittedReason consts.ConditionReason = "RefNotPermitted"
	// OnPremEventGatewayGWAdminNotReadyReason indicates the referenced GWAdmin
	// can't serve the OnPremEventGateway yet: it is being deleted, it hasn't
	// published its API or its TLS material, or it has no gw-admin pod.
	OnPremEventGatewayGWAdminNotReadyReason consts.ConditionReason = "NotReady"
)

// -----------------------------------------------------------------------------
// Entities of an OnPremEventGateway - Programmed Condition Constants
// -----------------------------------------------------------------------------

// The reasons of the Programmed condition (OnPremEventGatewayProgrammedType)
// the OnPremEventGateway controller sets on the entities of a gateway.
const (
	// OnPremEntityProgrammedReason indicates the entity is in the
	// configuration of its OnPremEventGateway, pushed to every gw-admin pod
	// serving: those not serving yet get it later.
	OnPremEntityProgrammedReason consts.ConditionReason = "Programmed"
	// OnPremEntityGatewayNotProgrammedReason indicates the entity is in the
	// configuration of its OnPremEventGateway, which is not programmed: its
	// Programmed condition tells why.
	OnPremEntityGatewayNotProgrammedReason consts.ConditionReason = "GatewayNotProgrammed"
	// OnPremEntityInvalidReason indicates the entity is left out of the
	// configuration of its OnPremEventGateway, e.g. it reads a Secret that
	// doesn't exist, or sets a field gw-admin doesn't support.
	OnPremEntityInvalidReason consts.ConditionReason = "Invalid"
	// OnPremEntityDetachedReason indicates the entity was in the
	// configuration of its OnPremEventGateway, and no longer belongs to it,
	// e.g. its parent entity was deleted.
	OnPremEntityDetachedReason consts.ConditionReason = "Detached"
	// OnPremEntityRefNotPermittedReason indicates the entity is left out of
	// the configuration of its OnPremEventGateway: it reads a Secret in
	// another namespace, and no KongReferenceGrant allows it.
	OnPremEntityRefNotPermittedReason consts.ConditionReason = "RefNotPermitted"
)
