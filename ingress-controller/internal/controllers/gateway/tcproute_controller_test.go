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

	"github.com/kong/kong-operator/v2/ingress-controller/internal/util"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	referencegranthelpers "github.com/kong/kong-operator/v2/test/helpers/referencegrant"
)

func newTCPRoute(backendRef gwtypes.BackendRef) gwtypes.TCPRoute {
	return gwtypes.TCPRoute{
		Name:       "route",
		Namespace:  "default",
		Generation: 1,
		Spec: gwtypes.TCPRouteSpec{
			Rules: []gwtypes.TCPRouteRule{{
				BackendRefs: []gwtypes.BackendRef{backendRef},
			}},
		},
	}
}

func TestGetTCPRouteRuleReason(t *testing.T) {
	ctx := t.Context()
	logger := logr.Discard()

	otherNS := gwtypes.Namespace("other")
	grantFromTCPRouteToService := gwtypes.ReferenceGrant{
		Name: "grant", Namespace: "other",
		Spec: gwtypes.ReferenceGrantSpec{
			From: []gwtypes.ReferenceGrantFrom{{
				Group:     gwtypes.V1Group,
				Kind:      "TCPRoute",
				Namespace: "default",
			}},
			To: []gwtypes.ReferenceGrantTo{{
				Group: "",
				Kind:  "Service",
			}},
		},
	}

	tests := []struct {
		name               string
		objects            []client.Object
		route              gwtypes.TCPRoute
		wantReason         gwtypes.RouteConditionReason
		wantMessageContain string
	}{
		{
			name: "resolves",
			objects: []client.Object{
				&corev1.Service{Name: "svc", Namespace: "default"},
			},
			route:      newTCPRoute(serviceBackendRef(nil)),
			wantReason: gwtypes.RouteReasonResolvedRefs,
		},
		{
			name:               "backend service missing",
			objects:            []client.Object{},
			route:              newTCPRoute(serviceBackendRef(nil)),
			wantReason:         gwtypes.RouteReasonBackendNotFound,
			wantMessageContain: "target default/svc",
		},
		{
			name:    "unsupported backend kind",
			objects: []client.Object{},
			route: newTCPRoute(gwtypes.BackendRef{
				Name:  "svc",
				Kind:  util.StringToGatewayAPIKindPtr("Foo"),
				Group: util.StringToTypedPtr[*gwtypes.Group]("example.com"),
			}),
			wantReason:         gwtypes.RouteReasonInvalidKind,
			wantMessageContain: "unsupported type example.com/Foo",
		},
		{
			name: "cross-namespace without matching grant",
			objects: []client.Object{
				&corev1.Service{Name: "svc", Namespace: "other"},
			},
			route:              newTCPRoute(serviceBackendRef(&otherNS)),
			wantReason:         gwtypes.RouteReasonRefNotPermitted,
			wantMessageContain: "no ReferenceGrant allowing reference is configured",
		},
		{
			name: "cross-namespace with matching grant",
			objects: []client.Object{
				&corev1.Service{Name: "svc", Namespace: "other"},
				grantFromTCPRouteToService.DeepCopy(),
			},
			route:      newTCPRoute(serviceBackendRef(&otherNS)),
			wantReason: gwtypes.RouteReasonResolvedRefs,
		},
		{
			name: "cross-namespace grant for wrong from-kind (TLSRoute, not TCPRoute)",
			objects: []client.Object{
				&corev1.Service{Name: "svc", Namespace: "other"},
				&gwtypes.ReferenceGrant{
					Name: "grant", Namespace: "other",
					Spec: gwtypes.ReferenceGrantSpec{
						From: []gwtypes.ReferenceGrantFrom{{
							Group:     gwtypes.V1Group,
							Kind:      "TLSRoute",
							Namespace: "default",
						}},
						To: []gwtypes.ReferenceGrantTo{{
							Group: "",
							Kind:  "Service",
						}},
					},
				},
			},
			route:              newTCPRoute(serviceBackendRef(&otherNS)),
			wantReason:         gwtypes.RouteReasonRefNotPermitted,
			wantMessageContain: "no ReferenceGrant allowing reference is configured",
		},
	}

	for _, gv := range referencegranthelpers.Versions() {
		t.Run(gv.Version, func(t *testing.T) {
			for _, tc := range tests {
				t.Run(tc.name, func(t *testing.T) {
					cl := fakeclient.NewClientBuilder().
						WithScheme(scheme.Get()).
						WithObjects(referencegranthelpers.AsVersion(gv, tc.objects)...).
						Build()
					reconciler := &TCPRouteReconciler{
						Client:                cl,
						Log:                   logger,
						ReferenceGrantVersion: gv,
					}

					reason, msg, err := reconciler.getTCPRouteRuleReason(ctx, tc.route)
					require.NoError(t, err)
					assert.Equal(t, tc.wantReason, reason)
					if tc.wantMessageContain != "" {
						assert.Contains(t, msg, tc.wantMessageContain)
					}
				})
			}
		})
	}
}

// Pinned to a single ReferenceGrant version on purpose: this test lists ReferenceGrants
// but never seeds one, so it asserts denial and gets an empty list at either version.
// The grant-permitted path is covered against both versions by TestGetTCPRouteRuleReason.
func TestSetRouteConditionResolvedRefsCondition_TCPRoute(t *testing.T) {
	ctx := t.Context()
	logger := logr.Discard()

	otherNS := gwtypes.Namespace("other")
	parentKey := "default/gw//"

	newParentStatuses := func(conds ...metav1.Condition) map[string]*gwtypes.RouteParentStatus {
		return map[string]*gwtypes.RouteParentStatus{
			parentKey: {
				ParentRef:      gwtypes.ParentReference{Name: "gw"},
				ControllerName: GetControllerName(),
				Conditions:     conds,
			},
		}
	}

	t.Run("inserts new condition when missing", func(t *testing.T) {
		cl := fakeclient.NewClientBuilder().
			WithScheme(scheme.Get()).
			WithObjects(&corev1.Service{Name: "svc", Namespace: "default"}).
			Build()
		r := &TCPRouteReconciler{Client: cl, Log: logger, ReferenceGrantVersion: testReferenceGrantVersion}
		route := newTCPRoute(serviceBackendRef(nil))
		parentStatuses := newParentStatuses()

		_, changed, err := r.setRouteConditionResolvedRefsCondition(ctx, &route, parentStatuses)
		require.NoError(t, err)
		assert.True(t, changed)

		conds := parentStatuses[parentKey].Conditions
		require.Len(t, conds, 1)
		assert.Equal(t, string(gwtypes.RouteConditionResolvedRefs), conds[0].Type)
		assert.Equal(t, metav1.ConditionTrue, conds[0].Status)
		assert.Equal(t, string(gwtypes.RouteReasonResolvedRefs), conds[0].Reason)
		assert.Equal(t, route.Generation, conds[0].ObservedGeneration)
	})

	t.Run("updates existing condition when reason changes", func(t *testing.T) {
		cl := fakeclient.NewClientBuilder().
			WithScheme(scheme.Get()).
			Build()
		r := &TCPRouteReconciler{Client: cl, Log: logger, ReferenceGrantVersion: testReferenceGrantVersion}
		route := newTCPRoute(serviceBackendRef(nil))
		parentStatuses := newParentStatuses(metav1.Condition{
			Type:   string(gwtypes.RouteConditionResolvedRefs),
			Status: metav1.ConditionTrue,
			Reason: string(gwtypes.RouteReasonResolvedRefs),
		})

		_, changed, err := r.setRouteConditionResolvedRefsCondition(ctx, &route, parentStatuses)
		require.NoError(t, err)
		assert.True(t, changed)

		conds := parentStatuses[parentKey].Conditions
		require.Len(t, conds, 1)
		assert.Equal(t, metav1.ConditionFalse, conds[0].Status)
		assert.Equal(t, string(gwtypes.RouteReasonBackendNotFound), conds[0].Reason)
	})

	t.Run("no-op when condition already matches", func(t *testing.T) {
		cl := fakeclient.NewClientBuilder().
			WithScheme(scheme.Get()).
			WithObjects(&corev1.Service{Name: "svc", Namespace: "default"}).
			Build()
		r := &TCPRouteReconciler{Client: cl, Log: logger, ReferenceGrantVersion: testReferenceGrantVersion}
		route := newTCPRoute(serviceBackendRef(nil))
		parentStatuses := newParentStatuses(metav1.Condition{
			Type:   string(gwtypes.RouteConditionResolvedRefs),
			Status: metav1.ConditionTrue,
			Reason: string(gwtypes.RouteReasonResolvedRefs),
		})

		_, changed, err := r.setRouteConditionResolvedRefsCondition(ctx, &route, parentStatuses)
		require.NoError(t, err)
		assert.False(t, changed)
	})

	t.Run("cross-namespace without grant flips condition to false", func(t *testing.T) {
		cl := fakeclient.NewClientBuilder().
			WithScheme(scheme.Get()).
			WithObjects(&corev1.Service{Name: "svc", Namespace: "other"}).
			Build()
		r := &TCPRouteReconciler{Client: cl, Log: logger, ReferenceGrantVersion: testReferenceGrantVersion}
		route := newTCPRoute(serviceBackendRef(&otherNS))
		parentStatuses := newParentStatuses()

		_, changed, err := r.setRouteConditionResolvedRefsCondition(ctx, &route, parentStatuses)
		require.NoError(t, err)
		assert.True(t, changed)

		conds := parentStatuses[parentKey].Conditions
		require.Len(t, conds, 1)
		assert.Equal(t, metav1.ConditionFalse, conds[0].Status)
		assert.Equal(t, string(gwtypes.RouteReasonRefNotPermitted), conds[0].Reason)
	})
}

func TestIsTCPReferenceGranted(t *testing.T) {
	svcKind := gwtypes.Kind("Service")
	emptyGroup := gwtypes.Group("")
	specificName := gwtypes.ObjectName("svc")
	otherName := gwtypes.ObjectName("other")

	makeGrant := func(from gwtypes.ReferenceGrantFrom, to gwtypes.ReferenceGrantTo) gwtypes.ReferenceGrantSpec {
		return gwtypes.ReferenceGrantSpec{
			From: []gwtypes.ReferenceGrantFrom{from},
			To:   []gwtypes.ReferenceGrantTo{to},
		}
	}

	backendRef := gwtypes.BackendRef{
		Name:  specificName,
		Kind:  &svcKind,
		Group: &emptyGroup,
	}

	tests := []struct {
		name   string
		spec   gwtypes.ReferenceGrantSpec
		fromNS string
		want   bool
	}{
		{
			name: "matching grant (any name)",
			spec: makeGrant(
				gwtypes.ReferenceGrantFrom{Group: gwtypes.V1Group, Kind: "TCPRoute", Namespace: "default"},
				gwtypes.ReferenceGrantTo{Group: "", Kind: "Service"},
			),
			fromNS: "default",
			want:   true,
		},
		{
			name: "matching grant with specific to.Name",
			spec: makeGrant(
				gwtypes.ReferenceGrantFrom{Group: gwtypes.V1Group, Kind: "TCPRoute", Namespace: "default"},
				gwtypes.ReferenceGrantTo{Group: "", Kind: "Service", Name: &specificName},
			),
			fromNS: "default",
			want:   true,
		},
		{
			name: "to.Name mismatch",
			spec: makeGrant(
				gwtypes.ReferenceGrantFrom{Group: gwtypes.V1Group, Kind: "TCPRoute", Namespace: "default"},
				gwtypes.ReferenceGrantTo{Group: "", Kind: "Service", Name: &otherName},
			),
			fromNS: "default",
			want:   false,
		},
		{
			name: "wrong from.Kind (TLSRoute)",
			spec: makeGrant(
				gwtypes.ReferenceGrantFrom{Group: gwtypes.V1Group, Kind: "TLSRoute", Namespace: "default"},
				gwtypes.ReferenceGrantTo{Group: "", Kind: "Service"},
			),
			fromNS: "default",
			want:   false,
		},
		{
			name: "wrong from.Namespace",
			spec: makeGrant(
				gwtypes.ReferenceGrantFrom{Group: gwtypes.V1Group, Kind: "TCPRoute", Namespace: "elsewhere"},
				gwtypes.ReferenceGrantTo{Group: "", Kind: "Service"},
			),
			fromNS: "default",
			want:   false,
		},
		{
			name: "wrong to.Kind",
			spec: makeGrant(
				gwtypes.ReferenceGrantFrom{Group: gwtypes.V1Group, Kind: "TCPRoute", Namespace: "default"},
				gwtypes.ReferenceGrantTo{Group: "", Kind: "ConfigMap"},
			),
			fromNS: "default",
			want:   false,
		},
		{
			name: "wrong to.Group",
			spec: makeGrant(
				gwtypes.ReferenceGrantFrom{Group: gwtypes.V1Group, Kind: "TCPRoute", Namespace: "default"},
				gwtypes.ReferenceGrantTo{Group: "example.com", Kind: "Service"},
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
