package konnect

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1 "github.com/kong/kong-operator/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kong-operator/api/configuration/v1beta1"
	konnectv1alpha1 "github.com/kong/kong-operator/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/controller/konnect/constraints"
	"github.com/kong/kong-operator/internal/utils/index"
	"github.com/kong/kong-operator/modules/manager/scheme"
)

func TestWatchOptions(t *testing.T) {
	testReconciliationWatchOptionsForEntity(t, &konnectv1alpha2.KonnectGatewayControlPlane{})
	testReconciliationWatchOptionsForEntity(t, &configurationv1alpha1.KongService{})
	testReconciliationWatchOptionsForEntity(t, &configurationv1.KongConsumer{})
	testReconciliationWatchOptionsForEntity(t, &configurationv1beta1.KongConsumerGroup{})
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

	tt := *ent
	t.Run(tt.GetTypeName(), func(t *testing.T) {
		cl := fakectrlruntimeclient.NewFakeClient()
		require.NotNil(t, cl)
		watchOptions := ReconciliationWatchOptionsForEntity(cl, ent)
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
	cp := &konnectv1alpha2.KonnectGatewayControlPlane{
		TypeMeta: metav1.TypeMeta{
			APIVersion: konnectv1alpha1.GroupVersion.String(),
			Kind:       "KonnectGatewayControlPlane",
		},
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
			extractFunc func(client.Client) client.IndexerFunc
			expected    []ctrl.Request
		}{
			{
				name:  "no ControlPlane reference",
				index: index.IndexFieldKongConsumerOnKonnectGatewayControlPlane,
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
				name:  "1 KongConsumer refers to KonnectGatewayControlPlane",
				index: index.IndexFieldKongConsumerOnKonnectGatewayControlPlane,
				list: []client.Object{
					&configurationv1.KongConsumer{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "consumer1",
							Namespace: "default",
						},
						Spec: configurationv1.KongConsumerSpec{
							ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
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
				name:  "1 KongConsumer refers to a different KonnectGatewayControlPlane",
				index: index.IndexFieldKongConsumerOnKonnectGatewayControlPlane,
				list: []client.Object{
					&configurationv1.KongConsumer{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "consumer1",
							Namespace: "default",
						},
						Spec: configurationv1.KongConsumerSpec{
							ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
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

		builderFunc := func(
			objs []client.Object, cp *konnectv1alpha2.KonnectGatewayControlPlane,
		) *fakectrlruntimeclient.ClientBuilder {
			return fakectrlruntimeclient.NewClientBuilder().
				WithScheme(scheme.Get()).
				WithObjects(append(objs, cp)...)
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				// Build a separate client for indices as we have kind of a chicken-egg problem here. We need a client
				// in the extract function passed to the builder's WithIndex function, but it's the builder that creates
				// the client. So we build the client for indices first (without the index) and then build the client
				// with the index.
				clForIndices := builderFunc(tt.list, cp).Build()
				require.NotNil(t, clForIndices)

				builder := builderFunc(tt.list, cp)
				for _, opt := range index.OptionsForKongConsumer(clForIndices) {
					builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
				}
				cl := builder.Build()
				require.NotNil(t, cl)

				f := enqueueObjectForKonnectGatewayControlPlane[configurationv1.KongConsumerList](cl, tt.index)
				requests := f(t.Context(), cp)
				require.Len(t, requests, len(tt.expected))
				require.Equal(t, tt.expected, requests)
			})
		}
	})
}

func TestEnqueueObjectForKongReferenceGrant(t *testing.T) {
	t.Run("KongService", func(t *testing.T) {
		tests := []struct {
			name     string
			grant    *configurationv1alpha1.KongReferenceGrant
			services []client.Object
			expected []ctrl.Request
		}{
			{
				name: "no matching services",
				grant: &configurationv1alpha1.KongReferenceGrant{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "grant1",
						Namespace: "target-ns",
					},
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     configurationv1alpha1.Group(configurationv1alpha1.GroupVersion.Group),
								Kind:      "KongService",
								Namespace: "source-ns",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "konnect.konghq.com",
								Kind:  "KonnectGatewayControlPlane",
							},
						},
					},
				},
				services: []client.Object{
					&configurationv1alpha1.KongService{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "service1",
							Namespace: "other-ns",
						},
					},
				},
				expected: []ctrl.Request{},
			},
			{
				name: "single matching service",
				grant: &configurationv1alpha1.KongReferenceGrant{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "grant1",
						Namespace: "target-ns",
					},
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     configurationv1alpha1.Group(configurationv1alpha1.GroupVersion.Group),
								Kind:      "KongService",
								Namespace: "source-ns",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "konnect.konghq.com",
								Kind:  "KonnectGatewayControlPlane",
							},
						},
					},
				},
				services: []client.Object{
					&configurationv1alpha1.KongService{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "service1",
							Namespace: "source-ns",
						},
					},
				},
				expected: []ctrl.Request{
					{
						NamespacedName: types.NamespacedName{
							Name:      "service1",
							Namespace: "source-ns",
						},
					},
				},
			},
			{
				name: "multiple matching services from same namespace",
				grant: &configurationv1alpha1.KongReferenceGrant{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "grant1",
						Namespace: "target-ns",
					},
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     configurationv1alpha1.Group(configurationv1alpha1.GroupVersion.Group),
								Kind:      "KongService",
								Namespace: "source-ns",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "konnect.konghq.com",
								Kind:  "KonnectGatewayControlPlane",
							},
						},
					},
				},
				services: []client.Object{
					&configurationv1alpha1.KongService{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "service1",
							Namespace: "source-ns",
						},
					},
					&configurationv1alpha1.KongService{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "service2",
							Namespace: "source-ns",
						},
					},
					&configurationv1alpha1.KongService{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "service3",
							Namespace: "other-ns",
						},
					},
				},
				expected: []ctrl.Request{
					{
						NamespacedName: types.NamespacedName{
							Name:      "service1",
							Namespace: "source-ns",
						},
					},
					{
						NamespacedName: types.NamespacedName{
							Name:      "service2",
							Namespace: "source-ns",
						},
					},
				},
			},
			{
				name: "multiple from namespaces",
				grant: &configurationv1alpha1.KongReferenceGrant{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "grant1",
						Namespace: "target-ns",
					},
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     configurationv1alpha1.Group(configurationv1alpha1.GroupVersion.Group),
								Kind:      "KongService",
								Namespace: "source-ns-1",
							},
							{
								Group:     configurationv1alpha1.Group(configurationv1alpha1.GroupVersion.Group),
								Kind:      "KongService",
								Namespace: "source-ns-2",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "konnect.konghq.com",
								Kind:  "KonnectGatewayControlPlane",
							},
						},
					},
				},
				services: []client.Object{
					&configurationv1alpha1.KongService{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "service1",
							Namespace: "source-ns-1",
						},
					},
					&configurationv1alpha1.KongService{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "service2",
							Namespace: "source-ns-2",
						},
					},
					&configurationv1alpha1.KongService{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "service3",
							Namespace: "other-ns",
						},
					},
				},
				expected: []ctrl.Request{
					{
						NamespacedName: types.NamespacedName{
							Name:      "service1",
							Namespace: "source-ns-1",
						},
					},
					{
						NamespacedName: types.NamespacedName{
							Name:      "service2",
							Namespace: "source-ns-2",
						},
					},
				},
			},
			{
				name: "from references different kind",
				grant: &configurationv1alpha1.KongReferenceGrant{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "grant1",
						Namespace: "target-ns",
					},
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     configurationv1alpha1.Group(configurationv1alpha1.GroupVersion.Group),
								Kind:      "KongConsumer",
								Namespace: "source-ns",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "konnect.konghq.com",
								Kind:  "KonnectGatewayControlPlane",
							},
						},
					},
				},
				services: []client.Object{
					&configurationv1alpha1.KongService{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "service1",
							Namespace: "source-ns",
						},
					},
				},
				expected: []ctrl.Request{},
			},
			{
				name: "from references different group",
				grant: &configurationv1alpha1.KongReferenceGrant{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "grant1",
						Namespace: "target-ns",
					},
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     "other.group",
								Kind:      "KongService",
								Namespace: "source-ns",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "konnect.konghq.com",
								Kind:  "KonnectGatewayControlPlane",
							},
						},
					},
				},
				services: []client.Object{
					&configurationv1alpha1.KongService{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "service1",
							Namespace: "source-ns",
						},
					},
				},
				expected: []ctrl.Request{},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				cl := fakectrlruntimeclient.NewClientBuilder().
					WithScheme(scheme.Get()).
					WithObjects(append(tt.services, tt.grant)...).
					Build()
				require.NotNil(t, cl)

				f := enqueueObjectsForKongReferenceGrant[configurationv1alpha1.KongServiceList](cl)

				requests := f(t.Context(), tt.grant)
				require.Len(t, requests, len(tt.expected))
				require.ElementsMatch(t, tt.expected, requests)
			})
		}
	})
}
