package konnect

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kong/gateway-operator/controller/konnect/constraints"
	"github.com/kong/gateway-operator/modules/manager/scheme"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

func TestWatchOptions(t *testing.T) {
	testReconciliationWatchOptionsForEntity(t, &konnectv1alpha1.KonnectGatewayControlPlane{})
	testReconciliationWatchOptionsForEntity(t, &configurationv1alpha1.KongService{})
	testReconciliationWatchOptionsForEntity(t, &configurationv1.KongConsumer{})
	testReconciliationWatchOptionsForEntity(t, &configurationv1alpha1.KongRoute{})
	testReconciliationWatchOptionsForEntity(t, &configurationv1alpha1.KongCACertificate{})
	testReconciliationWatchOptionsForEntity(t, &configurationv1alpha1.KongCertificate{})
	testReconciliationWatchOptionsForEntity(t, &configurationv1alpha1.KongKey{})
	testReconciliationWatchOptionsForEntity(t, &configurationv1alpha1.KongKeySet{})
}

func testReconciliationWatchOptionsForEntity[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](
	t *testing.T,
	ent TEnt,
) {
	t.Helper()

	var tt T = *ent
	t.Run(tt.GetTypeName(), func(t *testing.T) {
		cl := fakectrlruntimeclient.NewFakeClient()
		require.NotNil(t, cl)
		watchOptions := ReconciliationWatchOptionsForEntity[T, TEnt](cl, ent)
		_ = watchOptions
	})
}

func TestObjectListToReconcileRequests(t *testing.T) {
	t.Run("KongConsumer", func(t *testing.T) {
		tests := []struct {
			name string
			list []configurationv1.KongConsumer
		}{
			{
				name: "KongConsumer",
				list: []configurationv1.KongConsumer{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "consumer1",
							Namespace: "default",
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "consumer2",
							Namespace: "default",
						},
					},
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				requests := objectListToReconcileRequests(tt.list)
				require.Len(t, requests, len(tt.list))
				for i, item := range tt.list {
					require.Equal(t, item.GetName(), requests[i].Name)
					require.Equal(t, item.GetNamespace(), requests[i].Namespace)
				}
			})
		}
	})
}

func TestEnqueueObjectForKonnectGatewayControlPlane(t *testing.T) {
	cp := &konnectv1alpha1.KonnectGatewayControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test1",
			Namespace: "default",
		},
	}
	t.Run("KongConsumer", func(t *testing.T) {
		tests := []struct {
			name        string
			index       string
			list        []client.Object
			extractFunc client.IndexerFunc
			expected    []ctrl.Request
		}{
			{
				name:        "no ControlPlane reference",
				index:       IndexFieldKongConsumerOnKonnectGatewayControlPlane,
				extractFunc: kongConsumerReferencesKonnectGatewayControlPlane,
				list: []client.Object{
					&configurationv1.KongConsumer{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "consumer1",
							Namespace: "default",
						},
					},
					&configurationv1.KongConsumer{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "consumer2",
							Namespace: "default",
						},
					},
				},
			},
			{
				name:        "1 KongConumser refers to KonnectGatewayControlPlane",
				index:       IndexFieldKongConsumerOnKonnectGatewayControlPlane,
				extractFunc: kongConsumerReferencesKonnectGatewayControlPlane,
				list: []client.Object{
					&configurationv1.KongConsumer{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "consumer1",
							Namespace: "default",
						},
						Spec: configurationv1.KongConsumerSpec{
							ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
								Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
								KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
									Name: cp.Name,
								},
							},
						},
					},
					&configurationv1.KongConsumer{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "consumer2",
							Namespace: "default",
						},
					},
				},
				expected: []ctrl.Request{
					{
						NamespacedName: types.NamespacedName{
							Name:      "consumer1",
							Namespace: "default",
						},
					},
				},
			},
			{
				name:        "1 KongConumser refers to a different KonnectGatewayControlPlane",
				index:       IndexFieldKongConsumerOnKonnectGatewayControlPlane,
				extractFunc: kongConsumerReferencesKonnectGatewayControlPlane,
				list: []client.Object{
					&configurationv1.KongConsumer{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "consumer1",
							Namespace: "default",
						},
						Spec: configurationv1.KongConsumerSpec{
							ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
								Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
								KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
									Name: "different-cp",
								},
							},
						},
					},
					&configurationv1.KongConsumer{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "consumer2",
							Namespace: "default",
						},
					},
				},
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				cl := fakectrlruntimeclient.NewClientBuilder().
					WithScheme(scheme.Get()).
					WithObjects(tt.list...).
					WithIndex(&configurationv1.KongConsumer{}, tt.index, tt.extractFunc).
					Build()
				require.NotNil(t, cl)

				f := enqueueObjectForKonnectGatewayControlPlane[configurationv1.KongConsumerList](cl, tt.index)
				requests := f(context.Background(), cp)
				require.Len(t, requests, len(tt.expected))
				require.Equal(t, tt.expected, requests)
			})
		}
	})
}
