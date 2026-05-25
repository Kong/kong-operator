package konnect

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kong-operator/v2/api/configuration/v1beta1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/controller/konnect/constraints"
	"github.com/kong/kong-operator/v2/internal/utils/index"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
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
	testReconciliationWatchOptionsForEntity(t, &konnectv1alpha1.EventGatewayBackendCluster{})
	testReconciliationWatchOptionsForEntity(t, &konnectv1alpha1.EventGatewayListener{})
	testReconciliationWatchOptionsForEntity(t, &konnectv1alpha1.EventGatewayListenerPolicy{})
	testReconciliationWatchOptionsForEntity(t, &konnectv1alpha1.EventGatewayVirtualCluster{})
	testReconciliationWatchOptionsForEntity(t, &konnectv1alpha1.EventGatewayVirtualClusterConsumePolicy{})
	testReconciliationWatchOptionsForEntity(t, &konnectv1alpha1.EventGatewayVirtualClusterProducePolicy{})
	testReconciliationWatchOptionsForEntity(t, &konnectv1alpha1.KonnectEventDataPlaneCertificate{})
	testReconciliationWatchOptionsForEntity(t, &konnectv1alpha1.KonnectEventGateway{})
	testReconciliationWatchOptionsForEntity(t, &konnectv1alpha1.Portal{})
	testReconciliationWatchOptionsForEntity(t, &konnectv1alpha1.PortalCustomization{})
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

func TestEnqueueEventGatewayVirtualClusterForEventGatewayBackendCluster(t *testing.T) {
	backendCluster := &konnectv1alpha1.EventGatewayBackendCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: konnectv1alpha1.GroupVersion.String(),
			Kind:       "EventGatewayBackendCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "backend-cluster",
			Namespace: "default",
		},
	}

	matching := &konnectv1alpha1.EventGatewayVirtualCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "matching-virtual-cluster",
			Namespace: "default",
		},
		Spec: konnectv1alpha1.EventGatewayVirtualClusterSpec{
			EventGatewayBackendClusterRef: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: backendCluster.Name,
				},
			},
		},
	}
	nonMatching := &konnectv1alpha1.EventGatewayVirtualCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "other-virtual-cluster",
			Namespace: "default",
		},
		Spec: konnectv1alpha1.EventGatewayVirtualClusterSpec{
			EventGatewayBackendClusterRef: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: "different-backend-cluster",
				},
			},
		},
	}

	builderFunc := func(
		objs ...client.Object,
	) *fakectrlruntimeclient.ClientBuilder {
		return fakectrlruntimeclient.NewClientBuilder().
			WithScheme(scheme.Get()).
			WithObjects(append(objs, backendCluster)...)
	}

	clForIndices := builderFunc(matching, nonMatching).Build()
	require.NotNil(t, clForIndices)

	builder := builderFunc(matching, nonMatching)
	for _, opt := range index.OptionsForEventGatewayVirtualCluster() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	cl := builder.Build()
	require.NotNil(t, cl)

	requests := enqueueEventGatewayVirtualClusterForEventGatewayBackendCluster(cl)(t.Context(), backendCluster)
	require.Equal(t, []ctrl.Request{
		{
			NamespacedName: types.NamespacedName{
				Name:      matching.Name,
				Namespace: matching.Namespace,
			},
		},
	}, requests)
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

func TestEnqueueEventGatewayBackendClusterForKonnectEventGateway(t *testing.T) {
	parent := &konnectv1alpha1.KonnectEventGateway{
		TypeMeta: metav1.TypeMeta{
			APIVersion: konnectv1alpha1.GroupVersion.String(),
			Kind:       "KonnectEventGateway",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "event-gateway",
			Namespace: "default",
		},
	}

	tests := []struct {
		name     string
		objects  []client.Object
		expected []ctrl.Request
	}{
		{
			name: "no matching backend clusters",
			objects: []client.Object{
				&konnectv1alpha1.EventGatewayBackendCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "backend-cluster-1",
						Namespace: "default",
					},
				},
				&konnectv1alpha1.EventGatewayBackendCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "backend-cluster-2",
						Namespace: "default",
					},
					Spec: konnectv1alpha1.EventGatewayBackendClusterSpec{
						GatewayRef: commonv1alpha1.ObjectRef{
							Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
							NamespacedRef: &commonv1alpha1.NamespacedRef{
								Name: "other-event-gateway",
							},
						},
					},
				},
			},
		},
		{
			name: "matching backend cluster",
			objects: []client.Object{
				&konnectv1alpha1.EventGatewayBackendCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "backend-cluster-1",
						Namespace: "default",
					},
					Spec: konnectv1alpha1.EventGatewayBackendClusterSpec{
						GatewayRef: commonv1alpha1.ObjectRef{
							Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
							NamespacedRef: &commonv1alpha1.NamespacedRef{
								Name: parent.Name,
							},
						},
					},
				},
				&konnectv1alpha1.EventGatewayBackendCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "backend-cluster-2",
						Namespace: "default",
					},
					Spec: konnectv1alpha1.EventGatewayBackendClusterSpec{
						GatewayRef: commonv1alpha1.ObjectRef{
							Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
							NamespacedRef: &commonv1alpha1.NamespacedRef{
								Name: "other-event-gateway",
							},
						},
					},
				},
			},
			expected: []ctrl.Request{
				{
					NamespacedName: types.NamespacedName{
						Name:      "backend-cluster-1",
						Namespace: "default",
					},
				},
			},
		},
		{
			name: "cross-namespace matching backend cluster",
			objects: []client.Object{
				&konnectv1alpha1.EventGatewayBackendCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "backend-cluster-1",
						Namespace: "other-ns",
					},
					Spec: konnectv1alpha1.EventGatewayBackendClusterSpec{
						GatewayRef: commonv1alpha1.ObjectRef{
							Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
							NamespacedRef: &commonv1alpha1.NamespacedRef{
								Name:      parent.Name,
								Namespace: new(parent.Namespace),
							},
						},
					},
				},
				&konnectv1alpha1.EventGatewayBackendCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "backend-cluster-2",
						Namespace: "other-ns",
					},
					Spec: konnectv1alpha1.EventGatewayBackendClusterSpec{
						GatewayRef: commonv1alpha1.ObjectRef{
							Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
							NamespacedRef: &commonv1alpha1.NamespacedRef{
								Name:      parent.Name,
								Namespace: new("different-ns"),
							},
						},
					},
				},
			},
			expected: []ctrl.Request{
				{
					NamespacedName: types.NamespacedName{
						Name:      "backend-cluster-1",
						Namespace: "other-ns",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := fakectrlruntimeclient.NewClientBuilder().
				WithScheme(scheme.Get()).
				WithObjects(append(tt.objects, parent)...)
			for _, opt := range index.OptionsForEventGatewayBackendCluster() {
				builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
			}
			cl := builder.Build()
			require.NotNil(t, cl)

			requests := enqueueEventGatewayBackendClusterForKonnectEventGateway(cl)(t.Context(), parent)
			require.ElementsMatch(t, tt.expected, requests)
		})
	}
}

func TestEnqueuePortalPageForPortal(t *testing.T) {
	portal := &konnectv1alpha1.Portal{
		TypeMeta: metav1.TypeMeta{
			APIVersion: konnectv1alpha1.GroupVersion.String(),
			Kind:       "Portal",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "portal",
			Namespace: "target-ns",
		},
	}

	builder := fakectrlruntimeclient.NewClientBuilder().
		WithScheme(scheme.Get()).
		WithObjects(
			portal,
			&konnectv1alpha1.PortalPage{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "matching-page",
					Namespace: "source-ns",
				},
				Spec: konnectv1alpha1.PortalPageSpec{
					PortalRef: commonv1alpha1.ObjectRef{
						Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
						NamespacedRef: &commonv1alpha1.NamespacedRef{
							Name:      portal.Name,
							Namespace: new(portal.Namespace),
						},
					},
				},
			},
			&konnectv1alpha1.PortalPage{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "non-matching-page",
					Namespace: "source-ns",
				},
				Spec: konnectv1alpha1.PortalPageSpec{
					PortalRef: commonv1alpha1.ObjectRef{
						Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
						NamespacedRef: &commonv1alpha1.NamespacedRef{
							Name:      portal.Name,
							Namespace: new("different-ns"),
						},
					},
				},
			},
		)
	for _, opt := range index.OptionsForPortalPage() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	cl := builder.Build()
	require.NotNil(t, cl)

	requests := enqueuePortalPageForPortal(cl)(t.Context(), portal)
	require.Equal(t, []ctrl.Request{
		{
			NamespacedName: types.NamespacedName{
				Name:      "matching-page",
				Namespace: "source-ns",
			},
		},
	}, requests)
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

	t.Run("PortalPage", func(t *testing.T) {
		grant := &configurationv1alpha1.KongReferenceGrant{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "grant1",
				Namespace: "target-ns",
			},
			Spec: configurationv1alpha1.KongReferenceGrantSpec{
				From: []configurationv1alpha1.ReferenceGrantFrom{
					{
						Group:     configurationv1alpha1.Group(konnectv1alpha1.GroupVersion.Group),
						Kind:      "PortalPage",
						Namespace: "source-ns",
					},
				},
				To: []configurationv1alpha1.ReferenceGrantTo{
					{
						Group: configurationv1alpha1.Group(konnectv1alpha1.GroupVersion.Group),
						Kind:  "Portal",
					},
				},
			},
		}

		builder := fakectrlruntimeclient.NewClientBuilder().
			WithScheme(scheme.Get()).
			WithObjects(
				grant,
				&konnectv1alpha1.PortalPage{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "page-1",
						Namespace: "source-ns",
					},
				},
				&konnectv1alpha1.PortalPage{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "page-2",
						Namespace: "other-ns",
					},
				},
			)
		cl := builder.Build()
		require.NotNil(t, cl)

		requests := enqueueObjectsForKongReferenceGrant[konnectv1alpha1.PortalPageList](cl)(t.Context(), grant)
		require.Equal(t, []ctrl.Request{
			{
				NamespacedName: types.NamespacedName{
					Name:      "page-1",
					Namespace: "source-ns",
				},
			},
		}, requests)
	})

	t.Run("EventGatewayListenerPolicy", func(t *testing.T) {
		grant := &configurationv1alpha1.KongReferenceGrant{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "grant1",
				Namespace: "target-ns",
			},
			Spec: configurationv1alpha1.KongReferenceGrantSpec{
				From: []configurationv1alpha1.ReferenceGrantFrom{
					{
						Group:     configurationv1alpha1.Group(konnectv1alpha1.GroupVersion.Group),
						Kind:      "EventGatewayListenerPolicy",
						Namespace: "source-ns",
					},
				},
				To: []configurationv1alpha1.ReferenceGrantTo{
					{
						Group: configurationv1alpha1.Group(konnectv1alpha1.GroupVersion.Group),
						Kind:  "EventGatewayListener",
					},
				},
			},
		}

		builder := fakectrlruntimeclient.NewClientBuilder().
			WithScheme(scheme.Get()).
			WithObjects(
				grant,
				&konnectv1alpha1.EventGatewayListenerPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "policy-1",
						Namespace: "source-ns",
					},
				},
				&konnectv1alpha1.EventGatewayListenerPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "policy-2",
						Namespace: "other-ns",
					},
				},
			)
		cl := builder.Build()
		require.NotNil(t, cl)

		requests := enqueueObjectsForKongReferenceGrant[konnectv1alpha1.EventGatewayListenerPolicyList](cl)(t.Context(), grant)
		require.Equal(t, []ctrl.Request{
			{
				NamespacedName: types.NamespacedName{
					Name:      "policy-1",
					Namespace: "source-ns",
				},
			},
		}, requests)
	})
}
