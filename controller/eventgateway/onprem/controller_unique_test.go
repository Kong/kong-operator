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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
)

// What was sent decides, across reconciles and restarts of the operator,
// whether an entity that can't have its unique values stops the push.
func TestReconcileUniqueValues(t *testing.T) {
	u := newUniqueFixture(t)
	f := newPushFixture(t, interceptor.Funcs{}, func(f *pushFixture) []client.Object {
		return append(append(f.gwAdmin.Objects(), f.gateway), u.schemaRegistryFixture.virtualCluster()...)
	})
	reconcile := func(t *testing.T) {
		t.Helper()
		_, err := f.r.Reconcile(t.Context(), read(t, f.cl, f.gateway))
		require.NoError(t, err)
	}
	// restart forgets what the operator keeps in memory, as a restart does.
	restart := func() {
		f.r.lastConfigs.Delete(f.gateway.UID)
		f.r.lastEntities.Delete(f.gateway.UID)
	}
	programmed := func(t *testing.T, obj client.Object) *metav1.Condition {
		t.Helper()
		var conditions []metav1.Condition
		switch o := read(t, f.cl, obj).(type) {
		case *eventgatewayv1alpha1.OnPremEventGateway:
			conditions = o.Status.Conditions
		case entityObject:
			conditions = o.GetConditions()
		}
		cond := apimeta.FindStatusCondition(conditions, "Programmed")
		require.NotNil(t, cond)
		return cond
	}
	backendClusters := func() []string {
		f.pod().mu.Lock()
		defer f.pod().mu.Unlock()
		var names []string
		for _, bc := range f.pod().gateways[f.gatewayID].BackendClusters {
			names = append(names, bc.Name)
		}
		return names
	}

	reconcile(t)
	require.Equal(t, []string{"bc"}, backendClusters())

	duplicate := u.backendCluster("duplicate", "bc", 10)
	require.NoError(t, f.cl.Create(t.Context(), duplicate))
	reconcile(t)
	t.Run("a duplicate never sent is only left out", func(t *testing.T) {
		assert.Equal(t, metav1.ConditionTrue, programmed(t, f.gateway).Status, programmed(t, f.gateway).Message)
		cond := programmed(t, duplicate)
		assert.Equal(t, metav1.ConditionFalse, cond.Status)
		assert.Contains(t, cond.Message, `its spec.apiSpec.name "bc" is already the one of EventGatewayBackendCluster `+ns+"/bc")
		assert.Equal(t, []string{"bc"}, backendClusters())
	})

	t.Run("after a restart of the operator, it is still only left out", func(t *testing.T) {
		revision := read(t, f.cl, f.gateway).Status.ConfigRevision
		restart()
		reconcile(t)
		cond := programmed(t, f.gateway)
		assert.Equal(t, metav1.ConditionTrue, cond.Status, "what was sent is read back from a gw-admin pod: %s", cond.Message)
		assert.Equal(t, revision, read(t, f.cl, f.gateway).Status.ConfigRevision, "nothing changed")
	})

	renamed := u.backendCluster("renamed", "other", 20)
	require.NoError(t, f.cl.Create(t.Context(), renamed))
	reconcile(t)
	require.ElementsMatch(t, []string{"bc", "other"}, backendClusters())
	renamed = read(t, f.cl, renamed)
	renamed.Spec.APISpec.Name = "bc"
	require.NoError(t, f.cl.Update(t.Context(), renamed))

	t.Run("an entity sent renamed into a name another runs with stops the push", func(t *testing.T) {
		reconcile(t)
		cond := programmed(t, f.gateway)
		assert.Equal(t, metav1.ConditionFalse, cond.Status)
		assert.Equal(t, string(eventgatewayv1alpha1.OnPremEventGatewayInvalidEntitiesReason), cond.Reason)
		assert.Contains(t, cond.Message, "EventGatewayBackendCluster "+ns+"/renamed")
		assert.ElementsMatch(t, []string{"bc", "other"}, backendClusters(), "gw-admin keeps what it has")
	})

	t.Run("after a restart of the operator, it still stops the push", func(t *testing.T) {
		restart()
		reconcile(t)
		cond := programmed(t, f.gateway)
		assert.Equal(t, metav1.ConditionFalse, cond.Status)
		assert.Equal(t, string(eventgatewayv1alpha1.OnPremEventGatewayInvalidEntitiesReason), cond.Reason)
		assert.ElementsMatch(t, []string{"bc", "other"}, backendClusters())
	})

	t.Run("deleted, the push goes on", func(t *testing.T) {
		require.NoError(t, f.cl.Delete(t.Context(), read(t, f.cl, renamed)))
		reconcile(t)
		assert.Equal(t, metav1.ConditionTrue, programmed(t, f.gateway).Status, programmed(t, f.gateway).Message)
		assert.Equal(t, []string{"bc"}, backendClusters())
	})
}
