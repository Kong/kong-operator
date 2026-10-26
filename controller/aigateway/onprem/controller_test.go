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
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	"github.com/kong/kong-operator/v2/pkg/consts"
	multiinstanceai "github.com/kong/kong-operator/v2/pkg/multiinstance/aigateway"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

func TestSetReadySkippingLicenseCondition(t *testing.T) {
	tests := []struct {
		name        string
		conditions  []metav1.Condition
		wantStatus  metav1.ConditionStatus
		wantReason  string
		wantMessage string
	}{
		{
			name:       "no conditions",
			wantStatus: metav1.ConditionTrue,
			wantReason: string(aigatewayv1alpha1.ResourceReadyReason),
		},
		{
			name: "missing license does not gate Ready",
			conditions: []metav1.Condition{
				{
					Type:    string(aigatewayv1alpha1.LicenseValidType),
					Status:  metav1.ConditionFalse,
					Reason:  string(aigatewayv1alpha1.LicenseMissingReason),
					Message: "No enabled KongLicense resource found",
				},
			},
			wantStatus: metav1.ConditionTrue,
			wantReason: string(aigatewayv1alpha1.ResourceReadyReason),
		},
		{
			name: "provided license",
			conditions: []metav1.Condition{
				{
					Type:   string(aigatewayv1alpha1.LicenseValidType),
					Status: metav1.ConditionTrue,
					Reason: string(aigatewayv1alpha1.LicenseValidReason),
				},
			},
			wantStatus: metav1.ConditionTrue,
			wantReason: string(aigatewayv1alpha1.ResourceReadyReason),
		},
		{
			name: "other false condition gates Ready",
			conditions: []metav1.Condition{
				{
					Type:    string(aigatewayv1alpha1.OnPremAIGatewayDataPlanesConfiguredType),
					Status:  metav1.ConditionFalse,
					Reason:  "NotConfigured",
					Message: "no data planes configured",
				},
				{
					Type:   string(aigatewayv1alpha1.LicenseValidType),
					Status: metav1.ConditionTrue,
					Reason: string(aigatewayv1alpha1.LicenseValidReason),
				},
			},
			wantStatus:  metav1.ConditionFalse,
			wantReason:  string(aigatewayv1alpha1.DependenciesNotReadyReason),
			wantMessage: "no data planes configured",
		},
		{
			name: "stale Ready=False is overwritten and ObservedGeneration set",
			conditions: []metav1.Condition{
				{
					Type:   string(aigatewayv1alpha1.ReadyType),
					Status: metav1.ConditionFalse,
					Reason: string(aigatewayv1alpha1.WaitingToBecomeReadyReason),
				},
			},
			wantStatus: metav1.ConditionTrue,
			wantReason: string(aigatewayv1alpha1.ResourceReadyReason),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			onprem := &aigatewayv1alpha1.OnPremAIGateway{
				Generation: 3,
			}
			onprem.SetConditions(tc.conditions)

			setReadySkippingLicenseCondition(onprem)

			ready, ok := k8sutils.GetCondition(aigatewayv1alpha1.ReadyType, onprem)
			require.True(t, ok, "Ready condition not set")
			assert.Equal(t, tc.wantStatus, ready.Status)
			assert.Equal(t, tc.wantReason, ready.Reason)
			assert.Equal(t, tc.wantMessage, ready.Message)
			assert.Equal(t, onprem.GetGeneration(), ready.ObservedGeneration)
		})
	}
}

func TestConfigFromSpec(t *testing.T) {
	tests := []struct {
		name string
		spec aigatewayv1alpha1.OnPremAIGatewaySpec
		want multiinstanceai.Config
	}{
		{
			name: "nil dataplaneSync",
			want: multiinstanceai.Config{},
		},
		{
			name: "nil interval and timeout",
			spec: aigatewayv1alpha1.OnPremAIGatewaySpec{
				DataPlaneSync: &aigatewayv1alpha1.OnPremAIGatewayDataPlaneSync{},
			},
			want: multiinstanceai.Config{},
		},
		{
			name: "interval and timeout set",
			spec: aigatewayv1alpha1.OnPremAIGatewaySpec{
				DataPlaneSync: &aigatewayv1alpha1.OnPremAIGatewayDataPlaneSync{
					IntervalSeconds: new(int32(10)),
					TimeoutSeconds:  new(int32(60)),
				},
			},
			want: multiinstanceai.Config{
				SyncInterval: 10 * time.Second,
				SyncTimeout:  60 * time.Second,
			},
		},
		{
			name: "only interval set",
			spec: aigatewayv1alpha1.OnPremAIGatewaySpec{
				DataPlaneSync: &aigatewayv1alpha1.OnPremAIGatewayDataPlaneSync{
					IntervalSeconds: new(int32(5)),
				},
			},
			want: multiinstanceai.Config{
				SyncInterval: 5 * time.Second,
			},
		},
		{
			name: "only timeout set",
			spec: aigatewayv1alpha1.OnPremAIGatewaySpec{
				DataPlaneSync: &aigatewayv1alpha1.OnPremAIGatewayDataPlaneSync{
					TimeoutSeconds: new(int32(30)),
				},
			},
			want: multiinstanceai.Config{
				SyncTimeout: 30 * time.Second,
			},
		},
		{
			name: "explicit zero interval and timeout",
			spec: aigatewayv1alpha1.OnPremAIGatewaySpec{
				DataPlaneSync: &aigatewayv1alpha1.OnPremAIGatewayDataPlaneSync{
					IntervalSeconds: new(int32(0)),
					TimeoutSeconds:  new(int32(0)),
				},
			},
			want: multiinstanceai.Config{},
		},
		{
			name: "nil objectFilters",
			spec: aigatewayv1alpha1.OnPremAIGatewaySpec{
				DataPlaneSync: &aigatewayv1alpha1.OnPremAIGatewayDataPlaneSync{},
			},
			want: multiinstanceai.Config{},
		},
		{
			name: "objectFilters secrets matchLabels",
			spec: aigatewayv1alpha1.OnPremAIGatewaySpec{
				ObjectFilters: &aigatewayv1alpha1.OnPremAIGatewayObjectFilters{
					Secrets: &aigatewayv1alpha1.OnPremAIGatewayFilterForObjectType{
						MatchLabels: map[string]string{"team": "foo"},
					},
				},
			},
			want: multiinstanceai.Config{
				SecretLabelFilters: map[string]string{"team": "foo"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			onprem := &aigatewayv1alpha1.OnPremAIGateway{Spec: tc.spec}
			watchNamespaces := []string{"default"}
			tc.want.WatchNamespaces = watchNamespaces
			cfg := new(Reconciler).configFromSpec(onprem, watchNamespaces)
			assert.Equal(t, tc.want, cfg)
		})
	}
}

func TestValidateOnPremAIGatewayOptions(t *testing.T) {
	objectFilters := func(matchLabels map[string]string) *aigatewayv1alpha1.OnPremAIGatewayObjectFilters {
		return &aigatewayv1alpha1.OnPremAIGatewayObjectFilters{
			Secrets: &aigatewayv1alpha1.OnPremAIGatewayFilterForObjectType{MatchLabels: matchLabels},
		}
	}

	tests := []struct {
		name                string
		secretLabelSelector string
		objectFilters       *aigatewayv1alpha1.OnPremAIGatewayObjectFilters
		wantValid           bool
		wantMsgContains     string
	}{
		{
			name:      "no objectFilters",
			wantValid: true,
		},
		{
			name:          "no secrets filter",
			objectFilters: &aigatewayv1alpha1.OnPremAIGatewayObjectFilters{},
			wantValid:     true,
		},
		{
			name:          "unrelated matchLabels",
			objectFilters: objectFilters(map[string]string{"team": "foo"}),
			wantValid:     true,
		},
		{
			name:                "matchLabels key conflicts with the operator's secret label selector",
			secretLabelSelector: "app",
			objectFilters:       objectFilters(map[string]string{"team": "foo", "app": "true"}),
			wantValid:           false,
			wantMsgContains:     "operator's secret label filter 'app'",
		},
		{
			name:            "matchLabels key conflicts with the admin client certificate label",
			objectFilters:   objectFilters(map[string]string{consts.SecretOnPremAIGatewayAdminClientCertificateLabel: "true"}),
			wantValid:       false,
			wantMsgContains: "operator-managed label",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := &Reconciler{SecretLabelSelector: tc.secretLabelSelector}
			onprem := &aigatewayv1alpha1.OnPremAIGateway{
				Spec: aigatewayv1alpha1.OnPremAIGatewaySpec{ObjectFilters: tc.objectFilters},
			}
			msg, ok := r.validateOnPremAIGatewayOptions(onprem)
			assert.Equal(t, tc.wantValid, ok)
			if !tc.wantValid {
				assert.Contains(t, msg, tc.wantMsgContains)
			}
		})
	}
}

func TestResolveWatchNamespaces(t *testing.T) {
	tests := []struct {
		name           string
		watchNamespace *aigatewayv1alpha1.OnPremAIGatewayWatchNamespaces
		want           []string
		wantErr        bool
	}{
		{
			name: "unset means own namespace",
			want: []string{"default"},
		},
		{
			name:           "own",
			watchNamespace: &aigatewayv1alpha1.OnPremAIGatewayWatchNamespaces{Type: aigatewayv1alpha1.OnPremAIGatewayWatchNamespacesTypeOwn},
			want:           []string{"default"},
		},
		{
			name:           "all",
			watchNamespace: &aigatewayv1alpha1.OnPremAIGatewayWatchNamespaces{Type: aigatewayv1alpha1.OnPremAIGatewayWatchNamespacesTypeAll},
			want:           nil,
		},
		{
			name: "list adds the own namespace, sorted and deduplicated",
			watchNamespace: &aigatewayv1alpha1.OnPremAIGatewayWatchNamespaces{
				Type: aigatewayv1alpha1.OnPremAIGatewayWatchNamespacesTypeList,
				List: []string{"team-b", "default", "team-a"},
			},
			want: []string{"default", "team-a", "team-b"},
		},
		{
			name:           "unknown type",
			watchNamespace: &aigatewayv1alpha1.OnPremAIGatewayWatchNamespaces{Type: "bogus"},
			wantErr:        true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			onprem := &aigatewayv1alpha1.OnPremAIGateway{
				Namespace: "default",
				Spec:      aigatewayv1alpha1.OnPremAIGatewaySpec{WatchNamespaces: tc.watchNamespace},
			}
			got, err := resolveWatchNamespaces(onprem)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestValidateWatchNamespacesScope(t *testing.T) {
	gateway := func(wn *aigatewayv1alpha1.OnPremAIGatewayWatchNamespaces) *aigatewayv1alpha1.OnPremAIGateway {
		return &aigatewayv1alpha1.OnPremAIGateway{
			Namespace: "default",
			Spec:      aigatewayv1alpha1.OnPremAIGatewaySpec{WatchNamespaces: wn},
		}
	}

	tests := []struct {
		name               string
		operatorNamespaces []string
		gateway            *aigatewayv1alpha1.OnPremAIGateway
		wantErrContains    string
	}{
		{
			name:    "operator watches all namespaces",
			gateway: gateway(&aigatewayv1alpha1.OnPremAIGatewayWatchNamespaces{Type: aigatewayv1alpha1.OnPremAIGatewayWatchNamespacesTypeAll}),
		},
		{
			name:               "all exceeds a scoped operator",
			operatorNamespaces: []string{"default"},
			gateway:            gateway(&aigatewayv1alpha1.OnPremAIGatewayWatchNamespaces{Type: aigatewayv1alpha1.OnPremAIGatewayWatchNamespacesTypeAll}),
			wantErrContains:    "set to 'all'",
		},
		{
			name:               "own namespace outside the operator scope",
			operatorNamespaces: []string{"other"},
			gateway:            gateway(nil),
			wantErrContains:    `requests namespace "default"`,
		},
		{
			name:               "listed namespace outside the operator scope",
			operatorNamespaces: []string{"default"},
			gateway: gateway(&aigatewayv1alpha1.OnPremAIGatewayWatchNamespaces{
				Type: aigatewayv1alpha1.OnPremAIGatewayWatchNamespacesTypeList,
				List: []string{"team-a"},
			}),
			wantErrContains: `requests namespace "team-a"`,
		},
		{
			name:               "listed namespaces inside the operator scope",
			operatorNamespaces: []string{"default", "team-a"},
			gateway: gateway(&aigatewayv1alpha1.OnPremAIGatewayWatchNamespaces{
				Type: aigatewayv1alpha1.OnPremAIGatewayWatchNamespacesTypeList,
				List: []string{"team-a"},
			}),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateWatchNamespacesScope(tc.gateway, tc.operatorNamespaces)
			if tc.wantErrContains == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.wantErrContains)
		})
	}
}

func TestValidateWatchNamespaceGrants(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, operatorv1alpha1.AddToScheme(scheme))

	grant := &operatorv1alpha1.WatchNamespaceGrant{
		Name: "grant", Namespace: "team-a",
		Spec: operatorv1alpha1.WatchNamespaceGrantSpec{
			From: []operatorv1alpha1.WatchNamespaceGrantFrom{{
				Group:     aigatewayv1alpha1.GroupName,
				Kind:      "OnPremAIGateway",
				Namespace: "default",
			}},
		},
	}
	// A grant in a watched namespace that names a different group must not count.
	otherGroupGrant := &operatorv1alpha1.WatchNamespaceGrant{
		Name: "other-group", Namespace: "team-b",
		Spec: operatorv1alpha1.WatchNamespaceGrantSpec{
			From: []operatorv1alpha1.WatchNamespaceGrantFrom{{
				Group:     "gateway-operator.konghq.com",
				Kind:      "ControlPlane",
				Namespace: "default",
			}},
		},
	}

	onprem := &aigatewayv1alpha1.OnPremAIGateway{
		Name: "gw", Namespace: "default",
	}

	tests := []struct {
		name            string
		watchNamespaces []string
		wantErr         bool
	}{
		{
			name:            "own namespace needs no grant",
			watchNamespaces: []string{"default"},
		},
		{
			name:            "all namespaces need no grant",
			watchNamespaces: nil,
		},
		{
			name:            "granted namespace",
			watchNamespaces: []string{"default", "team-a"},
		},
		{
			name:            "ungranted namespace",
			watchNamespaces: []string{"default", "team-b"},
			wantErr:         true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := &Reconciler{Client: fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(grant, otherGroupGrant).
				Build()}
			err := r.validateWatchNamespaceGrants(t.Context(), onprem, tc.watchNamespaces)
			if tc.wantErr {
				require.ErrorContains(t, err, "WatchNamespaceGrant in namespace team-b")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestMapWatchNamespaceGrantToOnPremAIGateways(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, aigatewayv1alpha1.AddToScheme(scheme))
	require.NoError(t, operatorv1alpha1.AddToScheme(scheme))

	gateway := &aigatewayv1alpha1.OnPremAIGateway{
		Name: "gw", Namespace: "default",
	}
	grant := &operatorv1alpha1.WatchNamespaceGrant{
		Name: "grant", Namespace: "team-a",
		Spec: operatorv1alpha1.WatchNamespaceGrantSpec{
			From: []operatorv1alpha1.WatchNamespaceGrantFrom{{
				Group:     aigatewayv1alpha1.GroupName,
				Kind:      "OnPremAIGateway",
				Namespace: "default",
			}},
		},
	}
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(gateway, grant).
		Build()

	mapFn := mapWatchNamespaceGrantToOnPremAIGateways(cl, logr.Discard())
	requests := mapFn(t.Context(), grant)
	assert.Equal(t, []reconcile.Request{{NamespacedName: client.ObjectKeyFromObject(gateway)}}, requests)

	t.Run("an update enqueues the referents of the old grant too", func(t *testing.T) {
		// Dropping the "from" entry that matched the gateway must re-reconcile it:
		// the new object alone no longer names it.
		editedGrant := grant.DeepCopy()
		editedGrant.Spec.From = nil
		editedGrant.Spec.From = append(editedGrant.Spec.From, operatorv1alpha1.WatchNamespaceGrantFrom{
			Group:     aigatewayv1alpha1.GroupName,
			Kind:      "OnPremAIGateway",
			Namespace: "elsewhere",
		})

		q := workqueue.NewTypedRateLimitingQueue(
			workqueue.DefaultTypedControllerRateLimiter[reconcile.Request](),
		)
		defer q.ShutDown()

		watchNamespaceGrantHandler(cl, logr.Discard()).Update(
			t.Context(),
			event.UpdateEvent{ObjectOld: grant, ObjectNew: editedGrant},
			q,
		)

		require.Equal(t, 1, q.Len())
		req, _ := q.Get()
		assert.Equal(t, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(gateway)}, req)
	})
}
