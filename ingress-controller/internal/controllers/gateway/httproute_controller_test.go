package gateway

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/go-logr/logr"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/util"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/pkg/clientset/scheme"
	"github.com/kong/kong-operator/v2/pkg/metadata"
	referencegranthelpers "github.com/kong/kong-operator/v2/test/helpers/referencegrant"
)

func init() {
	if err := corev1.AddToScheme(scheme.Scheme); err != nil {
		fmt.Println("error while adding corev1 scheme")
		os.Exit(1)
	}
	if err := gatewayv1.Install(scheme.Scheme); err != nil {
		fmt.Println("error while adding gatewayv1 scheme")
		os.Exit(1)
	}
	if err := gatewayv1beta1.Install(scheme.Scheme); err != nil {
		fmt.Println("error while adding gatewayv1beta1 scheme")
		os.Exit(1)
	}
	if err := configurationv1.AddToScheme(scheme.Scheme); err != nil {
		fmt.Println("error while adding configurationv1 scheme")
		os.Exit(1)
	}
}

func TestEnsureNoStaleParentStatus(t *testing.T) {
	testCases := []struct {
		name                     string
		httproute                *gwtypes.HTTPRoute
		expectedAnyStatusRemoved bool
		expectedStatusesForRefs  []gwtypes.ParentReference
	}{
		{
			name: "no stale status",
			httproute: &gwtypes.HTTPRoute{
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{
							{Name: "defined-in-spec"},
						},
					},
				},
			},
			expectedAnyStatusRemoved: false,
			expectedStatusesForRefs:  nil, // There was no status for `defined-in-spec` created yet.
		},
		{
			name: "no stale status with existing status for spec parent ref",
			httproute: &gwtypes.HTTPRoute{
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{
							{Name: "defined-in-spec"},
						},
					},
				},
				Status: gwtypes.HTTPRouteStatus{
					RouteStatus: gwtypes.RouteStatus{
						Parents: []gwtypes.RouteParentStatus{
							{
								ControllerName: GetControllerName(),
								ParentRef:      gwtypes.ParentReference{Name: "defined-in-spec"},
							},
						},
					},
				},
			},
			expectedStatusesForRefs: []gwtypes.ParentReference{
				{Name: "defined-in-spec"},
			},
			expectedAnyStatusRemoved: false,
		},
		{
			name: "stale status",
			httproute: &gwtypes.HTTPRoute{
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{
							{Name: "defined-in-spec"},
						},
					},
				},
				Status: gwtypes.HTTPRouteStatus{
					RouteStatus: gwtypes.RouteStatus{
						Parents: []gwtypes.RouteParentStatus{
							{
								ControllerName: GetControllerName(),
								ParentRef:      gwtypes.ParentReference{Name: "not-defined-in-spec"},
							},
						},
					},
				},
			},
			expectedStatusesForRefs:  nil, // There was no status for `defined-in-spec` created yet.
			expectedAnyStatusRemoved: true,
		},
		{
			name: "stale status with existing status for spec parent ref",
			httproute: &gwtypes.HTTPRoute{
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{
							{Name: "defined-in-spec"},
						},
					},
				},
				Status: gwtypes.HTTPRouteStatus{
					RouteStatus: gwtypes.RouteStatus{
						Parents: []gwtypes.RouteParentStatus{
							{
								ControllerName: GetControllerName(),
								ParentRef:      gwtypes.ParentReference{Name: "not-defined-in-spec"},
							},
							{
								ControllerName: GetControllerName(),
								ParentRef:      gwtypes.ParentReference{Name: "defined-in-spec"},
							},
						},
					},
				},
			},
			expectedStatusesForRefs: []gwtypes.ParentReference{
				{Name: "defined-in-spec"},
			},
			expectedAnyStatusRemoved: true,
		},
		{
			name: "do not remove status for other controllers",
			httproute: &gwtypes.HTTPRoute{
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{
							{Name: "defined-in-spec"},
						},
					},
				},
				Status: gwtypes.HTTPRouteStatus{
					RouteStatus: gwtypes.RouteStatus{
						Parents: []gwtypes.RouteParentStatus{
							{
								ControllerName: gwtypes.GatewayController("another-controller"),
								ParentRef:      gwtypes.ParentReference{Name: "not-defined-in-spec"},
							},
							{
								ControllerName: GetControllerName(),
								ParentRef:      gwtypes.ParentReference{Name: "defined-in-spec"},
							},
						},
					},
				},
			},
			expectedStatusesForRefs: []gwtypes.ParentReference{
				{Name: "not-defined-in-spec"},
				{Name: "defined-in-spec"},
			},
			expectedAnyStatusRemoved: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			wasAnyStatusRemoved := ensureNoStaleParentStatus(tc.httproute)
			assert.Equal(t, tc.expectedAnyStatusRemoved, wasAnyStatusRemoved)

			actualStatuses := lo.Map(tc.httproute.Status.Parents, func(status gwtypes.RouteParentStatus, _ int) string {
				return parentReferenceKey(tc.httproute.Namespace, status.ParentRef)
			})
			expectedStatuses := lo.Map(tc.expectedStatusesForRefs, func(ref gwtypes.ParentReference, _ int) string {
				return parentReferenceKey(tc.httproute.Namespace, ref)
			})
			assert.ElementsMatch(t, expectedStatuses, actualStatuses)
		})
	}
}

func TestParentReferenceKey(t *testing.T) {
	const routeNamespace = "route-ns"
	testCases := []struct {
		name     string
		ref      gwtypes.ParentReference
		expected string
	}{
		{
			name: "only name",
			ref: gwtypes.ParentReference{
				Name: "foo",
			},
			expected: "route-ns/foo//",
		},
		{
			name: "name with namespace",
			ref: gwtypes.ParentReference{
				Name:      "foo",
				Namespace: new(gwtypes.Namespace("bar")),
			},
			expected: "bar/foo//",
		},
		{
			name: "name with port number",
			ref: gwtypes.ParentReference{
				Name: "foo",
				Port: new(gwtypes.PortNumber(1234)),
			},
			expected: "route-ns/foo//1234",
		},
		{
			name: "name with section name",
			ref: gwtypes.ParentReference{
				Name:        "foo",
				SectionName: new(gwtypes.SectionName("section")),
			},
			expected: "route-ns/foo/section/",
		},
		{
			name: "all fields",
			ref: gwtypes.ParentReference{
				Name:        "foo",
				Namespace:   new(gwtypes.Namespace("bar")),
				Port:        new(gwtypes.PortNumber(1234)),
				SectionName: new(gwtypes.SectionName("section")),
			},
			expected: "bar/foo/section/1234",
		},
		{
			name: "group and kind are ignored",
			ref: gwtypes.ParentReference{
				Name:  "foo",
				Group: new(gwtypes.Group("group")),
				Kind:  new(gwtypes.Kind("kind")),
			},
			expected: "route-ns/foo//",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual := parentReferenceKey(routeNamespace, tc.ref)
			assert.Equal(t, tc.expected, actual)
		})
	}
}

func TestHTTPRouteRuleReasonPluginReferences(t *testing.T) {
	ctx := context.Background()
	logger := logr.Discard()

	baseRoute := gwtypes.HTTPRoute{
		Name:      "route",
		Namespace: "default",
		Spec: gwtypes.HTTPRouteSpec{
			Rules: []gwtypes.HTTPRouteRule{{
				BackendRefs: []gwtypes.HTTPBackendRef{{
					Name:  "svc",
					Kind:  util.StringToGatewayAPIKindPtr("Service"),
					Group: util.StringToTypedPtr[*gwtypes.Group](""),
				}},
			}},
		},
	}

	kongPlugin := configurationv1.KongPlugin{
		Name:       "rate-limit",
		Namespace:  "default",
		PluginName: "rate-limiting",
	}

	tests := []struct {
		name               string
		objects            []client.Object
		route              gwtypes.HTTPRoute
		wantReason         gwtypes.RouteConditionReason
		wantMessageContain string
	}{
		{
			name: "extensionRef resolves",
			objects: []client.Object{
				&corev1.Service{Name: "svc", Namespace: "default"},
				&kongPlugin,
			},
			route: func() gwtypes.HTTPRoute {
				r := baseRoute.DeepCopy()
				r.Spec.Rules[0].Filters = []gwtypes.HTTPRouteFilter{{
					Type: gwtypes.HTTPRouteFilterExtensionRef,
					ExtensionRef: &gwtypes.LocalObjectReference{
						Group: "configuration.konghq.com",
						Kind:  "KongPlugin",
						Name:  "rate-limit",
					},
				}}
				return *r
			}(),
			wantReason: gwtypes.RouteReasonResolvedRefs,
		},
		{
			name: "extensionRef missing",
			objects: []client.Object{
				&corev1.Service{Name: "svc", Namespace: "default"},
			},
			route: func() gwtypes.HTTPRoute {
				r := baseRoute.DeepCopy()
				r.Spec.Rules[0].Filters = []gwtypes.HTTPRouteFilter{{
					Type: gwtypes.HTTPRouteFilterExtensionRef,
					ExtensionRef: &gwtypes.LocalObjectReference{
						Group: "configuration.konghq.com",
						Kind:  "KongPlugin",
						Name:  "missing-plugin",
					},
				}}
				return *r
			}(),
			wantReason:         gwtypes.RouteReasonBackendNotFound,
			wantMessageContain: "extensionRef default/missing-plugin does not exist",
		},
		{
			name: "extensionRef invalid kind",
			objects: []client.Object{
				&corev1.Service{Name: "svc", Namespace: "default"},
			},
			route: func() gwtypes.HTTPRoute {
				r := baseRoute.DeepCopy()
				r.Spec.Rules[0].Filters = []gwtypes.HTTPRouteFilter{{
					Type: gwtypes.HTTPRouteFilterExtensionRef,
					ExtensionRef: &gwtypes.LocalObjectReference{
						Group: "configuration.konghq.com",
						Kind:  "KongClusterPlugin",
						Name:  "rate-limit",
					},
				}}
				return *r
			}(),
			wantReason:         gwtypes.RouteReasonInvalidKind,
			wantMessageContain: "unsupported type configuration.konghq.com/KongClusterPlugin",
		},
		{
			name: "annotation plugin missing",
			objects: []client.Object{
				&corev1.Service{Name: "svc", Namespace: "default"},
			},
			route: func() gwtypes.HTTPRoute {
				r := baseRoute.DeepCopy()
				r.Annotations = map[string]string{
					metadata.AnnotationKeyPlugins: "missing-plugin",
				}
				return *r
			}(),
			wantReason:         gwtypes.RouteReasonBackendNotFound,
			wantMessageContain: "referenced KongPlugin default/missing-plugin does not exist",
		},
		{
			name: "annotation plugin cross-namespace without grant",
			objects: []client.Object{
				&corev1.Service{Name: "svc", Namespace: "default"},
				&configurationv1.KongPlugin{
					Name: "rate-limit", Namespace: "plugins",
					PluginName: "rate-limiting",
				},
			},
			route: func() gwtypes.HTTPRoute {
				r := baseRoute.DeepCopy()
				r.Annotations = map[string]string{
					metadata.AnnotationKeyPlugins: "plugins:rate-limit",
				}
				return *r
			}(),
			wantReason:         gwtypes.RouteReasonRefNotPermitted,
			wantMessageContain: "and no ReferenceGrant allowing reference is configured",
		},
		{
			name: "annotation plugin cross-namespace with grant",
			objects: []client.Object{
				&corev1.Service{Name: "svc", Namespace: "default"},
				&configurationv1.KongPlugin{
					Name: "rate-limit", Namespace: "plugins",
					PluginName: "rate-limiting",
				},
				&gwtypes.ReferenceGrant{
					Name: "grant", Namespace: "plugins",
					Spec: gwtypes.ReferenceGrantSpec{
						From: []gwtypes.ReferenceGrantFrom{{
							Group:     gwtypes.V1Group,
							Kind:      "HTTPRoute",
							Namespace: "default",
						}},
						To: []gwtypes.ReferenceGrantTo{{
							Group: "configuration.konghq.com",
							Kind:  "KongPlugin",
						}},
					},
				},
			},
			route: func() gwtypes.HTTPRoute {
				r := baseRoute.DeepCopy()
				r.Annotations = map[string]string{
					metadata.AnnotationKeyPlugins: "plugins:rate-limit",
				}
				return *r
			}(),
			wantReason: gwtypes.RouteReasonResolvedRefs,
		},
	}

	for _, gv := range referencegranthelpers.Versions() {
		t.Run(gv.Version, func(t *testing.T) {
			for _, tc := range tests {
				t.Run(tc.name, func(t *testing.T) {
					cl := fakeclient.NewClientBuilder().
						WithScheme(scheme.Scheme).
						WithObjects(referencegranthelpers.AsVersion(gv, tc.objects)...).
						Build()
					reconciler := &HTTPRouteReconciler{
						Client:                cl,
						Log:                   logger,
						ReferenceGrantVersion: gv,
					}

					reason, msg, err := reconciler.getHTTPRouteRuleReason(ctx, tc.route)
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
