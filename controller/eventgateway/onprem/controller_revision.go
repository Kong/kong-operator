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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"

	"sigs.k8s.io/controller-runtime/pkg/client"

	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminapi"
)

// The configuration of a gateway is derived from the API server alone, and
// numbered: status.configRevision increases by one every time it changes, and
// status.configHash tells which configuration that revision stands for. A
// revision is recorded before any gw-admin pod is pushed its configuration,
// so that a revision always stands for one configuration, across restarts of
// the operator. The CRD refuses a revision that decreases, or a hash that
// changes without a new revision.

// gatewayConfig is the configuration of a gateway Kong Operator pushes to
// gw-admin.
type gatewayConfig struct {
	// Gateway registers the gateway.
	Gateway gwadminapi.UpsertGatewayRequestOnPrem `json:"gateway"`
	// Entities are the entities of the gateway, each list sorted.
	Entities gwadminapi.GatewayEntities `json:"entities"`
}

// deriveConfig returns the configuration of the gateway, from the gateway
// and the configuration built from its entities (see buildConfiguration).
func deriveConfig(gateway *eventgatewayv1alpha1.OnPremEventGateway, cfg configuration) gatewayConfig {
	return gatewayConfig{
		Gateway: gwadminapi.UpsertGatewayRequestOnPrem{
			Name:      gateway.Name,
			CreatedAt: createdAt(gateway),
		},
		Entities: cfg.entities,
	}
}

// hash returns the hash of the configuration: of its JSON encoding, where a
// list missing and an empty one are the same, as for gw-admin.
func (c gatewayConfig) hash() (string, error) {
	c.Entities = withEmptyLists(c.Entities)
	encoded, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("failed to encode the configuration of the gateway: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// withEmptyLists returns the entities with an empty list for each one
// missing, the policies of a virtual cluster and of a listener included.
func withEmptyLists(e gwadminapi.GatewayEntities) gwadminapi.GatewayEntities {
	if e.BackendClusters == nil {
		e.BackendClusters = []gwadminapi.EventGatewayBackendClusterOnPrem{}
	}
	if e.Listeners == nil {
		e.Listeners = []gwadminapi.EventGatewayListenerOnPrem{}
	}
	if e.SchemaRegistries == nil {
		e.SchemaRegistries = []gwadminapi.EventGatewaySchemaRegistryOnPrem{}
	}
	if e.StaticKeys == nil {
		e.StaticKeys = []gwadminapi.EventGatewayStaticKeyOnPrem{}
	}
	if e.TlsTrustBundles == nil {
		e.TlsTrustBundles = []gwadminapi.EventGatewayTLSTrustBundleOnPrem{}
	}
	if e.VirtualClusters == nil {
		e.VirtualClusters = []gwadminapi.EventGatewayVirtualClusterOnPrem{}
	}
	e.VirtualClusters = slices.Clone(e.VirtualClusters)
	for i := range e.VirtualClusters {
		vc := &e.VirtualClusters[i]
		if vc.ClusterPolicies == nil {
			vc.ClusterPolicies = []gwadminapi.EventGatewayClusterPolicyOnPrem{}
		}
		if vc.ConsumePolicies == nil {
			vc.ConsumePolicies = []gwadminapi.EventGatewayConsumePolicyOnPrem{}
		}
		if vc.ProducePolicies == nil {
			vc.ProducePolicies = []gwadminapi.EventGatewayProducePolicyOnPrem{}
		}
	}
	e.Listeners = slices.Clone(e.Listeners)
	for i := range e.Listeners {
		if e.Listeners[i].Policies == nil {
			e.Listeners[i].Policies = []gwadminapi.EventGatewayListenerPolicyOnPrem{}
		}
	}
	return e
}

// recordRevision records a new revision of the configuration of the gateway
// when it changed since the last one. The revision is written with an
// optimistic lock: a gateway read from a stale cache, e.g. right after the
// previous revision, conflicts and is reconciled again, rather than reusing
// a revision.
func (r *Reconciler) recordRevision(ctx context.Context, gateway *eventgatewayv1alpha1.OnPremEventGateway, config gatewayConfig) error {
	hash, err := config.hash()
	if err != nil {
		return err
	}
	if gateway.Status.ConfigHash == hash {
		return nil
	}
	old := gateway.DeepCopy()
	gateway.Status.ConfigRevision++
	gateway.Status.ConfigHash = hash
	if err := r.Status().Patch(ctx, gateway, client.MergeFromWithOptions(old, client.MergeFromWithOptimisticLock{})); err != nil {
		revision := gateway.Status.ConfigRevision
		gateway.Status = old.Status
		return fmt.Errorf("failed to record revision %d of the configuration: %w", revision, err)
	}
	return nil
}
