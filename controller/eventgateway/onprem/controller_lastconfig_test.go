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
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
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
	policy := s.validateWith("validate", `{type: namespacedRef, namespacedRef: {name: orders}}`, 3)
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
	changed := f.s.validateWith("validate", `{type: namespacedRef, namespacedRef: {name: `+registry+`}}`, 3)
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

	t.Run("after a restart of the operator, a stopped push has none to push", func(t *testing.T) {
		f := newLastConfigFixture(t)
		// A certificate a pod trusts, no longer in the configuration.
		revoked := uuid.New()
		f.pod().mu.Lock()
		if f.pod().certificates[f.gatewayID] == nil {
			f.pod().certificates[f.gatewayID] = map[uuid.UUID]gwadminapi.EventGatewayDataPlaneCertificateOnPrem{}
		}
		f.pod().certificates[f.gatewayID][revoked] = gwadminapi.EventGatewayDataPlaneCertificateOnPrem{Id: revoked}
		f.pod().mu.Unlock()
		// Nothing remembered, as after a restart.
		f.r.lastConfigs.Delete(f.gateway.UID)
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
