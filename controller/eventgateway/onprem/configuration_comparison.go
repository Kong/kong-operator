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

package onprem

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"

	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminapi"
)

// sameEntities reports whether gw-admin holds the entities pushed, pushedKey
// being their comparisonKey once withGWAdminDefaults applied: a field left
// out and the default gw-admin sets on it are the same.
func sameEntities(held gwadminapi.GatewayEntitySet, pushedKey []byte) (bool, error) {
	heldKey, err := comparisonKey(held)
	if err != nil {
		return false, fmt.Errorf("failed to encode the entities gw-admin holds: %w", err)
	}
	return bytes.Equal(heldKey, pushedKey), nil
}

// comparisonKey returns the entities as sameEntities compares them: their
// JSON encoding, as gw-admin receives them, as the entities hold times, on
// whose unexported fields equality.Semantic.DeepEqual panics. Each list is
// sorted by id, as its order doesn't matter, and a missing list is empty.
// The encoding goes through a generic value, which sorts the keys of every
// object: an entity holding a [json.RawMessage], like a schema registry or a
// union such as the destination of a virtual cluster or a policy, keeps its
// nested fields in the order gw-admin sends them, while editedJSON sorts
// those it edits.
func comparisonKey(e gwadminapi.GatewayEntitySet) ([]byte, error) {
	v, err := jsonValue(withEmptyLists(sortedByID(withoutDestinationNames(e))))
	if err != nil {
		return nil, err
	}
	return json.Marshal(v)
}

// withoutDestinationNames returns the entities without the name of each
// destination that names its target by id: the backend cluster of a virtual
// cluster, or the virtual cluster a listener policy forwards to. The id tells
// the target, and gw-admin returns the name either empty or resolved from the
// id, depending on its version.
func withoutDestinationNames(e gwadminapi.GatewayEntitySet) gwadminapi.GatewayEntitySet {
	withoutName := func(destination map[string]any) {
		if _, ok := destination["id"]; ok {
			delete(destination, "name")
		}
	}
	e.VirtualClusters = slices.Clone(e.VirtualClusters)
	for i := range e.VirtualClusters {
		e.VirtualClusters[i].Destination = editedJSON(e.VirtualClusters[i].Destination, withoutName)
	}
	e.Listeners = slices.Clone(e.Listeners)
	for i := range e.Listeners {
		l := &e.Listeners[i]
		policies := make([]gwadminapi.EventGatewayListenerPolicyOnPrem, 0, len(l.Policies))
		for _, p := range l.Policies {
			policies = append(policies, editedJSON(p, func(fields map[string]any) {
				if config, ok := fields["config"].(map[string]any); ok {
					if destination, ok := config["destination"].(map[string]any); ok {
						withoutName(destination)
					}
				}
			}))
		}
		if l.Policies != nil {
			l.Policies = policies
		}
	}
	return e
}

// sortedByID returns the entities with each list sorted by id, the policies
// of a virtual cluster and of a listener included: gw-admin returns a child
// policy right after its parent.
func sortedByID(e gwadminapi.GatewayEntitySet) gwadminapi.GatewayEntitySet {
	e.BackendClusters = byID(e.BackendClusters, func(bc gwadminapi.EventGatewayBackendClusterOnPrem) gwadminapi.UUIDv4 { return bc.Id })
	e.TlsTrustBundles = byID(e.TlsTrustBundles, func(b gwadminapi.EventGatewayTLSTrustBundleOnPrem) gwadminapi.UUIDv4 { return b.Id })
	e.SchemaRegistries = byID(e.SchemaRegistries, func(r gwadminapi.EventGatewaySchemaRegistryOnPrem) gwadminapi.UUIDv4 { return r.Id })
	e.StaticKeys = byID(e.StaticKeys, func(k gwadminapi.EventGatewayStaticKeyOnPrem) gwadminapi.UUIDv4 { return k.Id })
	e.VirtualClusters = byID(e.VirtualClusters, func(vc gwadminapi.EventGatewayVirtualClusterOnPrem) gwadminapi.UUIDv4 { return vc.Id })
	for i := range e.VirtualClusters {
		vc := &e.VirtualClusters[i]
		vc.ClusterPolicies = byID(vc.ClusterPolicies, func(p gwadminapi.EventGatewayClusterPolicyOnPrem) gwadminapi.UUIDv4 { return p.Id })
		vc.ConsumePolicies = byID(vc.ConsumePolicies, func(p gwadminapi.EventGatewayConsumePolicyOnPrem) gwadminapi.UUIDv4 { return p.Id })
		vc.ProducePolicies = byID(vc.ProducePolicies, func(p gwadminapi.EventGatewayProducePolicyOnPrem) gwadminapi.UUIDv4 { return p.Id })
	}
	e.Listeners = byID(e.Listeners, func(l gwadminapi.EventGatewayListenerOnPrem) gwadminapi.UUIDv4 { return l.Id })
	for i := range e.Listeners {
		l := &e.Listeners[i]
		l.Policies = byID(l.Policies, func(p gwadminapi.EventGatewayListenerPolicyOnPrem) gwadminapi.UUIDv4 { return p.Id })
	}
	return e
}

// byID returns a copy of the entities sorted by id.
func byID[E any](entities []E, id func(E) gwadminapi.UUIDv4) []E {
	return slices.SortedFunc(slices.Values(entities), func(a, b E) int {
		idA, idB := id(a), id(b)
		return bytes.Compare(idA[:], idB[:])
	})
}

// withGWAdminDefaults returns the entities as gw-admin returns them: with the
// value it sets on each field left out, e.g. the empty name of the
// destination of a virtual cluster, the null fields of a policy of a virtual
// cluster (see policyNulls) or the condition and the TLS versions of a policy
// of a listener (see withListenerPolicyDefaults), without the empty
// descriptions of the TLS trust bundles and the static keys, which it drops,
// and with the single ports of a listener as strings, as it returns them.
// Otherwise they would be pushed again on every reconcile. A default missing
// here has the same effect. The value
// of a static key is compared as it is: gw-admin returns it, and a rotated
// key must be pushed again.
func withGWAdminDefaults(e gwadminapi.GatewayEntitySet) gwadminapi.GatewayEntitySet {
	e.BackendClusters = slices.Clone(e.BackendClusters)
	for i := range e.BackendClusters {
		if tls := &e.BackendClusters[i].Tls; tls.TlsVersions == nil {
			tls.TlsVersions = &[]gwadminapi.BackendClusterTLSTlsVersions{gwadminapi.Tls12, gwadminapi.Tls13}
		}
	}
	e.TlsTrustBundles = slices.Clone(e.TlsTrustBundles)
	for i := range e.TlsTrustBundles {
		e.TlsTrustBundles[i].Description = withoutEmpty(e.TlsTrustBundles[i].Description)
	}
	e.StaticKeys = slices.Clone(e.StaticKeys)
	for i := range e.StaticKeys {
		e.StaticKeys[i].Description = withoutEmpty(e.StaticKeys[i].Description)
	}
	e.VirtualClusters = slices.Clone(e.VirtualClusters)
	for i := range e.VirtualClusters {
		vc := &e.VirtualClusters[i]
		vc.Destination = editedJSON(vc.Destination, func(fields map[string]any) {
			if _, ok := fields["name"]; !ok {
				fields["name"] = ""
			}
		})
		vc.ConsumePolicies = withPolicyNulls(vc.ConsumePolicies)
		vc.ProducePolicies = withPolicyNulls(vc.ProducePolicies)
	}
	e.Listeners = slices.Clone(e.Listeners)
	for i := range e.Listeners {
		l := &e.Listeners[i]
		l.Ports = portsAsStrings(l.Ports)
		l.Policies = withListenerPolicyDefaults(l.Policies)
	}
	return e
}

// portsAsStrings returns a copy of the ports of a listener with each single
// port as a string, e.g. "9092", as gw-admin returns it.
func portsAsStrings(ports gwadminapi.EventGatewayListenerPorts) gwadminapi.EventGatewayListenerPorts {
	asStrings := make(gwadminapi.EventGatewayListenerPorts, 0, len(ports))
	for _, port := range ports {
		if n, err := port.AsEventGatewayListenerPortInteger(); err == nil {
			var s gwadminapi.EventGatewayListenerPort
			if s.FromEventGatewayListenerPortString(strconv.Itoa(n)) == nil {
				port = s
			}
		}
		asStrings = append(asStrings, port)
	}
	return asStrings
}

// withListenerPolicyDefaults returns a copy of the policies of a listener
// with the defaults gw-admin sets on the fields left out: an empty condition,
// and the TLS versions 1.2 to 1.3 of a TLS server.
func withListenerPolicyDefaults(policies []gwadminapi.EventGatewayListenerPolicyOnPrem) []gwadminapi.EventGatewayListenerPolicyOnPrem {
	withDefaults := make([]gwadminapi.EventGatewayListenerPolicyOnPrem, 0, len(policies))
	for _, p := range policies {
		withDefaults = append(withDefaults, editedJSON(p, func(fields map[string]any) {
			if _, ok := fields["condition"]; !ok {
				fields["condition"] = ""
			}
			config, ok := fields["config"].(map[string]any)
			if fields["type"] != "tls_server" || !ok {
				return
			}
			versions, ok := config["versions"].(map[string]any)
			if !ok {
				versions = map[string]any{}
				config["versions"] = versions
			}
			for field, version := range map[string]string{"min": "TLSv1.2", "max": "TLSv1.3"} {
				if _, ok := versions[field]; !ok {
					versions[field] = version
				}
			}
		}))
	}
	return withDefaults
}

// policyNulls are, by type of policy, the fields gw-admin returns as null
// when they are left out: the parent policy of a policy that can have one,
// and the configuration of a policy that has none.
var policyNulls = map[string][]string{
	"modify_headers": {"parent_policy_id"},
	"skip_record":    {"config", "parent_policy_id"},
}

// withPolicyNulls returns a copy of the policies with their fields of
// policyNulls left out set to null.
func withPolicyNulls[P any](policies []P) []P {
	withNulls := make([]P, 0, len(policies))
	for _, p := range policies {
		withNulls = append(withNulls, editedJSON(p, func(fields map[string]any) {
			typ, _ := fields["type"].(string)
			for _, field := range policyNulls[typ] {
				if _, ok := fields[field]; !ok {
					fields[field] = nil
				}
			}
		}))
	}
	return withNulls
}

// editedJSON returns v, of a gw-admin type, with its JSON object edited by
// edit, or v as it is when it can't be decoded: it is then pushed again on
// every reconcile, like an entity a default is missing for.
func editedJSON[T any](v T, edit func(map[string]any)) T {
	data, err := json.Marshal(v)
	var fields map[string]any
	if err != nil || json.Unmarshal(data, &fields) != nil || fields == nil {
		return v
	}
	edit(fields)
	var edited T
	if data, err = json.Marshal(fields); err != nil || json.Unmarshal(data, &edited) != nil {
		return v
	}
	return edited
}

// withoutEmpty returns nil for an empty string, s otherwise.
func withoutEmpty(s *string) *string {
	if s != nil && *s == "" {
		return nil
	}
	return s
}
