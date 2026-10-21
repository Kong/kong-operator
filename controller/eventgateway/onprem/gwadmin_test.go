package onprem

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kcfgconsts "github.com/kong/kong-operator/v2/api/common/consts"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/gwadmin/published"
	"github.com/kong/kong-operator/v2/internal/utils/gwadminref"
	"github.com/kong/kong-operator/v2/internal/utils/gwadminref/gwadminreftest"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

const (
	testGWAdminName    = "gw-admin"
	testAPIServiceName = "gw-admin-api"
)

// newTestGateway returns an OnPremEventGateway of the namespace ns
// referencing the GWAdmin of the test.
func newTestGateway(name string) *eventgatewayv1alpha1.OnPremEventGateway {
	gateway := &eventgatewayv1alpha1.OnPremEventGateway{Name: name, Namespace: ns}
	gateway.Spec.GWAdminRef.Name = testGWAdminName
	gateway.SetGeneration(1)
	return gateway
}

// newTestGWAdmin returns a GWAdmin of the namespace ns that has published its
// API, and the EndpointSlice of its API Service publishing the endpoints.
func newTestGWAdmin(endpoints ...discoveryv1.Endpoint) (*operatorv1alpha1.GWAdmin, *discoveryv1.EndpointSlice) {
	gw := &operatorv1alpha1.GWAdmin{Name: testGWAdminName, Namespace: ns}
	gw.Status.API = &operatorv1alpha1.GWAdminAPIStatus{ServiceName: testAPIServiceName, PortName: "api", Port: 8443}
	return gw, &discoveryv1.EndpointSlice{
		Name: testAPIServiceName + "-abcde", Namespace: ns,
		Labels:      apiEndpointSliceLabelsOf(testAPIServiceName),
		AddressType: discoveryv1.AddressTypeIPv4,
		Ports:       []discoveryv1.EndpointPort{{Name: new("api"), Port: new(int32(8443))}},
		Endpoints:   endpoints,
	}
}

// apiEndpointSliceLabelsOf returns the labels of the EndpointSlices of the
// API Service serviceName of the GWAdmin of the test.
func apiEndpointSliceLabelsOf(serviceName string) map[string]string {
	labels := apiEndpointSliceLabels(testGWAdminName)
	labels[discoveryv1.LabelServiceName] = serviceName
	return labels
}

// endpoint returns the endpoint of a gw-admin pod.
func endpoint(name, ip string, terminating bool) discoveryv1.Endpoint {
	return discoveryv1.Endpoint{
		Addresses:  []string{ip},
		TargetRef:  &corev1.ObjectReference{Kind: "Pod", Name: name},
		Conditions: discoveryv1.EndpointConditions{Terminating: new(terminating), Serving: new(true)},
	}
}

// grantGatewaysOf returns a KongReferenceGrant in the namespace ns allowing
// the OnPremEventGateways of namespace from to reference its GWAdmins.
func grantGatewaysOf(from string) *configurationv1alpha1.KongReferenceGrant {
	return gwadminreftest.Grant(ns, "grant", from, "")
}

// newGWAdminTestClient returns a fake client with the index of the gateways
// by GWAdmin, going through funcs.
func newGWAdminTestClient(t *testing.T, funcs interceptor.Funcs, objects ...client.Object) client.Client {
	t.Helper()
	b := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(objects...).
		WithStatusSubresource(&eventgatewayv1alpha1.OnPremEventGateway{}).
		WithInterceptorFuncs(funcs)
	for _, opt := range gwadminref.IndexOptions() {
		b = b.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	return b.Build()
}

func failingList(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
	return errors.New("unavailable")
}

func TestResolveGWAdmin(t *testing.T) {
	ready := []gwAdminPod{{name: "gw-admin-0", address: "10.0.0.1:8443"}}

	tests := []struct {
		name string
		// objects returns the objects of the cluster, from the GWAdmin of the
		// test and the EndpointSlice of its API Service.
		objects func(*operatorv1alpha1.GWAdmin, *discoveryv1.EndpointSlice) []client.Object
		// gatewayNamespace, when set, is the namespace of the gateway, which
		// references the GWAdmin of the namespace ns.
		gatewayNamespace string
		funcs            interceptor.Funcs
		wantReason       kcfgconsts.ConditionReason
		wantMessage      string
		wantErr          string
		wantPods         []gwAdminPod
	}{
		{
			name:     "ready",
			wantPods: ready,
		},
		{
			name: "the GWAdmin doesn't exist",
			objects: func(_ *operatorv1alpha1.GWAdmin, es *discoveryv1.EndpointSlice) []client.Object {
				return []client.Object{es}
			},
			wantReason:  eventgatewayv1alpha1.OnPremEventGatewayGWAdminNotFoundReason,
			wantMessage: "The GWAdmin default/gw-admin does not exist",
		},
		{
			name: "the GWAdmin can't be read",
			funcs: interceptor.Funcs{Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*operatorv1alpha1.GWAdmin); ok {
					return errors.New("unavailable")
				}
				return cl.Get(ctx, key, obj, opts...)
			}},
			wantErr: "failed to get the GWAdmin default/gw-admin: unavailable",
		},
		{
			name: "the GWAdmin is being deleted",
			objects: func(gw *operatorv1alpha1.GWAdmin, es *discoveryv1.EndpointSlice) []client.Object {
				gw.SetDeletionTimestamp(new(metav1.Now()))
				gw.SetFinalizers([]string{"example.com/hold"})
				return []client.Object{gw, es}
			},
			wantReason:  eventgatewayv1alpha1.OnPremEventGatewayGWAdminNotReadyReason,
			wantMessage: "The GWAdmin default/gw-admin is being deleted",
		},
		{
			name: "the GWAdmin hasn't published its API",
			objects: func(gw *operatorv1alpha1.GWAdmin, es *discoveryv1.EndpointSlice) []client.Object {
				gw.Status.API = nil
				return []client.Object{gw, es}
			},
			wantReason:  eventgatewayv1alpha1.OnPremEventGatewayGWAdminNotReadyReason,
			wantMessage: "The GWAdmin default/gw-admin hasn't published its API yet",
		},
		{
			name: "the GWAdmin has no pod",
			objects: func(gw *operatorv1alpha1.GWAdmin, _ *discoveryv1.EndpointSlice) []client.Object {
				return []client.Object{gw}
			},
			wantReason:  eventgatewayv1alpha1.OnPremEventGatewayGWAdminNotReadyReason,
			wantMessage: "The GWAdmin default/gw-admin has no gw-admin pod",
		},
		{
			name: "pods shutting down don't count",
			objects: func(gw *operatorv1alpha1.GWAdmin, es *discoveryv1.EndpointSlice) []client.Object {
				es.Endpoints = []discoveryv1.Endpoint{endpoint("gw-admin-1", "10.0.0.2", true)}
				return []client.Object{gw, es}
			},
			wantReason:  eventgatewayv1alpha1.OnPremEventGatewayGWAdminNotReadyReason,
			wantMessage: "The GWAdmin default/gw-admin has no gw-admin pod",
		},
		{
			name:    "the EndpointSlices can't be listed",
			funcs:   interceptor.Funcs{List: failingList},
			wantErr: "failed to list the EndpointSlices of the API Service default/gw-admin-api of the GWAdmin",
		},
		{
			name:             "a GWAdmin in another namespace, without a KongReferenceGrant",
			gatewayNamespace: "other",
			wantReason:       eventgatewayv1alpha1.OnPremEventGatewayGWAdminRefNotPermittedReason,
			wantMessage:      "The gateway may not use its GWAdmin: no KongReferenceGrant in namespace default allows the OnPremEventGateways of namespace other to reference the GWAdmin default/gw-admin",
		},
		{
			name:             "the KongReferenceGrants can't be listed",
			gatewayNamespace: "other",
			funcs:            interceptor.Funcs{List: failingList},
			wantErr:          "unavailable",
		},
		{
			name:             "a GWAdmin in another namespace, with a KongReferenceGrant",
			gatewayNamespace: "other",
			objects: func(gw *operatorv1alpha1.GWAdmin, es *discoveryv1.EndpointSlice) []client.Object {
				return []client.Object{gw, es, grantGatewaysOf("other")}
			},
			wantPods: ready,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gw, es := newTestGWAdmin(endpoint("gw-admin-0", "10.0.0.1", false), endpoint("gw-admin-1", "10.0.0.2", true))
			objects := []client.Object{gw, es}
			if tc.objects != nil {
				objects = tc.objects(gw, es)
			}
			gateway := newTestGateway("gateway")
			if tc.gatewayNamespace != "" {
				gateway.Namespace = tc.gatewayNamespace
				gateway.Spec.GWAdminRef.Namespace = new(ns)
			}
			r := &Reconciler{Client: newGWAdminTestClient(t, tc.funcs, append(objects, gateway)...)}

			pods, unresolved, err := r.resolveGWAdmin(t.Context(), gateway)
			switch {
			case tc.wantErr != "":
				require.ErrorContains(t, err, tc.wantErr)
				assert.Nil(t, unresolved)
			case tc.wantReason != "":
				require.NoError(t, err, "not retried: a change triggers the gateway again")
				require.NotNil(t, unresolved)
				assert.Equal(t, tc.wantReason, unresolved.reason)
				assert.Equal(t, tc.wantMessage, unresolved.message)
				assert.Empty(t, pods)
			default:
				require.NoError(t, err)
				assert.Nil(t, unresolved)
				assert.Equal(t, tc.wantPods, pods)
			}
		})
	}
}

func TestGWAdminPods(t *testing.T) {
	slice := func(name string, addressType discoveryv1.AddressType, portName string, endpoints ...discoveryv1.Endpoint) *discoveryv1.EndpointSlice {
		return &discoveryv1.EndpointSlice{
			Name: name, Namespace: ns, Labels: apiEndpointSliceLabelsOf(testAPIServiceName),
			AddressType: addressType,
			Ports:       []discoveryv1.EndpointPort{{Name: new(portName), Port: new(int32(8443))}},
			Endpoints:   endpoints,
		}
	}
	notAPod := discoveryv1.Endpoint{Addresses: []string{"10.0.0.9"}}
	notManaged := slice("gw-admin-api-manual", discoveryv1.AddressTypeIPv4, "api", endpoint("gw-admin-4", "10.0.0.5", false))
	delete(notManaged.Labels, discoveryv1.LabelManagedBy)
	otherService := slice("gw-admin-cp-abcde", discoveryv1.AddressTypeIPv4, "api", endpoint("gw-admin-5", "10.0.0.6", false))
	otherService.Labels = apiEndpointSliceLabelsOf("gw-admin-cp")
	gw, _ := newTestGWAdmin()
	cl := newGWAdminTestClient(t, interceptor.Funcs{},
		// Sorted before the IPv4 one by name: the IPv4 address is still used.
		slice("gw-admin-api-a6", discoveryv1.AddressTypeIPv6, "api", endpoint("gw-admin-0", "fd00::1", false), endpoint("gw-admin-6", "fd00::7", false)),
		slice("gw-admin-api-v4", discoveryv1.AddressTypeIPv4, "api",
			endpoint("gw-admin-1", "10.0.0.2", false), endpoint("gw-admin-0", "10.0.0.1", false),
			endpoint("gw-admin-2", "10.0.0.3", true), notAPod, discoveryv1.Endpoint{}),
		slice("gw-admin-api-other", discoveryv1.AddressTypeIPv4, "other", endpoint("gw-admin-3", "10.0.0.4", false)),
		notManaged, otherService,
	)

	pods, err := gwAdminPods(t.Context(), cl, gw)
	require.NoError(t, err)
	assert.Equal(t, []gwAdminPod{
		{name: "gw-admin-0", address: "10.0.0.1:8443"},
		{name: "gw-admin-1", address: "10.0.0.2:8443"},
		{name: "gw-admin-6", address: "[fd00::7]:8443"},
	}, pods, "a pod of a dual-stack Service is returned once, on its IPv4 address; a pod shutting down, of another port, "+
		"of another Service or of an EndpointSlice the EndpointSlice controller doesn't manage isn't, and neither is an endpoint that isn't a pod")
}

func TestReconcileReportsTheGWAdmin(t *testing.T) {
	conditions := func(t *testing.T, cl client.Client, gateway *eventgatewayv1alpha1.OnPremEventGateway) (resolved, programmed *metav1.Condition) {
		t.Helper()
		var got eventgatewayv1alpha1.OnPremEventGateway
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(gateway), &got))
		return apimeta.FindStatusCondition(got.Status.Conditions, string(eventgatewayv1alpha1.OnPremEventGatewayGWAdminResolvedType)),
			apimeta.FindStatusCondition(got.Status.Conditions, string(eventgatewayv1alpha1.OnPremEventGatewayProgrammedType))
	}

	t.Run("pods are found", func(t *testing.T) {
		gw, es := newTestGWAdmin(endpoint("gw-admin-0", "10.0.0.1", false))
		gateway := newTestGateway("gateway")
		cl := newGWAdminTestClient(t, interceptor.Funcs{}, gw, es, gateway)
		_, err := (&Reconciler{Client: cl}).Reconcile(t.Context(), gateway)
		require.NoError(t, err)
		resolved, programmed := conditions(t, cl, gateway)
		require.NotNil(t, resolved)
		assert.Equal(t, metav1.ConditionTrue, resolved.Status)
		assert.Equal(t, string(eventgatewayv1alpha1.OnPremEventGatewayGWAdminResolvedReason), resolved.Reason)
		assert.Equal(t, "The GWAdmin default/gw-admin serves the gateway", resolved.Message)
		require.NotNil(t, programmed)
		assert.Equal(t, metav1.ConditionFalse, programmed.Status)
		assert.Equal(t, string(eventgatewayv1alpha1.OnPremEventGatewayPendingReason), programmed.Reason)
		assert.Equal(t, eventgatewayv1alpha1.OnPremEventGatewayPendingMessage, programmed.Message)
	})

	t.Run("the GWAdmin isn't resolved", func(t *testing.T) {
		gateway := newTestGateway("gateway")
		cl := newGWAdminTestClient(t, interceptor.Funcs{}, gateway)
		_, err := (&Reconciler{Client: cl}).Reconcile(t.Context(), gateway)
		require.NoError(t, err, "not retried: a change of the GWAdmin triggers the gateway")
		resolved, programmed := conditions(t, cl, gateway)
		require.NotNil(t, resolved)
		assert.Equal(t, metav1.ConditionFalse, resolved.Status)
		assert.Equal(t, string(eventgatewayv1alpha1.OnPremEventGatewayGWAdminNotFoundReason), resolved.Reason)
		assert.Equal(t, "The GWAdmin default/gw-admin does not exist", resolved.Message)
		require.NotNil(t, programmed)
		assert.Equal(t, metav1.ConditionFalse, programmed.Status)
		assert.Equal(t, string(eventgatewayv1alpha1.DependenciesNotReadyReason), programmed.Reason)
		assert.Equal(t, eventgatewayv1alpha1.OnPremEventGatewayGWAdminNotResolvedMessage, programmed.Message)
	})

	t.Run("the conditions are carried over, their lastTransitionTime kept until they change", func(t *testing.T) {
		gw, es := newTestGWAdmin(endpoint("gw-admin-0", "10.0.0.1", false))
		gateway := newTestGateway("gateway")
		cl := newGWAdminTestClient(t, interceptor.Funcs{}, gw, es, gateway)
		r := &Reconciler{Client: cl}
		_, err := r.Reconcile(t.Context(), gateway)
		require.NoError(t, err)

		// Move the lastTransitionTime of both conditions back, to tell a kept
		// one from a new one.
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(gateway), gateway))
		past := metav1.NewTime(time.Now().Add(-time.Hour).Truncate(time.Second))
		for i := range gateway.Status.Conditions {
			gateway.Status.Conditions[i].LastTransitionTime = past
		}
		require.NoError(t, cl.Status().Update(t.Context(), gateway))

		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(gateway), gateway))
		_, err = r.Reconcile(t.Context(), gateway)
		require.NoError(t, err)
		resolved, programmed := conditions(t, cl, gateway)
		require.NotNil(t, resolved)
		require.NotNil(t, programmed)
		assert.True(t, resolved.LastTransitionTime.Equal(&past), "GWAdminResolved didn't change")
		assert.True(t, programmed.LastTransitionTime.Equal(&past), "Programmed didn't change")

		require.NoError(t, cl.Delete(t.Context(), es))
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(gateway), gateway))
		_, err = r.Reconcile(t.Context(), gateway)
		require.NoError(t, err)
		resolved, programmed = conditions(t, cl, gateway)
		require.NotNil(t, resolved)
		require.NotNil(t, programmed)
		assert.Equal(t, metav1.ConditionFalse, resolved.Status)
		assert.True(t, resolved.LastTransitionTime.After(past.Time), "GWAdminResolved turned False")
		assert.Equal(t, string(eventgatewayv1alpha1.DependenciesNotReadyReason), programmed.Reason)
		assert.True(t, programmed.LastTransitionTime.Equal(&past), "Programmed stayed False: only its reason changed")
	})

	t.Run("a gateway being deleted is left alone", func(t *testing.T) {
		gateway := newTestGateway("gateway")
		gateway.SetDeletionTimestamp(new(metav1.Now()))
		gateway.SetFinalizers([]string{"example.com/hold"})
		cl := newGWAdminTestClient(t, interceptor.Funcs{}, gateway)
		require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(gateway), gateway))
		_, err := (&Reconciler{Client: cl}).Reconcile(t.Context(), gateway)
		require.NoError(t, err)
		resolved, programmed := conditions(t, cl, gateway)
		assert.Nil(t, resolved)
		assert.Nil(t, programmed)
	})

	t.Run("an error writing the status is retried", func(t *testing.T) {
		gateway := newTestGateway("gateway")
		cl := newGWAdminTestClient(t, interceptor.Funcs{
			SubResourcePatch: func(context.Context, client.Client, string, client.Object, client.Patch, ...client.SubResourcePatchOption) error {
				return errors.New("unavailable")
			},
		}, gateway)
		_, err := (&Reconciler{Client: cl}).Reconcile(t.Context(), gateway)
		require.ErrorContains(t, err, "unavailable")
	})

	t.Run("an error reading the cluster is retried, and the status left alone", func(t *testing.T) {
		gateway := newTestGateway("gateway")
		cl := newGWAdminTestClient(t, interceptor.Funcs{Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if _, ok := obj.(*operatorv1alpha1.GWAdmin); ok {
				return errors.New("unavailable")
			}
			return cl.Get(ctx, key, obj, opts...)
		}}, gateway)
		_, err := (&Reconciler{Client: cl}).Reconcile(t.Context(), gateway)
		require.ErrorContains(t, err, "unavailable")
		resolved, programmed := conditions(t, cl, gateway)
		assert.Nil(t, resolved)
		assert.Nil(t, programmed)
	})
}

func requestsFor(objs ...client.Object) []reconcile.Request {
	requests := make([]reconcile.Request, 0, len(objs))
	for _, obj := range objs {
		requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(obj)})
	}
	return requests
}

func TestMapGWAdminToOnPremEventGateways(t *testing.T) {
	gateway, other := newTestGateway("gateway"), newTestGateway("other")
	other.Spec.GWAdminRef.Name = "other"
	gw, _ := newTestGWAdmin()
	cl := newGWAdminTestClient(t, interceptor.Funcs{}, gateway, other, gw)
	assert.Equal(t, requestsFor(gateway), mapGWAdminToOnPremEventGateways(cl)(t.Context(), gw))

	cl = newGWAdminTestClient(t, interceptor.Funcs{List: failingList})
	assert.Empty(t, mapGWAdminToOnPremEventGateways(cl)(t.Context(), gw), "an error maps to nothing")
}

func TestMapKongReferenceGrantToOnPremEventGateways(t *testing.T) {
	gateway := newTestGateway("gateway")
	gateway.Namespace = "other"
	gateway.Spec.GWAdminRef.Namespace = new(ns)
	// Neither depends on the grant: one uses a GWAdmin of its own namespace,
	// the other one is in the namespace of the grant.
	local := newTestGateway("local")
	local.Namespace = "other"
	sameNamespace := newTestGateway("same-namespace")
	gw, _ := newTestGWAdmin()
	grant := grantGatewaysOf("other")
	cl := newGWAdminTestClient(t, interceptor.Funcs{}, gateway, local, sameNamespace, gw, grant)
	assert.Equal(t, requestsFor(gateway), mapKongReferenceGrantToOnPremEventGateways(cl)(t.Context(), grant),
		"the gateways of the granted namespaces referencing a GWAdmin of the namespace of the grant")

	notGWAdmins := grant.DeepCopy()
	notGWAdmins.Spec.To[0].Kind = "Service"
	assert.Empty(t, mapKongReferenceGrantToOnPremEventGateways(cl)(t.Context(), notGWAdmins), "a grant that isn't about GWAdmins")

	assert.Empty(t, mapKongReferenceGrantToOnPremEventGateways(cl)(t.Context(), gw), "not a KongReferenceGrant")

	cl = newGWAdminTestClient(t, interceptor.Funcs{List: failingList})
	assert.Empty(t, mapKongReferenceGrantToOnPremEventGateways(cl)(t.Context(), grant))
}

func TestMapEndpointSliceToOnPremEventGateways(t *testing.T) {
	gateway := newTestGateway("gateway")
	cl := newGWAdminTestClient(t, interceptor.Funcs{}, gateway)
	_, slice := newTestGWAdmin()
	mapFn := mapEndpointSliceToOnPremEventGateways(cl)

	assert.Equal(t, requestsFor(gateway), mapFn(t.Context(), slice),
		"the gateways of the GWAdmin, whether it exists or not")

	cp := slice.DeepCopy()
	cp.Labels[published.APIServiceRoleLabel] = "cp"
	assert.Empty(t, mapFn(t.Context(), cp), "not the API Service")

	notManaged := slice.DeepCopy()
	delete(notManaged.Labels, discoveryv1.LabelManagedBy)
	assert.Empty(t, mapFn(t.Context(), notManaged), "not managed by the EndpointSlice controller")

	unlabelled := slice.DeepCopy()
	unlabelled.Labels = map[string]string{discoveryv1.LabelServiceName: testAPIServiceName}
	assert.Empty(t, mapFn(t.Context(), unlabelled), "not a Service of a GWAdmin")

	key, ok := gwAdminOfEndpointSlice(slice)
	assert.True(t, ok)
	assert.Equal(t, types.NamespacedName{Namespace: ns, Name: testGWAdminName}, key)
}

func TestGWAdminServingChanged(t *testing.T) {
	gw, _ := newTestGWAdmin()
	update := func(mutate func(*operatorv1alpha1.GWAdmin)) bool {
		updated := gw.DeepCopy()
		mutate(updated)
		return gwAdminServingChanged().Update(event.UpdateEvent{ObjectOld: gw, ObjectNew: updated})
	}

	assert.False(t, update(func(g *operatorv1alpha1.GWAdmin) {
		g.Generation++
		g.Status.Conditions = []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue}}
	}), "a change the gateways don't read")
	assert.True(t, update(func(g *operatorv1alpha1.GWAdmin) { g.Status.API.Port = 9443 }), "the API it publishes")
	assert.True(t, update(func(g *operatorv1alpha1.GWAdmin) { g.Status.API = nil }), "the API it no longer publishes")
	assert.True(t, update(func(g *operatorv1alpha1.GWAdmin) { g.SetDeletionTimestamp(new(metav1.Now())) }), "its deletion")
	assert.True(t, gwAdminServingChanged().Update(event.UpdateEvent{ObjectOld: &corev1.Secret{}, ObjectNew: &corev1.Secret{}}), "not a GWAdmin")
	assert.True(t, gwAdminServingChanged().Create(event.CreateEvent{Object: gw}))
	assert.True(t, gwAdminServingChanged().Delete(event.DeleteEvent{Object: gw}))
}
