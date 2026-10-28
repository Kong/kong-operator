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

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	"github.com/kong/kong-operator/v2/ingress-controller/pkg/manager"
	multiinstanceai "github.com/kong/kong-operator/v2/pkg/multiinstance/aigateway"
)

func TestWatchSetShrank(t *testing.T) {
	tests := []struct {
		name     string
		previous []string
		current  []string
		want     bool
	}{
		{
			name:     "same set",
			previous: []string{"default", "team-a"},
			current:  []string{"default", "team-a"},
		},
		{
			name:     "a namespace dropped",
			previous: []string{"default", "team-a"},
			current:  []string{"default"},
			want:     true,
		},
		{
			name:     "a namespace added",
			previous: []string{"default"},
			current:  []string{"default", "team-a"},
		},
		{
			name:    "every namespace narrowed to a list",
			current: []string{"default"},
			want:    true,
		},
		{
			name:     "a list widened to every namespace",
			previous: []string{"default"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, watchSetShrank(tc.previous, tc.current))
		})
	}
}

// programmedAuthStrategy builds an AIGatewayAuthStrategy in ns targeting the
// OnPremAIGateway gw in gwNamespace, with a Programmed=True condition.
func programmedAuthStrategy(name, ns, gwNamespace string) *aiconfigurationv1alpha1.AIGatewayAuthStrategy {
	ent := &aiconfigurationv1alpha1.AIGatewayAuthStrategy{
		Name:       name,
		Namespace:  ns,
		Generation: 2,
		Spec: aiconfigurationv1alpha1.AIGatewayAuthStrategySpec{
			AIGatewayRef: aiconfigurationv1alpha1.AIGatewayRef{
				Group: aiconfigurationv1alpha1.AIGatewayRefGroupOnPrem,
				Kind:  aiconfigurationv1alpha1.AIGatewayRefKindOnPrem,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Namespace: new(gwNamespace),
					Name:      "gw",
				},
			},
		},
	}
	ent.Status.Conditions = []metav1.Condition{{
		Type:               string(configurationv1.ConditionProgrammed),
		Status:             metav1.ConditionTrue,
		Reason:             string(configurationv1.ReasonProgrammed),
		ObservedGeneration: ent.Generation,
	}}
	return ent
}

func TestClearOutOfScopeEntityStatus(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, aiconfigurationv1alpha1.AddToScheme(scheme))
	require.NoError(t, aigatewayv1alpha1.AddToScheme(scheme))

	gwNN := k8stypes.NamespacedName{Namespace: "default", Name: "gw"}
	inScope := programmedAuthStrategy("in-scope", "team-a", "default")
	outOfScope := programmedAuthStrategy("out-of-scope", "team-b", "default")
	otherGateway := programmedAuthStrategy("other-gateway", "team-b", "team-a")

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(inScope, outOfScope, otherGateway).
		WithStatusSubresource(&aiconfigurationv1alpha1.AIGatewayAuthStrategy{}).
		Build()
	r := &Reconciler{Client: cl}

	programmedCondition := func(t *testing.T, key client.ObjectKey) *metav1.Condition {
		t.Helper()
		var got aiconfigurationv1alpha1.AIGatewayAuthStrategy
		require.NoError(t, cl.Get(t.Context(), key, &got))
		return apimeta.FindStatusCondition(got.Status.Conditions, string(configurationv1.ConditionProgrammed))
	}

	require.NoError(t, r.clearOutOfScopeEntityStatus(t.Context(), gwNN, []string{"default", "team-a"}))

	cond := programmedCondition(t, client.ObjectKeyFromObject(outOfScope))
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, string(configurationv1.ReasonInvalid), cond.Reason)
	require.Equal(t, int64(2), cond.ObservedGeneration)

	cond = programmedCondition(t, client.ObjectKeyFromObject(inScope))
	require.Equal(t, metav1.ConditionTrue, cond.Status)

	cond = programmedCondition(t, client.ObjectKeyFromObject(otherGateway))
	require.Equal(t, metav1.ConditionTrue, cond.Status)
}

// TestClearOutOfScopeEntityStatus_WatchingEveryNamespace makes sure that a
// gateway watching every namespace clears nothing.
func TestClearOutOfScopeEntityStatus_WatchingEveryNamespace(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, aiconfigurationv1alpha1.AddToScheme(scheme))

	programmed := programmedAuthStrategy("programmed", "team-b", "default")
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(programmed).
		WithStatusSubresource(&aiconfigurationv1alpha1.AIGatewayAuthStrategy{}).
		Build()
	r := &Reconciler{Client: cl}

	require.NoError(t, r.clearOutOfScopeEntityStatus(t.Context(), k8stypes.NamespacedName{Namespace: "default", Name: "gw"}, nil))

	var got aiconfigurationv1alpha1.AIGatewayAuthStrategy
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(programmed), &got))
	cond := apimeta.FindStatusCondition(got.Status.Conditions, string(configurationv1.ConditionProgrammed))
	require.Equal(t, metav1.ConditionTrue, cond.Status)
}

func TestSyncWatchedNamespaces(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, aiconfigurationv1alpha1.AddToScheme(scheme))
	require.NoError(t, aigatewayv1alpha1.AddToScheme(scheme))

	outOfScope := programmedAuthStrategy("out-of-scope", "team-b", "default")
	onprem := &aigatewayv1alpha1.OnPremAIGateway{
		Name:      "gw",
		Namespace: "default",
		UID:       "gateway-uid",
		Annotations: map[string]string{
			watchedNamespacesAnnotation: "default,team-b",
		},
	}
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(outOfScope, onprem).
		WithStatusSubresource(&aiconfigurationv1alpha1.AIGatewayAuthStrategy{}).
		Build()
	mgrID, err := manager.NewID(string(onprem.UID))
	require.NoError(t, err)

	r := &Reconciler{
		Client:           cl,
		InstancesManager: multiinstanceai.NewManager(logr.Discard()),
	}

	recorded := func(t *testing.T) string {
		t.Helper()
		var got aigatewayv1alpha1.OnPremAIGateway
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(onprem), &got))
		return got.Annotations[watchedNamespacesAnnotation]
	}

	// The set narrowed: the entity of the dropped namespace loses its Programmed
	// condition and the recorded set follows the spec.
	require.NoError(t, r.syncWatchedNamespaces(t.Context(), logr.Discard(), mgrID, onprem, []string{"default"}))
	require.Equal(t, "default", recorded(t))

	var got aiconfigurationv1alpha1.AIGatewayAuthStrategy
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(outOfScope), &got))
	cond := apimeta.FindStatusCondition(got.Status.Conditions, string(configurationv1.ConditionProgrammed))
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)

	// Watching every namespace is recorded as such.
	require.NoError(t, r.syncWatchedNamespaces(t.Context(), logr.Discard(), mgrID, onprem, nil))
	require.Equal(t, watchedNamespacesAll, recorded(t))
}

// TestSyncWatchedNamespaces_NoRecord makes sure that a gateway whose annotation a
// client dropped still clears the entities its watch set leaves out.
func TestSyncWatchedNamespaces_NoRecord(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, aiconfigurationv1alpha1.AddToScheme(scheme))
	require.NoError(t, aigatewayv1alpha1.AddToScheme(scheme))

	outOfScope := programmedAuthStrategy("out-of-scope", "team-b", "default")
	onprem := &aigatewayv1alpha1.OnPremAIGateway{
		Name:      "gw",
		Namespace: "default",
		UID:       "gateway-uid",
	}
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(outOfScope, onprem).
		WithStatusSubresource(&aiconfigurationv1alpha1.AIGatewayAuthStrategy{}).
		Build()
	mgrID, err := manager.NewID(string(onprem.UID))
	require.NoError(t, err)

	r := &Reconciler{
		Client:           cl,
		InstancesManager: multiinstanceai.NewManager(logr.Discard()),
	}

	require.NoError(t, r.syncWatchedNamespaces(t.Context(), logr.Discard(), mgrID, onprem, []string{"default"}))

	var got aiconfigurationv1alpha1.AIGatewayAuthStrategy
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(outOfScope), &got))
	cond := apimeta.FindStatusCondition(got.Status.Conditions, string(configurationv1.ConditionProgrammed))
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
}

// TestClearOutOfScopeEntityStatus_Credential makes sure that a credential is
// cleared through the consumer it references: it carries no aiGatewayRef of its
// own.
func TestClearOutOfScopeEntityStatus_Credential(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, aiconfigurationv1alpha1.AddToScheme(scheme))

	programmedCredential := func(consumerName, ns string) *aiconfigurationv1alpha1.AIGatewayConsumerCredential {
		cred := &aiconfigurationv1alpha1.AIGatewayConsumerCredential{
			Name:       consumerName + "-cred",
			Namespace:  ns,
			Generation: 3,
			Spec: aiconfigurationv1alpha1.AIGatewayConsumerCredentialSpec{
				AIGatewayConsumerRef: commonv1alpha1.ObjectRef{
					NamespacedRef: &commonv1alpha1.NamespacedRef{Name: consumerName},
				},
			},
		}
		cred.Status.Conditions = []metav1.Condition{{
			Type:               string(configurationv1.ConditionProgrammed),
			Status:             metav1.ConditionTrue,
			Reason:             string(configurationv1.ReasonProgrammed),
			ObservedGeneration: cred.Generation,
		}}
		return cred
	}

	consumer := func(name, ns, gwNamespace string) *aiconfigurationv1alpha1.AIGatewayConsumer {
		return &aiconfigurationv1alpha1.AIGatewayConsumer{
			Name:      name,
			Namespace: ns,
			Spec: aiconfigurationv1alpha1.AIGatewayConsumerSpec{
				AIGatewayRef: programmedAuthStrategy(name, ns, gwNamespace).Spec.AIGatewayRef,
			},
		}
	}

	outOfScope := programmedCredential("consumer", "team-b")
	otherGateway := programmedCredential("other-consumer", "team-b")
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(
			outOfScope,
			otherGateway,
			consumer("consumer", "team-b", "default"),
			consumer("other-consumer", "team-b", "team-a"),
		).
		WithStatusSubresource(&aiconfigurationv1alpha1.AIGatewayConsumerCredential{}).
		Build()
	r := &Reconciler{Client: cl}

	require.NoError(t, r.clearOutOfScopeEntityStatus(
		t.Context(),
		k8stypes.NamespacedName{Namespace: "default", Name: "gw"},
		[]string{"default"},
	))

	programmed := func(t *testing.T, key client.ObjectKey) metav1.ConditionStatus {
		t.Helper()
		var got aiconfigurationv1alpha1.AIGatewayConsumerCredential
		require.NoError(t, cl.Get(t.Context(), key, &got))
		cond := apimeta.FindStatusCondition(got.Status.Conditions, string(configurationv1.ConditionProgrammed))
		require.NotNil(t, cond)
		return cond.Status
	}

	require.Equal(t, metav1.ConditionFalse, programmed(t, client.ObjectKeyFromObject(outOfScope)))
	require.Equal(t, metav1.ConditionTrue, programmed(t, client.ObjectKeyFromObject(otherGateway)))
}
