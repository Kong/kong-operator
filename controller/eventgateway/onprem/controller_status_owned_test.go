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
	"sigs.k8s.io/controller-runtime/pkg/event"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
)

// withKonnectStatus sets on the policy what the Konnect reconciler sets, e.g.
// while its on-prem ancestor didn't exist yet, or under a Konnect gateway
// before it moved.
func withKonnectStatus(policy *configurationv1alpha1.EventGatewayVirtualClusterProducePolicy) {
	policy.Status.Conditions = []metav1.Condition{
		{
			Type: policy.GetStatusConditionTypeParentRefValid(), Status: metav1.ConditionFalse,
			Reason: policy.GetStatusConditionReasonParentRefNotProgrammed(), LastTransitionTime: metav1.Now(),
		},
		{
			Type: "Programmed", Status: metav1.ConditionFalse,
			Reason: konnectv1alpha1.KonnectEntityProgrammedReasonConditionWithStatusFalseExists, LastTransitionTime: metav1.Now(),
		},
	}
	policy.SetKonnectID("konnect-id")
	policy.Status.ServerURL = "https://eu.api.konghq.com"
	policy.Status.GatewayID = &configurationv1alpha1.KonnectEntityRef{ID: "gateway-id"}
	policy.Status.VirtualClusterID = &configurationv1alpha1.KonnectEntityRef{ID: "virtual-cluster-id"}
	policy.Status.ObservedGeneration = 1
}

// The controller owns the status of the entities of its gateways: what the
// Konnect reconciler set on one is cleared once the gateway reports it.
func TestOwnedEntityStatusLosesWhatKonnectSet(t *testing.T) {
	s := newSchemaRegistryFixture(t)
	policy := s.validateWith("validate", `{type: namespacedRef, namespacedRef: {name: orders}}`, 3)
	f := newPushFixture(t, interceptor.Funcs{}, func(f *pushFixture) []client.Object {
		return append(append(f.gwAdmin.Objects(), f.gateway), append(s.virtualCluster(), s.registry("orders", "gateway", 1), policy)...)
	})
	reconcile := func() {
		_, err := f.r.Reconcile(t.Context(), read(t, f.cl, f.gateway))
		require.NoError(t, err)
	}
	reconcile()
	withStatus := read(t, f.cl, policy)
	withKonnectStatus(withStatus)
	require.NoError(t, f.cl.Status().Update(t.Context(), withStatus))

	reconcile()
	got := read(t, f.cl, policy)
	require.Len(t, got.Status.Conditions, 1, "%v", got.Status.Conditions)
	programmed := apimeta.FindStatusCondition(got.Status.Conditions, string(eventgatewayv1alpha1.OnPremEventGatewayProgrammedType))
	require.NotNil(t, programmed)
	assert.Equal(t, string(eventgatewayv1alpha1.OnPremEntityProgrammedReason), programmed.Reason)
	assert.Empty(t, got.GetKonnectID())
	assert.Empty(t, got.Status.ServerURL)
	assert.Nil(t, got.Status.GatewayID)
	assert.Nil(t, got.Status.VirtualClusterID)
	assert.Zero(t, got.Status.ObservedGeneration)
}

// An entity that may not be of the gateway anymore, e.g. detached from it, may
// be under a Konnect gateway now: only its Programmed condition is set.
func TestUnownedEntityStatusKeepsWhatKonnectSet(t *testing.T) {
	policy := &configurationv1alpha1.EventGatewayVirtualClusterProducePolicy{Name: "policy", Namespace: ns}
	withKonnectStatus(policy)
	cl := newFakeClient(t, policy)
	r := &Reconciler{Client: cl}
	policy = read(t, cl, policy)

	require.NoError(t, r.setUnownedEntityConditions(t.Context(), policy, nil, entityCondition(policy, eventgatewayv1alpha1.OnPremEventGatewayProgrammedType,
		metav1.ConditionFalse, eventgatewayv1alpha1.OnPremEntityDetachedReason, "No longer in the configuration")))
	got := read(t, cl, policy)
	assert.Len(t, got.Status.Conditions, 2)
	assert.Equal(t, "konnect-id", got.GetKonnectID())
	assert.NotNil(t, got.Status.GatewayID)
}

// A Programmed condition someone else sets on an entity triggers its gateway,
// which sets it back; the one the controller sets doesn't.
func TestProgrammedSetByOthers(t *testing.T) {
	type state struct {
		cond *metav1.Condition
		// ours tells the controller applied it.
		ours bool
	}
	policyWith := func(s state) *configurationv1alpha1.EventGatewayVirtualClusterProducePolicy {
		p := &configurationv1alpha1.EventGatewayVirtualClusterProducePolicy{}
		if s.cond != nil {
			p.Status.Conditions = []metav1.Condition{*s.cond}
		}
		if s.ours {
			p.ManagedFields = []metav1.ManagedFieldsEntry{{
				Manager: entityStatusFieldManager, Operation: metav1.ManagedFieldsOperationApply, Subresource: "status",
				FieldsV1: metav1.NewFieldsV1(`{"f:status":{"f:conditions":{"k:{\"type\":\"Programmed\"}":{".":{},"f:status":{},"f:reason":{},"f:message":{}}}}}`),
			}}
		}
		return p
	}
	update := func(before, after state) bool {
		return programmedSetByOthers().Update(event.UpdateEvent{ObjectOld: policyWith(before), ObjectNew: policyWith(after)})
	}
	onPrem := &metav1.Condition{Type: "Programmed", Status: metav1.ConditionTrue, Reason: string(eventgatewayv1alpha1.OnPremEntityProgrammedReason), Message: "pushed"}
	konnect := &metav1.Condition{
		Type: "Programmed", Status: metav1.ConditionFalse,
		Reason: konnectv1alpha1.KonnectEntityProgrammedReasonConditionWithStatusFalseExists, Message: "parent not programmed",
	}
	assert.True(t, update(state{onPrem, true}, state{konnect, false}), "the Konnect reconciler set it")
	assert.True(t, update(state{}, state{konnect, false}))
	assert.False(t, update(state{konnect, false}, state{onPrem, true}), "the controller set it")
	assert.False(t, update(state{konnect, false}, state{konnect, false}), "unchanged")
	assert.False(t, update(state{onPrem, true}, state{}))
}

// A condition the controller set whose status, reason or message someone else
// changed since is theirs: it is neither removed nor taken for the
// controller's own.
func TestEntityConditionChangedBySomeoneElse(t *testing.T) {
	vc := &configurationv1alpha1.EventGatewayVirtualCluster{Name: "vc", Namespace: ns}
	cl := newFakeClient(t, vc)
	r := &Reconciler{Client: cl}
	vc = read(t, cl, vc)
	programmed := entityCondition(vc, eventgatewayv1alpha1.OnPremEventGatewayProgrammedType, metav1.ConditionFalse,
		eventgatewayv1alpha1.OnPremEntityDetachedReason, "No longer in the configuration")
	secretRefValid := *secretRefValidCondition(vc, "")
	require.NoError(t, r.setUnownedEntityConditions(t.Context(), vc, nil, programmed, secretRefValid))
	vc = read(t, cl, vc)
	require.True(t, ownsEntityCondition(vc, "Programmed"))

	// The Konnect reconciler sets its own.
	vc.Status.Conditions[0].Reason = konnectv1alpha1.KonnectEntityProgrammedReasonConditionWithStatusFalseExists
	vc.Status.Conditions[0].Message = "set by Konnect"
	require.Equal(t, "Programmed", vc.Status.Conditions[0].Type)
	require.NoError(t, cl.Status().Update(t.Context(), vc, client.FieldOwner("konnect")))
	vc = read(t, cl, vc)
	assert.False(t, ownsEntityCondition(vc, "Programmed"))
	assert.True(t, ownsEntityCondition(vc, string(konnectv1alpha1.SecretRefValidConditionType)))

	// The controller removes its conditions, e.g. the entity reaches no
	// OnPremEventGateway anymore.
	require.NoError(t, r.setUnownedEntityConditions(t.Context(), vc, []string{"Programmed", string(konnectv1alpha1.SecretRefValidConditionType)}))
	vc = read(t, cl, vc)
	require.Len(t, vc.Status.Conditions, 1, "%v", vc.Status.Conditions)
	assert.Equal(t, "set by Konnect", vc.Status.Conditions[0].Message, "the one someone else changed stays as they set it")
}
