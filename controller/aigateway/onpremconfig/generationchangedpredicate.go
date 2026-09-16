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

// Package onpremconfig holds the reconcilers for the AI Gateway configuration
// entities (AIGatewayModel, and its sibling kinds as they are added) that feed
// the on-prem AI Gateway control plane instances with configuration.
package onpremconfig

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// GenerationChangedPredicate is a custom predicate that triggers reconciliation
// for create, update (only if generation changes), and delete events.
// This adds onto the predicate.GenerationChangedPredicate which only triggers for
// update events when the generation changes.
type GenerationChangedPredicate struct{}

// UpdateFunc determines whether an update event should trigger reconciliation.
// It calls predicate.GenerationChangedPredicate.
func (p GenerationChangedPredicate) Update(e event.UpdateEvent) bool {
	return predicate.GenerationChangedPredicate{}.Update(e)
}

// CreateFunc determines whether a create event should trigger reconciliation.
// It always returns true, meaning all create events will trigger reconciliation.
func (p GenerationChangedPredicate) Create(e event.CreateEvent) bool {
	return true
}

// DeleteFunc determines whether a delete event should trigger reconciliation.
// It always returns true, meaning all delete events will trigger reconciliation.
func (p GenerationChangedPredicate) Delete(e event.DeleteEvent) bool {
	return true
}

// GenericFunc determines whether a generic event should trigger reconciliation.
func (p GenerationChangedPredicate) Generic(e event.TypedGenericEvent[client.Object]) bool {
	return false
}
