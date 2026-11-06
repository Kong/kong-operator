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
	"fmt"
	"slices"

	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
)

// The last configuration of a gateway that could be sent is remembered, so
// that, while the push is stopped by entities that can't be sent (see
// configuration.blocked), a gw-admin pod that lost it, e.g. restarted, gets it
// again, rather than nothing. It is the configuration recorded with the
// current revision (see recordRevision), remembered before any pod is pushed.
//
// It is kept in memory only: after a restart of the operator, the
// configuration is built again from the objects, and while one can't be sent,
// nothing is remembered and the pods keep the entities they hold.

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
}

// rememberConfig remembers the configuration of the gateway, recorded with
// its current revision, and the entities its policies use.
func (r *Reconciler) rememberConfig(
	gateway *eventgatewayv1alpha1.OnPremEventGateway, config gatewayConfig, inUse map[reportedEntity][]reportedEntity,
) error {
	hash, err := config.hash()
	if err != nil {
		return err
	}
	r.lastConfigs.Store(gateway.UID, &lastGoodConfig{
		revision: gateway.Status.ConfigRevision, hash: hash, config: config, inUse: inUse,
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

// pushedAs returns the last configuration as it is pushed, at its own
// revision, and the configuration pushing it: the current one, for its data
// plane certificates, which a stopped push still makes current, e.g. to stop
// trusting a certificate deleted, with the key of the last entities.
func (last *lastGoodConfig) pushedAs(cfg configuration) (gatewayConfig, configuration, error) {
	config := gatewayConfig{Gateway: last.config.Gateway, Entities: last.config.Entities}
	config.Version = configVersion(&eventgatewayv1alpha1.OnPremEventGateway{
		Status: eventgatewayv1alpha1.OnPremEventGatewayStatus{ConfigRevision: last.revision},
	})
	key, err := comparisonKey(withGWAdminDefaults(last.config.Entities))
	if err != nil {
		return gatewayConfig{}, configuration{}, fmt.Errorf("failed to encode the last entities of the gateway: %w", err)
	}
	pushing := cfg
	pushing.pushedKey = key
	// The paths gw-admin reports name the last entities, not those of the
	// current objects.
	pushing.objects = map[string]string{}
	return config, pushing, nil
}
