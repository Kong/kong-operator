package gateway

import (
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/gatewayapi"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/util"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

func newTCPRoute(backendRef gatewayapi.BackendRef) gatewayapi.TCPRoute {
	return gatewayapi.TCPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "route",
			Namespace:  "default",
			Generation: 1,
		},
		Spec: gatewayapi.TCPRouteSpec{
			Rules: []gatewayapi.TCPRouteRule{{
				BackendRefs: []gatewayapi.BackendRef{backendRef},
			}},
		},
	}
}

func TestGetTCPRouteRuleReason(t *testing.T) {
	ctx := t.Context()
	logger := logr.Discard()

	otherNS := gatewayapi.Namespace("other")
	grantFromTCPRouteToService := gatewayapi.ReferenceGrant{
		ObjectMeta: metav1.ObjectMeta{Name: "grant", Namespace: "other"},
		Spec: gatewayapi.ReferenceGrantSpec{
			From: []gatewayapi.ReferenceGrantFrom{{
				Group:     gatewayapi.V1Group,
				Kind:      "TCPRoute",
				Namespace: "default",
			}},
			To: []gatewayapi.ReferenceGrantTo{{
				Group: "",
				Kind:  "Service",
			}},
		},
	}

	tests := []struct {
		name               string
		enableRefGrant     bool
		objects            []client.Object
		route              gatewayapi.TCPRoute
		wantReason         gatewayapi.RouteConditionReason
		wantMessageContain string
	}{
		{
			name:           "resolves",
			enableRefGrant: true,
			objects: []client.Object{
				&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "default"}},
			},
			route:      newTCPRoute(serviceBackendRef(nil)),
			wantReason: gatewayapi.RouteReasonResolvedRefs,
		},
		{
			name:               "backend service missing",
			enableRefGrant:     true,
			objects:            []client.Object{},
			route:              newTCPRoute(serviceBackendRef(nil)),
			wantReason:         gatewayapi.RouteReasonBackendNotFound,
			wantMessageContain: "target default/svc",
		},
		{
			name:           "unsupported backend kind",
			enableRefGrant: true,
			objects:        []client.Object{},
			route: newTCPRoute(gatewayapi.BackendRef{
				BackendObjectReference: gatewayapi.BackendObjectReference{
					Name:  "svc",
					Kind:  util.StringToGatewayAPIKindPtr("Foo"),
					Group: util.StringToTypedPtr[*gatewayapi.Group]("example.com"),
				},
			}),
			wantReason:         gatewayapi.RouteReasonInvalidKind,
			wantMessageContain: "unsupported type example.com/Foo",
		},
		{
			name:           "cross-namespace without ReferenceGrant CRD",
			enableRefGrant: false,
			objects: []client.Object{
				&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "other"}},
			},
			route:              newTCPRoute(serviceBackendRef(&otherNS)),
			wantReason:         gatewayapi.RouteReasonRefNotPermitted,
			wantMessageContain: "install ReferenceGrant CRD and configure a proper grant",
		},
		{
			name:           "cross-namespace without matching grant",
			enableRefGrant: true,
			objects: []client.Object{
				&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "other"}},
			},
			route:              newTCPRoute(serviceBackendRef(&otherNS)),
			wantReason:         gatewayapi.RouteReasonRefNotPermitted,
			wantMessageContain: "no ReferenceGrant allowing reference is configured",
		},
		{
			name:           "cross-namespace with matching grant",
			enableRefGrant: true,
			objects: []client.Object{
				&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "other"}},
				grantFromTCPRouteToService.DeepCopy(),
			},
			route:      newTCPRoute(serviceBackendRef(&otherNS)),
			wantReason: gatewayapi.RouteReasonResolvedRefs,
		},
		{
			name:           "cross-namespace grant for wrong from-kind (TLSRoute, not TCPRoute)",
			enableRefGrant: true,
			objects: []client.Object{
				&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "other"}},
				&gatewayapi.ReferenceGrant{
					ObjectMeta: metav1.ObjectMeta{Name: "grant", Namespace: "other"},
					Spec: gatewayapi.ReferenceGrantSpec{
						From: []gatewayapi.ReferenceGrantFrom{{
							Group:     gatewayapi.V1Group,
							Kind:      "TLSRoute",
							Namespace: "default",
						}},
						To: []gatewayapi.ReferenceGrantTo{{
							Group: "",
							Kind:  "Service",
						}},
					},
				},
			},
			route:              newTCPRoute(serviceBackendRef(&otherNS)),
			wantReason:         gatewayapi.RouteReasonRefNotPermitted,
			wantMessageContain: "no ReferenceGrant allowing reference is configured",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cl := fakeclient.NewClientBuilder().
				WithScheme(scheme.Get()).
				WithObjects(tc.objects...).
				Build()
			reconciler := &TCPRouteReconciler{
				Client:               cl,
				Log:                  logger,
				enableReferenceGrant: tc.enableRefGrant,
			}

			reason, msg, err := reconciler.getTCPRouteRuleReason(ctx, tc.route)
			require.NoError(t, err)
			assert.Equal(t, tc.wantReason, reason)
			if tc.wantMessageContain != "" {
				assert.Contains(t, msg, tc.wantMessageContain)
			}
		})
	}
}

func TestSetRouteConditionResolvedRefsCondition_TCPRoute(t *testing.T) {
	ctx := t.Context()
	logger := logr.Discard()

	otherNS := gatewayapi.Namespace("other")
	parentKey := "default/gw//"

	newParentStatuses := func(conds ...metav1.Condition) map[string]*gatewayapi.RouteParentStatus {
		return map[string]*gatewayapi.RouteParentStatus{
			parentKey: {
				ParentRef:      gatewayapi.ParentReference{Name: "gw"},
				ControllerName: gatewayapi.GatewayController("test"),
				Conditions:     conds,
			},
		}
	}

	t.Run("inserts new condition when missing", func(t *testing.T) {
		cl := fakeclient.NewClientBuilder().
			WithScheme(scheme.Get()).
			WithObjects(&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "default"}}).
			Build()
		r := &TCPRouteReconciler{Client: cl, Log: logger}
		route := newTCPRoute(serviceBackendRef(nil))
		parentStatuses := newParentStatuses()

		_, changed, err := r.setRouteConditionResolvedRefsCondition(ctx, &route, parentStatuses)
		require.NoError(t, err)
		assert.True(t, changed)

		conds := parentStatuses[parentKey].Conditions
		require.Len(t, conds, 1)
		assert.Equal(t, string(gatewayapi.RouteConditionResolvedRefs), conds[0].Type)
		assert.Equal(t, metav1.ConditionTrue, conds[0].Status)
		assert.Equal(t, string(gatewayapi.RouteReasonResolvedRefs), conds[0].Reason)
		assert.Equal(t, route.Generation, conds[0].ObservedGeneration)
	})

	t.Run("updates existing condition when reason changes", func(t *testing.T) {
		cl := fakeclient.NewClientBuilder().
			WithScheme(scheme.Get()).
			Build()
		r := &TCPRouteReconciler{Client: cl, Log: logger}
		route := newTCPRoute(serviceBackendRef(nil))
		parentStatuses := newParentStatuses(metav1.Condition{
			Type:   string(gatewayapi.RouteConditionResolvedRefs),
			Status: metav1.ConditionTrue,
			Reason: string(gatewayapi.RouteReasonResolvedRefs),
		})

		_, changed, err := r.setRouteConditionResolvedRefsCondition(ctx, &route, parentStatuses)
		require.NoError(t, err)
		assert.True(t, changed)

		conds := parentStatuses[parentKey].Conditions
		require.Len(t, conds, 1)
		assert.Equal(t, metav1.ConditionFalse, conds[0].Status)
		assert.Equal(t, string(gatewayapi.RouteReasonBackendNotFound), conds[0].Reason)
	})

	t.Run("no-op when condition already matches", func(t *testing.T) {
		cl := fakeclient.NewClientBuilder().
			WithScheme(scheme.Get()).
			WithObjects(&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "default"}}).
			Build()
		r := &TCPRouteReconciler{Client: cl, Log: logger}
		route := newTCPRoute(serviceBackendRef(nil))
		parentStatuses := newParentStatuses(metav1.Condition{
			Type:   string(gatewayapi.RouteConditionResolvedRefs),
			Status: metav1.ConditionTrue,
			Reason: string(gatewayapi.RouteReasonResolvedRefs),
		})

		_, changed, err := r.setRouteConditionResolvedRefsCondition(ctx, &route, parentStatuses)
		require.NoError(t, err)
		assert.False(t, changed)
	})

	t.Run("cross-namespace without grant flips condition to false", func(t *testing.T) {
		cl := fakeclient.NewClientBuilder().
			WithScheme(scheme.Get()).
			WithObjects(&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "other"}}).
			Build()
		r := &TCPRouteReconciler{Client: cl, Log: logger, enableReferenceGrant: true}
		route := newTCPRoute(serviceBackendRef(&otherNS))
		parentStatuses := newParentStatuses()

		_, changed, err := r.setRouteConditionResolvedRefsCondition(ctx, &route, parentStatuses)
		require.NoError(t, err)
		assert.True(t, changed)

		conds := parentStatuses[parentKey].Conditions
		require.Len(t, conds, 1)
		assert.Equal(t, metav1.ConditionFalse, conds[0].Status)
		assert.Equal(t, string(gatewayapi.RouteReasonRefNotPermitted), conds[0].Reason)
	})
}

func TestIsTCPReferenceGranted(t *testing.T) {
	svcKind := gatewayapi.Kind("Service")
	emptyGroup := gatewayapi.Group("")
	specificName := gatewayapi.ObjectName("svc")
	otherName := gatewayapi.ObjectName("other")

	makeGrant := func(from gatewayapi.ReferenceGrantFrom, to gatewayapi.ReferenceGrantTo) gatewayapi.ReferenceGrantSpec {
		return gatewayapi.ReferenceGrantSpec{
			From: []gatewayapi.ReferenceGrantFrom{from},
			To:   []gatewayapi.ReferenceGrantTo{to},
		}
	}

	backendRef := gatewayapi.BackendRef{
		BackendObjectReference: gatewayapi.BackendObjectReference{
			Name:  specificName,
			Kind:  &svcKind,
			Group: &emptyGroup,
		},
	}

	tests := []struct {
		name   string
		spec   gatewayapi.ReferenceGrantSpec
		fromNS string
		want   bool
	}{
		{
			name: "matching grant (any name)",
			spec: makeGrant(
				gatewayapi.ReferenceGrantFrom{Group: gatewayapi.V1Group, Kind: "TCPRoute", Namespace: "default"},
				gatewayapi.ReferenceGrantTo{Group: "", Kind: "Service"},
			),
			fromNS: "default",
			want:   true,
		},
		{
			name: "matching grant with specific to.Name",
			spec: makeGrant(
				gatewayapi.ReferenceGrantFrom{Group: gatewayapi.V1Group, Kind: "TCPRoute", Namespace: "default"},
				gatewayapi.ReferenceGrantTo{Group: "", Kind: "Service", Name: &specificName},
			),
			fromNS: "default",
			want:   true,
		},
		{
			name: "to.Name mismatch",
			spec: makeGrant(
				gatewayapi.ReferenceGrantFrom{Group: gatewayapi.V1Group, Kind: "TCPRoute", Namespace: "default"},
				gatewayapi.ReferenceGrantTo{Group: "", Kind: "Service", Name: &otherName},
			),
			fromNS: "default",
			want:   false,
		},
		{
			name: "wrong from.Kind (TLSRoute)",
			spec: makeGrant(
				gatewayapi.ReferenceGrantFrom{Group: gatewayapi.V1Group, Kind: "TLSRoute", Namespace: "default"},
				gatewayapi.ReferenceGrantTo{Group: "", Kind: "Service"},
			),
			fromNS: "default",
			want:   false,
		},
		{
			name: "wrong from.Namespace",
			spec: makeGrant(
				gatewayapi.ReferenceGrantFrom{Group: gatewayapi.V1Group, Kind: "TCPRoute", Namespace: "elsewhere"},
				gatewayapi.ReferenceGrantTo{Group: "", Kind: "Service"},
			),
			fromNS: "default",
			want:   false,
		},
		{
			name: "wrong to.Kind",
			spec: makeGrant(
				gatewayapi.ReferenceGrantFrom{Group: gatewayapi.V1Group, Kind: "TCPRoute", Namespace: "default"},
				gatewayapi.ReferenceGrantTo{Group: "", Kind: "ConfigMap"},
			),
			fromNS: "default",
			want:   false,
		},
		{
			name: "wrong to.Group",
			spec: makeGrant(
				gatewayapi.ReferenceGrantFrom{Group: gatewayapi.V1Group, Kind: "TCPRoute", Namespace: "default"},
				gatewayapi.ReferenceGrantTo{Group: "example.com", Kind: "Service"},
			),
			fromNS: "default",
			want:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isTCPReferenceGranted(tc.spec, backendRef, tc.fromNS)
			assert.Equal(t, tc.want, got)
		})
	}
}
