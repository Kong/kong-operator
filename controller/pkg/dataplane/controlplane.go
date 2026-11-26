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

package dataplane

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ControlPlaneObject is the constraint for the control plane types referenced
// by the DataPlane (e.g. KonnectAIGateway, KonnectEventGateway,
// OnPremAIGateway).
type ControlPlaneObject interface {
	client.Object
	GetConditions() []metav1.Condition
}

// ControlPlaneRef identifies the control plane referenced by a DataPlane.
type ControlPlaneRef struct {
	// Kind is the human-readable kind of the referenced control plane. It must
	// match the Kind of one of the Config's ControlPlanes entries.
	Kind string
	// Name is the name of the referenced control plane. It is empty when the
	// DataPlane has no control plane reference configured.
	Name string
	// Namespace is the namespace of the referenced control plane, the
	// DataPlane's when empty. Another namespace needs a KongReferenceGrant.
	Namespace string
}

// ResolvedControlPlane carries the control plane resolved for a DataPlane
// through the reconcile flow. It abstracts over the concrete control plane
// kinds a DataPlane can reference (e.g. KonnectAIGateway, OnPremAIGateway for
// AIGatewayDataPlane); specialized controllers type-assert Object to the
// concrete type they support.
type ResolvedControlPlane struct {
	// Kind is the human-readable kind of the resolved control plane
	// (e.g. "KonnectAIGateway", "OnPremAIGateway").
	Kind string
	// IsKonnect reports whether the control plane is a Konnect-backed entity.
	// Only Konnect-backed control planes are checked for the Konnect Programmed
	// condition during resolution and trigger Konnect certificate automation.
	IsKonnect bool
	// Object is the resolved control plane object. It is nil when the DataPlane
	// has no control plane reference configured.
	Object ControlPlaneObject
	// Dependencies is what ControlPlaneKindConfig.Dependencies read for the
	// control plane, or nil.
	Dependencies any
	// CertificateRegistration is the ControlPlaneKindConfig.CertificateRegistration
	// of the kind of the control plane.
	CertificateRegistration *CertificateRegistrationConditions
}

// RegistersCertificate reports whether the client certificate of the DataPlane
// is registered on the control plane through a certificate object.
func (cp ResolvedControlPlane) RegistersCertificate() bool {
	return cp.IsKonnect || cp.CertificateRegistration != nil
}

// CertificateRegistrationConditions carries the condition type and reasons a
// non-Konnect control plane kind reports the registration of the client
// certificate of the DataPlane with, and the name of what it is registered on
// for the messages. Konnect-backed kinds use the Konnect certificate
// conditions of Config.Conditions.
type CertificateRegistrationConditions struct {
	// Type is the type of the condition.
	Type string
	// RegisteredReason is the reason used once the certificate is Programmed.
	RegisteredReason string
	// RegistrationFailedReason is the reason used when the certificate object
	// can't be ensured.
	RegistrationFailedReason string
	// NotProgrammedReason is the reason used while the certificate object is
	// not Programmed.
	NotProgrammedReason string
	// Target names what the certificate is registered on, e.g. "gw-admin".
	Target string
	// NotProgrammedHint, when set, is appended as is, so with its own leading
	// space, to the message used while the certificate object has no
	// Programmed condition, e.g. to name what programs it.
	NotProgrammedHint string
	// ReportProgrammedCondition, when set, adds the reason and message of the
	// Programmed condition of the certificate object, when it has one, to the
	// message used while it is not Programmed.
	ReportProgrammedCondition bool
	// CreatedEventReason and CreatedEventAction are the reason and action of
	// the event recorded on the DataPlane when the certificate object is
	// created.
	CreatedEventReason string
	CreatedEventAction string
	// UpdatedEventReason and UpdatedEventAction are the reason and action of
	// the event recorded on the DataPlane when the certificate object is
	// updated.
	UpdatedEventReason string
	UpdatedEventAction string
}

// DependenciesNotReadyError is returned by ControlPlaneKindConfig.Dependencies
// when something the DataPlane needs from the control plane is not available
// yet. The resolution condition is set to False with the NotReadyReason of
// the kind and the message of the error, and the reconcile is not retried: a
// watch must trigger it again.
type DependenciesNotReadyError struct {
	Message string
	// ControlPlaneGone tells the control plane won't be resolved again, e.g.
	// it is being deleted: the DataPlane is then released from it (see
	// DeploymentConfig.ControlPlaneGone), like when it doesn't exist.
	ControlPlaneGone bool
}

func (e *DependenciesNotReadyError) Error() string { return e.Message }

// IsResolved reports whether the DataPlane has a control plane reference resolved.
func (cp ResolvedControlPlane) IsResolved() bool {
	return cp.Object != nil
}

// ControlPlaneConditions carries the condition types, reasons and messages
// used when resolving a control plane of a given kind. Values are supplied by
// each specialized controller from its API package constants.
type ControlPlaneConditions struct {
	// ResolvedType is the type of the control plane resolution condition.
	ResolvedType string
	// ResolvedReason is the reason used when the control plane has been resolved.
	ResolvedReason string
	// ResolvedMessage is the message used when the control plane has been resolved.
	ResolvedMessage string
	// NotFoundReason is the reason used when the control plane was not found.
	NotFoundReason string
	// NotFoundMessage is the message used when the control plane was not found.
	NotFoundMessage string
	// NotPermittedReason is the reason used when the control plane is in
	// another namespace and no KongReferenceGrant allows the reference. Only
	// used by a kind whose references can name a namespace.
	NotPermittedReason string
	// NotProgrammedReason is the reason used when the control plane is not yet Programmed.
	NotProgrammedReason string
	// NotProgrammedMessage is the message used when the control plane is not yet Programmed.
	NotProgrammedMessage string
	// NotReadyReason is the reason used when a non-Konnect control plane is not yet Ready.
	// Only used when the kind config sets ReadinessConditionType.
	NotReadyReason string
	// NotReadyMessage is the message used when a non-Konnect control plane is not yet Ready.
	// Only used when the kind config sets ReadinessConditionType.
	NotReadyMessage string
	// EndpointMismatchReason is the reason used when the control plane returns
	// endpoints its serverURL cannot be used with. The condition message is the
	// error returned by the kind's EndpointCheck, so it names the endpoint.
	// Only used when the kind config sets EndpointCheck.
	EndpointMismatchReason string
}

// ControlPlaneKindConfig describes one kind of control plane that a DataPlane
// can reference. A DataPlane kind lists one entry per supported control plane
// kind in Config.ControlPlanes; each entry gets its own watch and index field.
type ControlPlaneKindConfig struct {
	// Kind is the human-readable control plane kind used in logs, errors and
	// events (e.g. "KonnectAIGateway", "OnPremAIGateway").
	Kind string
	// GVK identifies the control plane kind in a KongReferenceGrant. Only used
	// by a kind whose references can name a namespace.
	GVK metav1.GroupVersionKind
	// NewObject returns a new empty control plane object of this kind.
	NewObject func() ControlPlaneObject
	// ControlPlaneRefIndexField is the field index used to list DataPlanes by
	// their reference to this kind of control plane.
	ControlPlaneRefIndexField string
	// IsKonnect reports whether this control plane kind is Konnect-backed.
	// Konnect-backed control planes are checked for the Konnect Programmed
	// condition during resolution and trigger Konnect certificate automation.
	IsKonnect bool
	// ReadinessConditionType is the condition type on a non-Konnect control
	// plane (e.g. "Ready" for OnPremAIGateway) that must be True for the
	// control plane to be considered resolved. When empty, a non-Konnect
	// control plane is considered resolved as soon as it exists.
	ReadinessConditionType string
	// EndpointCheck reports whether the control plane's endpoints are usable
	// with its serverURL, naming the offending endpoint in the error. dp is
	// the reconciled DataPlane, for kinds whose verdict depends on its spec.
	// A non-nil error holds the DataPlane: it sets Resolved=False with
	// Conditions.EndpointMismatchReason and returns errEndpointsMismatch.
	// Nil for kinds that have no such endpoints.
	EndpointCheck func(dp Object, cp ControlPlaneObject) error
	// Conditions carries the control plane resolution condition types, reasons
	// and messages for this kind.
	Conditions ControlPlaneConditions
	// DisabledMessage, when set, disables the kind, e.g. when its CRDs are
	// not installed: no watch is registered for it, and a DataPlane
	// referencing it is reported in the resolution condition with
	// Conditions.NotFoundReason and this message, and not retried.
	DisabledMessage string
	// Dependencies, when set, reads what the DataPlane needs besides the
	// control plane object once it is resolved, e.g. the trust material of
	// an on-prem control plane. The result is carried in
	// ResolvedControlPlane.Dependencies. A *DependenciesNotReadyError is
	// reported in the resolution condition, with Conditions.NotReadyReason.
	Dependencies func(ctx context.Context, cl client.Client, cp ControlPlaneObject) (any, error)
	// CertificateRegistration, when set, makes the DataPlane register its
	// client certificate on the control plane through a certificate object
	// (Config.Certificate.Build), and wait for it to be Programmed before
	// building the Deployment, as Konnect-backed kinds do. It is meant for
	// non-Konnect kinds: on a Konnect-backed kind it replaces the Konnect
	// certificate conditions of Config.Conditions.
	CertificateRegistration *CertificateRegistrationConditions
}
