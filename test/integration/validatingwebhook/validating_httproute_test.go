package validatingwebhook

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kong/kong-operator/test/integration"
)

const invalidRegexPath = "/foo[[[["

type testCaseHTTPRouteValidation struct {
	Name                   string
	Route                  *gatewayv1.HTTPRoute
	WantCreateErrSubstring string
	// ExpressionsRouterOnly indicates that the test case only applies to expressions router mode.
	// If true and not in expressions router mode, the WantCreateErrSubstring is ignored (expect success).
	ExpressionsRouterOnly bool
}

func TestAdmissionWebhook_HTTPRoute(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	ns, _, _, ctrlClient, managedGatewayClass := bootstrapGateway(ctx, t, integration.GetEnv(), integration.GetClients().MgrClient)

	t.Log("creating a gateway client")
	gatewayClient := integration.GetClients().GatewayClient

	t.Log("creating a managed gateway")
	managedGateway := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      uuid.NewString(),
			Namespace: ns.Name,
		},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: gatewayv1.ObjectName(managedGatewayClass.Name),
			Listeners: []gatewayv1.Listener{
				{
					Name:     "http",
					Protocol: gatewayv1.HTTPProtocolType,
					Port:     gatewayv1.PortNumber(80),
				},
			},
		},
	}
	var err error
	managedGateway, err = gatewayClient.GatewayV1().Gateways(ns.Name).Create(ctx, managedGateway, metav1.CreateOptions{})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = gatewayClient.GatewayV1().Gateways(ns.Name).Delete(ctx, managedGateway.Name, metav1.DeleteOptions{})
	})
	t.Logf("created managed gateway: %q", managedGateway.Name)

	t.Logf("creating an unmanaged gatewayclass")
	unmanagedGatewayClass := &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: uuid.NewString(),
		},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: "example.com/unsupported-gateway-controller",
		},
	}
	unmanagedGatewayClass, err = gatewayClient.GatewayV1().GatewayClasses().Create(ctx, unmanagedGatewayClass, metav1.CreateOptions{})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = gatewayClient.GatewayV1().GatewayClasses().Delete(ctx, unmanagedGatewayClass.Name, metav1.DeleteOptions{})
	})
	t.Logf("created unmanaged gatewayclass: %q", unmanagedGatewayClass.Name)

	t.Log("creating an unmanaged gateway")
	unmanagedGateway := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      uuid.NewString(),
			Namespace: ns.Name,
		},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: gatewayv1.ObjectName(unmanagedGatewayClass.Name),
			Listeners: []gatewayv1.Listener{
				{
					Name:     "http",
					Protocol: gatewayv1.HTTPProtocolType,
					Port:     gatewayv1.PortNumber(80),
				},
			},
		},
	}
	unmanagedGateway, err = gatewayClient.GatewayV1().Gateways(ns.Name).Create(ctx, unmanagedGateway, metav1.CreateOptions{})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = gatewayClient.GatewayV1().Gateways(ns.Name).Delete(ctx, unmanagedGateway.Name, metav1.DeleteOptions{})
	})
	t.Logf("created unmanaged gateway: %q", unmanagedGateway.Name)

	_ = ctrlClient

	testCases := []testCaseHTTPRouteValidation{
		{
			Name: "a valid httproute linked to a managed gateway passes validation",
			Route: &gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name: uuid.NewString(),
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{{
							Namespace: (*gatewayv1.Namespace)(&managedGateway.Namespace),
							Name:      gatewayv1.ObjectName(managedGateway.Name),
						}},
					},
				},
			},
		},
		{
			Name: "a httproute linked to a non-existent gateway passes validation",
			Route: &gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name: uuid.NewString(),
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{{
							Namespace: (*gatewayv1.Namespace)(&managedGateway.Namespace),
							Name:      gatewayv1.ObjectName("fake-gateway"),
						}},
					},
				},
			},
		},
		{
			Name: "an invalid httproute will pass validation if it's not linked to a managed controller (it's not ours)",
			Route: &gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name: uuid.NewString(),
				},
				Spec: gatewayv1.HTTPRouteSpec{
					Rules: []gatewayv1.HTTPRouteRule{{
						Matches: []gatewayv1.HTTPRouteMatch{
							newHTTPRouteMatchWithPathRegex(invalidRegexPath),
						},
					}},
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{{
							Namespace: (*gatewayv1.Namespace)(&unmanagedGateway.Namespace),
							Name:      gatewayv1.ObjectName(unmanagedGateway.Name),
						}},
					},
				},
			},
		},
		{
			Name: "a httproute with valid regex expressions for a path and a header pass validation",
			Route: &gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name: uuid.NewString(),
				},
				Spec: gatewayv1.HTTPRouteSpec{
					Hostnames: []gatewayv1.Hostname{"foo.com"},
					Rules: []gatewayv1.HTTPRouteRule{
						{
							Matches: []gatewayv1.HTTPRouteMatch{
								newHTTPRouteMatchWithPathRegex("/path[1-8]"),
								newHTTPRouteMatchWithHeaderRegex("foo", "bar[1-8]"),
							},
						},
					},
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{{
							Namespace: (*gatewayv1.Namespace)(&managedGateway.Namespace),
							Name:      gatewayv1.ObjectName(managedGateway.Name),
						}},
					},
				},
			},
		},
		{
			Name: "a httproute with an invalid path regex fails validation",
			Route: &gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name: uuid.NewString(),
				},
				Spec: gatewayv1.HTTPRouteSpec{
					Rules: []gatewayv1.HTTPRouteRule{{
						Matches: []gatewayv1.HTTPRouteMatch{
							newHTTPRouteMatchWithPathRegex(invalidRegexPath),
						},
					}},
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{{
							Namespace: (*gatewayv1.Namespace)(&managedGateway.Namespace),
							Name:      gatewayv1.ObjectName(managedGateway.Name),
						}},
					},
				},
			},
			WantCreateErrSubstring: "HTTPRoute failed schema validation",
		},
		{
			Name: "a httproute with an invalid header regex fails validation",
			Route: &gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name: uuid.NewString(),
				},
				Spec: gatewayv1.HTTPRouteSpec{
					Rules: []gatewayv1.HTTPRouteRule{{
						Matches: []gatewayv1.HTTPRouteMatch{
							newHTTPRouteMatchWithHeaderRegex("foo", "[[[invalid-regex[[[["),
						},
					}},
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{{
							Namespace: (*gatewayv1.Namespace)(&managedGateway.Namespace),
							Name:      gatewayv1.ObjectName(managedGateway.Name),
						}},
					},
				},
			},
			WantCreateErrSubstring: "HTTPRoute failed schema validation",
		},
	}

	for _, tC := range testCases {
		t.Run(tC.Name, func(t *testing.T) {
			_, err := gatewayClient.GatewayV1().HTTPRoutes(ns.Name).Create(ctx, tC.Route, metav1.CreateOptions{})

			wantErr := tC.WantCreateErrSubstring
			if tC.ExpressionsRouterOnly && !isExpressionsRouterMode() {
				wantErr = ""
			}

			if wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), wantErr)
			} else {
				require.NoError(t, err)
				t.Cleanup(func() {
					_ = gatewayClient.GatewayV1().HTTPRoutes(ns.Name).Delete(ctx, tC.Route.Name, metav1.DeleteOptions{})
				})
			}
		})
	}
}

func newHTTPRouteMatchWithPathRegex(pathRegexp string) gatewayv1.HTTPRouteMatch {
	pathMatchType := gatewayv1.PathMatchRegularExpression
	return gatewayv1.HTTPRouteMatch{
		Path: &gatewayv1.HTTPPathMatch{
			Type:  &pathMatchType,
			Value: &pathRegexp,
		},
	}
}

func newHTTPRouteMatchWithHeaderRegex(name, value string) gatewayv1.HTTPRouteMatch {
	headerMatchType := gatewayv1.HeaderMatchRegularExpression
	return gatewayv1.HTTPRouteMatch{
		Headers: []gatewayv1.HTTPHeaderMatch{
			{
				Name:  gatewayv1.HTTPHeaderName(name),
				Value: value,
				Type:  &headerMatchType,
			},
		},
	}
}
