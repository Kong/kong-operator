/*
Copyright 2025 Kong, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package generic

import (
	"encoding/json"
	"fmt"
	"sort"

	"sigs.k8s.io/controller-runtime/pkg/client"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
)

// BackendClusterEntry wraps EventGatewayBackendClusterAPISpec inline and
// adds the cluster's Kubernetes UID as the admin API entity id.
type BackendClusterEntry struct {
	ID                                                string `json:"id"`
	konnectv1alpha1.EventGatewayBackendClusterAPISpec `json:",inline"`
}

// ListenerEntry wraps EventGatewayListenerAPISpec inline, adds the listener's
// Kubernetes UID as the admin API entity id, and embeds any
// EventGatewayListenerPolicy entries that target it.
type ListenerEntry struct {
	ID                                         string                `json:"id"`
	konnectv1alpha1.EventGatewayListenerAPISpec `json:",inline"`
	Policies                                   []ListenerPolicyEntry `json:"policies,omitempty"`
}

// VirtualClusterEntry wraps EventGatewayVirtualClusterAPISpec inline and adds
// the virtual cluster's Kubernetes UID as the admin API entity id.
type VirtualClusterEntry struct {
	ID                                                string `json:"id"`
	konnectv1alpha1.EventGatewayVirtualClusterAPISpec `json:",inline"`
}

// ListenerPolicyEntry wraps EventGatewayListenerPolicyAPISpec inline, adds
// the policy's Kubernetes UID as the admin API entity id, and carries a
// zero-based position that determines the policy's order in the execution
// chain. Position is required by the admin API.
type ListenerPolicyEntry struct {
	ID                                                string `json:"id"`
	Position                                          int    `json:"position"`
	konnectv1alpha1.EventGatewayListenerPolicyAPISpec `json:",inline"`
}

// MarshalJSON serializes the embedded APISpec via its own (union-aware)
// marshaler and then injects id and position. Without this, Go promotes
// EventGatewayListenerPolicyConfig.MarshalJSON onto ListenerPolicyEntry and
// silently drops the outer ID and Position fields.
func (e ListenerPolicyEntry) MarshalJSON() ([]byte, error) {
	inner, err := json.Marshal(e.EventGatewayListenerPolicyAPISpec)
	if err != nil {
		return nil, fmt.Errorf("marshal listener policy api spec: %w", err)
	}
	m := map[string]json.RawMessage{}
	if len(inner) > 0 && string(inner) != "null" {
		if err := json.Unmarshal(inner, &m); err != nil {
			return nil, fmt.Errorf("decode listener policy api spec: %w", err)
		}
	}
	idBytes, err := json.Marshal(e.ID)
	if err != nil {
		return nil, err
	}
	posBytes, err := json.Marshal(e.Position)
	if err != nil {
		return nil, err
	}
	m["id"] = idBytes
	m["position"] = posBytes
	return json.Marshal(m)
}

// EventGatewaySnapshot is the materialised view of cached event-gateway
// children, grouped by kind and reduced to their API spec plus the parent
// object's UID. Listener policies are embedded inside their parent listener.
type EventGatewaySnapshot struct {
	BackendClusters []BackendClusterEntry `json:"backend_clusters"`
	Listeners       []ListenerEntry       `json:"listeners"`
	VirtualClusters []VirtualClusterEntry `json:"virtual_clusters"`
}

// buildEventGatewaySnapshot walks items and groups specs by kind, then nests
// listener policies under the listener they reference. Policies are sorted by
// their Kubernetes name within each listener so the assigned position is
// stable across snapshot rebuilds. Unknown kinds and policies whose target
// listener is not in the bucket are ignored.
func buildEventGatewaySnapshot(items map[cacheKey]client.Object) EventGatewaySnapshot {
	var s EventGatewaySnapshot
	listenerByName := map[string]int{}
	type pendingPolicy struct {
		listenerName string
		policyName   string
		entry        ListenerPolicyEntry
	}
	var pending []pendingPolicy

	for _, obj := range items {
		switch o := obj.(type) {
		case *konnectv1alpha1.EventGatewayBackendCluster:
			s.BackendClusters = append(s.BackendClusters, BackendClusterEntry{
				ID:                                string(o.UID),
				EventGatewayBackendClusterAPISpec: o.Spec.APISpec,
			})
		case *konnectv1alpha1.EventGatewayListener:
			listenerByName[o.Name] = len(s.Listeners)
			s.Listeners = append(s.Listeners, ListenerEntry{
				ID:                          string(o.UID),
				EventGatewayListenerAPISpec: o.Spec.APISpec,
			})
		case *konnectv1alpha1.EventGatewayVirtualCluster:
			s.VirtualClusters = append(s.VirtualClusters, VirtualClusterEntry{
				ID:                                string(o.UID),
				EventGatewayVirtualClusterAPISpec: o.Spec.APISpec,
			})
		case *konnectv1alpha1.EventGatewayListenerPolicy:
			ref := o.GetEventGatewayListenerRef()
			if ref.NamespacedRef == nil {
				continue
			}
			pending = append(pending, pendingPolicy{
				listenerName: ref.NamespacedRef.Name,
				policyName:   o.Name,
				entry: ListenerPolicyEntry{
					ID:                                string(o.UID),
					EventGatewayListenerPolicyAPISpec: o.Spec.APISpec,
				},
			})
		}
	}

	sort.SliceStable(pending, func(i, j int) bool {
		if pending[i].listenerName != pending[j].listenerName {
			return pending[i].listenerName < pending[j].listenerName
		}
		return pending[i].policyName < pending[j].policyName
	})

	for _, p := range pending {
		idx, ok := listenerByName[p.listenerName]
		if !ok {
			continue
		}
		p.entry.Position = len(s.Listeners[idx].Policies)
		s.Listeners[idx].Policies = append(s.Listeners[idx].Policies, p.entry)
	}

	return s
}
