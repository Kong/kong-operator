package subtranslator

import (
	"strings"
	"testing"

	"github.com/kong/go-kong/kong"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/dataplane/kongstate"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/gatewayapi"
)

const (
	testHTTPRouteNamespace = "default"
	testHTTPRouteBackend   = "svc"
)

// httpRouteWithBackendTimeout builds a single-rule HTTPRoute with one backendRef and an
// optional backendRequest timeout, for exercising combined-mode service grouping.
func httpRouteWithBackendTimeout(name string, timeout *gatewayapi.Duration) *gatewayapi.HTTPRoute {
	rule := gatewayapi.HTTPRouteRule{
		BackendRefs: []gatewayapi.HTTPBackendRef{
			{
				BackendRef: gatewayapi.BackendRef{
					BackendObjectReference: gatewayapi.BackendObjectReference{
						Name: gatewayapi.ObjectName(testHTTPRouteBackend),
					},
				},
			},
		},
	}
	if timeout != nil {
		rule.Timeouts = &gatewayapi.HTTPRouteTimeouts{BackendRequest: timeout}
	}
	return &gatewayapi.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Namespace: testHTTPRouteNamespace, Name: name},
		Spec: gatewayapi.HTTPRouteSpec{
			Rules: []gatewayapi.HTTPRouteRule{rule},
		},
	}
}

// countServiceNamesWithTimeoutSuffix returns how many service names carry the ".timeout." suffix.
func countServiceNamesWithTimeoutSuffix(groups map[string][]httpRouteRuleMeta) int {
	count := 0
	for name := range groups {
		if strings.Contains(name, ".timeout.") {
			count++
		}
	}
	return count
}

func TestGroupRulesCombinedKeepsServiceNameForUniformTimeout(t *testing.T) {
	timeout := gatewayapi.Duration("500ms")
	groups := groupRulesFromHTTPRoutesByKongServiceName([]*gatewayapi.HTTPRoute{
		httpRouteWithBackendTimeout("route-a", &timeout),
		httpRouteWithBackendTimeout("route-b", &timeout),
	}, true)

	// Same backends + same timeout => single Kong service with the original (no-suffix) name.
	require.Len(t, groups, 1)
	require.Equal(t, 0, countServiceNamesWithTimeoutSuffix(groups), "uniform timeout must not rename the service")
	for _, rules := range groups {
		require.Len(t, rules, 2)
	}
}

func TestGroupRulesCombinedSplitsConflictingTimeouts(t *testing.T) {
	timeout500ms := gatewayapi.Duration("500ms")
	timeout1s := gatewayapi.Duration("1s")
	groups := groupRulesFromHTTPRoutesByKongServiceName([]*gatewayapi.HTTPRoute{
		httpRouteWithBackendTimeout("route-a", &timeout500ms),
		httpRouteWithBackendTimeout("route-b", &timeout1s),
	}, true)

	// Same backends + different timeouts => one Kong service per timeout, both suffixed.
	require.Len(t, groups, 2)
	require.Equal(t, 2, countServiceNamesWithTimeoutSuffix(groups))
	require.True(t, hasServiceNameWithSuffix(groups, ".timeout.500"), "expected a service suffixed with .timeout.500")
	require.True(t, hasServiceNameWithSuffix(groups, ".timeout.1000"), "expected a service suffixed with .timeout.1000")
}

func TestGroupRulesCombinedSplitsTimeoutAndNone(t *testing.T) {
	timeout := gatewayapi.Duration("500ms")
	groups := groupRulesFromHTTPRoutesByKongServiceName([]*gatewayapi.HTTPRoute{
		httpRouteWithBackendTimeout("route-a", &timeout),
		httpRouteWithBackendTimeout("route-b", nil),
	}, true)

	// Same backends, one with a timeout and one without => split; the no-timeout group keeps the base name.
	require.Len(t, groups, 2)
	require.Equal(t, 1, countServiceNamesWithTimeoutSuffix(groups), "only the timeout group is suffixed")
}

func TestGroupRulesCombinedDoesNotSplitEquivalentTimeouts(t *testing.T) {
	timeoutMs := gatewayapi.Duration("500ms")
	timeoutS := gatewayapi.Duration("0.5s")
	groups := groupRulesFromHTTPRoutesByKongServiceName([]*gatewayapi.HTTPRoute{
		httpRouteWithBackendTimeout("route-a", &timeoutMs),
		httpRouteWithBackendTimeout("route-b", &timeoutS),
	}, true)

	// 500ms and 0.5s are the same effective timeout => must not split.
	require.Len(t, groups, 1)
	require.Equal(t, 0, countServiceNamesWithTimeoutSuffix(groups))
}

// hasServiceNameWithSuffix reports whether any service name ends with the given suffix.
func hasServiceNameWithSuffix(groups map[string][]httpRouteRuleMeta, suffix string) bool {
	for name := range groups {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

func TestGroupRulesByBackendRefsSeparatesDifferentBackendRequestTimeouts(t *testing.T) {
	backendRef := gatewayapi.HTTPBackendRef{
		BackendRef: gatewayapi.BackendRef{
			BackendObjectReference: gatewayapi.BackendObjectReference{
				Name: "service-1",
			},
		},
	}
	timeout500ms := gatewayapi.Duration("500ms")
	timeout0s := gatewayapi.Duration("0s")

	groups := groupRulesByBackendRefs([]httpRouteRuleMeta{
		{
			Rule: gatewayapi.HTTPRouteRule{
				BackendRefs: []gatewayapi.HTTPBackendRef{backendRef},
				Timeouts: &gatewayapi.HTTPRouteTimeouts{
					BackendRequest: &timeout500ms,
				},
			},
		},
		{
			Rule: gatewayapi.HTTPRouteRule{
				BackendRefs: []gatewayapi.HTTPBackendRef{backendRef},
				Timeouts: &gatewayapi.HTTPRouteTimeouts{
					BackendRequest: &timeout0s,
				},
			},
		},
	})

	require.Len(t, groups, 2)
}

func TestApplyTimeoutToServiceFromHTTPRouteRuleMapsZeroToMaxKongTimeout(t *testing.T) {
	timeout := gatewayapi.Duration("0s")
	service := kongstate.Service{
		Service: kong.Service{
			ConnectTimeout: new(DefaultServiceTimeout),
			ReadTimeout:    new(DefaultServiceTimeout),
			WriteTimeout:   new(DefaultServiceTimeout),
		},
	}

	applyTimeoutToServiceFromHTTPRouteRule(&service, gatewayapi.HTTPRouteRule{
		Timeouts: &gatewayapi.HTTPRouteTimeouts{
			BackendRequest: &timeout,
		},
	})

	require.Equal(t, maxKongServiceTimeout, *service.ConnectTimeout)
	require.Equal(t, maxKongServiceTimeout, *service.ReadTimeout)
	require.Equal(t, maxKongServiceTimeout, *service.WriteTimeout)
}
