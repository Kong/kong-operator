package subtranslator

import (
	"sort"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/dataplane/kongstate"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/gatewayapi"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/store"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/util/builder"
)

func httpRouteWithRetry(name string, attempts *int) *gatewayapi.HTTPRoute {
	rule := gatewayapi.HTTPRouteRule{
		BackendRefs: []gatewayapi.HTTPBackendRef{
			builder.NewHTTPBackendRef(testHTTPRouteBackend).WithPort(80).Build(),
		},
	}
	if attempts != nil {
		rule.Retry = &gatewayapi.HTTPRouteRetry{Attempts: attempts}
	}
	return &gatewayapi.HTTPRoute{
		Namespace: testHTTPRouteNamespace, Name: name,
		Spec: gatewayapi.HTTPRouteSpec{
			Rules: []gatewayapi.HTTPRouteRule{rule},
		},
	}
}

func TestApplyRetriesToServiceFromHTTPRouteRule(t *testing.T) {
	tests := []struct {
		name             string
		retry            *gatewayapi.HTTPRouteRetry
		expectedRetries  *int
		expectedErrorMsg string
	}{
		{
			name:            "no retry stanza keeps the Kong default",
			expectedRetries: new(DefaultRetries),
		},
		{
			name:            "retry stanza defaults attempts to one",
			retry:           &gatewayapi.HTTPRouteRetry{},
			expectedRetries: new(1),
		},
		{
			name:            "attempts are applied",
			retry:           &gatewayapi.HTTPRouteRetry{Attempts: new(3)},
			expectedRetries: new(3),
		},
		{
			name:            "zero attempts disables retries",
			retry:           &gatewayapi.HTTPRouteRetry{Attempts: new(0)},
			expectedRetries: new(0),
		},
		{
			name:             "retry codes are rejected",
			retry:            &gatewayapi.HTTPRouteRetry{Codes: []gatewayapi.HTTPRouteRetryStatusCode{500}},
			expectedErrorMsg: "retry.codes is unsupported",
		},
		{
			name:             "retry backoff is rejected",
			retry:            &gatewayapi.HTTPRouteRetry{Backoff: new(gatewayapi.Duration("100ms"))},
			expectedErrorMsg: "retry.backoff is unsupported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := kongstate.Service{Retries: new(DefaultRetries)}
			err := applyRetriesToServiceFromHTTPRouteRule(&svc, gatewayapi.HTTPRouteRule{Retry: tt.retry})
			if tt.expectedErrorMsg != "" {
				require.ErrorContains(t, err, tt.expectedErrorMsg)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.expectedRetries, svc.Retries)
		})
	}
}

func TestGroupRulesByBackendRefsSeparatesDifferentRetries(t *testing.T) {
	groups := groupRulesByBackendRefs([]httpRouteRuleMeta{
		{Rule: gatewayapi.HTTPRouteRule{BackendRefs: []gatewayapi.HTTPBackendRef{{Name: testHTTPRouteBackend}}, Retry: &gatewayapi.HTTPRouteRetry{Attempts: new(0)}}},
		{Rule: gatewayapi.HTTPRouteRule{BackendRefs: []gatewayapi.HTTPBackendRef{{Name: testHTTPRouteBackend}}, Retry: &gatewayapi.HTTPRouteRetry{Attempts: new(3)}}},
	})

	require.Len(t, groups, 2)
}

func TestGroupRulesCombinedSplitsConflictingRetries(t *testing.T) {
	groups := groupRulesFromHTTPRoutesByKongServiceName([]*gatewayapi.HTTPRoute{
		httpRouteWithRetry("route-a", new(0)),
		httpRouteWithRetry("route-b", new(3)),
	}, true)

	require.Len(t, groups, 2)
	require.True(t, hasServiceNameWithSuffix(groups, ".retries.0"), "expected a service suffixed with .retries.0")
	require.True(t, hasServiceNameWithSuffix(groups, ".retries.3"), "expected a service suffixed with .retries.3")
}

func TestGroupRulesCombinedDoesNotSplitEquivalentRetries(t *testing.T) {
	groups := groupRulesFromHTTPRoutesByKongServiceName([]*gatewayapi.HTTPRoute{
		httpRouteWithRetry("route-a", new(5)),
		httpRouteWithRetry("route-b", nil),
	}, true)

	require.Len(t, groups, 1)
	require.False(t, hasServiceNameWithSuffix(groups, ".retries."))
}

func TestTranslateHTTPRoutesToKongstateServicesAppliesRetries(t *testing.T) {
	route := httpRouteWithRetry("retry-route", new(2))
	route.TypeMeta = metav1.TypeMeta{APIVersion: "gateway.networking.k8s.io/v1", Kind: "HTTPRoute"}
	route.Spec.CommonRouteSpec = commonRouteSpecMock("fake-gateway-1")

	fakeStore, err := store.NewFakeStore(store.FakeObjects{
		Services: []*corev1.Service{{
			Name:      testHTTPRouteBackend,
			Namespace: testHTTPRouteNamespace,
			Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{
				Port: 80,
			}}},
		}},
	})
	require.NoError(t, err)

	result := TranslateHTTPRoutesToKongstateServices(
		logr.Discard(),
		fakeStore,
		[]*gatewayapi.HTTPRoute{route},
		TranslateHTTPRouteToKongstateServiceOptions{},
	)
	require.Empty(t, result.HTTPRouteNameToTranslationErrors)
	require.Len(t, result.ServiceNameToKongstateService, 1)
	for _, svc := range result.ServiceNameToKongstateService {
		require.NotNil(t, svc.Retries)
		require.Equal(t, 2, *svc.Retries)
	}
}

func TestTranslateHTTPRoutesToKongstateServicesKeepsPerRuleRetries(t *testing.T) {
	routes := []*gatewayapi.HTTPRoute{
		httpRouteWithRetry("route-a", new(0)),
		httpRouteWithRetry("route-b", new(3)),
	}
	for _, route := range routes {
		route.TypeMeta = metav1.TypeMeta{APIVersion: "gateway.networking.k8s.io/v1", Kind: "HTTPRoute"}
		route.Spec.CommonRouteSpec = commonRouteSpecMock("fake-gateway-1")
	}

	fakeStore, err := store.NewFakeStore(store.FakeObjects{
		Services: []*corev1.Service{{
			Name:      testHTTPRouteBackend,
			Namespace: testHTTPRouteNamespace,
			Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{
				Port: 80,
			}}},
		}},
	})
	require.NoError(t, err)

	result := TranslateHTTPRoutesToKongstateServices(
		logr.Discard(),
		fakeStore,
		routes,
		TranslateHTTPRouteToKongstateServiceOptions{CombinedServicesFromDifferentHTTPRoutes: true},
	)
	require.Empty(t, result.HTTPRouteNameToTranslationErrors)
	require.Len(t, result.ServiceNameToKongstateService, 2)

	var retries []int
	for _, svc := range result.ServiceNameToKongstateService {
		require.NotNil(t, svc.Retries)
		retries = append(retries, *svc.Retries)
	}
	sort.Ints(retries)
	require.Equal(t, []int{0, 3}, retries, "rules sharing a backend must keep their own retry settings")
}

func TestTranslateHTTPRoutesToKongstateServicesRejectsUnsupportedRetry(t *testing.T) {
	route := httpRouteWithRetry("retry-route", new(2))
	route.TypeMeta = metav1.TypeMeta{APIVersion: "gateway.networking.k8s.io/v1", Kind: "HTTPRoute"}
	route.Spec.CommonRouteSpec = commonRouteSpecMock("fake-gateway-1")
	route.Spec.Rules[0].Retry.Codes = []gatewayapi.HTTPRouteRetryStatusCode{500}

	result := TranslateHTTPRoutesToKongstateServices(
		logr.Discard(),
		store.NewFakeStoreEmpty(),
		[]*gatewayapi.HTTPRoute{route},
		TranslateHTTPRouteToKongstateServiceOptions{},
	)

	nn := k8stypes.NamespacedName{Namespace: testHTTPRouteNamespace, Name: "retry-route"}
	require.Contains(t, result.HTTPRouteNameToTranslationErrors, nn)
	require.NotEmpty(t, result.HTTPRouteNameToTranslationErrors[nn])
	require.Contains(t, result.HTTPRouteNameToTranslationErrors[nn][0].Error(), "retry.codes is unsupported")
	require.Empty(t, result.ServiceNameToKongstateService)
}

func TestTranslateHTTPRoutesToKongstateServicesIsolatesUnsupportedRetryFromHealthyRoute(t *testing.T) {
	healthyRoute := httpRouteWithRetry("healthy-route", nil)
	healthyRoute.TypeMeta = metav1.TypeMeta{APIVersion: "gateway.networking.k8s.io/v1", Kind: "HTTPRoute"}
	healthyRoute.Spec.CommonRouteSpec = commonRouteSpecMock("fake-gateway-1")

	invalidRoute := httpRouteWithRetry("invalid-retry-route", nil)
	invalidRoute.TypeMeta = metav1.TypeMeta{APIVersion: "gateway.networking.k8s.io/v1", Kind: "HTTPRoute"}
	invalidRoute.Spec.CommonRouteSpec = commonRouteSpecMock("fake-gateway-1")
	invalidRoute.Spec.Rules[0].Retry = &gatewayapi.HTTPRouteRetry{
		Codes: []gatewayapi.HTTPRouteRetryStatusCode{503},
	}

	fakeStore, err := store.NewFakeStore(store.FakeObjects{
		Services: []*corev1.Service{{
			Name:      testHTTPRouteBackend,
			Namespace: testHTTPRouteNamespace,
			Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{
				Port: 80,
			}}},
		}},
	})
	require.NoError(t, err)

	result := TranslateHTTPRoutesToKongstateServices(
		logr.Discard(),
		fakeStore,
		[]*gatewayapi.HTTPRoute{healthyRoute, invalidRoute},
		TranslateHTTPRouteToKongstateServiceOptions{CombinedServicesFromDifferentHTTPRoutes: true},
	)

	healthyNN := k8stypes.NamespacedName{Namespace: testHTTPRouteNamespace, Name: "healthy-route"}
	invalidNN := k8stypes.NamespacedName{Namespace: testHTTPRouteNamespace, Name: "invalid-retry-route"}
	require.NotContains(t, result.HTTPRouteNameToTranslationErrors, healthyNN)
	require.Contains(t, result.HTTPRouteNameToTranslationErrors, invalidNN)
	require.ErrorContains(t, result.HTTPRouteNameToTranslationErrors[invalidNN][0], "retry.codes is unsupported")

	require.Len(t, result.ServiceNameToKongstateService, 1)
	for svcName, svc := range result.ServiceNameToKongstateService {
		require.NotContains(t, svcName, ".retries.")
		require.NotNil(t, svc.Retries)
		require.Equal(t, DefaultRetries, *svc.Retries)
		require.Len(t, svc.Routes, 1)
	}
}

func TestKongServiceConfigNameSuffixCombinesTimeoutAndRetries(t *testing.T) {
	suffix := kongServiceConfigNameSuffix("500|2")
	require.True(t, strings.HasPrefix(suffix, ".timeout.500"))
	require.True(t, strings.HasSuffix(suffix, ".retries.2"))
}
