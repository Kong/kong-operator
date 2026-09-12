package configuration

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	ctrlref "github.com/kong/kong-operator/v2/ingress-controller/internal/controllers/reference"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/util/kubernetes/object"
	"github.com/kong/kong-operator/v2/ingress-controller/pkg/manager/scheme"
	"github.com/kong/kong-operator/v2/ingress-controller/test/mocks"
)

func TestKongPluginReconcilersUpdateProgrammedCondition(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		object    client.Object
		reconcile func(client.Client, *mocks.Dataplane) reconcileFunc
		objectKey k8stypes.NamespacedName
	}{
		{
			name: "KongPlugin",
			object: &configurationv1.KongPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  "default",
					Name:       "plugin",
					Generation: 1,
				},
				PluginName: "rate-limiting",
				Status: configurationv1.KongPluginStatus{
					Conditions: []metav1.Condition{{
						Type:               string(configurationv1.ConditionProgrammed),
						Status:             metav1.ConditionFalse,
						ObservedGeneration: 1,
						Reason:             string(configurationv1.ReasonPending),
						Message:            "Waiting for controller",
					}},
				},
			},
			reconcile: func(cl client.Client, dataplane *mocks.Dataplane) reconcileFunc {
				r := &KongV1KongPluginReconciler{
					Client:            cl,
					Log:               logr.Discard(),
					Scheme:            scheme.Get(),
					DataplaneClient:   dataplane,
					ReferenceIndexers: ctrlref.NewCacheIndexers(logr.Discard()),
				}
				return r.Reconcile
			},
			objectKey: k8stypes.NamespacedName{Namespace: "default", Name: "plugin"},
		},
		{
			name: "KongClusterPlugin",
			object: &configurationv1.KongClusterPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "cluster-plugin",
					Generation: 1,
					Annotations: map[string]string{
						"kubernetes.io/ingress.class": "egress",
					},
				},
				PluginName: "openid-connect",
				Status: configurationv1.KongClusterPluginStatus{
					Conditions: []metav1.Condition{{
						Type:               string(configurationv1.ConditionProgrammed),
						Status:             metav1.ConditionFalse,
						ObservedGeneration: 1,
						Reason:             string(configurationv1.ReasonPending),
						Message:            "Waiting for controller",
					}},
				},
			},
			reconcile: func(cl client.Client, dataplane *mocks.Dataplane) reconcileFunc {
				r := &KongV1KongClusterPluginReconciler{
					Client:                     cl,
					Log:                        logr.Discard(),
					Scheme:                     scheme.Get(),
					DataplaneClient:            dataplane,
					IngressClassName:           "egress",
					DisableIngressClassLookups: true,
					ReferenceIndexers:          ctrlref.NewCacheIndexers(logr.Discard()),
				}
				return r.Reconcile
			},
			objectKey: k8stypes.NamespacedName{Name: "cluster-plugin"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeClient := fakectrlruntimeclient.NewClientBuilder().
				WithScheme(scheme.Get()).
				WithObjects(tc.object).
				WithStatusSubresource(tc.object).
				Build()

			dataplane := &mocks.Dataplane{KubernetesObjectReportsEnabled: true}
			dataplane.SetObjectStatus(tc.object.GetNamespace(), tc.object.GetName(), string(object.ConfigurationStatusSucceeded))

			_, err := tc.reconcile(fakeClient, dataplane)(t.Context(), ctrl.Request{NamespacedName: tc.objectKey})
			require.NoError(t, err)

			updated := tc.object.DeepCopyObject().(client.Object)
			require.NoError(t, fakeClient.Get(t.Context(), tc.objectKey, updated))

			conditions := getProgrammedConditions(t, updated)
			expected := []metav1.Condition{{
				Type:               string(configurationv1.ConditionProgrammed),
				Status:             metav1.ConditionTrue,
				ObservedGeneration: 1,
				Reason:             string(configurationv1.ReasonProgrammed),
				Message:            "Object was successfully configured in Kong.",
			}}
			assert.Empty(t, cmp.Diff(expected, conditions, cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime")))
		})
	}
}

type reconcileFunc func(ctx context.Context, req ctrl.Request) (ctrl.Result, error)

func getProgrammedConditions(t *testing.T, obj client.Object) []metav1.Condition {
	t.Helper()

	switch typed := obj.(type) {
	case *configurationv1.KongPlugin:
		return typed.Status.Conditions
	case *configurationv1.KongClusterPlugin:
		return typed.Status.Conditions
	default:
		t.Fatalf("unexpected object type %T", obj)
		return nil
	}
}
