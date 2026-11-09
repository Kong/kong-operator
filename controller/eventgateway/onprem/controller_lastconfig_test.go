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
	"encoding/json"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/gwadmin/gwadminapi"
)

// lastConfigFixture is a gateway pushed once, with a policy that can be made
// impossible to send.
type lastConfigFixture struct {
	*pushFixture

	s      schemaRegistryFixture
	policy *configurationv1alpha1.EventGatewayVirtualClusterProducePolicy
}

func newLastConfigFixture(t *testing.T) lastConfigFixture {
	t.Helper()
	s := newSchemaRegistryFixture(t)
	policy := s.validateWith("validate", `{name: orders}`, 3)
	f := newPushFixture(t, interceptor.Funcs{}, func(f *pushFixture) []client.Object {
		return append(append(f.gwAdmin.Objects(), f.gateway),
			append(s.virtualCluster(), s.registry("orders", "gateway", 1), s.registry("other", "gateway", 2), policy)...)
	})
	lf := lastConfigFixture{pushFixture: f, s: s, policy: policy}
	lf.reconcile(t)
	require.Equal(t, int64(1), read(t, f.cl, f.gateway).Status.ConfigRevision)
	return lf
}

func (f lastConfigFixture) reconcile(t *testing.T) {
	t.Helper()
	_, _ = f.r.Reconcile(t.Context(), read(t, f.cl, f.gateway))
}

// setRegistry makes the policy validate with the registry, missing to make it
// impossible to send.
func (f lastConfigFixture) setRegistry(t *testing.T, registry string) {
	t.Helper()
	changed := f.s.validateWith("validate", `{name: `+registry+`}`, 3)
	changed.ObjectMeta = read(t, f.cl, f.policy).ObjectMeta
	changed.Generation++
	require.NoError(t, f.cl.Update(t.Context(), changed))
}

func TestLastGoodConfig(t *testing.T) {
	t.Run("one older than the gateway records isn't pushed", func(t *testing.T) {
		f := newLastConfigFixture(t)
		f.setRegistry(t, "other")
		f.reconcile(t)
		require.Equal(t, int64(2), read(t, f.cl, f.gateway).Status.ConfigRevision)
		require.Equal(t, 2, f.pod().versions[f.gatewayID].DataVersion)
		// Revision 1 is remembered, e.g. as recording revision 2 raced with
		// another reconcile.
		f.r.lastConfigs.Store(f.gateway.UID, &lastGoodConfig{revision: 1})

		f.setRegistry(t, "missing")
		f.pod().restart()
		f.reconcile(t)
		assert.Empty(t, f.pod().gateways[f.gatewayID].VirtualClusters, "an older configuration isn't pushed")
	})

	t.Run("after a restart of the operator, what a pod holds stays held", func(t *testing.T) {
		f := newLastConfigFixture(t)
		orders := &configurationv1alpha1.EventGatewaySchemaRegistry{}
		require.NoError(t, f.cl.Get(t.Context(), client.ObjectKey{Namespace: ns, Name: "orders"}, orders))
		require.Contains(t, orders.GetFinalizers(), FinalizerInUse)
		// Nothing remembered, as after a restart.
		f.r.lastConfigs.Delete(f.gateway.UID)

		f.setRegistry(t, "missing")
		f.reconcile(t)
		require.NoError(t, f.cl.Get(t.Context(), client.ObjectKeyFromObject(orders), orders))
		assert.Contains(t, orders.GetFinalizers(), FinalizerInUse, "the registry the pod still serves stays held")
		last, ok := f.r.lastConfigs.Load(f.gateway.UID)
		require.True(t, ok, "the configuration read back is remembered")
		assert.Equal(t, int64(1), last.(*lastGoodConfig).revision)
	})

	t.Run("after a restart of the operator, with no pod holding it, a stopped push has none to push", func(t *testing.T) {
		f := newLastConfigFixture(t)
		// A certificate a pod trusts, no longer in the configuration.
		revoked := uuid.New()
		f.pod().mu.Lock()
		if f.pod().certificates[f.gatewayID] == nil {
			f.pod().certificates[f.gatewayID] = map[uuid.UUID]gwadminapi.EventGatewayDataPlaneCertificateOnPrem{}
		}
		f.pod().certificates[f.gatewayID][revoked] = gwadminapi.EventGatewayDataPlaneCertificateOnPrem{Id: revoked}
		f.pod().mu.Unlock()
		// Nothing remembered, as after a restart, and the pod holds another
		// revision, e.g. one an older operator pushed.
		f.r.lastConfigs.Delete(f.gateway.UID)
		f.pod().mu.Lock()
		version := f.pod().versions[f.gatewayID]
		version.DataVersion = 7
		f.pod().versions[f.gatewayID] = version
		f.pod().mu.Unlock()
		held := f.pod().gateways[f.gatewayID]

		f.setRegistry(t, "missing")
		f.reconcile(t)
		assert.NotContains(t, f.pod().holds(f.gatewayID), revoked, "the certificate stops being trusted")
		assert.Equal(t, held, f.pod().gateways[f.gatewayID], "the entities the pod holds stay as they are")

		f.pod().restart()
		f.reconcile(t)
		_, registered := f.pod().gateways[f.gatewayID]
		assert.True(t, registered, "a restarted pod serves the gateway, with no entities")
		assert.Empty(t, f.pod().gateways[f.gatewayID].VirtualClusters)
		assert.True(t, slices.ContainsFunc(f.pod().recorded(), func(call string) bool { return call == callExpected+" "+f.gatewayID.String() }),
			"and is told the gateways it serves")
	})

	t.Run("what it uses stays held while it is pushed in place of the current one", func(t *testing.T) {
		f := newLastConfigFixture(t)
		orders := &configurationv1alpha1.EventGatewaySchemaRegistry{}
		require.NoError(t, f.cl.Get(t.Context(), client.ObjectKey{Namespace: ns, Name: "orders"}, orders))
		require.Contains(t, orders.GetFinalizers(), FinalizerInUse)

		// The policy no longer uses it, but can't be sent: gw-admin still
		// serves it using the registry.
		f.setRegistry(t, "missing")
		f.reconcile(t)
		require.NoError(t, f.cl.Get(t.Context(), client.ObjectKeyFromObject(orders), orders))
		assert.Contains(t, orders.GetFinalizers(), FinalizerInUse, "the registry stays held")

		// Deleted meanwhile, it is kept, so the policy can be fixed back.
		require.NoError(t, f.cl.Delete(t.Context(), orders))
		f.reconcile(t)
		require.NoError(t, f.cl.Get(t.Context(), client.ObjectKeyFromObject(orders), orders))
		f.setRegistry(t, "orders")
		f.reconcile(t)
		assert.True(t, apimeta.IsStatusConditionTrue(read(t, f.cl, f.gateway).Status.Conditions,
			string(eventgatewayv1alpha1.OnPremEventGatewayProgrammedType)), "the fixed configuration is pushed")

		// Once nothing uses it, it goes.
		f.setRegistry(t, "other")
		f.reconcile(t)
		err := f.cl.Get(t.Context(), client.ObjectKeyFromObject(orders), orders)
		assert.True(t, apierrors.IsNotFound(err), "%v", err)
	})

	t.Run("the deletion of the gateway forgets it", func(t *testing.T) {
		f := newLastConfigFixture(t)
		_, ok := f.r.lastConfigs.Load(f.gateway.UID)
		require.True(t, ok)
		f.r.forget(f.gateway.UID)
		_, ok = f.r.lastConfigs.Load(f.gateway.UID)
		assert.False(t, ok)
	})
}

func TestLastGoodConfigPushedAsComparesItsClusterPolicyOrder(t *testing.T) {
	// The last configuration orders the cluster policies of its virtual
	// cluster by a chain, not by id.
	var entities gwadminapi.GatewayEntitySet
	require.NoError(t, json.Unmarshal([]byte(`{
		"virtual_clusters": [
			{"id":"00000000-0000-4000-8000-000000000001", "destination":{"id":"00000000-0000-4000-8000-000000000099"},
			 "cluster_policies":[
				{"id":"00000000-0000-4000-8000-000000000012","type":"acls"},
				{"id":"00000000-0000-4000-8000-000000000011","type":"acls"}]}
		]
	}`), &entities))
	ordered := []uuid.UUID{entities.VirtualClusters[0].Id}
	last := &lastGoodConfig{
		revision:               3,
		config:                 gatewayConfig{Entities: entities},
		orderedClusterPolicies: ordered,
	}

	for name, current := range map[string]configuration{
		"the current configuration orders them too":       {namespace: ns, orderedClusterPolicies: ordered},
		"the current configuration no longer orders them": {namespace: ns},
	} {
		t.Run(name, func(t *testing.T) {
			_, pushing, err := last.pushedAs(current)
			require.NoError(t, err)

			held := withGWAdminDefaults(entities)
			same, err := sameEntities(held, pushing.pushedKey, pushing.orderedClusterPolicies...)
			require.NoError(t, err)
			assert.True(t, same, "a pod holding the last entities isn't pushed them again")

			held.VirtualClusters = slices.Clone(held.VirtualClusters)
			held.VirtualClusters[0].ClusterPolicies = slices.Clone(held.VirtualClusters[0].ClusterPolicies)
			slices.Reverse(held.VirtualClusters[0].ClusterPolicies)
			same, err = sameEntities(held, pushing.pushedKey, pushing.orderedClusterPolicies...)
			require.NoError(t, err)
			assert.False(t, same, "a pod holding them in another order is")
		})
	}
}

func TestEntitiesInUseBySent(t *testing.T) {
	obj := func(o entityObject, name string) entityObject {
		o.SetNamespace(ns)
		o.SetName(name)
		o.SetUID(types.UID(uuid.NewString()))
		return o
	}
	backend := obj(&configurationv1alpha1.EventGatewayBackendCluster{}, "backend")
	vc := obj(&configurationv1alpha1.EventGatewayVirtualCluster{}, "vc")
	unused := obj(&configurationv1alpha1.EventGatewayVirtualCluster{}, "unused")
	registry := obj(&configurationv1alpha1.EventGatewaySchemaRegistry{}, "registry")
	forward := obj(&configurationv1alpha1.EventGatewayListenerPolicy{}, "forward")
	cfg := configuration{namespace: ns}
	for _, o := range []entityObject{backend, vc, unused, registry, forward} {
		cfg.included = append(cfg.included, entityStatus{obj: o})
	}
	goneID := uuid.NewString()

	var sent gwadminapi.GatewayEntitySet
	require.NoError(t, json.Unmarshal([]byte(`{
		"virtual_clusters": [
			{"id": "`+string(vc.GetUID())+`", "name": "vc", "destination": {"id": "`+string(backend.GetUID())+`"},
			 "produce_policies": [{"id": "`+goneID+`", "name": "validate", "type": "schema_validation",
			   "config": {"type": "confluent_schema_registry", "schema_registry": {"id": "`+string(registry.GetUID())+`"}}}]},
			{"id": "`+string(unused.GetUID())+`", "name": "unused", "destination": {"id": "`+string(backend.GetUID())+`"}}
		],
		"listeners": [{"id": "`+uuid.NewString()+`", "name": "l", "policies": [
			{"id": "`+string(forward.GetUID())+`", "name": "forward", "type": "forward_to_virtual_cluster",
			 "config": {"type": "port_mapping", "destination": {"id": "`+string(vc.GetUID())+`"}}}]}]
	}`), &sent))

	forwardPolicy := reportedEntity{typ: listenerPolicyType, key: client.ObjectKeyFromObject(forward)}
	gonePolicy := reportedEntity{typ: producePolicyType, key: client.ObjectKey{Namespace: ns, Name: "validate"}}
	assert.Equal(t, map[reportedEntity][]reportedEntity{
		{typ: virtualClusterType, key: client.ObjectKeyFromObject(vc)}:       {forwardPolicy},
		{typ: backendClusterType, key: client.ObjectKeyFromObject(backend)}:  {forwardPolicy},
		{typ: schemaRegistryType, key: client.ObjectKeyFromObject(registry)}: {gonePolicy},
	}, entitiesInUseBySent(sent, cfg), "a policy whose object is gone is named after its entity; an unused virtual cluster holds nothing")
}
