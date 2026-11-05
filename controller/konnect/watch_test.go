package konnect

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kong-operator/v2/api/configuration/v1beta1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/internal/utils/index"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

func TestWatchOptions(t *testing.T) {
	tests := []struct {
		name                string
		expectedOptionCount int
		watchOptions        func(client.Client) []func(*ctrl.Builder) *ctrl.Builder
	}{
		{
			name:                "KongConsumerGroup",
			expectedOptionCount: 4,
			watchOptions: func(cl client.Client) []func(*ctrl.Builder) *ctrl.Builder {
				return ReconciliationWatchOptionsForEntity(cl, &configurationv1beta1.KongConsumerGroup{})
			},
		},
		{
			name:                "KongConsumer",
			expectedOptionCount: 6,
			watchOptions: func(cl client.Client) []func(*ctrl.Builder) *ctrl.Builder {
				return ReconciliationWatchOptionsForEntity(cl, &configurationv1.KongConsumer{})
			},
		},
		{
			name:                "KongRoute",
			expectedOptionCount: 4,
			watchOptions: func(cl client.Client) []func(*ctrl.Builder) *ctrl.Builder {
				return ReconciliationWatchOptionsForEntity(cl, &configurationv1alpha1.KongRoute{})
			},
		},
		{
			name:                "KongService",
			expectedOptionCount: 7,
			watchOptions: func(cl client.Client) []func(*ctrl.Builder) *ctrl.Builder {
				return ReconciliationWatchOptionsForEntity(cl, &configurationv1alpha1.KongService{})
			},
		},
		{
			name:                "KonnectGatewayControlPlane",
			expectedOptionCount: 4,
			watchOptions: func(cl client.Client) []func(*ctrl.Builder) *ctrl.Builder {
				return ReconciliationWatchOptionsForEntity(cl, &konnectv1alpha2.KonnectGatewayControlPlane{})
			},
		},
		{
			name:                "KonnectCloudGatewayNetwork",
			expectedOptionCount: 2,
			watchOptions: func(cl client.Client) []func(*ctrl.Builder) *ctrl.Builder {
				return ReconciliationWatchOptionsForEntity(cl, &konnectv1alpha1.KonnectCloudGatewayNetwork{})
			},
		},
		{
			name:                "KonnectCloudGatewayDataPlaneGroupConfiguration",
			expectedOptionCount: 3,
			watchOptions: func(cl client.Client) []func(*ctrl.Builder) *ctrl.Builder {
				return ReconciliationWatchOptionsForEntity(cl, &konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration{})
			},
		},
		{
			name:                "KonnectCloudGatewayTransitGateway",
			expectedOptionCount: 2,
			watchOptions: func(cl client.Client) []func(*ctrl.Builder) *ctrl.Builder {
				return ReconciliationWatchOptionsForEntity(cl, &konnectv1alpha1.KonnectCloudGatewayTransitGateway{})
			},
		},
		{
			// Regression: this entity has the largest handwritten watch set. A
			// dropped option or an empty result must fail the exact-count and
			// non-empty assertions below.
			name:                "KongPluginBinding",
			expectedOptionCount: 9,
			watchOptions: func(cl client.Client) []func(*ctrl.Builder) *ctrl.Builder {
				return ReconciliationWatchOptionsForEntity(cl, &configurationv1alpha1.KongPluginBinding{})
			},
		},
		{
			name:                "KongUpstream",
			expectedOptionCount: 5,
			watchOptions: func(cl client.Client) []func(*ctrl.Builder) *ctrl.Builder {
				return ReconciliationWatchOptionsForEntity(cl, &configurationv1alpha1.KongUpstream{})
			},
		},
		{
			name:                "KongCredentialBasicAuth",
			expectedOptionCount: 4,
			watchOptions: func(cl client.Client) []func(*ctrl.Builder) *ctrl.Builder {
				return ReconciliationWatchOptionsForEntity(cl, &configurationv1alpha1.KongCredentialBasicAuth{})
			},
		},
		{
			name:                "KongCredentialAPIKey",
			expectedOptionCount: 4,
			watchOptions: func(cl client.Client) []func(*ctrl.Builder) *ctrl.Builder {
				return ReconciliationWatchOptionsForEntity(cl, &configurationv1alpha1.KongCredentialAPIKey{})
			},
		},
		{
			name:                "KongCredentialACL",
			expectedOptionCount: 4,
			watchOptions: func(cl client.Client) []func(*ctrl.Builder) *ctrl.Builder {
				return ReconciliationWatchOptionsForEntity(cl, &configurationv1alpha1.KongCredentialACL{})
			},
		},
		{
			name:                "KongCredentialJWT",
			expectedOptionCount: 4,
			watchOptions: func(cl client.Client) []func(*ctrl.Builder) *ctrl.Builder {
				return ReconciliationWatchOptionsForEntity(cl, &configurationv1alpha1.KongCredentialJWT{})
			},
		},
		{
			name:                "KongCredentialHMAC",
			expectedOptionCount: 4,
			watchOptions: func(cl client.Client) []func(*ctrl.Builder) *ctrl.Builder {
				return ReconciliationWatchOptionsForEntity(cl, &configurationv1alpha1.KongCredentialHMAC{})
			},
		},
		{
			name:                "KongCACertificate",
			expectedOptionCount: 5,
			watchOptions: func(cl client.Client) []func(*ctrl.Builder) *ctrl.Builder {
				return ReconciliationWatchOptionsForEntity(cl, &configurationv1alpha1.KongCACertificate{})
			},
		},
		{
			name:                "KongCertificate",
			expectedOptionCount: 5,
			watchOptions: func(cl client.Client) []func(*ctrl.Builder) *ctrl.Builder {
				return ReconciliationWatchOptionsForEntity(cl, &configurationv1alpha1.KongCertificate{})
			},
		},
		{
			name:                "KongTarget",
			expectedOptionCount: 3,
			watchOptions: func(cl client.Client) []func(*ctrl.Builder) *ctrl.Builder {
				return ReconciliationWatchOptionsForEntity(cl, &configurationv1alpha1.KongTarget{})
			},
		},
		{
			name:                "KongVault",
			expectedOptionCount: 5,
			watchOptions: func(cl client.Client) []func(*ctrl.Builder) *ctrl.Builder {
				return ReconciliationWatchOptionsForEntity(cl, &configurationv1alpha1.KongVault{})
			},
		},
		{
			name:                "KongKey",
			expectedOptionCount: 5,
			watchOptions: func(cl client.Client) []func(*ctrl.Builder) *ctrl.Builder {
				return ReconciliationWatchOptionsForEntity(cl, &configurationv1alpha1.KongKey{})
			},
		},
		{
			name:                "KongKeySet",
			expectedOptionCount: 4,
			watchOptions: func(cl client.Client) []func(*ctrl.Builder) *ctrl.Builder {
				return ReconciliationWatchOptionsForEntity(cl, &configurationv1alpha1.KongKeySet{})
			},
		},
		{
			name:                "KongSNI",
			expectedOptionCount: 3,
			watchOptions: func(cl client.Client) []func(*ctrl.Builder) *ctrl.Builder {
				return ReconciliationWatchOptionsForEntity(cl, &configurationv1alpha1.KongSNI{})
			},
		},
		{
			name:                "KongDataPlaneClientCertificate",
			expectedOptionCount: 4,
			watchOptions: func(cl client.Client) []func(*ctrl.Builder) *ctrl.Builder {
				return ReconciliationWatchOptionsForEntity(cl, &configurationv1alpha1.KongDataPlaneClientCertificate{})
			},
		},
		{
			name:                "MCPServer",
			expectedOptionCount: 2,
			watchOptions: func(cl client.Client) []func(*ctrl.Builder) *ctrl.Builder {
				return ReconciliationWatchOptionsForEntity(cl, &konnectv1alpha1.MCPServer{})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertWatchOptions(t, tt.watchOptions, tt.expectedOptionCount)
		})
	}

	t.Run("generated fallback", func(t *testing.T) {
		watchOptions := ReconciliationWatchOptionsForEntity(
			newFakeClient(t),
			&konnectv1alpha1.KonnectEventGateway{},
		)
		assertWatchOptionsApplicable(t, watchOptions)
	})
}

func assertWatchOptions(
	t *testing.T,
	watchOptionsForEntity func(client.Client) []func(*ctrl.Builder) *ctrl.Builder,
	expectedOptionCount int,
) {
	t.Helper()

	watchOptions := watchOptionsForEntity(newFakeClient(t))
	// A dropped option silently disables a reconciliation trigger, so assert
	// both that the primary watch is present and that the exact option set is
	// unchanged. The count includes the primary For option.
	require.NotEmpty(t, watchOptions, "entity must expose at least its primary watch")
	require.Len(t, watchOptions, expectedOptionCount)
	assertWatchOptionsApplicable(t, watchOptions)
}

func newFakeClient(t *testing.T) client.Client {
	t.Helper()

	cl := fakectrlruntimeclient.NewClientBuilder().
		WithScheme(scheme.Get()).
		Build()
	require.NotNil(t, cl)
	return cl
}

func assertWatchOptionsApplicable(
	t *testing.T,
	watchOptions []func(*ctrl.Builder) *ctrl.Builder,
) {
	t.Helper()

	require.NotEmpty(t, watchOptions, "entity must expose at least its primary watch")

	for i, watchOption := range watchOptions {
		t.Run(fmt.Sprintf("watch_option_%d", i), func(t *testing.T) {
			require.NotNil(t, watchOption)

			bldr := ctrl.NewControllerManagedBy(nil)
			require.NotNil(t, bldr)
			var applied *ctrl.Builder
			require.NotPanics(t, func() {
				applied = watchOption(bldr)
			})
			require.NotNil(t, applied, "watch option must return the configured builder")
			require.Same(t, bldr, applied, "watch option must return the same builder")
		})
	}
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
						Name:      "consumer1",
						Namespace: "default",
					},
					{
						Name:      "consumer2",
						Namespace: "default",
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
	backendCluster := &configurationv1alpha1.EventGatewayBackendCluster{
		APIVersion: configurationv1alpha1.GroupVersion.String(),
		Kind:       "EventGatewayBackendCluster",
		Name:       "backend-cluster",
		Namespace:  "default",
	}

	matching := &configurationv1alpha1.EventGatewayVirtualCluster{
		Name:      "matching-virtual-cluster",
		Namespace: "default",
		Spec: configurationv1alpha1.EventGatewayVirtualClusterSpec{
			EventGatewayBackendClusterRef: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: backendCluster.Name,
				},
			},
		},
	}
	nonMatching := &configurationv1alpha1.EventGatewayVirtualCluster{
		Name:      "other-virtual-cluster",
		Namespace: "default",
		Spec: configurationv1alpha1.EventGatewayVirtualClusterSpec{
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
			Name:      matching.Name,
			Namespace: matching.Namespace,
		},
	}, requests)
}

func TestEnqueueObjectForKonnectGatewayControlPlane(t *testing.T) {
	cp := &konnectv1alpha2.KonnectGatewayControlPlane{
		APIVersion: konnectv1alpha1.GroupVersion.String(),
		Kind:       "KonnectGatewayControlPlane",
		Name:       "test1",
		Namespace:  "default",
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
						Name:      "consumer1",
						Namespace: "default",
					},
					&configurationv1.KongConsumer{
						Name:      "consumer2",
						Namespace: "default",
					},
				},
			},
			{
				name:  "1 KongConsumer refers to KonnectGatewayControlPlane",
				index: index.IndexFieldKongConsumerOnKonnectGatewayControlPlane,
				list: []client.Object{
					&configurationv1.KongConsumer{
						Name:      "consumer1",
						Namespace: "default",
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
						Name:      "consumer2",
						Namespace: "default",
					},
				},
				expected: []ctrl.Request{
					{
						Name:      "consumer1",
						Namespace: "default",
					},
				},
			},
			{
				name:  "1 KongConsumer refers to a different KonnectGatewayControlPlane",
				index: index.IndexFieldKongConsumerOnKonnectGatewayControlPlane,
				list: []client.Object{
					&configurationv1.KongConsumer{
						Name:      "consumer1",
						Namespace: "default",
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
						Name:      "consumer2",
						Namespace: "default",
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
		APIVersion: konnectv1alpha1.GroupVersion.String(),
		Kind:       "KonnectEventGateway",
		Name:       "event-gateway",
		Namespace:  "default",
	}

	tests := []struct {
		name     string
		objects  []client.Object
		expected []ctrl.Request
	}{
		{
			name: "no matching backend clusters",
			objects: []client.Object{
				&configurationv1alpha1.EventGatewayBackendCluster{
					Name:      "backend-cluster-1",
					Namespace: "default",
				},
				&configurationv1alpha1.EventGatewayBackendCluster{
					Name:      "backend-cluster-2",
					Namespace: "default",
					Spec: configurationv1alpha1.EventGatewayBackendClusterSpec{
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
				&configurationv1alpha1.EventGatewayBackendCluster{
					Name:      "backend-cluster-1",
					Namespace: "default",
					Spec: configurationv1alpha1.EventGatewayBackendClusterSpec{
						GatewayRef: commonv1alpha1.ObjectRef{
							Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
							NamespacedRef: &commonv1alpha1.NamespacedRef{
								Name: parent.Name,
							},
						},
					},
				},
				&configurationv1alpha1.EventGatewayBackendCluster{
					Name:      "backend-cluster-2",
					Namespace: "default",
					Spec: configurationv1alpha1.EventGatewayBackendClusterSpec{
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
					Name:      "backend-cluster-1",
					Namespace: "default",
				},
			},
		},
		{
			name: "cross-namespace matching backend cluster",
			objects: []client.Object{
				&configurationv1alpha1.EventGatewayBackendCluster{
					Name:      "backend-cluster-1",
					Namespace: "other-ns",
					Spec: configurationv1alpha1.EventGatewayBackendClusterSpec{
						GatewayRef: commonv1alpha1.ObjectRef{
							Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
							NamespacedRef: &commonv1alpha1.NamespacedRef{
								Name:      parent.Name,
								Namespace: new(parent.Namespace),
							},
						},
					},
				},
				&configurationv1alpha1.EventGatewayBackendCluster{
					Name:      "backend-cluster-2",
					Namespace: "other-ns",
					Spec: configurationv1alpha1.EventGatewayBackendClusterSpec{
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
					Name:      "backend-cluster-1",
					Namespace: "other-ns",
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
		APIVersion: konnectv1alpha1.GroupVersion.String(),
		Kind:       "Portal",
		Name:       "portal",
		Namespace:  "target-ns",
	}

	builder := fakectrlruntimeclient.NewClientBuilder().
		WithScheme(scheme.Get()).
		WithObjects(
			portal,
			&konnectv1alpha1.PortalPage{
				Name:      "matching-page",
				Namespace: "source-ns",
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
				Name:      "non-matching-page",
				Namespace: "source-ns",
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
			Name:      "matching-page",
			Namespace: "source-ns",
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
					Name:      "grant1",
					Namespace: "target-ns",
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
						Name:      "service1",
						Namespace: "other-ns",
					},
				},
				expected: []ctrl.Request{},
			},
			{
				name: "single matching service",
				grant: &configurationv1alpha1.KongReferenceGrant{
					Name:      "grant1",
					Namespace: "target-ns",
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
						Name:      "service1",
						Namespace: "source-ns",
					},
				},
				expected: []ctrl.Request{
					{
						Name:      "service1",
						Namespace: "source-ns",
					},
				},
			},
			{
				name: "multiple matching services from same namespace",
				grant: &configurationv1alpha1.KongReferenceGrant{
					Name:      "grant1",
					Namespace: "target-ns",
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
						Name:      "service1",
						Namespace: "source-ns",
					},
					&configurationv1alpha1.KongService{
						Name:      "service2",
						Namespace: "source-ns",
					},
					&configurationv1alpha1.KongService{
						Name:      "service3",
						Namespace: "other-ns",
					},
				},
				expected: []ctrl.Request{
					{
						Name:      "service1",
						Namespace: "source-ns",
					},
					{
						Name:      "service2",
						Namespace: "source-ns",
					},
				},
			},
			{
				name: "multiple from namespaces",
				grant: &configurationv1alpha1.KongReferenceGrant{
					Name:      "grant1",
					Namespace: "target-ns",
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
						Name:      "service1",
						Namespace: "source-ns-1",
					},
					&configurationv1alpha1.KongService{
						Name:      "service2",
						Namespace: "source-ns-2",
					},
					&configurationv1alpha1.KongService{
						Name:      "service3",
						Namespace: "other-ns",
					},
				},
				expected: []ctrl.Request{
					{
						Name:      "service1",
						Namespace: "source-ns-1",
					},
					{
						Name:      "service2",
						Namespace: "source-ns-2",
					},
				},
			},
			{
				name: "from references different kind",
				grant: &configurationv1alpha1.KongReferenceGrant{
					Name:      "grant1",
					Namespace: "target-ns",
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
						Name:      "service1",
						Namespace: "source-ns",
					},
				},
				expected: []ctrl.Request{},
			},
			{
				name: "from references different group",
				grant: &configurationv1alpha1.KongReferenceGrant{
					Name:      "grant1",
					Namespace: "target-ns",
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
						Name:      "service1",
						Namespace: "source-ns",
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
			Name:      "grant1",
			Namespace: "target-ns",
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
					Name:      "page-1",
					Namespace: "source-ns",
				},
				&konnectv1alpha1.PortalPage{
					Name:      "page-2",
					Namespace: "other-ns",
				},
			)
		cl := builder.Build()
		require.NotNil(t, cl)

		requests := enqueueObjectsForKongReferenceGrant[konnectv1alpha1.PortalPageList](cl)(t.Context(), grant)
		require.Equal(t, []ctrl.Request{
			{
				Name:      "page-1",
				Namespace: "source-ns",
			},
		}, requests)
	})

	t.Run("EventGatewayListenerPolicy", func(t *testing.T) {
		grant := &configurationv1alpha1.KongReferenceGrant{
			Name:      "grant1",
			Namespace: "target-ns",
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
				&configurationv1alpha1.EventGatewayListenerPolicy{
					Name:      "policy-1",
					Namespace: "source-ns",
				},
				&configurationv1alpha1.EventGatewayListenerPolicy{
					Name:      "policy-2",
					Namespace: "other-ns",
				},
			)
		cl := builder.Build()
		require.NotNil(t, cl)

		requests := enqueueObjectsForKongReferenceGrant[configurationv1alpha1.EventGatewayListenerPolicyList](cl)(t.Context(), grant)
		require.Equal(t, []ctrl.Request{
			{
				Name:      "policy-1",
				Namespace: "source-ns",
			},
		}, requests)
	})
}
