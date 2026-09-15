package gatewayapi

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func mkNamespaceGetter(namespaces ...*corev1.Namespace) NamespaceGetter {
	byName := make(map[string]*corev1.Namespace, len(namespaces))
	for _, ns := range namespaces {
		byName[ns.Name] = ns
	}
	return func(name string) (*corev1.Namespace, error) {
		ns, ok := byName[name]
		if !ok {
			return nil, errNamespaceNotFound
		}
		return ns, nil
	}
}

var errNamespaceNotFound = errors.New("namespace not found")

func mkRoute(namespace string) *UDPRoute {
	return &UDPRoute{
		Namespace: namespace, Name: "route",
	}
}

func TestListenerAllowsNamespace(t *testing.T) {
	fromAll := NamespacesFromAll
	fromSame := NamespacesFromSame
	fromSelector := NamespacesFromSelector

	t.Run("nil AllowedRoutes allows any namespace", func(t *testing.T) {
		ok, err := ListenerAllowsNamespace(Listener{}, mkRoute("app"), "gw-ns", nil, mkNamespaceGetter())
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("From: All allows any namespace", func(t *testing.T) {
		listener := Listener{AllowedRoutes: &AllowedRoutes{Namespaces: &RouteNamespaces{From: &fromAll}}}
		ok, err := ListenerAllowsNamespace(listener, mkRoute("app"), "gw-ns", nil, mkNamespaceGetter())
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("From: Same, no parentRef namespace, matches gateway namespace", func(t *testing.T) {
		listener := Listener{AllowedRoutes: &AllowedRoutes{Namespaces: &RouteNamespaces{From: &fromSame}}}
		ok, err := ListenerAllowsNamespace(listener, mkRoute("gw-ns"), "gw-ns", nil, mkNamespaceGetter())
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("From: Same, no parentRef namespace, route in different namespace is excluded", func(t *testing.T) {
		listener := Listener{AllowedRoutes: &AllowedRoutes{Namespaces: &RouteNamespaces{From: &fromSame}}}
		ok, err := ListenerAllowsNamespace(listener, mkRoute("app"), "gw-ns", nil, mkNamespaceGetter())
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("From: Same, parentRef namespace set, compares against it instead of gateway namespace", func(t *testing.T) {
		listener := Listener{AllowedRoutes: &AllowedRoutes{Namespaces: &RouteNamespaces{From: &fromSame}}}
		parentRefNS := Namespace("app")
		ok, err := ListenerAllowsNamespace(listener, mkRoute("app"), "gw-ns", &parentRefNS, mkNamespaceGetter())
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("From: Selector, route namespace labels match", func(t *testing.T) {
		listener := Listener{AllowedRoutes: &AllowedRoutes{Namespaces: &RouteNamespaces{
			From:     &fromSelector,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"team": "checkout"}},
		}}}
		getNamespace := mkNamespaceGetter(&corev1.Namespace{
			Name: "app", Labels: map[string]string{"team": "checkout"},
		})
		ok, err := ListenerAllowsNamespace(listener, mkRoute("app"), "gw-ns", nil, getNamespace)
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("From: Selector, route namespace labels don't match", func(t *testing.T) {
		listener := Listener{AllowedRoutes: &AllowedRoutes{Namespaces: &RouteNamespaces{
			From:     &fromSelector,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"team": "checkout"}},
		}}}
		getNamespace := mkNamespaceGetter(&corev1.Namespace{
			Name: "app", Labels: map[string]string{"team": "platform"},
		})
		ok, err := ListenerAllowsNamespace(listener, mkRoute("app"), "gw-ns", nil, getNamespace)
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("From: Selector, route namespace not found, returns error", func(t *testing.T) {
		listener := Listener{AllowedRoutes: &AllowedRoutes{Namespaces: &RouteNamespaces{
			From:     &fromSelector,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"team": "checkout"}},
		}}}
		_, err := ListenerAllowsNamespace(listener, mkRoute("app"), "gw-ns", nil, mkNamespaceGetter())
		assert.ErrorIs(t, err, errNamespaceNotFound)
	})
}

func TestGatewayClassControlledBy(t *testing.T) {
	gwc := &GatewayClass{Spec: GatewayClassSpec{ControllerName: "konghq.com/kic-gateway-controller"}}

	assert.True(t, GatewayClassControlledBy(gwc, "konghq.com/kic-gateway-controller"))
	assert.False(t, GatewayClassControlledBy(gwc, "example.com/other-controller"))
}

func TestListenerResolvedRefsFalse(t *testing.T) {
	t.Run("listener not found returns false", func(t *testing.T) {
		assert.False(t, ListenerResolvedRefsFalse("tls", 1, nil))
	})

	t.Run("listener found but has no ResolvedRefs condition returns false", func(t *testing.T) {
		lss := []ListenerStatus{{
			Name:       "tls",
			Conditions: []metav1.Condition{{Type: string(ListenerConditionProgrammed), Status: metav1.ConditionFalse}},
		}}
		assert.False(t, ListenerResolvedRefsFalse("tls", 1, lss))
	})

	t.Run("listener found with ResolvedRefs=True returns false", func(t *testing.T) {
		lss := []ListenerStatus{{
			Name: "tls",
			Conditions: []metav1.Condition{
				{Type: string(ListenerConditionResolvedRefs), Status: metav1.ConditionTrue, ObservedGeneration: 1},
			},
		}}
		assert.False(t, ListenerResolvedRefsFalse("tls", 1, lss))
	})

	t.Run("listener found with ResolvedRefs=False at the current generation returns true", func(t *testing.T) {
		lss := []ListenerStatus{{
			Name: "tls",
			Conditions: []metav1.Condition{
				{
					Type:               string(ListenerConditionResolvedRefs),
					Status:             metav1.ConditionFalse,
					Reason:             string(ListenerReasonInvalidCertificateRef),
					ObservedGeneration: 1,
				},
			},
		}}
		assert.True(t, ListenerResolvedRefsFalse("tls", 1, lss))
	})

	t.Run("listener found with ResolvedRefs=False from a stale generation returns false", func(t *testing.T) {
		// The Gateway spec may have been fixed since this condition was
		// observed, and the controller just hasn't reconciled it yet - a stale
		// False must not be treated as a settled, permanent failure.
		lss := []ListenerStatus{{
			Name: "tls",
			Conditions: []metav1.Condition{
				{
					Type:               string(ListenerConditionResolvedRefs),
					Status:             metav1.ConditionFalse,
					Reason:             string(ListenerReasonInvalidCertificateRef),
					ObservedGeneration: 1,
				},
			},
		}}
		assert.False(t, ListenerResolvedRefsFalse("tls", 2, lss))
	})

	t.Run("only matches the requested listener name", func(t *testing.T) {
		lss := []ListenerStatus{
			{
				Name: "other",
				Conditions: []metav1.Condition{
					{Type: string(ListenerConditionResolvedRefs), Status: metav1.ConditionFalse, ObservedGeneration: 1},
				},
			},
			{
				Name: "tls",
				Conditions: []metav1.Condition{
					{Type: string(ListenerConditionResolvedRefs), Status: metav1.ConditionTrue, ObservedGeneration: 1},
				},
			},
		}
		assert.False(t, ListenerResolvedRefsFalse("tls", 1, lss))
	})
}

func TestListenerProgrammedFalseForSettledReason(t *testing.T) {
	t.Run("listener not found returns false", func(t *testing.T) {
		assert.False(t, ListenerProgrammedFalseForSettledReason("tls", 1, nil))
	})

	t.Run("listener found but has no Programmed condition returns false", func(t *testing.T) {
		lss := []ListenerStatus{{
			Name:       "tls",
			Conditions: []metav1.Condition{{Type: string(ListenerConditionResolvedRefs), Status: metav1.ConditionTrue}},
		}}
		assert.False(t, ListenerProgrammedFalseForSettledReason("tls", 1, lss))
	})

	t.Run("Programmed=True returns false", func(t *testing.T) {
		lss := []ListenerStatus{{
			Name: "tls",
			Conditions: []metav1.Condition{
				{Type: string(ListenerConditionProgrammed), Status: metav1.ConditionTrue, ObservedGeneration: 1},
			},
		}}
		assert.False(t, ListenerProgrammedFalseForSettledReason("tls", 1, lss))
	})

	t.Run("Programmed=False with reason Pending returns false (transient, keep waiting)", func(t *testing.T) {
		lss := []ListenerStatus{{
			Name: "tls",
			Conditions: []metav1.Condition{
				{
					Type:               string(ListenerConditionProgrammed),
					Status:             metav1.ConditionFalse,
					Reason:             string(ListenerReasonPending),
					ObservedGeneration: 1,
				},
			},
		}}
		assert.False(t, ListenerProgrammedFalseForSettledReason("tls", 1, lss))
	})

	t.Run("Programmed=False with a settled reason at the current generation returns true", func(t *testing.T) {
		lss := []ListenerStatus{{
			Name: "tls",
			Conditions: []metav1.Condition{
				{
					Type:               string(ListenerConditionProgrammed),
					Status:             metav1.ConditionFalse,
					Reason:             string(ListenerReasonInvalid),
					ObservedGeneration: 1,
				},
			},
		}}
		assert.True(t, ListenerProgrammedFalseForSettledReason("tls", 1, lss))
	})

	t.Run("Programmed=False with a settled reason from a stale generation returns false", func(t *testing.T) {
		lss := []ListenerStatus{{
			Name: "tls",
			Conditions: []metav1.Condition{
				{
					Type:               string(ListenerConditionProgrammed),
					Status:             metav1.ConditionFalse,
					Reason:             string(ListenerReasonInvalid),
					ObservedGeneration: 1,
				},
			},
		}}
		assert.False(t, ListenerProgrammedFalseForSettledReason("tls", 2, lss))
	})
}
