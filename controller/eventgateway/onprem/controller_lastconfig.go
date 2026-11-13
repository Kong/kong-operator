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
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sync"

	"github.com/google/uuid"
	"sigs.k8s.io/controller-runtime/pkg/client"

	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminapi"
	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminsync"
)

// The last configuration of a gateway that could be sent is remembered, so
// that, while the push is stopped by entities that can't be sent (see
// configuration.blocked), a gw-admin pod that lost it, e.g. restarted, gets it
// again, rather than nothing. It is the configuration recorded with the
// current revision (see recordRevision), remembered before any pod is pushed.
//
// It is kept in memory. After a restart of the operator, or when the one
// remembered isn't the revision the gateway records, it is read back from a
// gw-admin pod holding that revision (see recoverLastConfig), along with the
// entities its policies use, which stay held. Only when no pod holds it, e.g.
// every pod restarted too, is there none: the pods are then pushed the gateway
// and its data plane certificates, and keep the entities they hold.

// lastGoodConfig is the last configuration of a gateway that could be sent.
type lastGoodConfig struct {
	// revision is the revision it was recorded with (see recordRevision).
	revision int64
	// hash is its hash (see gatewayConfig.hash).
	hash   string
	config gatewayConfig
	// inUse are the entities its policies use (see entitiesInUse): they stay
	// held while it is pushed in place of the current configuration.
	inUse map[reportedEntity][]reportedEntity
	// orderedClusterPolicies are the virtual clusters whose cluster policies
	// it orders explicitly (see configuration.orderedClusterPolicies): what
	// gw-admin holds is compared with it in that order, which the current
	// configuration may no longer have.
	orderedClusterPolicies []uuid.UUID
}

// rememberConfig remembers the configuration of the gateway, recorded with
// its current revision, built as cfg, the entities its policies use and the
// virtual clusters whose cluster policies it orders.
func (r *Reconciler) rememberConfig(
	gateway *eventgatewayv1alpha1.OnPremEventGateway, config gatewayConfig, cfg configuration,
) error {
	hash, err := config.hash()
	if err != nil {
		return err
	}
	r.lastConfigs.Store(gateway.UID, &lastGoodConfig{
		revision: gateway.Status.ConfigRevision, hash: hash, config: config, inUse: cfg.entitiesInUse,
		orderedClusterPolicies: slices.Clone(cfg.orderedClusterPolicies),
	})
	return nil
}

// mergeEntitiesInUse returns the entities either uses, with the policies of
// both using each.
func mergeEntitiesInUse(current, last map[reportedEntity][]reportedEntity) map[reportedEntity][]reportedEntity {
	merged := make(map[reportedEntity][]reportedEntity, len(current)+len(last))
	for _, m := range []map[reportedEntity][]reportedEntity{current, last} {
		for entity, policies := range m {
			for _, p := range policies {
				if !slices.Contains(merged[entity], p) {
					merged[entity] = append(merged[entity], p)
				}
			}
		}
	}
	return merged
}

// lastGoodConfigOf returns the last configuration of the gateway that could
// be sent, or an error telling why there is none to push: it must be the
// configuration the gateway records, at its revision and with its hash, as a
// pod must not be pushed an older configuration than it holds.
func (r *Reconciler) lastGoodConfigOf(gateway *eventgatewayv1alpha1.OnPremEventGateway) (*lastGoodConfig, error) {
	v, ok := r.lastConfigs.Load(gateway.UID)
	if !ok {
		return nil, fmt.Errorf("no configuration of the gateway that could be sent is known since the operator started")
	}
	last := v.(*lastGoodConfig)
	if last.revision != gateway.Status.ConfigRevision || last.hash != gateway.Status.ConfigHash {
		return nil, fmt.Errorf("the last configuration known, revision %d, isn't the one the gateway records, revision %d",
			last.revision, gateway.Status.ConfigRevision)
	}
	return last, nil
}

// recoverLastConfig reads the last configuration of the gateway that could be
// sent from a gw-admin pod holding the revision the gateway records: a pod is
// only ever pushed a revision once it is recorded, with the configuration
// recorded with it. The configuration is remembered, so that the pods are not
// read again, with the entities its policies use, among the objects of cfg
// (see entitiesInUseBySent). It returns an error telling why there is none.
func (r *Reconciler) recoverLastConfig(
	ctx context.Context, gateway *eventgatewayv1alpha1.OnPremEventGateway, gatewayID uuid.UUID, cfg configuration,
) (*lastGoodConfig, error) {
	revision := gateway.Status.ConfigRevision
	if revision == 0 || gateway.Status.ConfigHash == "" {
		return nil, errors.New("the gateway records no configuration")
	}
	target, err := r.resolveGWAdmin(ctx, gateway, true)
	if err != nil {
		return nil, fmt.Errorf("its gw-admin pods can't be read: %w", err)
	}
	var (
		mu    sync.Mutex
		found *gwadminapi.GatewayEntities
	)
	// A pod that can't be read is one less to read it from: its error isn't
	// the push's.
	_, _ = r.GWAdminSync.ForEachPod(ctx, target, target.Pods,
		func(ctx context.Context, _ gwadminsync.Pod, gwAdmin gwadminapi.ClientInterface) error {
			held, ok, err := getEntities(ctx, gwAdmin, gatewayID)
			if err != nil || !ok || int64(held.ConfigVersion.DataVersion) != revision {
				return err
			}
			mu.Lock()
			defer mu.Unlock()
			if found == nil {
				found = &held
			}
			return nil
		})
	if found == nil {
		return nil, fmt.Errorf("no gw-admin pod holds revision %d", revision)
	}
	// gw-admin returns the name of a destination named by id, which isn't
	// sent (see withoutDestinationNames).
	entities := withoutDestinationNames(found.Entities)
	last := &lastGoodConfig{
		revision: revision,
		hash:     gateway.Status.ConfigHash,
		config:   gatewayConfig{Gateway: deriveConfig(gateway, cfg).Gateway, Entities: entities},
		inUse:    entitiesInUseBySent(entities, cfg),
		// The entities are as the pod returns them: whatever the virtual
		// clusters taken as ordered, a pod holding them compares the same.
		orderedClusterPolicies: slices.Clone(cfg.orderedClusterPolicies),
	}
	r.lastConfigs.Store(gateway.UID, last)
	return last, nil
}

// entitiesInUseBySent returns, as entitiesInUse does, the entities the
// policies of the entities sent reference, among the objects of cfg, which
// FinalizerInUse can hold, with the policies referencing each, and the backend
// cluster of each virtual cluster in use. A policy whose object is gone is
// named after its entity, without a namespace: it may have been of any.
func entitiesInUseBySent(sent gwadminapi.GatewayEntitySet, cfg configuration) map[reportedEntity][]reportedEntity {
	held := map[string]reportedEntity{}
	objects := map[string]reportedEntity{}
	for _, obj := range cfg.allObjects() {
		e := reportedEntity{typ: reflect.TypeOf(obj), key: client.ObjectKeyFromObject(obj)}
		// The entities sent are under the id of their entity (see
		// entityIDOf), whether the object is in the configuration or not.
		id := entityIDKey(obj)
		objects[id] = e
		if isHeldKind(e.typ) {
			held[id] = e
		}
	}
	inUse := map[reportedEntity][]reportedEntity{}
	use := func(t reflect.Type, id gwadminapi.UUIDv4, policy any) {
		self, ok := objects[id.String()]
		if !ok {
			name := id.String()
			if names := policyName(policy); len(names) > 0 {
				name = names[0].value
			}
			self = reportedEntity{typ: t, key: client.ObjectKey{Name: name}}
		}
		for _, ref := range sentReferences(policy, held, self) {
			if !slices.Contains(inUse[ref], self) {
				inUse[ref] = append(inUse[ref], self)
			}
		}
	}
	for _, vc := range sent.VirtualClusters {
		for _, p := range vc.ClusterPolicies {
			use(virtualClusterPolicyType, p.Id, p)
		}
		for _, p := range vc.ConsumePolicies {
			use(consumePolicyType, p.Id, p)
		}
		for _, p := range vc.ProducePolicies {
			use(producePolicyType, p.Id, p)
		}
	}
	for _, l := range sent.Listeners {
		for _, p := range l.Policies {
			use(listenerPolicyType, p.Id, p)
		}
	}
	// A virtual cluster in use holds its backend cluster, its destination.
	for _, vc := range sent.VirtualClusters {
		e, ok := held[vc.Id.String()]
		if !ok || len(inUse[e]) == 0 {
			continue
		}
		for _, backendCluster := range sentReferences(vc.Destination, held, e) {
			for _, policy := range inUse[e] {
				if !slices.Contains(inUse[backendCluster], policy) {
					inUse[backendCluster] = append(inUse[backendCluster], policy)
				}
			}
		}
	}
	return inUse
}

// pushedAs returns the last configuration as it is pushed, at its own
// revision, and the configuration pushing it: the current one, for its data
// plane certificates, which a stopped push still makes current, e.g. to stop
// trusting a certificate deleted, with the key of the last entities, and
// their cluster policies compared in the order they were pushed in.
func (last *lastGoodConfig) pushedAs(cfg configuration) (gatewayConfig, configuration, error) {
	config := gatewayConfig{Gateway: last.config.Gateway, Entities: last.config.Entities}
	config.Version = configVersion(&eventgatewayv1alpha1.OnPremEventGateway{
		Status: eventgatewayv1alpha1.OnPremEventGatewayStatus{ConfigRevision: last.revision},
	})
	key, err := comparisonKey(withGWAdminDefaults(last.config.Entities), last.orderedClusterPolicies...)
	if err != nil {
		return gatewayConfig{}, configuration{}, fmt.Errorf("failed to encode the last entities of the gateway: %w", err)
	}
	pushing := cfg
	pushing.pushedKey = key
	pushing.orderedClusterPolicies = last.orderedClusterPolicies
	// The paths gw-admin reports name the last entities, not those of the
	// current objects.
	pushing.objects = map[string]string{}
	return config, pushing, nil
}
