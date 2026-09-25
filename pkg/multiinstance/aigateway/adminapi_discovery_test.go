package aigateway

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/event"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	adminapidiscovery "github.com/kong/kong-operator/v2/internal/adminapi"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
)

const (
	testGatewayNamespace = "default"
	testGatewayName      = "gw"
)

func adminAPIEndpointSlice(namespace, serviceName, podName, podIP string) *discoveryv1.EndpointSlice {
	return &discoveryv1.EndpointSlice{
		Name:      serviceName + "-xyz",
		Namespace: namespace,
		Labels:    map[string]string{discoveryv1.LabelServiceName: serviceName},
		OwnerReferences: []metav1.OwnerReference{
			{APIVersion: "v1", Kind: "Service", Name: serviceName},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Ports: []discoveryv1.EndpointPort{
			{Name: new("admin"), Port: new(int32(8444))},
		},
		Endpoints: []discoveryv1.Endpoint{
			{
				Addresses: []string{podIP},
				TargetRef: &corev1.ObjectReference{Kind: "Pod", Namespace: namespace, Name: podName},
			},
		},
	}
}

func aigatewayDataPlane(name, namespace, gatewayName string) *aigatewayv1alpha1.AIGatewayDataPlane {
	dp := &aigatewayv1alpha1.AIGatewayDataPlane{
		Name:      name,
		Namespace: namespace,
	}
	if gatewayName != "" {
		dp.Spec.ControlPlaneRef = &aigatewayv1alpha1.ControlPlaneRef{
			Type:                aigatewayv1alpha1.ControlPlaneRefTypeOnPremNamespacedRef,
			OnPremNamespacedRef: &aigatewayv1alpha1.NamespacedRef{Name: gatewayName},
		}
	}
	return dp
}

func newDiscoveryTestClient(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()
	return fake.NewClientBuilder().
		WithScheme(managerscheme.Get()).
		WithObjects(objs...).
		Build()
}

func TestAdminAPIEndpointsReconciler_Reconcile(t *testing.T) {
	gatewayNN := k8stypes.NamespacedName{Namespace: testGatewayNamespace, Name: testGatewayName}

	tests := []struct {
		name         string
		objects      []client.Object
		wantAdminAPI sets.Set[adminapidiscovery.DiscoveredAdminAPI]
	}{
		{
			name: "discovers endpoints of the referencing data plane's Admin API Service",
			objects: []client.Object{
				aigatewayDataPlane("dp-1", testGatewayNamespace, testGatewayName),
				adminAPIEndpointSlice(testGatewayNamespace, "dp-1-admin", "pod-1", "10.0.0.1"),
			},
			wantAdminAPI: sets.New(
				adminapidiscovery.DiscoveredAdminAPI{
					Address:       "https://10.0.0.1:8444",
					TLSServerName: "pod.dp-1-admin.default.svc",
					PodRef:        k8stypes.NamespacedName{Namespace: testGatewayNamespace, Name: "pod-1"},
				},
			),
		},
		{
			name: "discovered Admin APIs of multiple data planes are unioned",
			objects: []client.Object{
				aigatewayDataPlane("dp-1", testGatewayNamespace, testGatewayName),
				aigatewayDataPlane("dp-2", testGatewayNamespace, testGatewayName),
				adminAPIEndpointSlice(testGatewayNamespace, "dp-1-admin", "pod-1", "10.0.0.1"),
				adminAPIEndpointSlice(testGatewayNamespace, "dp-2-admin", "pod-2", "10.0.0.2"),
			},
			wantAdminAPI: sets.New(
				adminapidiscovery.DiscoveredAdminAPI{
					Address:       "https://10.0.0.1:8444",
					TLSServerName: "pod.dp-1-admin.default.svc",
					PodRef:        k8stypes.NamespacedName{Namespace: testGatewayNamespace, Name: "pod-1"},
				},
				adminapidiscovery.DiscoveredAdminAPI{
					Address:       "https://10.0.0.2:8444",
					TLSServerName: "pod.dp-2-admin.default.svc",
					PodRef:        k8stypes.NamespacedName{Namespace: testGatewayNamespace, Name: "pod-2"},
				},
			),
		},
		{
			name: "data planes referencing a different gateway are not discovered",
			objects: []client.Object{
				aigatewayDataPlane("dp-1", testGatewayNamespace, "other-gateway"),
				adminAPIEndpointSlice(testGatewayNamespace, "dp-1-admin", "pod-1", "10.0.0.1"),
			},
			wantAdminAPI: sets.New[adminapidiscovery.DiscoveredAdminAPI](),
		},
		{
			name: "no data planes reference the gateway",
			objects: []client.Object{
				adminAPIEndpointSlice(testGatewayNamespace, "dp-1-admin", "pod-1", "10.0.0.1"),
			},
			wantAdminAPI: sets.New[adminapidiscovery.DiscoveredAdminAPI](),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var (
				gotAdminAPI sets.Set[adminapidiscovery.DiscoveredAdminAPI]
				called      bool
			)
			r := &AdminAPIEndpointsReconciler{
				Client:     newDiscoveryTestClient(t, tt.objects...),
				GatewayNN:  gatewayNN,
				Discoverer: mustDiscoverer(t),
				Log:        ctrllog.Log,
				OnDiscovery: func(_ context.Context, adminAPIs sets.Set[adminapidiscovery.DiscoveredAdminAPI]) {
					called = true
					gotAdminAPI = adminAPIs
				},
			}

			_, err := r.Reconcile(context.Background(), ctrl.Request{})
			require.NoError(t, err)
			require.True(t, called, "OnDiscovery should be called")
			require.Equal(t, tt.wantAdminAPI, gotAdminAPI)
		})
	}
}

// TestAdminAPIEndpointsReconciler_Reconcile_ReturnsErrorOnDiscoveryFailure verifies
// that a data plane whose Admin API endpoints cannot be discovered fails the whole
// reconciliation: OnDiscovery is not called with a partial set, and the error makes
// controller-runtime requeue. Once the failure clears, the next reconciliation
// notifies with the complete set.
func TestAdminAPIEndpointsReconciler_Reconcile_ReturnsErrorOnDiscoveryFailure(t *testing.T) {
	gatewayNN := k8stypes.NamespacedName{Namespace: testGatewayNamespace, Name: testGatewayName}

	var failDP1List atomic.Bool
	failDP1List.Store(true)
	c := fake.NewClientBuilder().
		WithScheme(managerscheme.Get()).
		WithObjects(
			aigatewayDataPlane("dp-1", testGatewayNamespace, testGatewayName),
			aigatewayDataPlane("dp-2", testGatewayNamespace, testGatewayName),
			adminAPIEndpointSlice(testGatewayNamespace, "dp-2-admin", "pod-2", "10.0.0.2"),
		).
		WithInterceptorFuncs(interceptor.Funcs{
			List: func(
				ctx context.Context,
				parent client.WithWatch,
				list client.ObjectList,
				opts ...client.ListOption,
			) error {
				listOpts := &client.ListOptions{}
				for _, opt := range opts {
					opt.ApplyToList(listOpts)
				}
				// Fail discovery of dp-1's Admin API Service only.
				if failDP1List.Load() &&
					listOpts.LabelSelector != nil &&
					listOpts.LabelSelector.String() == "kubernetes.io/service-name=dp-1-admin" {
					return errors.New("injected list failure")
				}
				return parent.List(ctx, list, opts...)
			},
		}).
		Build()

	var (
		gotAdminAPI sets.Set[adminapidiscovery.DiscoveredAdminAPI]
		called      bool
	)
	r := &AdminAPIEndpointsReconciler{
		Client:     c,
		GatewayNN:  gatewayNN,
		Discoverer: mustDiscoverer(t),
		Log:        ctrllog.Log,
		OnDiscovery: func(_ context.Context, adminAPIs sets.Set[adminapidiscovery.DiscoveredAdminAPI]) {
			called = true
			gotAdminAPI = adminAPIs
		},
	}

	_, err := r.Reconcile(context.Background(), ctrl.Request{})
	require.Error(t, err)
	require.False(t, called, "OnDiscovery must not be called with a partial set")

	// Once the failure clears, the next reconciliation notifies with the
	// complete set.
	failDP1List.Store(false)
	_, err = r.Reconcile(context.Background(), ctrl.Request{})
	require.NoError(t, err)
	require.True(t, called, "OnDiscovery should be called once discovery succeeds")
	require.Equal(t, sets.New(
		adminapidiscovery.DiscoveredAdminAPI{
			Address:       "https://10.0.0.2:8444",
			TLSServerName: "pod.dp-2-admin.default.svc",
			PodRef:        k8stypes.NamespacedName{Namespace: testGatewayNamespace, Name: "pod-2"},
		},
	), gotAdminAPI)
}

func TestAdminAPIEndpointsReconciler_Predicates(t *testing.T) {
	gatewayNN := k8stypes.NamespacedName{Namespace: testGatewayNamespace, Name: testGatewayName}
	r := &AdminAPIEndpointsReconciler{GatewayNN: gatewayNN}

	t.Run("data plane predicate only accepts the gateway's referencing data planes", func(t *testing.T) {
		p := r.gatewayDataPlanePredicate()
		require.True(t, p.Create(createEvent(aigatewayDataPlane("dp-1", testGatewayNamespace, testGatewayName))))
		require.False(t, p.Create(createEvent(aigatewayDataPlane("dp-1", testGatewayNamespace, "other-gateway"))))
		require.False(t, p.Create(createEvent(aigatewayDataPlane("dp-1", "other-namespace", testGatewayName))))
		require.False(t, p.Create(createEvent(aigatewayDataPlane("dp-1", testGatewayNamespace, ""))))
	})

	t.Run("data plane update predicate accepts events where either side references the gateway", func(t *testing.T) {
		p := r.gatewayDataPlanePredicate()
		// Data plane stops referencing the gateway: must trigger rediscovery
		// to drop its stale endpoints.
		require.True(t, p.Update(updateEvent(
			aigatewayDataPlane("dp-1", testGatewayNamespace, testGatewayName),
			aigatewayDataPlane("dp-1", testGatewayNamespace, "other-gateway"),
		)))
		// Data plane starts referencing the gateway.
		require.True(t, p.Update(updateEvent(
			aigatewayDataPlane("dp-1", testGatewayNamespace, "other-gateway"),
			aigatewayDataPlane("dp-1", testGatewayNamespace, testGatewayName),
		)))
		// Neither side references the gateway.
		require.False(t, p.Update(updateEvent(
			aigatewayDataPlane("dp-1", testGatewayNamespace, "other-gateway"),
			aigatewayDataPlane("dp-1", testGatewayNamespace, "another-gateway"),
		)))
	})

	t.Run("endpoint slice predicate only accepts admin service slices in the gateway namespace", func(t *testing.T) {
		p := r.adminAPIEndpointSlicePredicate()
		require.True(t, p.Create(createEvent(adminAPIEndpointSlice(testGatewayNamespace, "dp-1-admin", "pod-1", "10.0.0.1"))))
		// Non-admin Service EndpointSlice.
		require.False(t, p.Create(createEvent(adminAPIEndpointSlice(testGatewayNamespace, "dp-1-ingress", "pod-1", "10.0.0.1"))))
		// Admin Service EndpointSlice in another namespace.
		require.False(t, p.Create(createEvent(adminAPIEndpointSlice("other-namespace", "dp-1-admin", "pod-1", "10.0.0.1"))))
	})
}

// createEvent wraps an object in a Create event for predicate testing.
func createEvent(obj client.Object) event.TypedCreateEvent[client.Object] {
	return event.TypedCreateEvent[client.Object]{Object: obj}
}

// updateEvent wraps an old/new object pair in an Update event for predicate testing.
func updateEvent(oldObj, newObj client.Object) event.TypedUpdateEvent[client.Object] {
	return event.TypedUpdateEvent[client.Object]{ObjectOld: oldObj, ObjectNew: newObj}
}

// mustDiscoverer returns a Discoverer matching the Admin API Service port name
// used by the AIGatewayDataPlane Admin API Services.
func mustDiscoverer(t *testing.T) *adminapidiscovery.Discoverer {
	t.Helper()
	d, err := adminapidiscovery.NewDiscoverer(sets.New("admin"))
	require.NoError(t, err)
	return d
}
