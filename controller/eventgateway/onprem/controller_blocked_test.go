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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	kcfgconsts "github.com/kong/kong-operator/v2/api/common/consts"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
)

// An entity that can't be sent stops the configuration from being pushed:
// left out, a policy would no longer apply, e.g. records would no longer be
// validated or encrypted.
func TestInvalidEntityBlocksThePush(t *testing.T) {
	condition := func(t *testing.T, conds []metav1.Condition, conditionType kcfgconsts.ConditionType) *metav1.Condition {
		t.Helper()
		cond := apimeta.FindStatusCondition(conds, string(conditionType))
		require.NotNil(t, cond, "no %s condition", conditionType)
		return cond
	}
	entitiesPushed := func(f *pushFixture) bool {
		return slices.Contains(f.pod().recorded(), callEntities)
	}
	const blockedBy = "these entities can't be sent: EventGatewayVirtualClusterProducePolicy " + ns + "/validate: "

	t.Run("a new gateway isn't pushed until its entities can be sent", func(t *testing.T) {
		s := newSchemaRegistryFixture(t)
		policy := s.validateWith("validate", `{name: orders}`, 3)
		f := newPushFixture(t, interceptor.Funcs{}, func(f *pushFixture) []client.Object {
			return append(append(f.gwAdmin.Objects(), f.gateway), append(s.virtualCluster(), policy)...)
		})

		_, err := f.r.Reconcile(t.Context(), read(t, f.cl, f.gateway))
		require.NoError(t, err)
		assert.False(t, entitiesPushed(f), "nothing is pushed")

		gateway := read(t, f.cl, f.gateway)
		assert.Zero(t, gateway.Status.ConfigRevision, "the configuration not pushed isn't recorded")
		for _, conditionType := range []kcfgconsts.ConditionType{
			eventgatewayv1alpha1.OnPremEventGatewayConfigurationPushedType, eventgatewayv1alpha1.OnPremEventGatewayProgrammedType,
		} {
			cond := condition(t, gateway.Status.Conditions, conditionType)
			assert.Equal(t, metav1.ConditionFalse, cond.Status, conditionType)
			assert.Equal(t, string(eventgatewayv1alpha1.OnPremEventGatewayInvalidEntitiesReason), cond.Reason, conditionType)
			assert.Contains(t, cond.Message, blockedBy, conditionType)
		}
		resolved := condition(t, gateway.Status.Conditions, eventgatewayv1alpha1.OnPremEventGatewayGWAdminResolvedType)
		assert.Equal(t, metav1.ConditionTrue, resolved.Status, "its GWAdmin still serves it")

		blocking := condition(t, read(t, f.cl, policy).Status.Conditions, eventgatewayv1alpha1.OnPremEventGatewayProgrammedType)
		assert.Equal(t, metav1.ConditionFalse, blocking.Status)
		assert.Equal(t, string(eventgatewayv1alpha1.OnPremEntityInvalidReason), blocking.Reason)
		assert.True(t, strings.HasPrefix(blocking.Message, "Can't be sent: the configuration of the OnPremEventGateway "+ns+"/gateway isn't pushed until it is fixed or deleted: "), blocking.Message)

		vc := &configurationv1alpha1.EventGatewayVirtualCluster{}
		require.NoError(t, f.cl.Get(t.Context(), client.ObjectKey{Namespace: ns, Name: "vc"}, vc))
		vcCond := condition(t, vc.Status.Conditions, eventgatewayv1alpha1.OnPremEventGatewayProgrammedType)
		assert.Equal(t, metav1.ConditionFalse, vcCond.Status, "nothing of the configuration is pushed")
		assert.Equal(t, string(eventgatewayv1alpha1.OnPremEntityGatewayNotProgrammedReason), vcCond.Reason)

		// Once what it references exists, the configuration is pushed.
		require.NoError(t, f.cl.Create(t.Context(), s.registry("orders", "gateway", 1)))
		_, err = f.r.Reconcile(t.Context(), read(t, f.cl, f.gateway))
		require.NoError(t, err)
		assert.True(t, entitiesPushed(f), "the configuration is pushed")
		gateway = read(t, f.cl, f.gateway)
		assert.Equal(t, int64(1), gateway.Status.ConfigRevision)
		assert.Equal(t, metav1.ConditionTrue, condition(t, gateway.Status.Conditions, eventgatewayv1alpha1.OnPremEventGatewayProgrammedType).Status)
		assert.Equal(t, metav1.ConditionTrue, condition(t, read(t, f.cl, policy).Status.Conditions, eventgatewayv1alpha1.OnPremEventGatewayProgrammedType).Status)
	})

	t.Run("gw-admin keeps the configuration it has while an entity can't be sent", func(t *testing.T) {
		s := newSchemaRegistryFixture(t)
		policy := s.validateWith("validate", `{name: orders}`, 3)
		f := newPushFixture(t, interceptor.Funcs{}, func(f *pushFixture) []client.Object {
			return append(append(f.gwAdmin.Objects(), f.gateway), append(s.virtualCluster(), s.registry("orders", "gateway", 1), policy)...)
		})
		_, err := f.r.Reconcile(t.Context(), read(t, f.cl, f.gateway))
		require.NoError(t, err)
		require.True(t, entitiesPushed(f))
		revision := read(t, f.cl, f.gateway).Status.ConfigRevision
		held := f.pod().gateways[f.gatewayID]
		require.Len(t, held.VirtualClusters, 1)
		require.Len(t, held.VirtualClusters[0].ProducePolicies, 1, "the policy is pushed")
		recordedEvents(f.recorder)

		// It now names a registry that doesn't exist.
		changed := s.validateWith("validate", `{name: missing}`, 3)
		changed.ObjectMeta = read(t, f.cl, policy).ObjectMeta
		changed.Generation++
		require.NoError(t, f.cl.Update(t.Context(), changed))
		f.pod().reset()

		heldVersion := f.pod().versions[f.gatewayID]
		_, err = f.r.Reconcile(t.Context(), read(t, f.cl, f.gateway))
		require.NoError(t, err)
		// The pod is pushed the last configuration that could be sent, the one
		// it holds: the fake doesn't apply gw-admin's defaults, so it may be
		// written again, the same.
		assert.Equal(t, held, f.pod().gateways[f.gatewayID], "gw-admin keeps the policy as it was")
		assert.Equal(t, heldVersion, f.pod().versions[f.gatewayID], "at the same version")
		gateway := read(t, f.cl, f.gateway)
		assert.Equal(t, revision, gateway.Status.ConfigRevision, "no new revision")
		programmed := condition(t, gateway.Status.Conditions, eventgatewayv1alpha1.OnPremEventGatewayProgrammedType)
		assert.Equal(t, metav1.ConditionFalse, programmed.Status)
		assert.Equal(t, string(eventgatewayv1alpha1.OnPremEventGatewayInvalidEntitiesReason), programmed.Reason)
		assert.Contains(t, programmed.Message, fmt.Sprintf("gw-admin pods are pushed revision %d, the last that could be sent", revision))
		events := recordedEvents(f.recorder)
		require.Len(t, events, 1, "a Warning tells it stopped being pushed")
		assert.Contains(t, events[0], "Warning InvalidEntities")

		t.Run("a gw-admin pod restarted meanwhile gets it", func(t *testing.T) {
			f.pod().restart()
			_, err := f.r.Reconcile(t.Context(), read(t, f.cl, f.gateway))
			require.NoError(t, err)
			assert.Equal(t, held, f.pod().gateways[f.gatewayID])
			assert.Equal(t, heldVersion, f.pod().versions[f.gatewayID])
		})

		t.Run("after a restart of the operator, it is read back from a pod holding it", func(t *testing.T) {
			restarted := newPushFixture(t, interceptor.Funcs{}, nil)
			restarted.r.Client, restarted.r.GWAdminSync.Client, restarted.r.GWAdminSync.NewClient = f.cl, f.cl, f.pods.newClient
			_, err := restarted.r.Reconcile(t.Context(), read(t, f.cl, f.gateway))
			require.NoError(t, err)
			assert.Equal(t, held, f.pod().gateways[f.gatewayID], "a pod keeps the entities it holds")
			programmed := condition(t, read(t, f.cl, f.gateway).Status.Conditions, eventgatewayv1alpha1.OnPremEventGatewayProgrammedType)
			assert.Equal(t, string(eventgatewayv1alpha1.OnPremEventGatewayInvalidEntitiesReason), programmed.Reason)
			assert.Contains(t, programmed.Message, fmt.Sprintf("gw-admin pods are pushed revision %d, the last that could be sent", revision))

			// A pod restarted since gets it, from what was read back.
			f.pod().restart()
			_, err = restarted.r.Reconcile(t.Context(), read(t, f.cl, f.gateway))
			require.NoError(t, err)
			require.Len(t, f.pod().gateways[f.gatewayID].VirtualClusters, 1)
			assert.Len(t, f.pod().gateways[f.gatewayID].VirtualClusters[0].ProducePolicies, 1, "the policy is pushed as it was")
			assert.Equal(t, heldVersion, f.pod().versions[f.gatewayID])
		})

		t.Run("after a restart of the operator, with no pod holding it, there is none", func(t *testing.T) {
			f.pod().restart()
			restarted := newPushFixture(t, interceptor.Funcs{}, nil)
			restarted.r.Client, restarted.r.GWAdminSync.Client, restarted.r.GWAdminSync.NewClient = f.cl, f.cl, f.pods.newClient
			_, err := restarted.r.Reconcile(t.Context(), read(t, f.cl, f.gateway))
			require.NoError(t, err)
			assert.Empty(t, f.pod().gateways[f.gatewayID].VirtualClusters)
			programmed := condition(t, read(t, f.cl, f.gateway).Status.Conditions, eventgatewayv1alpha1.OnPremEventGatewayProgrammedType)
			assert.Equal(t, string(eventgatewayv1alpha1.OnPremEventGatewayInvalidEntitiesReason), programmed.Reason)
			assert.Contains(t, programmed.Message, "gw-admin keeps the one it has")
		})
	})

	t.Run("a gateway never pushed has nothing to push while an entity can't be sent", func(t *testing.T) {
		s := newSchemaRegistryFixture(t)
		policy := s.validateWith("validate", `{name: missing}`, 3)
		f := newPushFixture(t, interceptor.Funcs{}, func(f *pushFixture) []client.Object {
			return append(append(f.gwAdmin.Objects(), f.gateway), append(s.virtualCluster(), policy)...)
		})
		_, err := f.r.Reconcile(t.Context(), read(t, f.cl, f.gateway))
		require.NoError(t, err)
		assert.False(t, entitiesPushed(f))
		_, remembered := f.r.lastConfigs.Load(f.gateway.UID)
		assert.False(t, remembered, "nothing is remembered")
	})

	t.Run("an entity of another namespace naming the gateway doesn't hold it back", func(t *testing.T) {
		s := newSchemaRegistryFixture(t)
		other := s.registry("other", "gateway", 2)
		other.SetNamespace("elsewhere")
		other.Spec.ParentRef.Namespace = new(ns)
		f := newPushFixture(t, interceptor.Funcs{}, func(f *pushFixture) []client.Object {
			return append(append(f.gwAdmin.Objects(), f.gateway), append(s.virtualCluster(), other)...)
		})
		_, err := f.r.Reconcile(t.Context(), read(t, f.cl, f.gateway))
		require.NoError(t, err)
		assert.True(t, entitiesPushed(f), "the configuration is pushed")
		cond := condition(t, read(t, f.cl, other).Status.Conditions, eventgatewayv1alpha1.OnPremEventGatewayProgrammedType)
		assert.Equal(t, metav1.ConditionFalse, cond.Status)
		assert.True(t, strings.HasPrefix(cond.Message, "Left out of the configuration"), cond.Message)
	})
}
