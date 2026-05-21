package konnect

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

func TestGetAPIAuthRef(t *testing.T) {
	const (
		namespace          = "default"
		apiAuthName        = "api-auth"
		portalName         = "portal"
		eventGatewayName   = "event-gateway"
		backendClusterName = "backend-cluster"
		virtualClusterName = "virtual-cluster"
		listenerName       = "listener"
	)

	wantAPIAuth := types.NamespacedName{
		Namespace: namespace,
		Name:      apiAuthName,
	}

	tests := []struct {
		name             string
		objects          []client.Object
		resolve          func(context.Context, client.Client) (types.NamespacedName, error)
		wantNN           types.NamespacedName
		wantErrorContain string
	}{
		{
			name: "portal child resolves API auth via portal parent",
			objects: []client.Object{
				newTestAPIAuthConfiguration(namespace, apiAuthName),
				newTestPortal(namespace, portalName, apiAuthName),
			},
			resolve: func(ctx context.Context, cl client.Client) (types.NamespacedName, error) {
				return getAPIAuthRef(ctx, cl, &konnectv1alpha1.PortalPage{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "portal-page",
						Namespace: namespace,
					},
					Spec: konnectv1alpha1.PortalPageSpec{
						PortalRef: testNamespacedObjectRef(portalName),
					},
				})
			},
			wantNN: wantAPIAuth,
		},
		{
			name: "event gateway child resolves API auth via event gateway parent",
			objects: []client.Object{
				newTestAPIAuthConfiguration(namespace, apiAuthName),
				newTestEventGateway(namespace, eventGatewayName, apiAuthName),
			},
			resolve: func(ctx context.Context, cl client.Client) (types.NamespacedName, error) {
				return getAPIAuthRef(ctx, cl, &konnectv1alpha1.EventGatewayBackendCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      backendClusterName,
						Namespace: namespace,
					},
					Spec: konnectv1alpha1.EventGatewayBackendClusterSpec{
						GatewayRef: testNamespacedObjectRef(eventGatewayName),
					},
				})
			},
			wantNN: wantAPIAuth,
		},
		{
			name: "backend cluster child resolves API auth via backend cluster parent",
			objects: []client.Object{
				newTestAPIAuthConfiguration(namespace, apiAuthName),
				newTestEventGateway(namespace, eventGatewayName, apiAuthName),
				newTestEventGatewayBackendCluster(namespace, backendClusterName, eventGatewayName),
			},
			resolve: func(ctx context.Context, cl client.Client) (types.NamespacedName, error) {
				return getAPIAuthRef(ctx, cl, &konnectv1alpha1.EventGatewayVirtualCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      virtualClusterName,
						Namespace: namespace,
					},
					Spec: konnectv1alpha1.EventGatewayVirtualClusterSpec{
						EventGatewayBackendClusterRef: testNamespacedObjectRef(backendClusterName),
					},
				})
			},
			wantNN: wantAPIAuth,
		},
		{
			name: "virtual cluster child resolves API auth via virtual cluster parent",
			objects: []client.Object{
				newTestAPIAuthConfiguration(namespace, apiAuthName),
				newTestEventGateway(namespace, eventGatewayName, apiAuthName),
				newTestEventGatewayBackendCluster(namespace, backendClusterName, eventGatewayName),
				newTestEventGatewayVirtualCluster(namespace, virtualClusterName, backendClusterName),
			},
			resolve: func(ctx context.Context, cl client.Client) (types.NamespacedName, error) {
				return getAPIAuthRef(ctx, cl, &konnectv1alpha1.EventGatewayVirtualClusterConsumePolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "consume-policy",
						Namespace: namespace,
					},
					Spec: konnectv1alpha1.EventGatewayVirtualClusterConsumePolicySpec{
						EventGatewayVirtualClusterRef: testNamespacedObjectRef(virtualClusterName),
					},
				})
			},
			wantNN: wantAPIAuth,
		},
		{
			name: "listener child resolves API auth via event gateway listener parent",
			objects: []client.Object{
				newTestAPIAuthConfiguration(namespace, apiAuthName),
				newTestEventGateway(namespace, eventGatewayName, apiAuthName),
				newTestEventGatewayListener(namespace, listenerName, eventGatewayName),
			},
			resolve: func(ctx context.Context, cl client.Client) (types.NamespacedName, error) {
				return getAPIAuthRef(ctx, cl, &konnectv1alpha1.EventGatewayListenerPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "listener-policy",
						Namespace: namespace,
					},
					Spec: konnectv1alpha1.EventGatewayListenerPolicySpec{
						EventGatewayListenerRef: testNamespacedObjectRef(listenerName),
					},
				})
			},
			wantNN: wantAPIAuth,
		},
		{
			name: "listener child rejects unsupported ref type",
			resolve: func(ctx context.Context, cl client.Client) (types.NamespacedName, error) {
				return getAPIAuthRef(ctx, cl, &konnectv1alpha1.EventGatewayListenerPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "listener-policy",
						Namespace: namespace,
					},
					Spec: konnectv1alpha1.EventGatewayListenerPolicySpec{
						EventGatewayListenerRef: commonv1alpha1.ObjectRef{
							Type:      commonv1alpha1.ObjectRefTypeKonnectID,
							KonnectID: new("listener-konnect-id"),
						},
					},
				})
			},
			wantErrorContain: "unsupported ref type",
		},
		{
			name: "root entity is unsupported",
			resolve: func(ctx context.Context, cl client.Client) (types.NamespacedName, error) {
				return getAPIAuthRef(ctx, cl, &konnectv1alpha1.KonnectEventGateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:      eventGatewayName,
						Namespace: namespace,
					},
				})
			},
			wantErrorContain: "unsupported entity type",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cl := fake.NewClientBuilder().
				WithScheme(scheme.Get()).
				WithObjects(tc.objects...).
				Build()

			nn, err := tc.resolve(t.Context(), cl)
			if tc.wantErrorContain != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, tc.wantErrorContain)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tc.wantNN, nn)
		})
	}
}

func TestGetAPIAuthRefViaParent(t *testing.T) {
	const (
		namespace        = "default"
		apiAuthName      = "api-auth"
		eventGatewayName = "event-gateway"
		listenerName     = "listener"
		backendName      = "backend-cluster"
	)

	wantAPIAuth := types.NamespacedName{
		Namespace: namespace,
		Name:      apiAuthName,
	}

	tests := []struct {
		name             string
		objects          []client.Object
		resolve          func(context.Context, client.Client) (types.NamespacedName, error)
		wantNN           types.NamespacedName
		wantErrorContain string
	}{
		{
			name: "listener parent resolves API auth",
			objects: []client.Object{
				newTestAPIAuthConfiguration(namespace, apiAuthName),
				newTestEventGateway(namespace, eventGatewayName, apiAuthName),
				newTestEventGatewayListener(namespace, listenerName, eventGatewayName),
			},
			resolve: func(ctx context.Context, cl client.Client) (types.NamespacedName, error) {
				return getAPIAuthRefViaParent[
					konnectv1alpha1.EventGatewayListener,
					konnectv1alpha1.KonnectEventGateway,
				](ctx, cl, &konnectv1alpha1.EventGatewayListenerPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "listener-policy",
						Namespace: namespace,
					},
					Spec: konnectv1alpha1.EventGatewayListenerPolicySpec{
						EventGatewayListenerRef: testNamespacedObjectRef(listenerName),
					},
				})
			},
			wantNN: wantAPIAuth,
		},
		{
			name: "backend cluster parent resolves API auth",
			objects: []client.Object{
				newTestAPIAuthConfiguration(namespace, apiAuthName),
				newTestEventGateway(namespace, eventGatewayName, apiAuthName),
				newTestEventGatewayBackendCluster(namespace, backendName, eventGatewayName),
			},
			resolve: func(ctx context.Context, cl client.Client) (types.NamespacedName, error) {
				return getAPIAuthRefViaParent[
					konnectv1alpha1.EventGatewayBackendCluster,
					konnectv1alpha1.KonnectEventGateway,
				](ctx, cl, &konnectv1alpha1.EventGatewayVirtualCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "virtual-cluster",
						Namespace: namespace,
					},
					Spec: konnectv1alpha1.EventGatewayVirtualClusterSpec{
						EventGatewayBackendClusterRef: testNamespacedObjectRef(backendName),
					},
				})
			},
			wantNN: wantAPIAuth,
		},
		{
			name: "listener parent rejects unsupported ref type",
			resolve: func(ctx context.Context, cl client.Client) (types.NamespacedName, error) {
				return getAPIAuthRefViaParent[
					konnectv1alpha1.EventGatewayListener,
					konnectv1alpha1.KonnectEventGateway,
				](ctx, cl, &konnectv1alpha1.EventGatewayListenerPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "listener-policy",
						Namespace: namespace,
					},
					Spec: konnectv1alpha1.EventGatewayListenerPolicySpec{
						EventGatewayListenerRef: commonv1alpha1.ObjectRef{
							Type:      commonv1alpha1.ObjectRefTypeKonnectID,
							KonnectID: new("listener-konnect-id"),
						},
					},
				})
			},
			wantErrorContain: "unsupported ref type",
		},
		{
			name: "backend cluster parent returns get error when parent is missing",
			objects: []client.Object{
				newTestAPIAuthConfiguration(namespace, apiAuthName),
				newTestEventGateway(namespace, eventGatewayName, apiAuthName),
			},
			resolve: func(ctx context.Context, cl client.Client) (types.NamespacedName, error) {
				return getAPIAuthRefViaParent[
					konnectv1alpha1.EventGatewayBackendCluster,
					konnectv1alpha1.KonnectEventGateway,
				](ctx, cl, &konnectv1alpha1.EventGatewayVirtualCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "virtual-cluster",
						Namespace: namespace,
					},
					Spec: konnectv1alpha1.EventGatewayVirtualClusterSpec{
						EventGatewayBackendClusterRef: testNamespacedObjectRef(backendName),
					},
				})
			},
			wantErrorContain: "failed to get EventGatewayBackendCluster",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cl := fake.NewClientBuilder().
				WithScheme(scheme.Get()).
				WithObjects(tc.objects...).
				Build()

			nn, err := tc.resolve(t.Context(), cl)
			if tc.wantErrorContain != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, tc.wantErrorContain)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tc.wantNN, nn)
		})
	}
}

func newTestAPIAuthConfiguration(namespace, name string) *konnectv1alpha1.KonnectAPIAuthConfiguration { //nolint:unparam
	return &konnectv1alpha1.KonnectAPIAuthConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
}

func newTestPortal(namespace, name, apiAuthName string) *konnectv1alpha1.Portal {
	return &konnectv1alpha1.Portal{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: konnectv1alpha1.PortalSpec{
			KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
				APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
					Name: apiAuthName,
				},
			},
		},
	}
}

func newTestEventGateway(namespace, name, apiAuthName string) *konnectv1alpha1.KonnectEventGateway { //nolint:unparam
	return &konnectv1alpha1.KonnectEventGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: konnectv1alpha1.KonnectEventGatewaySpec{
			KonnectConfiguration: konnectv1alpha2.KonnectConfiguration{
				APIAuthConfigurationRef: konnectv1alpha2.KonnectAPIAuthConfigurationRef{
					Name: apiAuthName,
				},
			},
		},
	}
}

func newTestEventGatewayBackendCluster(namespace, name, gatewayName string) *konnectv1alpha1.EventGatewayBackendCluster {
	return &konnectv1alpha1.EventGatewayBackendCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: konnectv1alpha1.EventGatewayBackendClusterSpec{
			GatewayRef: testNamespacedObjectRef(gatewayName),
		},
	}
}

func newTestEventGatewayVirtualCluster(namespace, name, backendClusterName string) *konnectv1alpha1.EventGatewayVirtualCluster {
	return &konnectv1alpha1.EventGatewayVirtualCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: konnectv1alpha1.EventGatewayVirtualClusterSpec{
			EventGatewayBackendClusterRef: testNamespacedObjectRef(backendClusterName),
		},
	}
}

func newTestEventGatewayListener(namespace, name, gatewayName string) *konnectv1alpha1.EventGatewayListener {
	return &konnectv1alpha1.EventGatewayListener{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: konnectv1alpha1.EventGatewayListenerSpec{
			GatewayRef: testNamespacedObjectRef(gatewayName),
		},
	}
}

func testNamespacedObjectRef(name string) commonv1alpha1.ObjectRef {
	return commonv1alpha1.ObjectRef{
		Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
		NamespacedRef: &commonv1alpha1.NamespacedRef{
			Name: name,
		},
	}
}
