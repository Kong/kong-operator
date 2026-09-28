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

package onpremconfig

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
)

// aiGatewayRefGetter is implemented by every AI Gateway configuration entity.
type aiGatewayRefGetter interface {
	GetAIGatewayRef() aiconfigurationv1alpha1.AIGatewayRef
}

// OnPremAIGatewayTargetedPredicate filters out events for entities whose
// AIGatewayRef does not target an OnPremAIGateway: entities referencing a
// KonnectAIGateway are owned by the Konnect reconciler, and repointing between
// the two kinds is forbidden by CEL validation, so an entity never changes its
// target kind.
type OnPremAIGatewayTargetedPredicate struct{}

// CreateFunc determines whether a create event should trigger reconciliation.
func (p OnPremAIGatewayTargetedPredicate) Create(e event.CreateEvent) bool {
	return targetsOnPremAIGateway(e.Object)
}

// UpdateFunc determines whether an update event should trigger reconciliation.
func (p OnPremAIGatewayTargetedPredicate) Update(e event.UpdateEvent) bool {
	return targetsOnPremAIGateway(e.ObjectNew)
}

// DeleteFunc determines whether a delete event should trigger reconciliation.
func (p OnPremAIGatewayTargetedPredicate) Delete(e event.DeleteEvent) bool {
	return targetsOnPremAIGateway(e.Object)
}

// GenericFunc determines whether a generic event should trigger reconciliation.
func (p OnPremAIGatewayTargetedPredicate) Generic(e event.TypedGenericEvent[client.Object]) bool {
	return false
}

func targetsOnPremAIGateway(object client.Object) bool {
	ent, ok := object.(aiGatewayRefGetter)
	if !ok {
		return false
	}
	return ent.GetAIGatewayRef().TargetsOnPremAIGateway()
}
