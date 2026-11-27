package gateway

import (
	"fmt"
	"os"
	"testing"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/controllers"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/util"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/util/builder"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/pkg/clientset/scheme"
)

func init() {
	if err := corev1.AddToScheme(scheme.Scheme); err != nil {
		fmt.Println("error while adding core1 scheme")
		os.Exit(1)
	}
	if err := gatewayv1.Install(scheme.Scheme); err != nil {
		fmt.Println("error while adding gatewayv1 scheme")
		os.Exit(1)
	}
}

func TestIsGatewayProgrammedSettledForRoute(t *testing.T) {
	udpListener := gwtypes.Listener{
		Name:     "udp",
		Protocol: gwtypes.UDPProtocolType,
		Port:     9999,
	}

	testCases := []struct {
		name                  string
		gateway               *gwtypes.Gateway
		listener              string
		attachedListenerNames []gwtypes.SectionName
		wantReady             bool
	}{
		{
			name: "gateway programmed and matching listener programmed",
			gateway: programmedGatewayForRouteReadinessTest(
				[]gwtypes.Listener{udpListener},
				[]gwtypes.ListenerStatus{programmedListenerStatusForRouteReadinessTest("udp", metav1.ConditionTrue)},
			),
			listener:              "udp",
			attachedListenerNames: []gwtypes.SectionName{"udp"},
			wantReady:             true,
		},
		{
			name: "gateway programmed but matching listener status missing",
			gateway: programmedGatewayForRouteReadinessTest(
				[]gwtypes.Listener{udpListener},
				nil,
			),
			listener:              "udp",
			attachedListenerNames: []gwtypes.SectionName{"udp"},
			wantReady:             false,
		},
		{
			name: "gateway programmed but matching listener not programmed",
			gateway: programmedGatewayForRouteReadinessTest(
				[]gwtypes.Listener{udpListener},
				[]gwtypes.ListenerStatus{programmedListenerStatusForRouteReadinessTest("udp", metav1.ConditionFalse)},
			),
			listener:              "udp",
			attachedListenerNames: []gwtypes.SectionName{"udp"},
			wantReady:             false,
		},
		{
			name: "gateway not programmed even when matching listener is programmed",
			gateway: &gwtypes.Gateway{
				Generation: 1,
				Spec: gwtypes.GatewaySpec{
					Listeners: []gwtypes.Listener{udpListener},
				},
				Status: gwtypes.GatewayStatus{
					Listeners: []gwtypes.ListenerStatus{programmedListenerStatusForRouteReadinessTest("udp", metav1.ConditionTrue)},
				},
			},
			listener:              "udp",
			attachedListenerNames: []gwtypes.SectionName{"udp"},
			wantReady:             false,
		},
		{
			name: "specified listener absent from spec is terminal not transient",
			gateway: programmedGatewayForRouteReadinessTest(
				[]gwtypes.Listener{{Name: "other", Protocol: gwtypes.UDPProtocolType, Port: 9999}},
				nil,
			),
			listener:  "udp",
			wantReady: true,
		},
		{
			name: "no section name waits for matching route protocol listener, ignoring unrelated programmed listener",
			gateway: programmedGatewayForRouteReadinessTest(
				[]gwtypes.Listener{
					{Name: "http", Protocol: gwtypes.HTTPProtocolType, Port: 80},
					udpListener,
				},
				[]gwtypes.ListenerStatus{programmedListenerStatusForRouteReadinessTest("http", metav1.ConditionTrue)},
			),
			attachedListenerNames: []gwtypes.SectionName{"udp"},
			wantReady:             false,
		},
		{
			name: "matching listener not programmed because ResolvedRefs is permanently false is not transient",
			gateway: programmedGatewayForRouteReadinessTest(
				[]gwtypes.Listener{udpListener},
				[]gwtypes.ListenerStatus{{
					Name: "udp",
					Conditions: []metav1.Condition{
						{
							Type:               string(gwtypes.ListenerConditionResolvedRefs),
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
							Reason:             string(gwtypes.ListenerReasonInvalidCertificateRef),
						},
						{
							Type:               string(gwtypes.ListenerConditionProgrammed),
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
							Reason:             "Pending",
							Message:            "Listener references are not resolved yet.",
						},
					},
				}},
			),
			listener:              "udp",
			attachedListenerNames: []gwtypes.SectionName{"udp"},
			wantReady:             true,
		},
		{
			name: "no section name ignores same protocol listener that did not attach",
			gateway: programmedGatewayForRouteReadinessTest(
				[]gwtypes.Listener{
					{Name: "udp-attached", Protocol: gwtypes.UDPProtocolType, Port: 9999},
					{Name: "udp-unattached", Protocol: gwtypes.UDPProtocolType, Port: 9998},
				},
				[]gwtypes.ListenerStatus{programmedListenerStatusForRouteReadinessTest("udp-unattached", metav1.ConditionTrue)},
			),
			attachedListenerNames: []gwtypes.SectionName{"udp-attached"},
			wantReady:             false,
		},
		{
			name: "two attached listeners, one programmed and one still transiently pending proceeds via the programmed one",
			gateway: programmedGatewayForRouteReadinessTest(
				[]gwtypes.Listener{
					{Name: "udp-ready", Protocol: gwtypes.UDPProtocolType, Port: 9999},
					{Name: "udp-pending", Protocol: gwtypes.UDPProtocolType, Port: 9998},
				},
				[]gwtypes.ListenerStatus{
					programmedListenerStatusForRouteReadinessTest("udp-ready", metav1.ConditionTrue),
					programmedListenerStatusForRouteReadinessTest("udp-pending", metav1.ConditionFalse),
				},
			),
			attachedListenerNames: []gwtypes.SectionName{"udp-ready", "udp-pending"},
			wantReady:             true,
		},
		{
			name: "two attached listeners, pending one first and programmed one second still proceeds",
			gateway: programmedGatewayForRouteReadinessTest(
				[]gwtypes.Listener{
					{Name: "udp-pending", Protocol: gwtypes.UDPProtocolType, Port: 9998},
					{Name: "udp-ready", Protocol: gwtypes.UDPProtocolType, Port: 9999},
				},
				[]gwtypes.ListenerStatus{
					programmedListenerStatusForRouteReadinessTest("udp-pending", metav1.ConditionFalse),
					programmedListenerStatusForRouteReadinessTest("udp-ready", metav1.ConditionTrue),
				},
			),
			attachedListenerNames: []gwtypes.SectionName{"udp-pending", "udp-ready"},
			wantReady:             true,
		},
		{
			name: "two attached listeners, one permanently broken and one still transiently pending waits for the pending one",
			gateway: programmedGatewayForRouteReadinessTest(
				[]gwtypes.Listener{
					{Name: "udp-broken", Protocol: gwtypes.UDPProtocolType, Port: 9999},
					{Name: "udp-pending", Protocol: gwtypes.UDPProtocolType, Port: 9998},
				},
				[]gwtypes.ListenerStatus{
					{
						Name: "udp-broken",
						Conditions: []metav1.Condition{
							{
								Type:               string(gwtypes.ListenerConditionResolvedRefs),
								Status:             metav1.ConditionFalse,
								ObservedGeneration: 1,
								Reason:             string(gwtypes.ListenerReasonInvalidCertificateRef),
							},
							{
								Type:               string(gwtypes.ListenerConditionProgrammed),
								Status:             metav1.ConditionFalse,
								ObservedGeneration: 1,
								Reason:             "Pending",
							},
						},
					},
					programmedListenerStatusForRouteReadinessTest("udp-pending", metav1.ConditionFalse),
				},
			),
			attachedListenerNames: []gwtypes.SectionName{"udp-broken", "udp-pending"},
			wantReady:             false,
		},
		{
			name: "two attached listeners, one programmed and one permanently broken proceeds via the programmed one",
			gateway: programmedGatewayForRouteReadinessTest(
				[]gwtypes.Listener{
					{Name: "udp-ready", Protocol: gwtypes.UDPProtocolType, Port: 9999},
					{Name: "udp-broken", Protocol: gwtypes.UDPProtocolType, Port: 9998},
				},
				[]gwtypes.ListenerStatus{
					programmedListenerStatusForRouteReadinessTest("udp-ready", metav1.ConditionTrue),
					{
						Name: "udp-broken",
						Conditions: []metav1.Condition{
							{
								Type:               string(gwtypes.ListenerConditionResolvedRefs),
								Status:             metav1.ConditionFalse,
								ObservedGeneration: 1,
								Reason:             string(gwtypes.ListenerReasonInvalidCertificateRef),
							},
							{
								Type:               string(gwtypes.ListenerConditionProgrammed),
								Status:             metav1.ConditionFalse,
								ObservedGeneration: 1,
								Reason:             "Pending",
							},
						},
					},
				},
			),
			attachedListenerNames: []gwtypes.SectionName{"udp-ready", "udp-broken"},
			wantReady:             true,
		},
		{
			name: "matching listener not programmed for a settled non-ResolvedRefs reason is not transient",
			gateway: programmedGatewayForRouteReadinessTest(
				[]gwtypes.Listener{udpListener},
				[]gwtypes.ListenerStatus{{
					Name: "udp",
					Conditions: []metav1.Condition{
						{
							Type:               string(gwtypes.ListenerConditionProgrammed),
							Status:             metav1.ConditionFalse,
							ObservedGeneration: 1,
							Reason:             string(gwtypes.ListenerReasonInvalid),
						},
					},
				}},
			),
			listener:              "udp",
			attachedListenerNames: []gwtypes.SectionName{"udp"},
			wantReady:             true,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			gateway := supportedGatewayWithCondition{
				gateway:               tt.gateway,
				listenerName:          tt.listener,
				attachedListenerNames: tt.attachedListenerNames,
			}
			assert.Equal(t, tt.wantReady, isGatewayProgrammedSettledForRoute(gateway))
		})
	}
}

func programmedGatewayForRouteReadinessTest(
	listeners []gwtypes.Listener,
	listenerStatuses []gwtypes.ListenerStatus,
) *gwtypes.Gateway {
	return &gwtypes.Gateway{
		Generation: 1,
		Spec: gwtypes.GatewaySpec{
			Listeners: listeners,
		},
		Status: gwtypes.GatewayStatus{
			Conditions: []metav1.Condition{{
				Type:               string(gwtypes.GatewayConditionProgrammed),
				Status:             metav1.ConditionTrue,
				ObservedGeneration: 1,
				Reason:             string(gwtypes.GatewayReasonProgrammed),
			}},
			Listeners: listenerStatuses,
		},
	}
}

func programmedListenerStatusForRouteReadinessTest(
	name gwtypes.SectionName,
	conditionStatus metav1.ConditionStatus,
) gwtypes.ListenerStatus {
	reason := string(gwtypes.ListenerReasonProgrammed)
	if conditionStatus != metav1.ConditionTrue {
		reason = string(gwtypes.ListenerReasonPending)
	}
	return gwtypes.ListenerStatus{
		Name: name,
		Conditions: []metav1.Condition{{
			Type:               string(gwtypes.ListenerConditionProgrammed),
			Status:             conditionStatus,
			ObservedGeneration: 1,
			Reason:             reason,
		}},
	}
}

func TestFilterHostnames(t *testing.T) {
	commonGateway := &gwtypes.Gateway{
		Spec: gwtypes.GatewaySpec{
			Listeners: []gwtypes.Listener{
				{
					Name:     "listener-1",
					Hostname: util.StringToTypedPtr[*gwtypes.Hostname]("very.specific.com"),
				},
				{
					Name:     "listener-2",
					Hostname: util.StringToTypedPtr[*gwtypes.Hostname]("*.wildcard.io"),
				},
				{
					Name:     "listener-3",
					Hostname: util.StringToTypedPtr[*gwtypes.Hostname]("*.anotherwildcard.io"),
				},
				{
					Name: "listener-4",
				},
			},
		},
	}

	testCases := []struct {
		name              string
		gateways          []supportedGatewayWithCondition
		httpRoute         *gwtypes.HTTPRoute
		expectedHTTPRoute *gwtypes.HTTPRoute
		hasError          bool
		errString         string
	}{
		{
			name: "listener 1 - specific",
			gateways: []supportedGatewayWithCondition{
				{
					gateway:      commonGateway,
					listenerName: "listener-1",
				},
			},
			httpRoute: &gwtypes.HTTPRoute{
				Spec: gwtypes.HTTPRouteSpec{
					Hostnames: []gwtypes.Hostname{
						"*.anotherwildcard.io",
						"*.nonmatchingwildcard.io",
						"very.specific.com",
					},
				},
			},
			expectedHTTPRoute: &gwtypes.HTTPRoute{
				Spec: gwtypes.HTTPRouteSpec{
					Hostnames: []gwtypes.Hostname{
						"very.specific.com",
					},
				},
			},
		},
		{
			name: "listener 1 - wildcard",
			gateways: []supportedGatewayWithCondition{
				{
					gateway:      commonGateway,
					listenerName: "listener-1",
				},
			},
			httpRoute: &gwtypes.HTTPRoute{
				Spec: gwtypes.HTTPRouteSpec{
					Hostnames: []gwtypes.Hostname{
						"non.matching.com",
						"*.specific.com",
					},
				},
			},
			expectedHTTPRoute: &gwtypes.HTTPRoute{
				Spec: gwtypes.HTTPRouteSpec{
					Hostnames: []gwtypes.Hostname{
						"very.specific.com",
					},
				},
			},
		},
		{
			name: "listener 2",
			gateways: []supportedGatewayWithCondition{
				{
					gateway:      commonGateway,
					listenerName: "listener-2",
				},
			},
			httpRoute: &gwtypes.HTTPRoute{
				Spec: gwtypes.HTTPRouteSpec{
					Hostnames: []gwtypes.Hostname{
						"non.matching.com",
						"wildcard.io",
						"foo.wildcard.io",
						"bar.wildcard.io",
						"foo.bar.wildcard.io",
					},
				},
			},
			expectedHTTPRoute: &gwtypes.HTTPRoute{
				Spec: gwtypes.HTTPRouteSpec{
					Hostnames: []gwtypes.Hostname{
						"foo.wildcard.io",
						"bar.wildcard.io",
						"foo.bar.wildcard.io",
					},
				},
			},
		},
		{
			name: "listener 3 - wildcard",
			gateways: []supportedGatewayWithCondition{
				{
					gateway:      commonGateway,
					listenerName: "listener-3",
				},
			},
			httpRoute: &gwtypes.HTTPRoute{
				Spec: gwtypes.HTTPRouteSpec{
					Hostnames: []gwtypes.Hostname{
						"*.anotherwildcard.io",
					},
				},
			},
			expectedHTTPRoute: &gwtypes.HTTPRoute{
				Spec: gwtypes.HTTPRouteSpec{
					Hostnames: []gwtypes.Hostname{
						"*.anotherwildcard.io",
					},
				},
			},
		},
		{
			name: "no listner specified - no hostname",
			gateways: []supportedGatewayWithCondition{
				{
					gateway: commonGateway,
				},
			},
			httpRoute: &gwtypes.HTTPRoute{
				Spec: gwtypes.HTTPRouteSpec{
					Hostnames: []gwtypes.Hostname{},
				},
			},
			expectedHTTPRoute: &gwtypes.HTTPRoute{
				Spec: gwtypes.HTTPRouteSpec{
					Hostnames: []gwtypes.Hostname{},
				},
			},
		},
		{
			name: "listener 1 - no match",
			gateways: []supportedGatewayWithCondition{
				{
					gateway:      commonGateway,
					listenerName: "listner-1",
				},
			},
			httpRoute: &gwtypes.HTTPRoute{
				Spec: gwtypes.HTTPRouteSpec{
					Hostnames: []gwtypes.Hostname{
						"specific.but.wrong.com",
						"wildcard.io",
					},
				},
			},
			expectedHTTPRoute: &gwtypes.HTTPRoute{
				Spec: gwtypes.HTTPRouteSpec{
					Hostnames: []gwtypes.Hostname{},
				},
			},
			hasError:  true,
			errString: "no matching hostnames in listener",
		},
	}

	for _, tc := range testCases {
		filteredHTTPRoute, err := filterHostnames(tc.gateways, tc.httpRoute)
		if tc.hasError {
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tc.errString)
		} else {
			assert.NoError(t, err)
			assert.Equal(t, tc.expectedHTTPRoute.Spec, filteredHTTPRoute.Spec, tc.name)
		}
	}
}

func TestFilterHostnames_TLSRoute(t *testing.T) {
	// Listeners modelled on the gateway-api TLSRouteHostnameIntersection conformance:
	// abc.example.com, *.example.com, *.com, and an empty (match-any) listener.
	commonGateway := &gwtypes.Gateway{
		Spec: gwtypes.GatewaySpec{
			Listeners: []gwtypes.Listener{
				{
					Name:     "listener-exact-hostname",
					Hostname: util.StringToTypedPtr[*gwtypes.Hostname]("abc.example.com"),
				},
				{
					Name:     "listener-more-specific-wc-hostname",
					Hostname: util.StringToTypedPtr[*gwtypes.Hostname]("*.example.com"),
				},
				{
					Name:     "listener-less-specific-wc-hostname",
					Hostname: util.StringToTypedPtr[*gwtypes.Hostname]("*.com"),
				},
				{
					Name: "listener-empty-hostname",
				},
			},
		},
	}

	testCases := []struct {
		name             string
		gateways         []supportedGatewayWithCondition
		tlsRoute         *gwtypes.TLSRoute
		expectedTLSRoute *gwtypes.TLSRoute
		hasError         bool
		errString        string
	}{
		{
			name: "exact listener × wildcard route — listener hostname returned",
			gateways: []supportedGatewayWithCondition{
				{gateway: commonGateway, listenerName: "listener-exact-hostname"},
			},
			tlsRoute: &gwtypes.TLSRoute{
				Spec: gwtypes.TLSRouteSpec{
					Hostnames: []gwtypes.Hostname{"*.example.com"},
				},
			},
			expectedTLSRoute: &gwtypes.TLSRoute{
				Spec: gwtypes.TLSRouteSpec{
					Hostnames: []gwtypes.Hostname{"abc.example.com"},
				},
			},
		},
		{
			name: "more-specific wildcard listener × exact route — exact route returned",
			gateways: []supportedGatewayWithCondition{
				{gateway: commonGateway, listenerName: "listener-more-specific-wc-hostname"},
			},
			tlsRoute: &gwtypes.TLSRoute{
				Spec: gwtypes.TLSRouteSpec{
					Hostnames: []gwtypes.Hostname{"abc.example.com"},
				},
			},
			expectedTLSRoute: &gwtypes.TLSRoute{
				Spec: gwtypes.TLSRouteSpec{
					Hostnames: []gwtypes.Hostname{"abc.example.com"},
				},
			},
		},
		{
			name: "more-specific wildcard listener × less-specific wildcard route — listener returned",
			gateways: []supportedGatewayWithCondition{
				{gateway: commonGateway, listenerName: "listener-more-specific-wc-hostname"},
			},
			tlsRoute: &gwtypes.TLSRoute{
				Spec: gwtypes.TLSRouteSpec{
					Hostnames: []gwtypes.Hostname{"*.com"},
				},
			},
			expectedTLSRoute: &gwtypes.TLSRoute{
				Spec: gwtypes.TLSRouteSpec{
					Hostnames: []gwtypes.Hostname{"*.example.com"},
				},
			},
		},
		{
			name: "less-specific wildcard listener × more-specific wildcard route — route returned",
			gateways: []supportedGatewayWithCondition{
				{gateway: commonGateway, listenerName: "listener-less-specific-wc-hostname"},
			},
			tlsRoute: &gwtypes.TLSRoute{
				Spec: gwtypes.TLSRouteSpec{
					Hostnames: []gwtypes.Hostname{"*.example.com"},
				},
			},
			expectedTLSRoute: &gwtypes.TLSRoute{
				Spec: gwtypes.TLSRouteSpec{
					Hostnames: []gwtypes.Hostname{"*.example.com"},
				},
			},
		},
		{
			name: "empty listener × wildcard route — route returned verbatim",
			gateways: []supportedGatewayWithCondition{
				{gateway: commonGateway, listenerName: "listener-empty-hostname"},
			},
			tlsRoute: &gwtypes.TLSRoute{
				Spec: gwtypes.TLSRouteSpec{
					Hostnames: []gwtypes.Hostname{"*.com"},
				},
			},
			expectedTLSRoute: &gwtypes.TLSRoute{
				Spec: gwtypes.TLSRouteSpec{
					Hostnames: []gwtypes.Hostname{"*.com"},
				},
			},
		},
		{
			name: "no intersection — returns ErrNoMatchingListenerHostname",
			gateways: []supportedGatewayWithCondition{
				{gateway: commonGateway, listenerName: "listener-exact-hostname"},
			},
			tlsRoute: &gwtypes.TLSRoute{
				Spec: gwtypes.TLSRouteSpec{
					Hostnames: []gwtypes.Hostname{"some.other.org"},
				},
			},
			expectedTLSRoute: &gwtypes.TLSRoute{
				Spec: gwtypes.TLSRouteSpec{
					Hostnames: []gwtypes.Hostname{},
				},
			},
			hasError:  true,
			errString: "no matching hostnames in listener",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			filteredTLSRoute, err := filterHostnames(tc.gateways, tc.tlsRoute)
			if tc.hasError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tc.errString)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.expectedTLSRoute.Spec, filteredTLSRoute.Spec)
			}
		})
	}
}

func TestGetSupportedGatewayForRoute(t *testing.T) {
	gatewayClass := &gwtypes.GatewayClass{
		Name: "test-gatewayclass",
		Spec: gwtypes.GatewayClassSpec{
			ControllerName: gwtypes.GatewayController("konghq.com/kic-gateway-controller"),
		},
	}

	namespace := &corev1.Namespace{
		Name: "test-namespace",
	}

	routeConditionAccepted := func(status metav1.ConditionStatus, reason gwtypes.RouteConditionReason) metav1.Condition {
		return metav1.Condition{
			Type:   string(gwtypes.RouteConditionAccepted),
			Status: status,
			Reason: string(reason),
		}
	}

	type expected struct {
		condition             metav1.Condition
		message               string
		listenerName          string
		attachedListenerNames []gwtypes.SectionName
	}

	goodGroup := gwtypes.Group(gatewayv1.GroupName)
	goodKind := gwtypes.Kind("Gateway")
	basicHTTPRoute := func() *gwtypes.HTTPRoute {
		return &gwtypes.HTTPRoute{
			TypeMeta:  gwtypes.V1GatewayTypeMeta,
			Name:      "basic-httproute",
			Namespace: "test-namespace",
			Spec: gwtypes.HTTPRouteSpec{
				CommonRouteSpec: gwtypes.CommonRouteSpec{
					ParentRefs: []gwtypes.ParentReference{
						{
							Group: &goodGroup,
							Kind:  &goodKind,
							Name:  "test-gateway",
						},
					},
				},
				Rules: []gwtypes.HTTPRouteRule{
					{
						BackendRefs: []gwtypes.HTTPBackendRef{
							builder.NewHTTPBackendRef("fake-service").WithPort(80).Build(),
						},
					},
				},
			},
		}
	}
	t.Run("HTTPRoute", func(t *testing.T) {
		gatewayWithHTTP80Ready := func() *gwtypes.Gateway {
			return &gwtypes.Gateway{
				TypeMeta:  gwtypes.V1GatewayTypeMeta,
				Name:      "test-gateway",
				Namespace: "test-namespace",
				UID:       "ce7f0678-f59a-483c-80d1-243d3738d22c",
				Spec: gwtypes.GatewaySpec{
					GatewayClassName: "test-gatewayclass",
					Listeners:        builder.NewListener("http").WithPort(80).HTTP().IntoSlice(),
				},
				Status: gwtypes.GatewayStatus{
					Listeners: []gwtypes.ListenerStatus{
						{
							Name: "http",
							Conditions: []metav1.Condition{
								{
									Type:   string(gwtypes.ListenerConditionProgrammed),
									Status: metav1.ConditionTrue,
								},
							},
							SupportedKinds: supportedRouteGroupKinds,
						},
					},
				},
			}
		}

		tests := []struct {
			name     string
			route    *gwtypes.HTTPRoute
			expected []expected
			objects  []client.Object
		}{
			{
				name:  "basic HTTPRoute gets accepted",
				route: basicHTTPRoute(),
				objects: []client.Object{
					gatewayWithHTTP80Ready(),
					gatewayClass,
					namespace,
				},
				expected: []expected{
					{
						condition: routeConditionAccepted(metav1.ConditionTrue, gwtypes.RouteReasonAccepted),
					},
				},
			},
			{
				name:  "basic HTTPRoute with TLS configuration gets accepted",
				route: basicHTTPRoute(),
				objects: []client.Object{
					func() *gwtypes.Gateway {
						gw := gatewayWithHTTP80Ready()
						gw.Spec.Listeners = builder.
							NewListener("http").WithPort(443).HTTPS().
							WithTLSConfig(&gwtypes.GatewayTLSConfig{
								Mode: new(gwtypes.TLSModeTerminate),
							}).
							IntoSlice()
						return gw
					}(),
					gatewayClass,
					namespace,
				},
				expected: []expected{
					{
						condition: routeConditionAccepted(metav1.ConditionTrue, gwtypes.RouteReasonAccepted),
					},
				},
			},
			{
				name: "basic HTTPRoute specifying existing section name gets Accepted",
				route: func() *gwtypes.HTTPRoute {
					r := basicHTTPRoute()
					r.Spec.ParentRefs[0].SectionName = new(gwtypes.SectionName("http"))
					return r
				}(),
				objects: []client.Object{
					gatewayWithHTTP80Ready(),
					gatewayClass,
					namespace,
				},
				expected: []expected{
					{
						listenerName: "http",
						condition:    routeConditionAccepted(metav1.ConditionTrue, gwtypes.RouteReasonAccepted),
					},
				},
			},
			{
				name: "basic HTTPRoute specifying existing port gets Accepted",
				route: func() *gwtypes.HTTPRoute {
					r := basicHTTPRoute()
					r.Spec.CommonRouteSpec.ParentRefs[0].Port = new(gwtypes.PortNumber(80))
					return r
				}(),
				objects: []client.Object{
					gatewayWithHTTP80Ready(),
					gatewayClass,
					namespace,
				},
				expected: []expected{
					{
						condition: routeConditionAccepted(metav1.ConditionTrue, gwtypes.RouteReasonAccepted),
					},
				},
			},
			{
				name: "basic HTTPRoute without section name tracks only attached listeners",
				route: func() *gwtypes.HTTPRoute {
					r := basicHTTPRoute()
					r.Spec.CommonRouteSpec.ParentRefs[0].Port = new(gwtypes.PortNumber(80))
					return r
				}(),
				objects: []client.Object{
					func() *gwtypes.Gateway {
						gw := gatewayWithHTTP80Ready()
						gw.Spec.Listeners = []gwtypes.Listener{
							{Name: "http", Port: 80, Protocol: gwtypes.HTTPProtocolType},
							{Name: "http-unattached", Port: 81, Protocol: gwtypes.HTTPProtocolType},
						}
						gw.Status.Listeners = []gwtypes.ListenerStatus{
							{
								Name: "http",
								Conditions: []metav1.Condition{{
									Type:   string(gwtypes.ListenerConditionProgrammed),
									Status: metav1.ConditionTrue,
								}},
								SupportedKinds: supportedRouteGroupKinds,
							},
							{
								Name: "http-unattached",
								Conditions: []metav1.Condition{{
									Type:   string(gwtypes.ListenerConditionProgrammed),
									Status: metav1.ConditionTrue,
								}},
								SupportedKinds: supportedRouteGroupKinds,
							},
						}
						return gw
					}(),
					gatewayClass,
					namespace,
				},
				expected: []expected{
					{
						condition:             routeConditionAccepted(metav1.ConditionTrue, gwtypes.RouteReasonAccepted),
						attachedListenerNames: []gwtypes.SectionName{"http"},
					},
				},
			},
			{
				name:  "basic HTTPRoute whose only matching listener is not yet Programmed stays in attachedListenerNames",
				route: basicHTTPRoute(),
				objects: []client.Object{
					func() *gwtypes.Gateway {
						gw := gatewayWithHTTP80Ready()
						gw.Status.Listeners[0].Conditions[0].Status = metav1.ConditionFalse
						return gw
					}(),
					gatewayClass,
					namespace,
				},
				expected: []expected{
					{
						condition:             routeConditionAccepted(metav1.ConditionFalse, gwtypes.RouteReasonNotAllowedByListeners),
						attachedListenerNames: []gwtypes.SectionName{"http"},
					},
				},
			},
			{
				name: "basic HTTPRoute specifying non-existing port does not get Accepted",
				route: func() *gwtypes.HTTPRoute {
					r := basicHTTPRoute()
					r.Spec.CommonRouteSpec.ParentRefs[0].Port = new(gwtypes.PortNumber(80))
					return r
				}(),
				objects: []client.Object{
					func() *gwtypes.Gateway {
						gw := gatewayWithHTTP80Ready()
						gw.Spec.Listeners = builder.NewListener("http").WithPort(81).HTTP().IntoSlice()
						return gw
					}(),
					gatewayClass,
					namespace,
				},
				expected: []expected{
					{
						condition: routeConditionAccepted(metav1.ConditionFalse, gwtypes.RouteReasonNoMatchingParent),
					},
				},
			},
			{
				name:  "basic HTTPRoute does not get accepted if it is not in the supported kinds",
				route: basicHTTPRoute(),
				objects: []client.Object{
					func() *gwtypes.Gateway {
						gw := gatewayWithHTTP80Ready()
						gw.Status.Listeners[0].SupportedKinds = nil
						return gw
					}(),
					gatewayClass,
					namespace,
				},
				expected: []expected{
					{
						condition: routeConditionAccepted(metav1.ConditionFalse, gwtypes.RouteReasonNotAllowedByListeners),
					},
				},
			},
			{
				name:  "basic HTTPRoute does not get accepted if it is not permitted by allowed routes",
				route: basicHTTPRoute(),
				objects: []client.Object{
					func() *gwtypes.Gateway {
						gw := gatewayWithHTTP80Ready()
						gw.Spec.Listeners = builder.NewListener("http").
							WithPort(80).
							HTTP().
							WithAllowedRoutes(
								&gwtypes.AllowedRoutes{
									Kinds: builder.NewRouteGroupKind().TCPRoute().IntoSlice(),
								},
							).
							IntoSlice()
						return gw
					}(),
					gatewayClass,
					namespace,
				},
				expected: []expected{
					{
						condition: routeConditionAccepted(metav1.ConditionFalse, gwtypes.RouteReasonNotAllowedByListeners),
					},
				},
			},
			{
				name:  "basic HTTPRoute does get accepted if allowed routes only specified Same namespace",
				route: basicHTTPRoute(),
				objects: []client.Object{
					func() *gwtypes.Gateway {
						gw := gatewayWithHTTP80Ready()
						gw.Spec.Listeners = builder.NewListener("http").
							WithPort(80).
							HTTP().
							WithAllowedRoutes(builder.NewAllowedRoutesFromSameNamespaces()).
							IntoSlice()
						return gw
					}(),
					gatewayClass,
					namespace,
				},
				expected: []expected{
					{
						condition: routeConditionAccepted(metav1.ConditionTrue, gwtypes.RouteReasonAccepted),
					},
				},
			},
			{
				name: "HTTPRoute does not get accepted if Listener hostnames do not match route hostnames",
				route: func() *gwtypes.HTTPRoute {
					r := basicHTTPRoute()
					r.Spec.Hostnames = []gwtypes.Hostname{"very.specific.com"}
					return r
				}(),
				objects: []client.Object{
					func() *gwtypes.Gateway {
						gw := gatewayWithHTTP80Ready()
						gw.Spec.Listeners = builder.NewListener("http").
							WithPort(80).
							HTTP().
							WithAllowedRoutes(builder.NewAllowedRoutesFromSameNamespaces()).
							WithHostname("hostname.com").
							IntoSlice()
						return gw
					}(),
					gatewayClass,
					namespace,
				},
				expected: []expected{
					{
						condition: routeConditionAccepted(metav1.ConditionFalse, gwtypes.RouteReasonNoMatchingListenerHostname),
					},
				},
			},
			{
				name:  "HTTPRoute does not get accepted if Listener TLSConfig uses PassThrough",
				route: basicHTTPRoute(),
				objects: []client.Object{
					func() *gwtypes.Gateway {
						gw := gatewayWithHTTP80Ready()
						gw.Spec.Listeners = builder.
							NewListener("https").WithPort(443).HTTPS().
							WithTLSConfig(&gwtypes.GatewayTLSConfig{
								Mode: new(gwtypes.TLSModePassthrough),
							}).
							IntoSlice()
						gw.Status.Listeners[0].Name = "https"
						return gw
					}(),
					gatewayClass,
					namespace,
				},
				expected: []expected{
					{
						condition: routeConditionAccepted(metav1.ConditionFalse, gwtypes.RouteReasonNoMatchingParent),
					},
				},
			},
			{
				name:  "HTTPRoute does not get accepted if Listener doesn't match route's protocol",
				route: basicHTTPRoute(),
				objects: []client.Object{
					func() *gwtypes.Gateway {
						gw := gatewayWithHTTP80Ready()
						// Using matching Listener name, but wrong protocol.
						gw.Spec.Listeners = builder.NewListener("http").WithPort(80).UDP().IntoSlice()
						return gw
					}(),
					gatewayClass,
					namespace,
				},
				expected: []expected{
					{
						condition: routeConditionAccepted(metav1.ConditionFalse, gwtypes.RouteReasonNoMatchingParent),
					},
				},
			},
			{
				name:  "HTTPRoute does not get accepted if Listener doesn't match route's section name",
				route: basicHTTPRoute(),
				objects: []client.Object{
					func() *gwtypes.Gateway {
						gw := gatewayWithHTTP80Ready()
						// Using Listener's name not matching Route's section name.
						gw.Spec.Listeners = builder.NewListener("not-http").WithPort(80).HTTP().IntoSlice()
						return gw
					}(),
					gatewayClass,
					namespace,
				},
				expected: []expected{
					{
						condition: routeConditionAccepted(metav1.ConditionFalse, gwtypes.RouteReasonNotAllowedByListeners),
					},
				},
			},
			{
				name: "HTTPRoute with connection-error retries is accepted",
				route: func() *gwtypes.HTTPRoute {
					r := basicHTTPRoute()
					r.Spec.Rules[0].Retry = &gwtypes.HTTPRouteRetry{Attempts: new(2)}
					return r
				}(),
				objects: []client.Object{
					gatewayWithHTTP80Ready(),
					gatewayClass,
					namespace,
				},
				expected: []expected{
					{
						condition: routeConditionAccepted(metav1.ConditionTrue, gwtypes.RouteReasonAccepted),
					},
				},
			},
			{
				name: "HTTPRoute with retry codes is rejected as unsupported",
				route: func() *gwtypes.HTTPRoute {
					r := basicHTTPRoute()
					r.Spec.Rules[0].Retry = &gwtypes.HTTPRouteRetry{
						Codes: []gwtypes.HTTPRouteRetryStatusCode{500},
					}
					return r
				}(),
				objects: []client.Object{
					gatewayWithHTTP80Ready(),
					gatewayClass,
					namespace,
				},
				expected: []expected{
					{
						condition: routeConditionAccepted(metav1.ConditionFalse, gwtypes.RouteReasonUnsupportedValue),
						message:   "rules[0].retry.codes is unsupported: Kong Gateway retries transport-level failures only and cannot retry on HTTP response status codes",
					},
				},
			},
			{
				name: "HTTPRoute with retry backoff is rejected as unsupported",
				route: func() *gwtypes.HTTPRoute {
					r := basicHTTPRoute()
					r.Spec.Rules[0].Retry = &gwtypes.HTTPRouteRetry{
						Backoff: new(gwtypes.Duration("100ms")),
					}
					return r
				}(),
				objects: []client.Object{
					gatewayWithHTTP80Ready(),
					gatewayClass,
					namespace,
				},
				expected: []expected{
					{
						condition: routeConditionAccepted(metav1.ConditionFalse, gwtypes.RouteReasonUnsupportedValue),
						message:   "rules[0].retry.backoff is unsupported: Kong Gateway retries immediately and has no configurable backoff",
					},
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				fakeClient := fakeclient.
					NewClientBuilder().
					WithScheme(scheme.Scheme).
					WithObjects(tt.objects...).
					Build()

				got, err := getSupportedGatewayForRoute(t.Context(), logr.Discard(), fakeClient, tt.route, controllers.NewOptionalNamespacedName(mo.None[k8stypes.NamespacedName]()))
				require.NoError(t, err)
				require.Len(t, got, len(tt.expected))

				for i := range got {
					assert.Equalf(t, "test-namespace", got[i].gateway.Namespace, "gateway namespace #%d", i)
					assert.Equalf(t, "test-gateway", got[i].gateway.Name, "gateway name #%d", i)
					assert.Equalf(t, tt.expected[i].listenerName, got[i].listenerName, "listenerName #%d", i)
					if tt.expected[i].attachedListenerNames != nil {
						assert.Equalf(t, tt.expected[i].attachedListenerNames, got[i].attachedListenerNames, "attachedListenerNames #%d", i)
					}
					expectedCondition := tt.expected[i].condition
					expectedCondition.Message = tt.expected[i].message
					assert.Equalf(t, expectedCondition, got[i].condition, "condition #%d", i)
				}
			})
		}
	})

	t.Run("TCPRoute", func(t *testing.T) {
		basicTCPRoute := func() *gwtypes.TCPRoute {
			return &gwtypes.TCPRoute{
				TypeMeta:  gwtypes.TCPRouteTypeMeta,
				Name:      "basic-tcproute",
				Namespace: "test-namespace",
				Spec: gwtypes.TCPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{
							{
								Group: &goodGroup,
								Kind:  &goodKind,
								Name:  "test-gateway",
							},
						},
					},
				},
			}
		}
		gatewayWithTCP80Ready := func() *gwtypes.Gateway {
			return &gwtypes.Gateway{
				TypeMeta:  gwtypes.V1GatewayTypeMeta,
				Name:      "test-gateway",
				Namespace: "test-namespace",
				UID:       "ce7f0678-f59a-483c-80d1-243d3738d22c",
				Spec: gwtypes.GatewaySpec{
					GatewayClassName: "test-gatewayclass",
					Listeners:        builder.NewListener("tcp").WithPort(80).TCP().IntoSlice(),
				},
				Status: gwtypes.GatewayStatus{
					Listeners: []gwtypes.ListenerStatus{
						{
							Name: "tcp",
							Conditions: []metav1.Condition{
								{
									Type:   string(gwtypes.ListenerConditionProgrammed),
									Status: metav1.ConditionTrue,
								},
							},
							SupportedKinds: builder.NewRouteGroupKind().TCPRoute().IntoSlice(),
						},
					},
				},
			}
		}

		type expected struct {
			condition    metav1.Condition
			listenerName string
		}
		tests := []struct {
			name     string
			route    *gwtypes.TCPRoute
			expected expected
			objects  []client.Object
			wantErr  bool
		}{
			{
				name:  "basic TCPRoute does get accepted because it is in supported kinds",
				route: basicTCPRoute(),
				objects: []client.Object{
					gatewayWithTCP80Ready(),
					gatewayClass,
					namespace,
				},
				expected: expected{
					condition: routeConditionAccepted(metav1.ConditionTrue, gwtypes.RouteReasonAccepted),
				},
			},
			{
				name:  "basic TCPRoute does not get accepted because it is not in supported kinds",
				route: basicTCPRoute(),
				objects: []client.Object{
					func() *gwtypes.Gateway {
						gw := gatewayWithTCP80Ready()
						gw.Status.Listeners[0].SupportedKinds = nil
						return gw
					}(),
					gatewayClass,
					namespace,
				},
				expected: expected{
					condition: routeConditionAccepted(metav1.ConditionFalse, gwtypes.RouteReasonNotAllowedByListeners),
				},
			},
			{
				name: "TCPRoute specifying existing port gets Accepted",
				route: func() *gwtypes.TCPRoute {
					r := basicTCPRoute()
					r.Spec.CommonRouteSpec.ParentRefs[0].Port = new(gwtypes.PortNumber(80))
					r.Spec.Rules = []gwtypes.TCPRouteRule{
						{
							BackendRefs: builder.NewBackendRef("fake-service").WithPort(80).ToSlice(),
						},
					}
					return r
				}(),
				objects: []client.Object{
					gatewayWithTCP80Ready(),
					gatewayClass,
					namespace,
				},
				expected: expected{
					condition: routeConditionAccepted(metav1.ConditionTrue, gwtypes.RouteReasonAccepted),
				},
			},
			{
				name: "TCPRoute specifying non existing port does not get Accepted",
				route: func() *gwtypes.TCPRoute {
					r := basicTCPRoute()
					r.Spec.CommonRouteSpec.ParentRefs[0].Port = new(gwtypes.PortNumber(8000))
					r.Spec.Rules = []gwtypes.TCPRouteRule{
						{
							BackendRefs: builder.NewBackendRef("fake-service").WithPort(80).ToSlice(),
						},
					}
					return r
				}(),
				objects: []client.Object{
					gatewayWithTCP80Ready(),
					gatewayClass,
					namespace,
				},
				expected: expected{
					condition: routeConditionAccepted(metav1.ConditionFalse, gwtypes.RouteReasonNoMatchingParent),
				},
			},
			{
				name: "TCPRoute specifying in sectionName existing listener gets Accepted",
				route: func() *gwtypes.TCPRoute {
					r := basicTCPRoute()
					r.Spec.CommonRouteSpec.ParentRefs[0].Port = new(gwtypes.PortNumber(80))
					r.Spec.CommonRouteSpec.ParentRefs[0].SectionName = new(gwtypes.SectionName("tcp"))
					r.Spec.Rules = []gwtypes.TCPRouteRule{
						{
							BackendRefs: builder.NewBackendRef("fake-service").WithPort(80).ToSlice(),
						},
					}
					return r
				}(),
				objects: []client.Object{
					gatewayWithTCP80Ready(),
					gatewayClass,
					namespace,
				},
				expected: expected{
					listenerName: "tcp",
					condition:    routeConditionAccepted(metav1.ConditionTrue, gwtypes.RouteReasonAccepted),
				},
			},
			{
				name: "TCPRoute specifying in sectionName non existing listener does not get Accepted",
				route: func() *gwtypes.TCPRoute {
					r := basicTCPRoute()
					r.Spec.CommonRouteSpec.ParentRefs[0].Port = new(gwtypes.PortNumber(80))
					r.Spec.CommonRouteSpec.ParentRefs[0].SectionName = new(gwtypes.SectionName("unknown-listener"))
					r.Spec.Rules = []gwtypes.TCPRouteRule{
						{
							BackendRefs: builder.NewBackendRef("fake-service").WithPort(80).ToSlice(),
						},
					}
					return r
				}(),
				objects: []client.Object{
					gatewayWithTCP80Ready(),
					gatewayClass,
					namespace,
				},
				expected: expected{
					listenerName: "unknown-listener",
					condition:    routeConditionAccepted(metav1.ConditionFalse, gwtypes.RouteReasonNoMatchingParent),
				},
			},
			{
				name: "TCPRoute specifying in sectionName existing listener with a matching port gets Accepted",
				route: func() *gwtypes.TCPRoute {
					r := basicTCPRoute()
					r.Spec.CommonRouteSpec.ParentRefs[0].Port = new(gwtypes.PortNumber(80))
					r.Spec.CommonRouteSpec.ParentRefs[0].SectionName = new(gwtypes.SectionName("tcp"))
					r.Spec.Rules = []gwtypes.TCPRouteRule{
						{
							BackendRefs: builder.NewBackendRef("fake-service").WithPort(80).ToSlice(),
						},
					}
					return r
				}(),
				objects: []client.Object{
					gatewayWithTCP80Ready(),
					gatewayClass,
					namespace,
				},
				expected: expected{
					listenerName: "tcp",
					condition:    routeConditionAccepted(metav1.ConditionTrue, gwtypes.RouteReasonAccepted),
				},
			},
			{
				name: "TCPRoute specifying in sectionName existing listener with a non-matching port does not gets Accepted",
				route: func() *gwtypes.TCPRoute {
					r := basicTCPRoute()
					r.Spec.CommonRouteSpec.ParentRefs[0].Port = new(gwtypes.PortNumber(8080))
					r.Spec.CommonRouteSpec.ParentRefs[0].SectionName = new(gwtypes.SectionName("tcp"))
					r.Spec.Rules = []gwtypes.TCPRouteRule{
						{
							BackendRefs: builder.NewBackendRef("fake-service").WithPort(80).ToSlice(),
						},
					}
					return r
				}(),
				objects: []client.Object{
					gatewayWithTCP80Ready(),
					gatewayClass,
					namespace,
				},
				expected: expected{
					listenerName: "tcp",
					condition:    routeConditionAccepted(metav1.ConditionFalse, gwtypes.RouteReasonNoMatchingParent),
				},
			},
			{
				name: "TCPRoute specifying in sectionName non existing listener with an existing port does not gets Accepted",
				route: func() *gwtypes.TCPRoute {
					r := basicTCPRoute()
					r.Spec.CommonRouteSpec.ParentRefs[0].Port = new(gwtypes.PortNumber(80))
					r.Spec.CommonRouteSpec.ParentRefs[0].SectionName = new(gwtypes.SectionName("unknown-listener"))
					r.Spec.Rules = []gwtypes.TCPRouteRule{
						{
							BackendRefs: builder.NewBackendRef("fake-service").WithPort(80).ToSlice(),
						},
					}
					return r
				}(),
				objects: []client.Object{
					gatewayWithTCP80Ready(),
					gatewayClass,
					namespace,
				},
				expected: expected{
					listenerName: "unknown-listener",
					condition:    routeConditionAccepted(metav1.ConditionFalse, gwtypes.RouteReasonNoMatchingParent),
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				fakeClient := fakeclient.
					NewClientBuilder().
					WithScheme(scheme.Scheme).
					WithObjects(tt.objects...).
					Build()

				got, err := getSupportedGatewayForRoute(t.Context(), logr.Discard(), fakeClient, tt.route, controllers.OptionalNamespacedName{})
				require.NoError(t, err)
				require.Len(t, got, 1)
				match := got[0]

				assert.Equal(t, "test-namespace", match.gateway.Namespace)
				assert.Equal(t, "test-gateway", match.gateway.Name)
				assert.Equal(t, tt.expected.listenerName, match.listenerName)
				assert.Equal(t, tt.expected.condition, match.condition)
			})
		}
	})

	t.Run("UDPRoute", func(t *testing.T) {
		basicUDPRoute := func() *gwtypes.UDPRoute {
			return &gwtypes.UDPRoute{
				TypeMeta:  gwtypes.UDPRouteTypeMeta,
				Name:      "basic-udproute",
				Namespace: "test-namespace",
				Spec: gwtypes.UDPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{
							{
								Group: &goodGroup,
								Kind:  &goodKind,
								Name:  "test-gateway",
							},
						},
					},
				},
			}
		}
		gatewayWithUDP53Ready := func() *gwtypes.Gateway {
			return &gwtypes.Gateway{
				TypeMeta:  gwtypes.V1GatewayTypeMeta,
				Name:      "test-gateway",
				Namespace: "test-namespace",
				UID:       "ce7f0678-f59a-483c-80d1-243d3738d22c",
				Spec: gwtypes.GatewaySpec{
					GatewayClassName: "test-gatewayclass",
					Listeners:        builder.NewListener("udp").WithPort(53).UDP().IntoSlice(),
				},
				Status: gwtypes.GatewayStatus{
					Listeners: []gwtypes.ListenerStatus{
						{
							Name: "udp",
							Conditions: []metav1.Condition{
								{
									Type:   string(gwtypes.ListenerConditionProgrammed),
									Status: metav1.ConditionTrue,
								},
							},
							SupportedKinds: builder.NewRouteGroupKind().UDPRoute().IntoSlice(),
						},
					},
				},
			}
		}

		type expected struct {
			condition    metav1.Condition
			listenerName string
		}
		tests := []struct {
			name     string
			route    *gwtypes.UDPRoute
			expected expected
			objects  []client.Object
			wantErr  bool
		}{
			{
				name:  "basic UDPRoute does get accepted because it is in supported kinds",
				route: basicUDPRoute(),
				objects: []client.Object{
					gatewayWithUDP53Ready(),
					gatewayClass,
					namespace,
				},
				expected: expected{
					condition: routeConditionAccepted(metav1.ConditionTrue, gwtypes.RouteReasonAccepted),
				},
			},
			{
				name:  "basic UDPRoute does not get accepted because it is not in supported kinds",
				route: basicUDPRoute(),
				objects: []client.Object{
					func() *gwtypes.Gateway {
						gw := gatewayWithUDP53Ready()
						gw.Status.Listeners[0].SupportedKinds = nil
						return gw
					}(),
					gatewayClass,
					namespace,
				},
				expected: expected{
					condition: routeConditionAccepted(metav1.ConditionFalse, gwtypes.RouteReasonNotAllowedByListeners),
				},
			},
			{
				name: "UDPRoute specifying existing port gets Accepted",
				route: func() *gwtypes.UDPRoute {
					r := basicUDPRoute()
					r.Spec.CommonRouteSpec.ParentRefs[0].Port = new(gwtypes.PortNumber(53))
					return r
				}(),
				objects: []client.Object{
					gatewayWithUDP53Ready(),
					gatewayClass,
					namespace,
				},
				expected: expected{
					condition: routeConditionAccepted(metav1.ConditionTrue, gwtypes.RouteReasonAccepted),
				},
			},
			{
				name: "UDPRoute specifying non existing port does not get Accepted",
				route: func() *gwtypes.UDPRoute {
					r := basicUDPRoute()
					r.Spec.CommonRouteSpec.ParentRefs[0].Port = new(gwtypes.PortNumber(8000))
					return r
				}(),
				objects: []client.Object{
					gatewayWithUDP53Ready(),
					gatewayClass,
					namespace,
				},
				expected: expected{
					condition: routeConditionAccepted(metav1.ConditionFalse, gwtypes.RouteReasonNoMatchingParent),
				},
			},
			{
				name: "UDPRoute specifying in sectionName existing listener gets Accepted",
				route: func() *gwtypes.UDPRoute {
					r := basicUDPRoute()
					r.Spec.CommonRouteSpec.ParentRefs[0].Port = new(gwtypes.PortNumber(53))
					r.Spec.CommonRouteSpec.ParentRefs[0].SectionName = new(gwtypes.SectionName("udp"))
					return r
				}(),
				objects: []client.Object{
					gatewayWithUDP53Ready(),
					gatewayClass,
					namespace,
				},
				expected: expected{
					listenerName: "udp",
					condition:    routeConditionAccepted(metav1.ConditionTrue, gwtypes.RouteReasonAccepted),
				},
			},
			{
				name: "UDPRoute specifying in sectionName non existing listener does not get Accepted",
				route: func() *gwtypes.UDPRoute {
					r := basicUDPRoute()
					r.Spec.CommonRouteSpec.ParentRefs[0].Port = new(gwtypes.PortNumber(53))
					r.Spec.CommonRouteSpec.ParentRefs[0].SectionName = new(gwtypes.SectionName("unknown-listener"))
					return r
				}(),
				objects: []client.Object{
					gatewayWithUDP53Ready(),
					gatewayClass,
					namespace,
				},
				expected: expected{
					listenerName: "unknown-listener",
					condition:    routeConditionAccepted(metav1.ConditionFalse, gwtypes.RouteReasonNoMatchingParent),
				},
			},
			{
				name: "UDPRoute specifying in sectionName existing listener with a matching port gets Accepted",
				route: func() *gwtypes.UDPRoute {
					r := basicUDPRoute()
					r.Spec.CommonRouteSpec.ParentRefs[0].Port = new(gwtypes.PortNumber(53))
					r.Spec.CommonRouteSpec.ParentRefs[0].SectionName = new(gwtypes.SectionName("udp"))
					return r
				}(),
				objects: []client.Object{
					gatewayWithUDP53Ready(),
					gatewayClass,
					namespace,
				},
				expected: expected{
					listenerName: "udp",
					condition:    routeConditionAccepted(metav1.ConditionTrue, gwtypes.RouteReasonAccepted),
				},
			},
			{
				name: "UDPRoute specifying in sectionName existing listener with a non-matching port does not get Accepted",
				route: func() *gwtypes.UDPRoute {
					r := basicUDPRoute()
					r.Spec.CommonRouteSpec.ParentRefs[0].Port = new(gwtypes.PortNumber(533))
					r.Spec.CommonRouteSpec.ParentRefs[0].SectionName = new(gwtypes.SectionName("udp"))
					return r
				}(),
				objects: []client.Object{
					gatewayWithUDP53Ready(),
					gatewayClass,
					namespace,
				},
				expected: expected{
					listenerName: "udp",
					condition:    routeConditionAccepted(metav1.ConditionFalse, gwtypes.RouteReasonNoMatchingParent),
				},
			},
			{
				name: "UDPRoute specifying in sectionName non existing listener with an existing port does not get Accepted",
				route: func() *gwtypes.UDPRoute {
					r := basicUDPRoute()
					r.Spec.CommonRouteSpec.ParentRefs[0].Port = new(gwtypes.PortNumber(53))
					r.Spec.CommonRouteSpec.ParentRefs[0].SectionName = new(gwtypes.SectionName("unknown-listener"))
					return r
				}(),
				objects: []client.Object{
					gatewayWithUDP53Ready(),
					gatewayClass,
					namespace,
				},
				expected: expected{
					listenerName: "unknown-listener",
					condition:    routeConditionAccepted(metav1.ConditionFalse, gwtypes.RouteReasonNoMatchingParent),
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				fakeClient := fakeclient.
					NewClientBuilder().
					WithScheme(scheme.Scheme).
					WithObjects(tt.objects...).
					Build()

				got, err := getSupportedGatewayForRoute(t.Context(), logr.Discard(), fakeClient, tt.route, controllers.OptionalNamespacedName{})
				require.NoError(t, err)
				require.Len(t, got, 1)
				match := got[0]

				assert.Equal(t, "test-namespace", match.gateway.Namespace)
				assert.Equal(t, "test-gateway", match.gateway.Name)
				assert.Equal(t, tt.expected.listenerName, match.listenerName)
				assert.Equal(t, tt.expected.condition, match.condition)
			})
		}
	})

	t.Run("TLSRoute", func(t *testing.T) {
		basicTLSRoute := func() *gwtypes.TLSRoute {
			return &gwtypes.TLSRoute{
				TypeMeta:  gwtypes.TLSRouteTypeMeta,
				Name:      "basic-tlsroute",
				Namespace: "test-namespace",
				Spec: gwtypes.TLSRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{
							{
								Group: &goodGroup,
								Kind:  &goodKind,
								Name:  "test-gateway",
							},
						},
					},
				},
			}
		}
		gatewayWithTLS443PassthroughReady := func() *gwtypes.Gateway {
			return &gwtypes.Gateway{
				TypeMeta:  gwtypes.V1GatewayTypeMeta,
				Name:      "test-gateway",
				Namespace: "test-namespace",
				UID:       "ce7f0678-f59a-483c-80d1-243d3738d22c",
				Spec: gwtypes.GatewaySpec{
					GatewayClassName: "test-gatewayclass",
					Listeners: builder.NewListener("tls").
						WithPort(443).
						TLS().
						WithTLSConfig(&gwtypes.GatewayTLSConfig{
							Mode: new(gwtypes.TLSModePassthrough),
						}).IntoSlice(),
				},
				Status: gwtypes.GatewayStatus{
					Listeners: []gwtypes.ListenerStatus{
						{
							Name: "tls",
							Conditions: []metav1.Condition{
								{
									Type:   string(gwtypes.ListenerConditionProgrammed),
									Status: metav1.ConditionTrue,
								},
							},
							SupportedKinds: builder.NewRouteGroupKind().TLSRoute().IntoSlice(),
						},
					},
				},
			}
		}

		type expected struct {
			condition    metav1.Condition
			listenerName string
		}
		tests := []struct {
			name     string
			route    *gwtypes.TLSRoute
			expected []expected
			objects  []client.Object
			wantErr  bool
		}{
			{
				name:  "basic TLSRoute does get accepted because it is in supported kinds and there is a listener with TLS in passthrough mode",
				route: basicTLSRoute(),
				objects: []client.Object{
					gatewayWithTLS443PassthroughReady(),
					gatewayClass,
					namespace,
				},
				expected: []expected{
					{
						condition: routeConditionAccepted(metav1.ConditionTrue, gwtypes.RouteReasonAccepted),
					},
				},
			},
			{
				name:  "basic TLSRouteget accepted because there is a listener with TLS in terminate mode",
				route: basicTLSRoute(),
				objects: []client.Object{
					func() *gwtypes.Gateway {
						gw := gatewayWithTLS443PassthroughReady()
						gw.Spec.Listeners = builder.NewListener("tls").
							WithPort(443).
							TLS().
							WithTLSConfig(&gwtypes.GatewayTLSConfig{
								Mode: new(gwtypes.TLSModeTerminate),
							}).IntoSlice()
						return gw
					}(),
					gatewayClass,
					namespace,
				},
				expected: []expected{
					{
						condition: routeConditionAccepted(metav1.ConditionTrue, gwtypes.RouteReasonAccepted),
					},
				},
			},
			{
				name:  "TLSRoute does not get accepted because it is not in supported kinds",
				route: basicTLSRoute(),
				objects: []client.Object{
					func() *gwtypes.Gateway {
						gw := gatewayWithTLS443PassthroughReady()
						gw.Status.Listeners[0].SupportedKinds = nil
						return gw
					}(),
					gatewayClass,
					namespace,
				},
				expected: []expected{
					{
						condition: routeConditionAccepted(metav1.ConditionFalse, gwtypes.RouteReasonNotAllowedByListeners),
					},
				},
			},
			{
				name: "TLSRoute specifying existing port gets Accepted",
				route: func() *gwtypes.TLSRoute {
					r := basicTLSRoute()
					r.Spec.CommonRouteSpec.ParentRefs[0].Port = new(gwtypes.PortNumber(443))
					return r
				}(),
				objects: []client.Object{
					gatewayWithTLS443PassthroughReady(),
					gatewayClass,
					namespace,
				},
				expected: []expected{
					{
						condition: routeConditionAccepted(metav1.ConditionTrue, gwtypes.RouteReasonAccepted),
					},
				},
			},
			{
				name: "TLSRoute specifying non existing port does not get Accepted",
				route: func() *gwtypes.TLSRoute {
					r := basicTLSRoute()
					r.Spec.CommonRouteSpec.ParentRefs[0].Port = new(gwtypes.PortNumber(444))
					return r
				}(),
				objects: []client.Object{
					gatewayWithTLS443PassthroughReady(),
					gatewayClass,
					namespace,
				},
				expected: []expected{
					{
						condition: routeConditionAccepted(metav1.ConditionFalse, gwtypes.RouteReasonNoMatchingParent),
					},
				},
			},
			{
				name: "TLSRoute specifying in sectionName existing listener gets Accepted",
				route: func() *gwtypes.TLSRoute {
					r := basicTLSRoute()
					r.Spec.CommonRouteSpec.ParentRefs[0].Port = new(gwtypes.PortNumber(443))
					r.Spec.CommonRouteSpec.ParentRefs[0].SectionName = new(gwtypes.SectionName("tls"))
					return r
				}(),
				objects: []client.Object{
					gatewayWithTLS443PassthroughReady(),
					gatewayClass,
					namespace,
				},
				expected: []expected{
					{
						listenerName: "tls",
						condition:    routeConditionAccepted(metav1.ConditionTrue, gwtypes.RouteReasonAccepted),
					},
				},
			},
			// unmatched sectionName.
			{
				name: "TLSRoute specifying in sectionName non existing listener does not get Accepted",
				route: func() *gwtypes.TLSRoute {
					r := basicTLSRoute()
					r.Spec.CommonRouteSpec.ParentRefs[0].Port = new(gwtypes.PortNumber(443))
					r.Spec.CommonRouteSpec.ParentRefs[0].SectionName = new(gwtypes.SectionName("unknown-listener"))
					return r
				}(),
				objects: []client.Object{
					gatewayWithTLS443PassthroughReady(),
					gatewayClass,
					namespace,
				},
				expected: []expected{
					{
						listenerName: "unknown-listener",
						condition:    routeConditionAccepted(metav1.ConditionFalse, gwtypes.RouteReasonNoMatchingParent),
					},
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				fakeClient := fakeclient.
					NewClientBuilder().
					WithScheme(scheme.Scheme).
					WithObjects(tt.objects...).
					Build()

				got, err := getSupportedGatewayForRoute(t.Context(), logr.Discard(), fakeClient, tt.route, controllers.OptionalNamespacedName{})
				require.NoError(t, err)
				require.Len(t, got, len(tt.expected))

				for i := range got {
					assert.Equalf(t, "test-namespace", got[i].gateway.Namespace, "gateway namespace #%d", i)
					assert.Equalf(t, "test-gateway", got[i].gateway.Name, "gateway name #%d", i)
					assert.Equalf(t, tt.expected[i].listenerName, got[i].listenerName, "listenerName #%d", i)
					assert.Equalf(t, tt.expected[i].condition, got[i].condition, "condition #%d", i)
				}
			})
		}
	})

	bustedParentHTTPRoute := basicHTTPRoute()
	badGroup := gwtypes.Group("kechavakunduz.cholpon.uz")
	badKind := gwtypes.Kind("razzoq")
	bustedParentHTTPRoute.Spec.ParentRefs = []gwtypes.ParentReference{
		{
			Name:  "not-a-gateway",
			Kind:  &badKind,
			Group: &badGroup,
		},
	}
	t.Run("unsupported parentRef kind is skipped, not rejected", func(t *testing.T) {
		fakeClient := fakeclient.
			NewClientBuilder().
			WithScheme(scheme.Scheme).
			Build()

		// A route whose only parent is of a kind this controller cannot resolve belongs to some
		// other controller. It must resolve to "no supported gateway", which callers handle by
		// dropping the route, rather than to a hard error that retries forever.
		_, err := getSupportedGatewayForRoute(t.Context(), logr.Discard(), fakeClient, bustedParentHTTPRoute, controllers.OptionalNamespacedName{})
		require.ErrorIs(t, err, ErrNoSupportedGateway)
	})

	t.Run("single Gateway", func(t *testing.T) {
		namedGateway := func(name string) *gwtypes.Gateway {
			return &gwtypes.Gateway{
				TypeMeta:  gwtypes.V1GatewayTypeMeta,
				Name:      name,
				Namespace: "test-namespace",
				UID:       "ce7f0678-f59a-483c-80d1-243d3738d22c",
				Spec: gwtypes.GatewaySpec{
					GatewayClassName: "test-gatewayclass",
					Listeners:        builder.NewListener("http").WithPort(80).HTTP().IntoSlice(),
				},
				Status: gwtypes.GatewayStatus{
					Listeners: []gwtypes.ListenerStatus{
						{
							Name: "http",
							Conditions: []metav1.Condition{
								{
									Type:   string(gwtypes.ListenerConditionProgrammed),
									Status: metav1.ConditionTrue,
								},
							},
							SupportedKinds: supportedRouteGroupKinds,
						},
					},
				},
			}
		}

		basicHTTPRoute := func(gateway string) *gwtypes.HTTPRoute {
			return &gwtypes.HTTPRoute{
				TypeMeta:  gwtypes.V1GatewayTypeMeta,
				Name:      "basic-httproute",
				Namespace: "test-namespace",
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{
							{
								Group: &goodGroup,
								Kind:  &goodKind,
								Name:  gwtypes.ObjectName(gateway),
							},
						},
					},
					Rules: []gwtypes.HTTPRouteRule{
						{
							BackendRefs: []gwtypes.HTTPBackendRef{
								builder.NewHTTPBackendRef("fake-service").WithPort(80).Build(),
							},
						},
					},
				},
			}
		}

		tests := []struct {
			name        string
			route       *gwtypes.HTTPRoute
			expected    []expected
			expectedErr error
			objects     []client.Object
		}{
			{
				name:  "HTTPRoute with bound Gateway parent is accepted",
				route: basicHTTPRoute("good-gateway"),
				objects: []client.Object{
					namedGateway("good-gateway"),
					gatewayClass,
					namespace,
				},
				expected: []expected{
					{
						condition: routeConditionAccepted(metav1.ConditionTrue, gwtypes.RouteReasonAccepted),
					},
				},
			},
			{
				name:  "HTTPRoute with other parent Gateway finds no matching Gateways",
				route: basicHTTPRoute("good-gateway"),
				objects: []client.Object{
					namedGateway("bad-gateway"),
					gatewayClass,
					namespace,
				},
				expected:    []expected{},
				expectedErr: ErrNoSupportedGateway,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				fakeClient := fakeclient.
					NewClientBuilder().
					WithScheme(scheme.Scheme).
					WithObjects(tt.objects...).
					Build()

				got, err := getSupportedGatewayForRoute(
					t.Context(),
					logr.Discard(),
					fakeClient,
					tt.route,
					controllers.NewOptionalNamespacedName(
						mo.Some(
							k8stypes.NamespacedName{
								Namespace: namespace.Name,
								Name:      "good-gateway",
							},
						),
					),
				)
				if tt.expectedErr != nil {
					require.Equal(t, tt.expectedErr, err)
				} else {
					require.NoError(t, err)
				}
				require.Len(t, got, len(tt.expected))

				for i := range got {
					assert.Equalf(t, "test-namespace", got[i].gateway.Namespace, "gateway namespace #%d", i)
					assert.Equalf(t, "good-gateway", got[i].gateway.Name, "gateway name #%d", i)
					assert.Equalf(t, tt.expected[i].listenerName, got[i].listenerName, "listenerName #%d", i)
					assert.Equalf(t, tt.expected[i].condition, got[i].condition, "condition #%d", i)
				}
			})
		}
	})
}

func TestEnsureParentsProgrammedCondition(t *testing.T) {
	createGateway := func(nn k8stypes.NamespacedName) *gwtypes.Gateway {
		return &gwtypes.Gateway{
			TypeMeta:  gwtypes.V1GatewayTypeMeta,
			Name:      nn.Name,
			Namespace: nn.Namespace,
			UID:       k8stypes.UID(uuid.NewString()),
			Spec: gwtypes.GatewaySpec{
				GatewayClassName: "test-gatewayclass",
				Listeners:        builder.NewListener("http").WithPort(80).HTTP().IntoSlice(),
			},
			Status: gwtypes.GatewayStatus{
				Listeners: []gwtypes.ListenerStatus{
					{
						Name: "http-1",
						Conditions: []metav1.Condition{
							{
								Type:   string(gwtypes.ListenerConditionProgrammed),
								Status: metav1.ConditionTrue,
							},
						},
						SupportedKinds: supportedRouteGroupKinds,
					},
					{
						Name: "http-2",
						Conditions: []metav1.Condition{
							{
								Type:   string(gwtypes.ListenerConditionProgrammed),
								Status: metav1.ConditionTrue,
							},
						},
						SupportedKinds: supportedRouteGroupKinds,
					},
				},
			},
		}
	}

	t.Run("HTTPRoute", func(t *testing.T) {
		gatewayNN1 := k8stypes.NamespacedName{
			Namespace: "test-namespace",
			Name:      "test-gateway",
		}
		gateway1 := createGateway(gatewayNN1)
		gatewayNN2 := k8stypes.NamespacedName{
			Namespace: "test-namespace",
			Name:      "test-gateway-2",
		}
		gateway2 := createGateway(gatewayNN2)

		tests := []struct {
			name           string
			httpRouteFunc  func() *gwtypes.HTTPRoute
			gatewayFunc    func() []supportedGatewayWithCondition
			expectedUpdate bool
			expectedStatus *gwtypes.HTTPRouteStatus
		}{
			{
				name: "Programmed condition gets properly set to Status True when parent status is already set in route",
				httpRouteFunc: func() *gwtypes.HTTPRoute {
					return &gwtypes.HTTPRoute{
						TypeMeta:   gwtypes.V1HTTPRouteTypeMeta,
						Name:       "basic-httproute",
						Namespace:  gatewayNN1.Namespace,
						Generation: 42,
						Spec: gwtypes.HTTPRouteSpec{
							CommonRouteSpec: gwtypes.CommonRouteSpec{
								ParentRefs: []gwtypes.ParentReference{
									{
										Group: new(gwtypes.Group(gatewayv1.GroupName)),
										Kind:  new(gwtypes.Kind("Gateway")),
										Name:  "test-gateway",
									},
								},
							},
							Rules: []gwtypes.HTTPRouteRule{
								{
									BackendRefs: []gwtypes.HTTPBackendRef{
										builder.NewHTTPBackendRef("fake-service").WithPort(80).Build(),
									},
								},
							},
						},
						Status: gwtypes.HTTPRouteStatus{
							RouteStatus: gwtypes.RouteStatus{
								Parents: []gwtypes.RouteParentStatus{
									{
										ParentRef: gwtypes.ParentReference{
											Kind:      new(gwtypes.Kind("Gateway")),
											Group:     new(gwtypes.Group(gatewayv1.GroupName)),
											Name:      gwtypes.ObjectName(gatewayNN1.Name),
											Namespace: (*gwtypes.Namespace)(&gatewayNN1.Namespace),
										},
										ControllerName: "konghq.com/kic-gateway-controller",
										Conditions: []metav1.Condition{
											{
												Type:               string(gwtypes.GatewayConditionAccepted),
												Message:            "",
												ObservedGeneration: 1,
												Status:             metav1.ConditionTrue,
												Reason:             string(gwtypes.RouteConditionAccepted),
												LastTransitionTime: metav1.Now(),
											},
											{
												Type:               ConditionTypeProgrammed,
												Message:            "",
												ObservedGeneration: 1,
												Status:             metav1.ConditionUnknown,
												Reason:             string(metav1.ConditionUnknown),
												LastTransitionTime: metav1.Now(),
											},
										},
									},
								},
							},
						},
					}
				},
				expectedUpdate: true,
				expectedStatus: &gwtypes.HTTPRouteStatus{
					Parents: []gwtypes.RouteParentStatus{
						{
							ParentRef: gwtypes.ParentReference{
								Kind:      new(gwtypes.Kind("Gateway")),
								Group:     new(gwtypes.Group(gatewayv1.GroupName)),
								Name:      gwtypes.ObjectName(gatewayNN1.Name),
								Namespace: (*gwtypes.Namespace)(&gatewayNN1.Namespace),
							},
							ControllerName: "konghq.com/kic-gateway-controller",
							Conditions: []metav1.Condition{
								{
									Type:               string(gwtypes.GatewayConditionAccepted),
									Message:            "",
									ObservedGeneration: 1,
									Status:             metav1.ConditionTrue,
									Reason:             string(gwtypes.RouteConditionAccepted),
									LastTransitionTime: metav1.Now(),
								},
								{
									Type:               ConditionTypeProgrammed,
									Message:            "",
									ObservedGeneration: 42,
									Status:             metav1.ConditionTrue,
									Reason:             string(gwtypes.RouteConditionAccepted),
									LastTransitionTime: metav1.Now(),
								},
							},
						},
					},
				},
				gatewayFunc: func() []supportedGatewayWithCondition {
					return []supportedGatewayWithCondition{
						{gateway: gateway1},
					}
				},
			},
			{
				name: "Programmed condition gets properly set to Status True when Programmed condition is not present in route's parent status",
				httpRouteFunc: func() *gwtypes.HTTPRoute {
					return &gwtypes.HTTPRoute{
						TypeMeta:   gwtypes.V1HTTPRouteTypeMeta,
						Name:       "basic-httproute",
						Namespace:  gatewayNN1.Namespace,
						Generation: 42,
						Spec: gwtypes.HTTPRouteSpec{
							CommonRouteSpec: gwtypes.CommonRouteSpec{
								ParentRefs: []gwtypes.ParentReference{
									{
										Group: new(gwtypes.Group(gatewayv1.GroupName)),
										Kind:  new(gwtypes.Kind("Gateway")),
										Name:  "test-gateway",
									},
								},
							},
							Rules: []gwtypes.HTTPRouteRule{
								{
									BackendRefs: []gwtypes.HTTPBackendRef{
										builder.NewHTTPBackendRef("fake-service").WithPort(80).Build(),
									},
								},
							},
						},
						Status: gwtypes.HTTPRouteStatus{
							RouteStatus: gwtypes.RouteStatus{
								Parents: []gwtypes.RouteParentStatus{
									{
										ParentRef: gwtypes.ParentReference{
											Kind:      new(gwtypes.Kind("Gateway")),
											Group:     new(gwtypes.Group(gatewayv1.GroupName)),
											Name:      gwtypes.ObjectName(gatewayNN1.Name),
											Namespace: (*gwtypes.Namespace)(&gatewayNN1.Namespace),
										},
										ControllerName: "konghq.com/kic-gateway-controller",
										Conditions: []metav1.Condition{
											{
												Type:               string(gwtypes.GatewayConditionAccepted),
												Message:            "",
												ObservedGeneration: 1,
												Status:             metav1.ConditionTrue,
												Reason:             string(gwtypes.RouteConditionAccepted),
												LastTransitionTime: metav1.Now(),
											},
										},
									},
								},
							},
						},
					}
				},
				expectedUpdate: true,
				expectedStatus: &gwtypes.HTTPRouteStatus{
					Parents: []gwtypes.RouteParentStatus{
						{
							ParentRef: gwtypes.ParentReference{
								Kind:      new(gwtypes.Kind("Gateway")),
								Group:     new(gwtypes.Group(gatewayv1.GroupName)),
								Name:      gwtypes.ObjectName(gatewayNN1.Name),
								Namespace: (*gwtypes.Namespace)(&gatewayNN1.Namespace),
							},
							ControllerName: "konghq.com/kic-gateway-controller",
							Conditions: []metav1.Condition{
								{
									Type:               string(gwtypes.GatewayConditionAccepted),
									Message:            "",
									ObservedGeneration: 1,
									Status:             metav1.ConditionTrue,
									Reason:             string(gwtypes.RouteConditionAccepted),
									LastTransitionTime: metav1.Now(),
								},
								{
									Type:               ConditionTypeProgrammed,
									Message:            "",
									ObservedGeneration: 42,
									Status:             metav1.ConditionTrue,
									Reason:             string(gwtypes.RouteConditionAccepted),
									LastTransitionTime: metav1.Now(),
								},
							},
						},
					},
				},
				gatewayFunc: func() []supportedGatewayWithCondition {
					return []supportedGatewayWithCondition{
						{gateway: gateway1},
					}
				},
			},
			{
				name: "Programmed condition gets properly set to Status True when Programmed condition is not present in route's parent status and Parent Section is specified",
				httpRouteFunc: func() *gwtypes.HTTPRoute {
					return &gwtypes.HTTPRoute{
						TypeMeta:   gwtypes.V1HTTPRouteTypeMeta,
						Name:       "basic-httproute",
						Namespace:  gatewayNN1.Namespace,
						Generation: 42,
						Spec: gwtypes.HTTPRouteSpec{
							CommonRouteSpec: gwtypes.CommonRouteSpec{
								ParentRefs: []gwtypes.ParentReference{
									{
										Group:       new(gwtypes.Group(gatewayv1.GroupName)),
										Kind:        new(gwtypes.Kind("Gateway")),
										Name:        "test-gateway",
										SectionName: new(gwtypes.SectionName("http-2")),
									},
								},
							},
							Rules: []gwtypes.HTTPRouteRule{
								{
									BackendRefs: []gwtypes.HTTPBackendRef{
										builder.NewHTTPBackendRef("fake-service").WithPort(80).Build(),
									},
								},
							},
						},
						Status: gwtypes.HTTPRouteStatus{
							RouteStatus: gwtypes.RouteStatus{
								Parents: []gwtypes.RouteParentStatus{
									{
										ParentRef: gwtypes.ParentReference{
											Kind:        new(gwtypes.Kind("Gateway")),
											Group:       new(gwtypes.Group(gatewayv1.GroupName)),
											Name:        gwtypes.ObjectName(gatewayNN1.Name),
											Namespace:   (*gwtypes.Namespace)(&gatewayNN1.Namespace),
											SectionName: new(gwtypes.SectionName("http-2")),
										},
										ControllerName: "konghq.com/kic-gateway-controller",
										Conditions:     []metav1.Condition{},
									},
								},
							},
						},
					}
				},
				expectedUpdate: true,
				expectedStatus: &gwtypes.HTTPRouteStatus{
					Parents: []gwtypes.RouteParentStatus{
						{
							ParentRef: gwtypes.ParentReference{
								Kind:        new(gwtypes.Kind("Gateway")),
								Group:       new(gwtypes.Group(gatewayv1.GroupName)),
								Name:        gwtypes.ObjectName(gatewayNN1.Name),
								Namespace:   (*gwtypes.Namespace)(&gatewayNN1.Namespace),
								SectionName: new(gwtypes.SectionName("http-2")),
							},
							ControllerName: "konghq.com/kic-gateway-controller",
							Conditions: []metav1.Condition{
								{
									Type:               ConditionTypeProgrammed,
									Message:            "",
									ObservedGeneration: 42,
									Status:             metav1.ConditionTrue,
									Reason:             string(gwtypes.RouteConditionAccepted),
									LastTransitionTime: metav1.Now(),
								},
							},
						},
					},
				},
				gatewayFunc: func() []supportedGatewayWithCondition {
					return []supportedGatewayWithCondition{
						{
							gateway:      gateway1,
							listenerName: "http-2",
						},
					}
				},
			},
			{
				name: "Programmed condition gets properly set to Status True when route's parent status is not set and Parent Section is specified with 2 gateways both with section name specified",
				httpRouteFunc: func() *gwtypes.HTTPRoute {
					return &gwtypes.HTTPRoute{
						TypeMeta:   gwtypes.V1HTTPRouteTypeMeta,
						Name:       "basic-httproute",
						Namespace:  gatewayNN1.Namespace,
						Generation: 42,
						Spec: gwtypes.HTTPRouteSpec{
							CommonRouteSpec: gwtypes.CommonRouteSpec{
								ParentRefs: []gwtypes.ParentReference{
									{
										Group:       new(gwtypes.Group(gatewayv1.GroupName)),
										Kind:        new(gwtypes.Kind("Gateway")),
										Name:        gwtypes.ObjectName(gateway1.Name),
										SectionName: new(gwtypes.SectionName("http-2")),
									},
									{
										Group:       new(gwtypes.Group(gatewayv1.GroupName)),
										Kind:        new(gwtypes.Kind("Gateway")),
										Name:        gwtypes.ObjectName(gateway2.Name),
										SectionName: new(gwtypes.SectionName("http-1")),
									},
								},
							},
							Rules: []gwtypes.HTTPRouteRule{
								{
									BackendRefs: []gwtypes.HTTPBackendRef{
										builder.NewHTTPBackendRef("fake-service").WithPort(80).Build(),
									},
								},
							},
						},
						Status: gwtypes.HTTPRouteStatus{},
					}
				},
				expectedUpdate: true,
				expectedStatus: &gwtypes.HTTPRouteStatus{
					Parents: []gwtypes.RouteParentStatus{
						{
							ParentRef: gwtypes.ParentReference{
								Kind:        new(gwtypes.Kind("Gateway")),
								Group:       new(gwtypes.Group(gatewayv1.GroupName)),
								Name:        gwtypes.ObjectName(gatewayNN1.Name),
								Namespace:   (*gwtypes.Namespace)(&gatewayNN1.Namespace),
								SectionName: new(gwtypes.SectionName("http-2")),
							},
							ControllerName: "konghq.com/kic-gateway-controller",
							Conditions: []metav1.Condition{
								{
									Type:               ConditionTypeProgrammed,
									Message:            "",
									ObservedGeneration: 42,
									Status:             metav1.ConditionTrue,
									Reason:             string(gwtypes.RouteConditionAccepted),
									LastTransitionTime: metav1.Now(),
								},
							},
						},
						{
							ParentRef: gwtypes.ParentReference{
								Kind:        new(gwtypes.Kind("Gateway")),
								Group:       new(gwtypes.Group(gatewayv1.GroupName)),
								Name:        gwtypes.ObjectName(gatewayNN2.Name),
								Namespace:   (*gwtypes.Namespace)(&gatewayNN2.Namespace),
								SectionName: new(gwtypes.SectionName("http-1")),
							},
							ControllerName: "konghq.com/kic-gateway-controller",
							Conditions: []metav1.Condition{
								{
									Type:               ConditionTypeProgrammed,
									Message:            "",
									ObservedGeneration: 42,
									Status:             metav1.ConditionTrue,
									Reason:             string(gwtypes.RouteConditionAccepted),
									LastTransitionTime: metav1.Now(),
								},
							},
						},
					},
				},
				gatewayFunc: func() []supportedGatewayWithCondition {
					return []supportedGatewayWithCondition{
						{
							gateway:      gateway1,
							listenerName: "http-2",
						},
						{
							gateway:      gateway2,
							listenerName: "http-1",
						},
					}
				},
			},
			{
				name: "Programmed condition gets properly added to route's parents status when no status for that parent is present yet",
				httpRouteFunc: func() *gwtypes.HTTPRoute {
					return &gwtypes.HTTPRoute{
						TypeMeta:   gwtypes.V1HTTPRouteTypeMeta,
						Name:       "basic-httproute",
						Namespace:  gatewayNN1.Namespace,
						Generation: 42,
						Spec: gwtypes.HTTPRouteSpec{
							CommonRouteSpec: gwtypes.CommonRouteSpec{
								ParentRefs: []gwtypes.ParentReference{
									{
										Group: new(gwtypes.Group(gatewayv1.GroupName)),
										Kind:  new(gwtypes.Kind("Gateway")),
										Name:  "test-gateway",
									},
								},
							},
							Rules: []gwtypes.HTTPRouteRule{
								{
									BackendRefs: []gwtypes.HTTPBackendRef{
										builder.NewHTTPBackendRef("fake-service").WithPort(80).Build(),
									},
								},
							},
						},
					}
				},
				expectedUpdate: true,
				expectedStatus: &gwtypes.HTTPRouteStatus{
					Parents: []gwtypes.RouteParentStatus{
						{
							ParentRef: gwtypes.ParentReference{
								Kind:      new(gwtypes.Kind("Gateway")),
								Group:     new(gwtypes.Group(gatewayv1.GroupName)),
								Name:      gwtypes.ObjectName(gatewayNN1.Name),
								Namespace: (*gwtypes.Namespace)(&gatewayNN1.Namespace),
							},
							ControllerName: "konghq.com/kic-gateway-controller",
							Conditions: []metav1.Condition{
								{
									Type:               ConditionTypeProgrammed,
									Message:            "",
									ObservedGeneration: 42,
									Status:             metav1.ConditionTrue,
									Reason:             string(gwtypes.RouteConditionAccepted),
									LastTransitionTime: metav1.Now(),
								},
							},
						},
					},
				},
				gatewayFunc: func() []supportedGatewayWithCondition {
					return []supportedGatewayWithCondition{
						{gateway: gateway1},
					}
				},
			},
			{
				name: "no update is being done when an expected Programmed condition is already in place",
				httpRouteFunc: func() *gwtypes.HTTPRoute {
					return &gwtypes.HTTPRoute{
						TypeMeta:   gwtypes.V1HTTPRouteTypeMeta,
						Name:       "basic-httproute",
						Namespace:  gatewayNN1.Namespace,
						Generation: 42,
						Spec: gwtypes.HTTPRouteSpec{
							CommonRouteSpec: gwtypes.CommonRouteSpec{
								ParentRefs: []gwtypes.ParentReference{
									{
										Group: new(gwtypes.Group(gatewayv1.GroupName)),
										Kind:  new(gwtypes.Kind("Gateway")),
										Name:  "test-gateway",
									},
								},
							},
							Rules: []gwtypes.HTTPRouteRule{
								{
									BackendRefs: []gwtypes.HTTPBackendRef{
										builder.NewHTTPBackendRef("fake-service").WithPort(80).Build(),
									},
								},
							},
						},
						Status: gwtypes.HTTPRouteStatus{
							RouteStatus: gwtypes.RouteStatus{
								Parents: []gwtypes.RouteParentStatus{
									{
										ParentRef: gwtypes.ParentReference{
											Kind:      new(gwtypes.Kind("Gateway")),
											Group:     new(gwtypes.Group(gatewayv1.GroupName)),
											Name:      gwtypes.ObjectName(gatewayNN1.Name),
											Namespace: (*gwtypes.Namespace)(&gatewayNN1.Namespace),
										},
										ControllerName: "konghq.com/kic-gateway-controller",
										Conditions: []metav1.Condition{
											{
												Type:               ConditionTypeProgrammed,
												Message:            "",
												ObservedGeneration: 42,
												Status:             metav1.ConditionTrue,
												Reason:             string(gwtypes.RouteConditionAccepted),
												LastTransitionTime: metav1.Now(),
											},
										},
									},
								},
							},
						},
					}
				},
				expectedUpdate: false,
				gatewayFunc: func() []supportedGatewayWithCondition {
					return []supportedGatewayWithCondition{
						{gateway: gateway1},
					}
				},
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				var (
					ctx       = t.Context()
					httproute = tc.httpRouteFunc()
					gateways  = tc.gatewayFunc()
				)

				fakeClient := fakeclient.
					NewClientBuilder().
					WithScheme(scheme.Scheme).
					WithObjects(httproute).
					WithStatusSubresource(httproute).
					Build()

				updated, err := ensureParentsProgrammedCondition(ctx, fakeClient.Status(), httproute, httproute.Status.Parents, gateways,
					metav1.Condition{
						Status: metav1.ConditionTrue,
						Reason: string(gwtypes.RouteConditionAccepted),
					},
				)
				require.NoError(t, err)
				if tc.expectedUpdate {
					require.True(t, updated)
					require.NoError(t, fakeClient.Get(ctx, client.ObjectKeyFromObject(httproute), httproute))

					diff := cmp.Diff(*tc.expectedStatus, httproute.Status,
						// Don't compare the time since metav1.Now() is used.
						cmp.FilterPath(
							func(p cmp.Path) bool { return p.String() == "RouteStatus.Parents.Conditions.LastTransitionTime" },
							cmp.Ignore(),
						),
					)
					if diff != "" {
						t.Errorf("HTTPRoute Status not as expected:\n%s", diff)
					}
				} else {
					require.False(t, updated)
				}
			})
		}
	})
}

func TestIsRouteAcceptedByListener(t *testing.T) {
	testCases := []struct {
		name          string
		gateway       gwtypes.Gateway
		httpRoute     *gwtypes.HTTPRoute
		expectedValue bool
	}{
		{
			name: "accepted, allowedRoutes from the same namespace",
			httpRoute: &gwtypes.HTTPRoute{
				Name:      "route",
				Namespace: "default",
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{
							{
								Name:        "gateway",
								SectionName: new(gwtypes.SectionName("listener-1")),
							},
						},
					},
				},
			},
			gateway: gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gateway",
					Namespace: "default",
				},
				Spec: gwtypes.GatewaySpec{
					Listeners: []gwtypes.Listener{
						{
							Name:          "listener-1",
							AllowedRoutes: builder.NewAllowedRoutesFromSameNamespaces(),
							Protocol:      gwtypes.HTTPProtocolType,
						},
					},
				},
				Status: gwtypes.GatewayStatus{
					Listeners: []gwtypes.ListenerStatus{
						{
							Name: gwtypes.SectionName("listener-1"),
							SupportedKinds: []gwtypes.RouteGroupKind{
								{
									Group: new(gwtypes.Group("gateway.networking.k8s.io")),
									Kind:  "HTTPRoute",
								},
							},
							Conditions: []metav1.Condition{
								{
									Type:   string(gwtypes.ListenerConditionProgrammed),
									Status: metav1.ConditionTrue,
								},
							},
						},
					},
				},
			},
			expectedValue: true,
		},
		{
			name: "accepted, allowedRoutes from selected namespace",
			httpRoute: &gwtypes.HTTPRoute{
				Name:      "route",
				Namespace: "other-namespace",
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{
							{
								Name:        "gateway",
								SectionName: new(gwtypes.SectionName("listener-1")),
							},
						},
					},
				},
			},
			gateway: gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gateway",
					Namespace: "default",
				},
				Spec: gwtypes.GatewaySpec{
					Listeners: []gwtypes.Listener{
						{
							Name: "listener-1",
							AllowedRoutes: builder.NewAllowedRoutesFromSelectorNamespace(&metav1.LabelSelector{
								MatchLabels: map[string]string{
									"konghq.com/allowed-namespace": "true",
								},
							}),
							Protocol: gwtypes.HTTPProtocolType,
						},
					},
				},
				Status: gwtypes.GatewayStatus{
					Listeners: []gwtypes.ListenerStatus{
						{
							Name: gwtypes.SectionName("listener-1"),
							SupportedKinds: []gwtypes.RouteGroupKind{
								{
									Group: new(gwtypes.Group("gateway.networking.k8s.io")),
									Kind:  "HTTPRoute",
								},
							},
							Conditions: []metav1.Condition{
								{
									Type:   string(gwtypes.ListenerConditionProgrammed),
									Status: metav1.ConditionTrue,
								},
							},
						},
					},
				},
			},
			expectedValue: true,
		},
		{
			name: "route accepted but listener not programmed",
			httpRoute: &gwtypes.HTTPRoute{
				Name:      "route",
				Namespace: "default",
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{
							{
								Name:        "gateway",
								SectionName: new(gwtypes.SectionName("listener-1")),
							},
						},
					},
				},
			},
			gateway: gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gateway",
					Namespace: "default",
				},
				Spec: gwtypes.GatewaySpec{
					Listeners: []gwtypes.Listener{
						{
							Name:          "listener-1",
							AllowedRoutes: builder.NewAllowedRoutesFromSameNamespaces(),
							Protocol:      gwtypes.HTTPProtocolType,
						},
					},
				},
				Status: gwtypes.GatewayStatus{
					Listeners: []gwtypes.ListenerStatus{
						{
							Name: gwtypes.SectionName("listener-1"),
							SupportedKinds: []gwtypes.RouteGroupKind{
								{
									Group: new(gwtypes.Group("gateway.networking.k8s.io")),
									Kind:  "HTTPRoute",
								},
							},
							Conditions: []metav1.Condition{
								{
									Type:   string(gwtypes.ListenerConditionProgrammed),
									Status: metav1.ConditionFalse,
								},
							},
						},
					},
				},
			},
			expectedValue: true,
		},
		{
			name: "not accepted, not in listener's supportedKinds",
			httpRoute: &gwtypes.HTTPRoute{
				Name:      "route",
				Namespace: "default",
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{
							{
								Name:        "gateway",
								SectionName: new(gwtypes.SectionName("listener-1")),
							},
						},
					},
				},
			},
			gateway: gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gateway",
					Namespace: "default",
				},
				Spec: gwtypes.GatewaySpec{
					Listeners: []gwtypes.Listener{
						{
							Name:          "listener-1",
							AllowedRoutes: builder.NewAllowedRoutesFromSameNamespaces(),
							Protocol:      gwtypes.HTTPProtocolType,
						},
					},
				},
				Status: gwtypes.GatewayStatus{
					Listeners: []gwtypes.ListenerStatus{
						{
							Name: gwtypes.SectionName("listener-1"),
							SupportedKinds: []gwtypes.RouteGroupKind{
								{
									Group: new(gwtypes.Group("gateway.networking.k8s.io")),
									Kind:  "GRPCRoute",
								},
							},
							Conditions: []metav1.Condition{
								{
									Type:   string(gwtypes.ListenerConditionProgrammed),
									Status: metav1.ConditionTrue,
								},
							},
						},
					},
				},
			},
			expectedValue: false,
		},
		{
			name: "not accepted, wrong sectionName",
			httpRoute: &gwtypes.HTTPRoute{
				Name:      "route",
				Namespace: "default",
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{
							{
								Name:        "gateway",
								SectionName: new(gwtypes.SectionName("wrong-listener")),
							},
						},
					},
				},
			},
			gateway: gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gateway",
					Namespace: "default",
				},
				Spec: gwtypes.GatewaySpec{
					Listeners: []gwtypes.Listener{
						{
							Name:          "listener-1",
							AllowedRoutes: builder.NewAllowedRoutesFromSameNamespaces(),
							Protocol:      gwtypes.HTTPProtocolType,
						},
					},
				},
				Status: gwtypes.GatewayStatus{
					Listeners: []gwtypes.ListenerStatus{
						{
							Name: gwtypes.SectionName("listener-1"),
							SupportedKinds: []gwtypes.RouteGroupKind{
								{
									Group: new(gwtypes.Group("gateway.networking.k8s.io")),
									Kind:  "HTTPRoute",
								},
							},
							Conditions: []metav1.Condition{
								{
									Type:   string(gwtypes.ListenerConditionProgrammed),
									Status: metav1.ConditionTrue,
								},
							},
						},
					},
				},
			},
			expectedValue: false,
		},
		{
			name: "not accepted, wrong port",
			httpRoute: &gwtypes.HTTPRoute{
				Name:      "route",
				Namespace: "default",
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{
							{
								Name:        "gateway",
								SectionName: new(gwtypes.SectionName("listener-1")),
								Port:        new(gwtypes.PortNumber(8080)),
							},
						},
					},
				},
			},
			gateway: gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gateway",
					Namespace: "default",
				},
				Spec: gwtypes.GatewaySpec{
					Listeners: []gwtypes.Listener{
						{
							Name:          "listener-1",
							AllowedRoutes: builder.NewAllowedRoutesFromSameNamespaces(),
							Protocol:      gwtypes.HTTPProtocolType,
							Port:          9090,
						},
					},
				},
				Status: gwtypes.GatewayStatus{
					Listeners: []gwtypes.ListenerStatus{
						{
							Name: gwtypes.SectionName("listener-1"),
							SupportedKinds: []gwtypes.RouteGroupKind{
								{
									Group: new(gwtypes.Group("gateway.networking.k8s.io")),
									Kind:  "HTTPRoute",
								},
							},
							Conditions: []metav1.Condition{
								{
									Type:   string(gwtypes.ListenerConditionProgrammed),
									Status: metav1.ConditionTrue,
								},
							},
						},
					},
				},
			},
			expectedValue: false,
		},
		{
			name: "not accepted, wrong protocol",
			httpRoute: &gwtypes.HTTPRoute{
				Name:      "route",
				Namespace: "default",
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{
							{
								Name:        "gateway",
								SectionName: new(gwtypes.SectionName("listener-1")),
							},
						},
					},
				},
			},
			gateway: gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gateway",
					Namespace: "default",
				},
				Spec: gwtypes.GatewaySpec{
					Listeners: []gwtypes.Listener{
						{
							Name:          "listener-1",
							AllowedRoutes: builder.NewAllowedRoutesFromSameNamespaces(),
							Protocol:      gwtypes.UDPProtocolType,
						},
					},
				},
				Status: gwtypes.GatewayStatus{
					Listeners: []gwtypes.ListenerStatus{
						{
							Name: gwtypes.SectionName("listener-1"),
							SupportedKinds: []gwtypes.RouteGroupKind{
								{
									Group: new(gwtypes.Group("gateway.networking.k8s.io")),
									Kind:  "HTTPRoute",
								},
							},
							Conditions: []metav1.Condition{
								{
									Type:   string(gwtypes.ListenerConditionProgrammed),
									Status: metav1.ConditionTrue,
								},
							},
						},
					},
				},
			},
			expectedValue: false,
		},
		{
			name: "not accepted, wrong hostnames",
			httpRoute: &gwtypes.HTTPRoute{
				Name:      "route",
				Namespace: "default",
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{
							{
								Name:        "gateway",
								SectionName: new(gwtypes.SectionName("listener-1")),
							},
						},
					},
					Hostnames: []gwtypes.Hostname{
						"wrong.hostname.com",
					},
				},
			},
			gateway: gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gateway",
					Namespace: "default",
				},
				Spec: gwtypes.GatewaySpec{
					Listeners: []gwtypes.Listener{
						{
							Name:          "listener-1",
							AllowedRoutes: builder.NewAllowedRoutesFromSameNamespaces(),
							Protocol:      gwtypes.HTTPProtocolType,
							Hostname:      new(gwtypes.Hostname("foo.bar.com")),
						},
					},
				},
				Status: gwtypes.GatewayStatus{
					Listeners: []gwtypes.ListenerStatus{
						{
							Name: gwtypes.SectionName("listener-1"),
							SupportedKinds: []gwtypes.RouteGroupKind{
								{
									Group: new(gwtypes.Group("gateway.networking.k8s.io")),
									Kind:  "HTTPRoute",
								},
							},
							Conditions: []metav1.Condition{
								{
									Type:   string(gwtypes.ListenerConditionProgrammed),
									Status: metav1.ConditionTrue,
								},
							},
						},
					},
				},
			},
			expectedValue: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var (
				ctx       = t.Context()
				namespace = &corev1.Namespace{
					Name: "other-namespace",
					Labels: map[string]string{
						"konghq.com/allowed-namespace": "true",
					},
				}
				fakeClient = fakeclient.
						NewClientBuilder().
						WithScheme(scheme.Scheme).
						WithObjects(namespace).
						Build()
			)

			ok, err := isRouteAcceptedByListener(ctx, fakeClient, tc.httpRoute, tc.gateway, 0, tc.httpRoute.Spec.ParentRefs[0])
			assert.NoError(t, err)
			assert.Equal(t, tc.expectedValue, ok)
		})
	}
}

func TestParentRefsForRoute(t *testing.T) {
	gatewayGroup := gwtypes.Group(gatewayv1.GroupName)
	gatewayKind := gwtypes.Kind("Gateway")
	listenerSetKind := gwtypes.Kind("ListenerSet")
	otherGroup := gwtypes.Group("example.com")
	otherKind := gwtypes.Kind("Frobnicator")

	routeWithParents := func(refs ...gwtypes.ParentReference) *gwtypes.HTTPRoute {
		return &gwtypes.HTTPRoute{
			TypeMeta:  gwtypes.V1GatewayTypeMeta,
			Name:      "httproute",
			Namespace: "test-namespace",
			Spec: gwtypes.HTTPRouteSpec{
				CommonRouteSpec: gwtypes.CommonRouteSpec{ParentRefs: refs},
			},
		}
	}

	testCases := []struct {
		name     string
		route    *gwtypes.HTTPRoute
		expected []gwtypes.ParentReference
	}{
		{
			name:     "a Gateway parentRef is kept",
			route:    routeWithParents(gwtypes.ParentReference{Group: &gatewayGroup, Kind: &gatewayKind, Name: "gw"}),
			expected: []gwtypes.ParentReference{{Group: &gatewayGroup, Kind: &gatewayKind, Name: "gw"}},
		},
		{
			name:     "an omitted Group and Kind default to a Gateway and are kept",
			route:    routeWithParents(gwtypes.ParentReference{Name: "gw"}),
			expected: []gwtypes.ParentReference{{Name: "gw"}},
		},
		{
			name:     "a ListenerSet parentRef is dropped",
			route:    routeWithParents(gwtypes.ParentReference{Group: &gatewayGroup, Kind: &listenerSetKind, Name: "ls"}),
			expected: []gwtypes.ParentReference{},
		},
		{
			name:     "a parentRef from an unrelated group is dropped",
			route:    routeWithParents(gwtypes.ParentReference{Group: &otherGroup, Kind: &otherKind, Name: "other"}),
			expected: []gwtypes.ParentReference{},
		},
		{
			name: "an unsupported parentRef does not discard the supported ones alongside it",
			route: routeWithParents(
				gwtypes.ParentReference{Group: &gatewayGroup, Kind: &listenerSetKind, Name: "ls"},
				gwtypes.ParentReference{Group: &gatewayGroup, Kind: &gatewayKind, Name: "gw"},
			),
			expected: []gwtypes.ParentReference{{Group: &gatewayGroup, Kind: &gatewayKind, Name: "gw"}},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			refs, err := parentRefsForRoute(tc.route)
			require.NoError(t, err)
			require.Equal(t, tc.expected, refs)
		})
	}
}
