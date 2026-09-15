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
	// Name is the name of the referenced control plane in the DataPlane's
	// namespace. It is empty when the DataPlane has no control plane reference
	// configured.
	Name string
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
}

// IsConfigured reports whether the DataPlane has a control plane reference
// configured and resolved.
func (cp ResolvedControlPlane) IsConfigured() bool {
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
	// NotProgrammedReason is the reason used when the control plane is not yet Programmed.
	NotProgrammedReason string
	// NotProgrammedMessage is the message used when the control plane is not yet Programmed.
	NotProgrammedMessage string
}

// ControlPlaneKindConfig describes one kind of control plane that a DataPlane
// can reference. A DataPlane kind lists one entry per supported control plane
// kind in Config.ControlPlanes; each entry gets its own watch and index field.
type ControlPlaneKindConfig struct {
	// Kind is the human-readable control plane kind used in logs, errors and
	// events (e.g. "KonnectAIGateway", "OnPremAIGateway").
	Kind string
	// NewObject returns a new empty control plane object of this kind.
	NewObject func() ControlPlaneObject
	// ControlPlaneRefIndexField is the field index used to list DataPlanes by
	// their reference to this kind of control plane.
	ControlPlaneRefIndexField string
	// IsKonnect reports whether this control plane kind is Konnect-backed.
	// Konnect-backed control planes are checked for the Konnect Programmed
	// condition during resolution and trigger Konnect certificate automation.
	IsKonnect bool
	// Conditions carries the control plane resolution condition types, reasons
	// and messages for this kind.
	Conditions ControlPlaneConditions
}
