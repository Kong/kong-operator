package gateway

import (
	"context"
	"testing"

	"github.com/blang/semver/v4"
	"github.com/kong/go-kong/kong"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/annotations"
	gatewaycontroller "github.com/kong/kong-operator/v2/ingress-controller/internal/controllers/gateway"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/dataplane/translator"
	"github.com/kong/kong-operator/v2/ingress-controller/pkg/manager/scheme"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

func TestValidateHTTPRoute(t *testing.T) {
	var (
		group              = gwtypes.Group("gateway.networking.k8s.io")
		defaultGWNamespace = gwtypes.Namespace(corev1.NamespaceDefault)
		exampleGroup       = gwtypes.Group("example")
		podKind            = gwtypes.Kind("Pod")
		gatewayClassName   = gwtypes.ObjectName("kong")
		gatewayClass       = &gwtypes.GatewayClass{
			Name: string(gatewayClassName),
			Spec: gwtypes.GatewayClassSpec{
				ControllerName: gatewaycontroller.GetControllerName(),
			},
		}
		kongVersionSupportWildcardSNI = semver.MustParse("3.7.0")
	)

	for _, tt := range []struct {
		msg           string
		route         *gwtypes.HTTPRoute
		cachedObjects []client.Object
		valid         bool
		validationMsg string
		err           error
	}{
		{
			msg: "route with no parentRef is accepted with no validations",
			route: &gwtypes.HTTPRoute{
				Namespace: corev1.NamespaceDefault,
				Name:      "testing-httproute",
			}, // no parentRefs
			cachedObjects: []client.Object{
				gatewayClass,
				&gwtypes.Gateway{
					Namespace: corev1.NamespaceDefault,
					Name:      "testing-gateway",
					Spec: gwtypes.GatewaySpec{
						GatewayClassName: gatewayClassName,
						Listeners: []gwtypes.Listener{{
							Name:     "http",
							Port:     80,
							Protocol: (gwtypes.HTTPProtocolType),
							AllowedRoutes: &gwtypes.AllowedRoutes{
								Kinds: []gwtypes.RouteGroupKind{{
									Group: &group,
									Kind:  "HTTPRoute",
								}},
							},
						}},
					},
				},
			},
			valid: true,
		},
		{
			msg: "route with parentRef to non-gateway object is accepted with no validation",
			route: &gwtypes.HTTPRoute{
				Namespace: corev1.NamespaceDefault,
				Name:      "testing-httproute",
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{
							{
								Kind:      new(gwtypes.Kind("Service")),
								Namespace: new(gwtypes.Namespace(corev1.NamespaceDefault)),
								Name:      gwtypes.ObjectName("kuma-cp"),
							},
						},
					},
				},
			}, // parentRef to a Service
			cachedObjects: []client.Object{
				gatewayClass,
				&gwtypes.Gateway{
					Namespace: corev1.NamespaceDefault,
					Name:      "testing-gateway",
					Spec: gwtypes.GatewaySpec{
						GatewayClassName: gatewayClassName,
						Listeners: []gwtypes.Listener{{
							Name:     "http",
							Port:     80,
							Protocol: (gwtypes.HTTPProtocolType),
							AllowedRoutes: &gwtypes.AllowedRoutes{
								Kinds: []gwtypes.RouteGroupKind{{
									Group: &group,
									Kind:  "HTTPRoute",
								}},
							},
						}},
					},
				},
			},
			valid: true,
		},
		{
			msg: "parentRefs which omit the namespace pass validation in the same namespace",
			route: &gwtypes.HTTPRoute{
				Namespace: corev1.NamespaceDefault,
				Name:      "testing-httproute",
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{{
							Name: "testing-gateway",
						}},
					},
				},
			},
			cachedObjects: []client.Object{
				gatewayClass,
				&gwtypes.Gateway{
					Namespace: corev1.NamespaceDefault,
					Name:      "testing-gateway",
					Spec: gwtypes.GatewaySpec{
						GatewayClassName: gatewayClassName,
						Listeners: []gwtypes.Listener{{
							Name:     "http",
							Port:     80,
							Protocol: (gwtypes.HTTPProtocolType),
							AllowedRoutes: &gwtypes.AllowedRoutes{
								Kinds: []gwtypes.RouteGroupKind{{
									Group: &group,
									Kind:  "HTTPRoute",
								}},
							},
						}},
					},
				},
			},
			valid: true,
		},
		{
			msg: "valid HTTPRoute with multiple plugins of the same type attached - passes validation",
			route: &gwtypes.HTTPRoute{
				Namespace: corev1.NamespaceDefault,
				Name:      "testing-httproute",
				Annotations: map[string]string{
					"konghq.com/plugins": "plugin1,plugin2,plugin3",
				},
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{{
							Name: "testing-gateway",
						}},
					},
				},
			},
			cachedObjects: []client.Object{
				gatewayClass,
				&gwtypes.Gateway{
					Namespace: corev1.NamespaceDefault,
					Name:      "testing-gateway",
					Spec: gwtypes.GatewaySpec{
						GatewayClassName: gatewayClassName,
						Listeners: []gwtypes.Listener{{
							Name:     "http",
							Port:     80,
							Protocol: (gwtypes.HTTPProtocolType),
							AllowedRoutes: &gwtypes.AllowedRoutes{
								Kinds: []gwtypes.RouteGroupKind{{
									Group: &group,
									Kind:  "HTTPRoute",
								}},
							},
						}},
					},
				},
				&configurationv1.KongPlugin{
					Name:       "plugin1",
					Namespace:  "default",
					PluginName: "foo",
				},
				&configurationv1.KongPlugin{
					Name:       "plugin2",
					Namespace:  "default",
					PluginName: "bar",
				},
				&configurationv1.KongPlugin{
					Name:       "plugin3",
					Namespace:  "default",
					PluginName: "foo",
				},
			},
			valid: true,
		},
		{
			msg: "valid HTTPRoute with multiple plugins of different types and one no-existing attached - passes validation",
			route: &gwtypes.HTTPRoute{
				Namespace: corev1.NamespaceDefault,
				Name:      "testing-httproute",
				Annotations: map[string]string{
					"konghq.com/plugins": "plugin1,plugin2,plugin3",
				},
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{{
							Name: "testing-gateway",
						}},
					},
				},
			},
			cachedObjects: []client.Object{
				gatewayClass,
				&gwtypes.Gateway{
					Namespace: corev1.NamespaceDefault,
					Name:      "testing-gateway",
					Spec: gwtypes.GatewaySpec{
						GatewayClassName: gatewayClassName,
						Listeners: []gwtypes.Listener{{
							Name:     "http",
							Port:     80,
							Protocol: (gwtypes.HTTPProtocolType),
							AllowedRoutes: &gwtypes.AllowedRoutes{
								Kinds: []gwtypes.RouteGroupKind{{
									Group: &group,
									Kind:  "HTTPRoute",
								}},
							},
						}},
					},
				},
				&configurationv1.KongPlugin{
					Name:       "plugin1",
					Namespace:  "default",
					PluginName: "foo",
				},
				&configurationv1.KongPlugin{
					Name:       "plugin2",
					Namespace:  "default",
					PluginName: "bar",
				},
			},
			valid: true,
		},
		{
			msg: "if an HTTPRoute is using queryparams matching it fails validation due to only supporting expression router",
			route: &gwtypes.HTTPRoute{
				Namespace: corev1.NamespaceDefault,
				Name:      "testing-httproute",
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{{
							Name: "testing-gateway",
						}},
					},
					Rules: []gwtypes.HTTPRouteRule{{
						Matches: []gwtypes.HTTPRouteMatch{{
							QueryParams: []gwtypes.HTTPQueryParamMatch{{
								Name:  "user-agent",
								Value: "netscape navigator",
							}},
						}},
						BackendRefs: []gwtypes.HTTPBackendRef{{
							Namespace: &defaultGWNamespace,
						}},
					}},
				},
			},
			cachedObjects: []client.Object{
				gatewayClass,
				&gwtypes.Gateway{
					Namespace: corev1.NamespaceDefault,
					Name:      "testing-gateway",
					Spec: gwtypes.GatewaySpec{
						GatewayClassName: gatewayClassName,
						Listeners: []gwtypes.Listener{{
							Name:     "http",
							Port:     80,
							Protocol: (gwtypes.HTTPProtocolType),
							AllowedRoutes: &gwtypes.AllowedRoutes{
								Kinds: []gwtypes.RouteGroupKind{{
									Group: &group,
									Kind:  "HTTPRoute",
								}},
							},
						}},
					},
				},
			},
			valid:         false,
			validationMsg: "HTTPRoute spec did not pass validation: rules[0].matches[0]: queryparam matching is supported with expression router only",
		},
		{
			msg: "we don't support any group except core kubernetes for backendRefs",
			route: &gwtypes.HTTPRoute{
				Namespace: corev1.NamespaceDefault,
				Name:      "testing-httproute",
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{{
							Name: "testing-gateway",
						}},
					},
					Rules: []gwtypes.HTTPRouteRule{{
						Matches: []gwtypes.HTTPRouteMatch{{
							Headers: []gwtypes.HTTPHeaderMatch{{
								Name:  "Content-Type",
								Value: "audio/vorbis",
							}},
						}},
						BackendRefs: []gwtypes.HTTPBackendRef{
							{
								Group:     &exampleGroup,
								Kind:      &podKind,
								Namespace: &defaultGWNamespace,
								Name:      "service1",
							},
						},
					}},
				},
			},
			cachedObjects: []client.Object{
				gatewayClass,
				&gwtypes.Gateway{
					Namespace: corev1.NamespaceDefault,
					Name:      "testing-gateway",
					Spec: gwtypes.GatewaySpec{
						GatewayClassName: gatewayClassName,
						Listeners: []gwtypes.Listener{{
							Name:     "http",
							Port:     80,
							Protocol: (gwtypes.HTTPProtocolType),
							AllowedRoutes: &gwtypes.AllowedRoutes{
								Kinds: []gwtypes.RouteGroupKind{{
									Group: &group,
									Kind:  "HTTPRoute",
								}},
							},
						}},
					},
				},
			},
			valid:         false,
			validationMsg: "HTTPRoute spec did not pass validation: rules[0].backendRefs[0]: example is not a supported group for httproute backendRefs, only core is supported",
		},
		{
			msg: "we don't support any core kind except Service for backendRefs",
			route: &gwtypes.HTTPRoute{
				Namespace: corev1.NamespaceDefault,
				Name:      "testing-httproute",
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{{
							Name: "testing-gateway",
						}},
					},
					Rules: []gwtypes.HTTPRouteRule{{
						Matches: []gwtypes.HTTPRouteMatch{{
							Headers: []gwtypes.HTTPHeaderMatch{{
								Name:  "Content-Type",
								Value: "audio/vorbis",
							}},
						}},
						BackendRefs: []gwtypes.HTTPBackendRef{
							{
								Kind:      &podKind,
								Namespace: &defaultGWNamespace,
								Name:      "service1",
							},
						},
					}},
				},
			},
			cachedObjects: []client.Object{
				gatewayClass,
				&gwtypes.Gateway{
					Namespace: corev1.NamespaceDefault,
					Name:      "testing-gateway",
					Spec: gwtypes.GatewaySpec{
						GatewayClassName: gatewayClassName,
						Listeners: []gwtypes.Listener{{
							Name:     "http",
							Port:     80,
							Protocol: (gwtypes.HTTPProtocolType),
							AllowedRoutes: &gwtypes.AllowedRoutes{
								Kinds: []gwtypes.RouteGroupKind{{
									Group: &group,
									Kind:  "HTTPRoute",
								}},
							},
						}},
					},
				},
			},
			valid:         false,
			validationMsg: "HTTPRoute spec did not pass validation: rules[0].backendRefs[0]: Pod is not a supported kind for httproute backendRefs, only Service is supported",
		},
		{
			msg: "we do not support RequestMirror filter",
			route: &gwtypes.HTTPRoute{
				Namespace: corev1.NamespaceDefault,
				Name:      "testing-httproute",
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{{
							Name: "testing-gateway",
						}},
					},
					Rules: []gwtypes.HTTPRouteRule{{
						Matches: []gwtypes.HTTPRouteMatch{{
							Headers: []gwtypes.HTTPHeaderMatch{{
								Name:  "Content-Type",
								Value: "audio/vorbis",
							}},
						}},
						BackendRefs: []gwtypes.HTTPBackendRef{
							{
								Name: "service1",
							},
						},
						Filters: []gwtypes.HTTPRouteFilter{
							{
								Type: gwtypes.HTTPRouteFilterRequestMirror,
							},
						},
					}},
				},
			},
			cachedObjects: []client.Object{
				gatewayClass,
				&gwtypes.Gateway{
					Namespace: corev1.NamespaceDefault,
					Name:      "testing-gateway",
					Spec: gwtypes.GatewaySpec{
						GatewayClassName: gatewayClassName,
						Listeners: []gwtypes.Listener{{
							Name:     "http",
							Port:     80,
							Protocol: (gwtypes.HTTPProtocolType),
							AllowedRoutes: &gwtypes.AllowedRoutes{
								Kinds: []gwtypes.RouteGroupKind{{
									Group: &group,
									Kind:  "HTTPRoute",
								}},
							},
						}},
					},
				},
			},
			valid:         false,
			validationMsg: "HTTPRoute spec did not pass validation: rules[0].filters[0]: filter type RequestMirror is unsupported",
		},
		{
			msg: "we do not support CORS filter",
			route: &gwtypes.HTTPRoute{
				Namespace: corev1.NamespaceDefault,
				Name:      "testing-httproute",
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{{
							Name: "testing-gateway",
						}},
					},
					Rules: []gwtypes.HTTPRouteRule{{
						Matches: []gwtypes.HTTPRouteMatch{{
							Headers: []gwtypes.HTTPHeaderMatch{{
								Name:  "Content-Type",
								Value: "audio/vorbis",
							}},
						}},
						BackendRefs: []gwtypes.HTTPBackendRef{
							{
								Name: "service1",
							},
						},
						Filters: []gwtypes.HTTPRouteFilter{
							{
								Type: gwtypes.HTTPRouteFilterCORS,
							},
						},
					}},
				},
			},
			cachedObjects: []client.Object{
				gatewayClass,
				&gwtypes.Gateway{
					Namespace: corev1.NamespaceDefault,
					Name:      "testing-gateway",
					Spec: gwtypes.GatewaySpec{
						GatewayClassName: gatewayClassName,
						Listeners: []gwtypes.Listener{{
							Name:     "http",
							Port:     80,
							Protocol: (gwtypes.HTTPProtocolType),
							AllowedRoutes: &gwtypes.AllowedRoutes{
								Kinds: []gwtypes.RouteGroupKind{{
									Group: &group,
									Kind:  "HTTPRoute",
								}},
							},
						}},
					},
				},
			},
			valid:         false,
			validationMsg: "HTTPRoute spec did not pass validation: rules[0].filters[0]: filter type CORS is unsupported",
		},
		{
			msg: "we only support setting the timeout to the same value",
			route: &gwtypes.HTTPRoute{
				Namespace: corev1.NamespaceDefault,
				Name:      "testing-httproute",
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{{
							Name: "testing-gateway",
						}},
					},
					Rules: []gwtypes.HTTPRouteRule{{
						Matches: []gwtypes.HTTPRouteMatch{{
							Headers: []gwtypes.HTTPHeaderMatch{{
								Name:  "Content-Type",
								Value: "audio/vorbis",
							}},
						}},
						BackendRefs: []gwtypes.HTTPBackendRef{
							{
								Name: "service1",
							},
						},
						Timeouts: &gwtypes.HTTPRouteTimeouts{
							BackendRequest: new(gwtypes.Duration("1s")),
						},
					}, {
						Matches: []gwtypes.HTTPRouteMatch{{
							Headers: []gwtypes.HTTPHeaderMatch{{
								Name:  "Content-Type",
								Value: "audio/vorbis",
							}},
						}},
						BackendRefs: []gwtypes.HTTPBackendRef{
							{
								Name: "service1",
							},
						},
						Timeouts: &gwtypes.HTTPRouteTimeouts{
							BackendRequest: new(gwtypes.Duration("1s")),
						},
					}},
				},
			},
			cachedObjects: []client.Object{
				gatewayClass,
				&gwtypes.Gateway{
					Namespace: corev1.NamespaceDefault,
					Name:      "testing-gateway",
					Spec: gwtypes.GatewaySpec{
						Listeners: []gwtypes.Listener{{
							Name:     "http",
							Port:     80,
							Protocol: (gwtypes.HTTPProtocolType),
							AllowedRoutes: &gwtypes.AllowedRoutes{
								Kinds: []gwtypes.RouteGroupKind{{
									Group: &group,
									Kind:  "HTTPRoute",
								}},
							},
						}},
					},
				},
			},
			valid: true,
		},
		{
			msg: "supports setting the timeout to different values",
			route: &gwtypes.HTTPRoute{
				Namespace: corev1.NamespaceDefault,
				Name:      "testing-httproute",
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{{
							Name: "testing-gateway",
						}},
					},
					Rules: []gwtypes.HTTPRouteRule{{
						Matches: []gwtypes.HTTPRouteMatch{{
							Headers: []gwtypes.HTTPHeaderMatch{{
								Name:  "Content-Type",
								Value: "audio/vorbis",
							}},
						}},
						BackendRefs: []gwtypes.HTTPBackendRef{
							{
								Name: "service1",
							},
						},
						Timeouts: &gwtypes.HTTPRouteTimeouts{
							BackendRequest: new(gwtypes.Duration("1s")),
						},
					}, {
						Matches: []gwtypes.HTTPRouteMatch{{
							Headers: []gwtypes.HTTPHeaderMatch{{
								Name:  "Content-Type",
								Value: "audio/vorbis",
							}},
						}},
						BackendRefs: []gwtypes.HTTPBackendRef{
							{
								Name: "service1",
							},
						},
						Timeouts: &gwtypes.HTTPRouteTimeouts{
							BackendRequest: new(gwtypes.Duration("5s")),
						},
					}},
				},
			},
			cachedObjects: []client.Object{
				gatewayClass,
				&gwtypes.Gateway{
					Namespace: corev1.NamespaceDefault,
					Name:      "testing-gateway",
					Spec: gwtypes.GatewaySpec{
						GatewayClassName: gatewayClassName,
						Listeners: []gwtypes.Listener{{
							Name:     "http",
							Port:     80,
							Protocol: (gwtypes.HTTPProtocolType),
							AllowedRoutes: &gwtypes.AllowedRoutes{
								Kinds: []gwtypes.RouteGroupKind{{
									Group: &group,
									Kind:  "HTTPRoute",
								}},
							},
						}},
					},
				},
			},
			valid: true,
		},
		{
			msg: "we do not support filters in backendRefs",
			route: &gwtypes.HTTPRoute{
				Namespace: corev1.NamespaceDefault,
				Name:      "testing-httproute",
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{{
							Name: "testing-gateway",
						}},
					},
					Rules: []gwtypes.HTTPRouteRule{{
						Matches: []gwtypes.HTTPRouteMatch{{
							Headers: []gwtypes.HTTPHeaderMatch{{
								Name:  "Content-Type",
								Value: "audio/vorbis",
							}},
						}},
						BackendRefs: []gwtypes.HTTPBackendRef{
							{
								Name: "service1",
								Filters: []gwtypes.HTTPRouteFilter{
									{
										Type: gwtypes.HTTPRouteFilterRequestHeaderModifier,
									},
								},
							},
						},
					}},
				},
			},
			cachedObjects: []client.Object{
				gatewayClass,
				&gwtypes.Gateway{
					Namespace: corev1.NamespaceDefault,
					Name:      "testing-gateway",
					Spec: gwtypes.GatewaySpec{
						GatewayClassName: gatewayClassName,
						Listeners: []gwtypes.Listener{{
							Name:     "http",
							Port:     80,
							Protocol: (gwtypes.HTTPProtocolType),
							AllowedRoutes: &gwtypes.AllowedRoutes{
								Kinds: []gwtypes.RouteGroupKind{{
									Group: &group,
									Kind:  "HTTPRoute",
								}},
							},
						}},
					},
				},
			},
			valid:         false,
			validationMsg: "HTTPRoute spec did not pass validation: rules[0].backendRefs[0]: filters in backendRef is unsupported",
		},
		{
			msg: "invalid protocols",
			route: &gwtypes.HTTPRoute{
				Namespace: corev1.NamespaceDefault,
				Name:      "testing-httproute",
				Annotations: map[string]string{
					annotations.AnnotationPrefix + annotations.ProtocolsKey: "ohno",
				},
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{{
							Name: "testing-gateway",
						}},
					},
					Rules: []gwtypes.HTTPRouteRule{},
				},
			},
			cachedObjects: []client.Object{
				gatewayClass,
				&gwtypes.Gateway{
					Namespace: corev1.NamespaceDefault,
					Name:      "testing-gateway",
					Spec: gwtypes.GatewaySpec{
						GatewayClassName: gatewayClassName,
						Listeners: []gwtypes.Listener{{
							Name:     "http",
							Port:     80,
							Protocol: (gwtypes.HTTPProtocolType),
							AllowedRoutes: &gwtypes.AllowedRoutes{
								Kinds: []gwtypes.RouteGroupKind{{
									Group: &group,
									Kind:  "HTTPRoute",
								}},
							},
						}},
					},
				},
			},
			valid:         false,
			validationMsg: "HTTPRoute has invalid Kong annotations: invalid konghq.com/protocols value: ohno",
		},
		{
			msg: "HTTPRoute URLRewrite ReplaceFullPath",
			route: &gwtypes.HTTPRoute{
				Namespace: corev1.NamespaceDefault,
				Name:      "testing-httproute",
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{{
							Name: "testing-gateway",
						}},
					},
					Rules: []gwtypes.HTTPRouteRule{
						{
							Filters: []gwtypes.HTTPRouteFilter{
								{
									Type: gwtypes.HTTPRouteFilterURLRewrite,
									URLRewrite: &gwtypes.HTTPURLRewriteFilter{
										Path: &gwtypes.HTTPPathModifier{
											Type:            gwtypes.FullPathHTTPPathModifier,
											ReplaceFullPath: new("/new-path"),
										},
									},
								},
							},
						},
					},
				},
			},
			cachedObjects: []client.Object{
				gatewayClass,
				&gwtypes.Gateway{
					Namespace: corev1.NamespaceDefault,
					Name:      "testing-gateway",
					Spec: gwtypes.GatewaySpec{
						GatewayClassName: gatewayClassName,
						Listeners: []gwtypes.Listener{{
							Name:     "http",
							Port:     80,
							Protocol: (gwtypes.HTTPProtocolType),
							AllowedRoutes: &gwtypes.AllowedRoutes{
								Kinds: []gwtypes.RouteGroupKind{{
									Group: &group,
									Kind:  "HTTPRoute",
								}},
							},
						}},
					},
				},
			},
			valid: true,
		},
		{
			msg: "HTTPRoute URLRewrite Hostname",
			route: &gwtypes.HTTPRoute{
				Namespace: corev1.NamespaceDefault,
				Name:      "testing-httproute",
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{{
							Name: "testing-gateway",
						}},
					},
					Rules: []gwtypes.HTTPRouteRule{
						{
							Filters: []gwtypes.HTTPRouteFilter{
								{
									Type: gwtypes.HTTPRouteFilterURLRewrite,
									URLRewrite: &gwtypes.HTTPURLRewriteFilter{
										Hostname: new(gwtypes.PreciseHostname("example.com")),
									},
								},
							},
						},
					},
				},
			},
			cachedObjects: []client.Object{
				gatewayClass,
				&gwtypes.Gateway{
					Namespace: corev1.NamespaceDefault,
					Name:      "testing-gateway",
					Spec: gwtypes.GatewaySpec{
						GatewayClassName: gatewayClassName,
						Listeners: []gwtypes.Listener{{
							Name:     "http",
							Port:     80,
							Protocol: (gwtypes.HTTPProtocolType),
							AllowedRoutes: &gwtypes.AllowedRoutes{
								Kinds: []gwtypes.RouteGroupKind{{
									Group: &group,
									Kind:  "HTTPRoute",
								}},
							},
						}},
					},
				},
			},
			valid: true,
		},
		{
			msg: "HTTPRoute URLRewrite ReplacePrefixMatch",
			route: &gwtypes.HTTPRoute{
				Namespace: corev1.NamespaceDefault,
				Name:      "testing-httproute",
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{{
							Name: "testing-gateway",
						}},
					},
					Rules: []gwtypes.HTTPRouteRule{
						{
							Filters: []gwtypes.HTTPRouteFilter{
								{
									Type: gwtypes.HTTPRouteFilterURLRewrite,
									URLRewrite: &gwtypes.HTTPURLRewriteFilter{
										Path: &gwtypes.HTTPPathModifier{
											Type:               gwtypes.PrefixMatchHTTPPathModifier,
											ReplacePrefixMatch: new("/new"),
										},
									},
								},
							},
						},
					},
				},
			},
			cachedObjects: []client.Object{
				gatewayClass,
				&gwtypes.Gateway{
					Namespace: corev1.NamespaceDefault,
					Name:      "testing-gateway",
					Spec: gwtypes.GatewaySpec{
						GatewayClassName: gatewayClassName,
						Listeners: []gwtypes.Listener{{
							Name:     "http",
							Port:     80,
							Protocol: (gwtypes.HTTPProtocolType),
							AllowedRoutes: &gwtypes.AllowedRoutes{
								Kinds: []gwtypes.RouteGroupKind{{
									Group: &group,
									Kind:  "HTTPRoute",
								}},
							},
						}},
					},
				},
			},
			valid: true,
		},
		{
			msg: "HTTPRoute with unsupported filter when reference both existing and non-existing gateway should be rejected",
			cachedObjects: []client.Object{
				gatewayClass,
				&gwtypes.Gateway{
					Namespace: corev1.NamespaceDefault,
					Name:      "existing-managed-gateway",
					Spec:      gwtypes.GatewaySpec{GatewayClassName: gatewayClassName},
				},
			},
			route: &gwtypes.HTTPRoute{
				Namespace: corev1.NamespaceDefault,
				Name:      "example-route",
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{
							{
								Name:      "non-existent-gateway",
								Namespace: new(gwtypes.Namespace(corev1.NamespaceDefault)),
							},
							{
								Name:      "existing-managed-gateway",
								Namespace: new(gwtypes.Namespace(corev1.NamespaceDefault)),
							},
						},
					},
					Rules: []gwtypes.HTTPRouteRule{{
						Filters: []gwtypes.HTTPRouteFilter{{
							// RequestMirror is explicitly unsupported — should be rejected.
							Type: gwtypes.HTTPRouteFilterRequestMirror,
						}},
					}},
				},
			},
			valid:         false,
			validationMsg: "HTTPRoute spec did not pass validation: rules[0].filters[0]: filter type RequestMirror is unsupported",
		},
		{
			msg: "unsupported retry fields are left to Route status",
			cachedObjects: []client.Object{
				gatewayClass,
				&gwtypes.Gateway{
					Namespace: corev1.NamespaceDefault,
					Name:      "existing-managed-gateway",
					Spec:      gwtypes.GatewaySpec{GatewayClassName: gatewayClassName},
				},
			},
			route: &gwtypes.HTTPRoute{
				Namespace: corev1.NamespaceDefault,
				Name:      "example-route",
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{{
							Name:      "existing-managed-gateway",
							Namespace: new(gwtypes.Namespace(corev1.NamespaceDefault)),
						}},
					},
					Rules: []gwtypes.HTTPRouteRule{{
						Retry: &gwtypes.HTTPRouteRetry{
							Codes: []gwtypes.HTTPRouteRetryStatusCode{500},
						},
					}},
				},
			},
			valid: true,
		},
	} {
		t.Run(tt.msg, func(t *testing.T) {
			fakeClient := fakeclient.
				NewClientBuilder().
				WithScheme(scheme.Get()).
				WithObjects(tt.cachedObjects...).
				Build()

			// Passed routesValidator is irrelevant for the above test cases.
			valid, validMsg, err := ValidateHTTPRoute(
				t.Context(), mockRoutesValidator{}, kongVersionSupportWildcardSNI, translator.FeatureFlags{}, tt.route, fakeClient,
			)
			assert.Equal(t, tt.valid, valid, tt.msg)
			assert.Equal(t, tt.validationMsg, validMsg, tt.msg)
			assert.Equal(t, tt.err, err, tt.msg)
		})
	}
}

type mockRoutesValidator struct{}

func (mockRoutesValidator) Validate(_ context.Context, _ *kong.Route) (bool, string, error) {
	return true, "", nil
}

func TestValidateHTTPRouteRegexes(t *testing.T) {
	testCases := []struct {
		name          string
		route         *gwtypes.HTTPRoute
		expectError   bool
		errorContains string
	}{
		{
			name: "valid path regex",
			route: &gwtypes.HTTPRoute{
				Name:      "test-route",
				Namespace: corev1.NamespaceDefault,
				Spec: gwtypes.HTTPRouteSpec{
					Rules: []gwtypes.HTTPRouteRule{{
						Matches: []gwtypes.HTTPRouteMatch{{
							Path: &gwtypes.HTTPPathMatch{
								Type:  new(gwtypes.PathMatchRegularExpression),
								Value: new("/path[1-8]"),
							},
						}},
					}},
				},
			},
			expectError: false,
		},
		{
			name: "valid header regex",
			route: &gwtypes.HTTPRoute{
				Name:      "test-route",
				Namespace: corev1.NamespaceDefault,
				Spec: gwtypes.HTTPRouteSpec{
					Rules: []gwtypes.HTTPRouteRule{{
						Matches: []gwtypes.HTTPRouteMatch{{
							Headers: []gwtypes.HTTPHeaderMatch{{
								Name:  "foo",
								Value: "bar[1-8]",
								Type:  new(gwtypes.HeaderMatchRegularExpression),
							}},
						}},
					}},
				},
			},
			expectError: false,
		},
		{
			name: "invalid path regex with unclosed bracket",
			route: &gwtypes.HTTPRoute{
				Name:      "test-route",
				Namespace: corev1.NamespaceDefault,
				Spec: gwtypes.HTTPRouteSpec{
					Rules: []gwtypes.HTTPRouteRule{{
						Matches: []gwtypes.HTTPRouteMatch{{
							Path: &gwtypes.HTTPPathMatch{
								Type:  new(gwtypes.PathMatchRegularExpression),
								Value: new("/foo[[[["),
							},
						}},
					}},
				},
			},
			expectError:   true,
			errorContains: "invalid regex",
		},
		{
			name: "invalid header regex with unclosed bracket",
			route: &gwtypes.HTTPRoute{
				Name:      "test-route",
				Namespace: corev1.NamespaceDefault,
				Spec: gwtypes.HTTPRouteSpec{
					Rules: []gwtypes.HTTPRouteRule{{
						Matches: []gwtypes.HTTPRouteMatch{{
							Headers: []gwtypes.HTTPHeaderMatch{{
								Name:  "foo",
								Value: "[[[invalid-regex[[[[",
								Type:  new(gwtypes.HeaderMatchRegularExpression),
							}},
						}},
					}},
				},
			},
			expectError:   true,
			errorContains: "invalid regex",
		},
		{
			name: "exact path match type is not validated as regex",
			route: &gwtypes.HTTPRoute{
				Name:      "test-route",
				Namespace: corev1.NamespaceDefault,
				Spec: gwtypes.HTTPRouteSpec{
					Rules: []gwtypes.HTTPRouteRule{{
						Matches: []gwtypes.HTTPRouteMatch{{
							Path: &gwtypes.HTTPPathMatch{
								Type:  new(gwtypes.PathMatchExact),
								Value: new("/foo[[[["),
							},
						}},
					}},
				},
			},
			expectError: false,
		},
		{
			name: "exact header match type is not validated as regex",
			route: &gwtypes.HTTPRoute{
				Name:      "test-route",
				Namespace: corev1.NamespaceDefault,
				Spec: gwtypes.HTTPRouteSpec{
					Rules: []gwtypes.HTTPRouteRule{{
						Matches: []gwtypes.HTTPRouteMatch{{
							Headers: []gwtypes.HTTPHeaderMatch{{
								Name:  "foo",
								Value: "[[[invalid-regex[[[[",
								Type:  new(gwtypes.HeaderMatchExact),
							}},
						}},
					}},
				},
			},
			expectError: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateHTTPRouteRegexes(tc.route)
			if tc.expectError {
				assert.Error(t, err)
				if tc.errorContains != "" {
					assert.Contains(t, err.Error(), tc.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
