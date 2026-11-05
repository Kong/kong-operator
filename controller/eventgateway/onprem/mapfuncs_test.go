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
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
)

func TestEntitiesOf(t *testing.T) {
	objs, err := entitiesOf(&configurationv1alpha1.EventGatewayListenerList{Items: []configurationv1alpha1.EventGatewayListener{{Name: "a"}, {Name: "b"}}})
	require.NoError(t, err)
	assert.Len(t, objs, 2)
}

func TestOnPremEventGatewaysIn(t *testing.T) {
	a, b := newTestGateway("a"), newTestGateway("b")
	c := newTestGateway("c")
	c.Namespace = "other"
	cl := newFakeClient(t, a, b, c)
	assert.ElementsMatch(t, requestsFor(a, b, c), onPremEventGatewaysIn(t.Context(), cl, ns, "other", ns))

	cl = newFakeClientWithInterceptor(t, interceptor.Funcs{List: failingList})
	assert.Empty(t, onPremEventGatewaysIn(t.Context(), cl, ns))

	walkErr := &walkError{err: errors.New("unavailable"), namespaces: []string{ns}}
	assert.Equal(t, "unavailable", walkErr.Error())
	assert.ErrorIs(t, walkErr, walkErr.err)
}

func TestProgrammedChanged(t *testing.T) {
	listener := func(conds ...metav1.Condition) *configurationv1alpha1.EventGatewayListener {
		l := &configurationv1alpha1.EventGatewayListener{}
		l.SetConditions(conds)
		return l
	}
	programmed := func(status metav1.ConditionStatus, reason, message string) metav1.Condition {
		return metav1.Condition{Type: string(eventgatewayv1alpha1.OnPremEventGatewayProgrammedType), Status: status, Reason: reason, Message: message}
	}
	update := func(oldObj, newObj client.Object) bool {
		return programmedChanged().Update(event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj})
	}
	deleting := programmed(metav1.ConditionFalse, string(eventgatewayv1alpha1.OnPremEntityGatewayNotProgrammedReason), "being deleted")
	notFound := programmed(metav1.ConditionFalse, string(eventgatewayv1alpha1.OnPremEntityGatewayNotFoundReason), "does not exist")

	assert.True(t, update(listener(notFound), listener(deleting)), "the reason changed")
	assert.True(t, update(listener(deleting), listener(programmed(metav1.ConditionFalse, deleting.Reason, "other"))), "the message changed")
	assert.True(t, update(listener(), listener(deleting)), "set")
	assert.True(t, update(listener(deleting), listener()), "removed")
	assert.False(t, update(listener(), listener()), "none")
	other := metav1.Condition{Type: "Other", Status: metav1.ConditionTrue, Reason: "Other"}
	assert.False(t, update(listener(deleting), listener(deleting, other)), "another condition changed")
	assert.False(t, update(&metav1.PartialObjectMetadata{}, listener(deleting)), "no conditions")

	l := listener(deleting)
	assert.False(t, programmedChanged().Create(event.CreateEvent{Object: l}))
	assert.False(t, programmedChanged().Delete(event.DeleteEvent{Object: l}))
	assert.False(t, programmedChanged().Generic(event.GenericEvent{Object: l}))
}

func TestAncestorEntities(t *testing.T) {
	typeNames := func(ancestors map[reflect.Type]client.Object) []string {
		var names []string
		for t := range ancestors {
			names = append(names, t.Elem().Name())
		}
		return names
	}
	assert.ElementsMatch(t, []string{"EventGatewayVirtualCluster", "EventGatewayBackendCluster"},
		typeNames(ancestorEntities(reflect.TypeFor[*configurationv1alpha1.EventGatewayVirtualClusterPolicy]())))
	assert.ElementsMatch(t, []string{"EventGatewayVirtualCluster", "EventGatewayBackendCluster"},
		typeNames(ancestorEntities(reflect.TypeFor[*configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy]())),
		"a parent consume policy is no parent entity: the walk to the gateway goes through the virtual cluster")
	assert.Empty(t, ancestorEntities(reflect.TypeFor[*configurationv1alpha1.EventGatewayBackendCluster]()))
}

func clusterPolicy(name, vc string) *configurationv1alpha1.EventGatewayVirtualClusterPolicy {
	return &configurationv1alpha1.EventGatewayVirtualClusterPolicy{
		Name: name, Namespace: ns,
		Spec: configurationv1alpha1.EventGatewayVirtualClusterPolicySpec{EventGatewayVirtualClusterRef: namespacedRef(vc)},
	}
}

func consumePolicy(name, vc, parent string) *configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy {
	p := &configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy{
		Name: name, Namespace: ns,
		Spec: configurationv1alpha1.EventGatewayVirtualClusterConsumePolicySpec{EventGatewayVirtualClusterRef: namespacedRef(vc)},
	}
	if parent != "" {
		p.Spec.APISpec.ParentPolicyRef = configurationv1alpha1.EventGatewayVirtualClusterConsumePolicyRef{Name: parent}
	}
	return p
}

func TestMapAncestorToDescendants(t *testing.T) {
	bc := backendCluster(ns, "bc", onPremGatewayRef("gateway"))
	vc1, vc2, otherVC := virtualCluster("vc1", "bc"), virtualCluster("vc2", "bc"), virtualCluster("other", "other-bc")
	p1, p2, otherP := clusterPolicy("p1", "vc1"), clusterPolicy("p2", "vc2"), clusterPolicy("other", "other")
	parent, child, grandchild := consumePolicy("parent", "vc1", ""), consumePolicy("child", "vc1", "parent"), consumePolicy("grandchild", "vc1", "child")
	cl := newFakeClient(t, bc, vc1, vc2, otherVC, p1, p2, otherP, parent, child, grandchild)

	toClusterPolicies := mapAncestorToDescendants(cl, reflect.TypeFor[*configurationv1alpha1.EventGatewayVirtualClusterPolicy]())
	assert.ElementsMatch(t, requestsFor(p1, p2), toClusterPolicies(t.Context(), bc), "the policies of the virtual clusters of the backend cluster")
	assert.Equal(t, requestsFor(p1), toClusterPolicies(t.Context(), vc1))
	assert.Empty(t, toClusterPolicies(t.Context(), listener("l", onPremGatewayRef("gateway"))), "no descendant of the kind")

	toConsumePolicies := mapAncestorToDescendants(cl, reflect.TypeFor[*configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy]())
	assert.ElementsMatch(t, requestsFor(parent, child, grandchild), toConsumePolicies(t.Context(), bc))
	assert.Empty(t, toConsumePolicies(t.Context(), parent), "found through their virtual cluster, not their parent policy")

	toVirtualClusters := mapAncestorToDescendants(cl, reflect.TypeFor[*configurationv1alpha1.EventGatewayVirtualCluster]())
	assert.ElementsMatch(t, requestsFor(vc1, vc2), toVirtualClusters(t.Context(), bc), "the virtual clusters only")

	cl = newFakeClientWithInterceptor(t, interceptor.Funcs{List: failingList})
	assert.Empty(t, mapAncestorToDescendants(cl, reflect.TypeFor[*configurationv1alpha1.EventGatewayVirtualClusterPolicy]())(t.Context(), bc))
}

func TestMapGatewayToEntities(t *testing.T) {
	gateway := newTestGateway("gateway")
	bc, otherBC := backendCluster(ns, "bc", onPremGatewayRef("gateway")), backendCluster(ns, "other-bc", onPremGatewayRef("other"))
	vc, otherVC := virtualCluster("vc", "bc"), virtualCluster("other-vc", "other-bc")
	policy, otherPolicy := clusterPolicy("policy", "vc"), clusterPolicy("other-policy", "other-vc")
	l, otherL := listener("l", onPremGatewayRef("gateway")), listener("other-l", onPremGatewayRef("other"))
	cl := newFakeClient(t, bc, otherBC, vc, otherVC, policy, otherPolicy, l, otherL)

	for _, tc := range []struct {
		target reflect.Type
		want   []reconcile.Request
	}{
		{target: reflect.TypeFor[*configurationv1alpha1.EventGatewayBackendCluster](), want: requestsFor(bc)},
		{target: reflect.TypeFor[*configurationv1alpha1.EventGatewayListener](), want: requestsFor(l)},
		{target: reflect.TypeFor[*configurationv1alpha1.EventGatewayVirtualCluster](), want: requestsFor(vc)},
		{target: reflect.TypeFor[*configurationv1alpha1.EventGatewayVirtualClusterPolicy](), want: requestsFor(policy)},
		{target: reflect.TypeFor[*configurationv1alpha1.EventGatewayStaticKey]()},
	} {
		t.Run(tc.target.Elem().Name(), func(t *testing.T) {
			assert.ElementsMatch(t, tc.want, mapGatewayToEntities(cl, tc.target)(t.Context(), gateway))
		})
	}

	t.Run("a failure to list is skipped", func(t *testing.T) {
		cl := newFakeClientWithInterceptor(t, interceptor.Funcs{List: failingList})
		assert.Empty(t, mapGatewayToEntities(cl, reflect.TypeFor[*configurationv1alpha1.EventGatewayListener]())(t.Context(), gateway))
	})

	t.Run("only the attached kinds leading to the target are listed", func(t *testing.T) {
		var listed []string
		cl := newFakeClientWithInterceptor(t, interceptor.Funcs{
			List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				listed = append(listed, reflect.TypeOf(list).Elem().Name())
				return c.List(ctx, list, opts...)
			},
		}, bc, vc, policy, l)
		assert.Equal(t, requestsFor(policy), mapGatewayToEntities(cl, reflect.TypeFor[*configurationv1alpha1.EventGatewayVirtualClusterPolicy]())(t.Context(), gateway))
		assert.Equal(t, []string{"EventGatewayBackendClusterList", "EventGatewayVirtualClusterList", "EventGatewayVirtualClusterPolicyList"}, listed)

		listed = nil
		assert.Empty(t, mapGatewayToEntities(cl, reflect.TypeFor[*configurationv1alpha1.EventGatewayStaticKey]())(t.Context(), gateway))
		assert.Equal(t, []string{"EventGatewayStaticKeyList"}, listed)
	})
}

func TestEveryEntityKindReachesItsGateway(t *testing.T) {
	attached := map[reflect.Type]bool{}
	for _, a := range attachedEntities {
		attached[reflect.TypeOf(a.list()).Elem()] = true
	}
	for _, entity := range entityTypes() {
		t.Run(reflect.TypeOf(entity).Elem().Name(), func(t *testing.T) {
			listType := reflect.TypeOf(entity).Elem().Name() + "List"
			isAttached := false
			for typ := range attached {
				if typ.Name() == listType {
					isAttached = true
				}
			}
			_, hasParents := parentEntities[reflect.TypeOf(entity)]
			assert.True(t, isAttached || hasParents,
				"an entity kind must be attached to a gateway directly (attachedEntities) or through its parents (parentEntities)")
		})
	}
}

func TestReportUnreachable(t *testing.T) {
	pushed := metav1.Condition{
		Type: "Programmed", Status: metav1.ConditionTrue, Reason: "Programmed", Message: "In the configuration of the OnPremEventGateway default/gateway, pushed to its gw-admin pods",
		ObservedGeneration: 1, LastTransitionTime: metav1.Now(),
	}
	byKonnect := metav1.Condition{
		Type: "Programmed", Status: metav1.ConditionTrue, Reason: "Programmed",
		ObservedGeneration: 1, LastTransitionTime: metav1.Now(),
	}
	detached := metav1.Condition{
		Type: "Programmed", Status: metav1.ConditionFalse, Reason: "Detached",
		Message:            "No longer in the configuration of the OnPremEventGateway default/gateway: it doesn't belong to it anymore, e.g. its parent was deleted",
		ObservedGeneration: 1, LastTransitionTime: metav1.Now(),
	}
	withStatus := func(p *configurationv1alpha1.EventGatewayVirtualClusterPolicy, conditions ...metav1.Condition) *configurationv1alpha1.EventGatewayVirtualClusterPolicy {
		p.Generation = 1
		p.Status.Conditions = conditions
		return p
	}

	tests := []struct {
		name    string
		objects []client.Object
		key     string
		// want is the reason and the message of the Programmed condition, or
		// "" when it is left alone.
		wantReason, wantMessage string
		// wantRemoved tells the Programmed condition is removed.
		wantRemoved bool
	}{
		{
			name: "the backend cluster of the virtual cluster of a policy set for an OnPremEventGateway doesn't exist",
			objects: []client.Object{
				virtualCluster("vc", "bc"), withStatus(clusterPolicy("policy", "vc"), pushed),
			},
			key: "policy", wantReason: "Detached",
			wantMessage: "Not in the configuration of its OnPremEventGateway: the EventGatewayBackendCluster default/bc it descends from does not exist",
		},
		{
			name: "the virtual cluster of a policy set for an OnPremEventGateway doesn't exist",
			objects: []client.Object{
				withStatus(clusterPolicy("policy", "vc"), pushed),
			},
			key: "policy", wantReason: "Detached",
			wantMessage: "Not in the configuration of its OnPremEventGateway: its EventGatewayVirtualCluster default/vc does not exist",
		},
		{
			name: "the parent of a policy the gateway reconcile reported Detached doesn't exist: it is named",
			objects: []client.Object{
				virtualCluster("vc", "bc"), withStatus(clusterPolicy("policy", "vc"), detached),
			},
			key: "policy", wantReason: "Detached",
			wantMessage: "Not in the configuration of its OnPremEventGateway: the EventGatewayBackendCluster default/bc it descends from does not exist",
		},
		{
			name: "the parent of a policy not registered yet doesn't exist",
			objects: []client.Object{
				withStatus(clusterPolicy("policy", "vc"), metav1.Condition{
					Type: "Programmed", Status: metav1.ConditionFalse, Reason: "NotRegistered",
					Message:            "In the configuration of the OnPremEventGateway default/gateway, not registered yet on the gw-admin pods gw-admin-0",
					ObservedGeneration: 1, LastTransitionTime: metav1.Now(),
				}),
			},
			key: "policy", wantReason: "Detached",
			wantMessage: "Not in the configuration of its OnPremEventGateway: its EventGatewayVirtualCluster default/vc does not exist",
		},
		{
			name: "the parent of a policy set by the Konnect controllers with a message naming an OnPremEventGateway doesn't exist",
			objects: []client.Object{
				virtualCluster("vc", "bc"), withStatus(clusterPolicy("policy", "vc"), metav1.Condition{
					Type: "Programmed", Status: metav1.ConditionTrue, Reason: "Programmed",
					Message:            "Programmed, unlike its former OnPremEventGateway",
					ObservedGeneration: 1, LastTransitionTime: metav1.Now(),
				}),
			},
			key: "policy", wantReason: "Programmed",
		},
		{
			name: "the parent of a policy set by the Konnect controllers doesn't exist",
			objects: []client.Object{
				virtualCluster("vc", "bc"), withStatus(clusterPolicy("policy", "vc"), byKonnect),
			},
			key: "policy",
		},
		{
			name: "the OnPremEventGateway doesn't exist",
			objects: []client.Object{
				backendCluster(ns, "bc", onPremGatewayRef("gateway")), virtualCluster("vc", "bc"), withStatus(clusterPolicy("policy", "vc")),
			},
			key: "policy", wantReason: "GatewayNotFound", wantMessage: "The OnPremEventGateway default/gateway does not exist",
		},
		{
			name: "the OnPremEventGateway exists: its reconcile sets the status",
			objects: []client.Object{
				newTestGateway("gateway"), backendCluster(ns, "bc", onPremGatewayRef("gateway")), virtualCluster("vc", "bc"),
				withStatus(clusterPolicy("policy", "vc"), pushed),
			},
			key: "policy", wantReason: "Programmed",
		},
		{
			name: "of a KonnectEventGateway",
			objects: []client.Object{
				backendCluster(ns, "bc", konnectGatewayRef("konnect")), virtualCluster("vc", "bc"), withStatus(clusterPolicy("policy", "vc")),
			},
			key: "policy",
		},
		{
			name: "of a KonnectEventGateway, set by the Konnect controllers",
			objects: []client.Object{
				backendCluster(ns, "bc", konnectGatewayRef("konnect")), virtualCluster("vc", "bc"), withStatus(clusterPolicy("policy", "vc"), byKonnect),
			},
			key: "policy", wantReason: "Programmed",
		},
		{
			name: "now of a KonnectEventGateway, set for an OnPremEventGateway",
			objects: []client.Object{
				backendCluster(ns, "bc", konnectGatewayRef("konnect")), virtualCluster("vc", "bc"),
				withStatus(clusterPolicy("policy", "vc"), metav1.Condition{
					Type: "Programmed", Status: metav1.ConditionFalse, Reason: "GatewayNotFound", Message: "The OnPremEventGateway default/gateway does not exist",
					ObservedGeneration: 1, LastTransitionTime: metav1.Now(),
				}),
			},
			key: "policy", wantRemoved: true,
		},
		{name: "the entity doesn't exist", key: "policy"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cl := newFakeClient(t, tc.objects...)
			r := &Reconciler{Client: cl}
			key := client.ObjectKey{Namespace: ns, Name: tc.key}
			require.NoError(t, r.reportUnreachable(t.Context(), key, &configurationv1alpha1.EventGatewayVirtualClusterPolicy{}))
			var got configurationv1alpha1.EventGatewayVirtualClusterPolicy
			if err := cl.Get(t.Context(), key, &got); err != nil {
				return
			}
			cond := apimeta.FindStatusCondition(got.Status.Conditions, "Programmed")
			if tc.wantRemoved {
				assert.Nil(t, cond)
				return
			}
			if tc.wantReason == "" {
				if cond != nil {
					assert.NotEqual(t, metav1.ConditionFalse, cond.Status, "left alone")
				}
				return
			}
			require.NotNil(t, cond)
			assert.Equal(t, tc.wantReason, cond.Reason)
			if tc.wantMessage != "" {
				assert.Equal(t, tc.wantMessage, cond.Message)
			}
		})
	}

	t.Run("a Detached entity changed since reports its generation and its missing parent", func(t *testing.T) {
		p := withStatus(clusterPolicy("policy", "vc"), metav1.Condition{
			Type: "Programmed", Status: metav1.ConditionFalse, Reason: "Detached",
			Message:            "Not in the configuration of its OnPremEventGateway: its EventGatewayVirtualCluster default/vc does not exist",
			ObservedGeneration: 1, LastTransitionTime: metav1.Now(),
		})
		p.Generation = 2
		p.Spec.EventGatewayVirtualClusterRef.NamespacedRef.Name = "other-vc"
		cl := newFakeClient(t, p)
		r := &Reconciler{Client: cl}
		require.NoError(t, r.reportUnreachable(t.Context(), client.ObjectKeyFromObject(p), &configurationv1alpha1.EventGatewayVirtualClusterPolicy{}))
		cond := apimeta.FindStatusCondition(read(t, cl, p).Status.Conditions, "Programmed")
		require.NotNil(t, cond)
		assert.Equal(t, "Detached", cond.Reason)
		assert.Equal(t, "Not in the configuration of its OnPremEventGateway: its EventGatewayVirtualCluster default/other-vc does not exist", cond.Message)
		assert.Equal(t, int64(2), cond.ObservedGeneration)
	})

	t.Run("an entity being deleted is left alone", func(t *testing.T) {
		p := withStatus(clusterPolicy("policy", "vc"), pushed)
		p.DeletionTimestamp = new(metav1.Now())
		p.Finalizers = []string{"example.com/hold"}
		cl := newFakeClient(t, p)
		r := &Reconciler{Client: cl}
		require.NoError(t, r.reportUnreachable(t.Context(), client.ObjectKeyFromObject(p), &configurationv1alpha1.EventGatewayVirtualClusterPolicy{}))
		assert.True(t, apimeta.IsStatusConditionTrue(read(t, cl, p).Status.Conditions, "Programmed"))
	})

	t.Run("an error reading a parent is returned", func(t *testing.T) {
		p := withStatus(clusterPolicy("policy", "vc"), pushed)
		cl := newFakeClientWithInterceptor(t, interceptor.Funcs{
			Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*configurationv1alpha1.EventGatewayVirtualCluster); ok {
					return errors.New("unavailable")
				}
				return cl.Get(ctx, key, obj, opts...)
			},
		}, p)
		r := &Reconciler{Client: cl}
		require.ErrorContains(t, r.reportUnreachable(t.Context(), client.ObjectKeyFromObject(p), &configurationv1alpha1.EventGatewayVirtualClusterPolicy{}), "unavailable")
	})

	t.Run("an error reading the gateway is returned", func(t *testing.T) {
		l := listener("listener", onPremGatewayRef("gateway"))
		cl := newFakeClientWithInterceptor(t, interceptor.Funcs{
			Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*eventgatewayv1alpha1.OnPremEventGateway); ok {
					return errors.New("unavailable")
				}
				return cl.Get(ctx, key, obj, opts...)
			},
		}, l)
		r := &Reconciler{Client: cl}
		require.ErrorContains(t, r.reportUnreachable(t.Context(), client.ObjectKeyFromObject(l), &configurationv1alpha1.EventGatewayListener{}), "unavailable")
	})
}
