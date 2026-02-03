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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	configurationv1 "github.com/kong/kong-operator/api/configuration/v1"
	"github.com/kong/kong-operator/ingress-controller/internal/gatewayapi"
	"github.com/kong/kong-operator/ingress-controller/internal/util"
	"github.com/kong/kong-operator/pkg/clientset/scheme"
	"github.com/kong/kong-operator/pkg/metadata"
)

func init() {
	if err := corev1.AddToScheme(scheme.Scheme); err != nil {
		fmt.Println("error while adding corev1 scheme")
		os.Exit(1)
	}
	if err := gatewayapi.InstallV1(scheme.Scheme); err != nil {
		fmt.Println("error while adding gatewayv1 scheme")
		os.Exit(1)
	}
	if err := gatewayapi.InstallV1beta1(scheme.Scheme); err != nil {
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
		httproute                *gatewayapi.HTTPRoute
		expectedAnyStatusRemoved bool
		expectedStatusesForRefs  []gatewayapi.ParentReference
	}{
		{
			name: "no stale status",
			httproute: &gatewayapi.HTTPRoute{
				Spec: gatewayapi.HTTPRouteSpec{
					CommonRouteSpec: gatewayapi.CommonRouteSpec{
						ParentRefs: []gatewayapi.ParentReference{
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
			httproute: &gatewayapi.HTTPRoute{
				Spec: gatewayapi.HTTPRouteSpec{
					CommonRouteSpec: gatewayapi.CommonRouteSpec{
						ParentRefs: []gatewayapi.ParentReference{
							{Name: "defined-in-spec"},
						},
					},
				},
				Status: gatewayapi.HTTPRouteStatus{
					RouteStatus: gatewayapi.RouteStatus{
						Parents: []gatewayapi.RouteParentStatus{
							{
								ControllerName: GetControllerName(),
								ParentRef:      gatewayapi.ParentReference{Name: "defined-in-spec"},
							},
						},
					},
				},
			},
			expectedStatusesForRefs: []gatewayapi.ParentReference{
				{Name: "defined-in-spec"},
			},
			expectedAnyStatusRemoved: false,
		},
		{
			name: "stale status",
			httproute: &gatewayapi.HTTPRoute{
				Spec: gatewayapi.HTTPRouteSpec{
					CommonRouteSpec: gatewayapi.CommonRouteSpec{
						ParentRefs: []gatewayapi.ParentReference{
							{Name: "defined-in-spec"},
						},
					},
				},
				Status: gatewayapi.HTTPRouteStatus{
					RouteStatus: gatewayapi.RouteStatus{
						Parents: []gatewayapi.RouteParentStatus{
							{
								ControllerName: GetControllerName(),
								ParentRef:      gatewayapi.ParentReference{Name: "not-defined-in-spec"},
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
			httproute: &gatewayapi.HTTPRoute{
				Spec: gatewayapi.HTTPRouteSpec{
					CommonRouteSpec: gatewayapi.CommonRouteSpec{
						ParentRefs: []gatewayapi.ParentReference{
							{Name: "defined-in-spec"},
						},
					},
				},
				Status: gatewayapi.HTTPRouteStatus{
					RouteStatus: gatewayapi.RouteStatus{
						Parents: []gatewayapi.RouteParentStatus{
							{
								ControllerName: GetControllerName(),
								ParentRef:      gatewayapi.ParentReference{Name: "not-defined-in-spec"},
							},
							{
								ControllerName: GetControllerName(),
								ParentRef:      gatewayapi.ParentReference{Name: "defined-in-spec"},
							},
						},
					},
				},
			},
			expectedStatusesForRefs: []gatewayapi.ParentReference{
				{Name: "defined-in-spec"},
			},
			expectedAnyStatusRemoved: true,
		},
		{
			name: "do not remove status for other controllers",
			httproute: &gatewayapi.HTTPRoute{
				Spec: gatewayapi.HTTPRouteSpec{
					CommonRouteSpec: gatewayapi.CommonRouteSpec{
						ParentRefs: []gatewayapi.ParentReference{
							{Name: "defined-in-spec"},
						},
					},
				},
				Status: gatewayapi.HTTPRouteStatus{
					RouteStatus: gatewayapi.RouteStatus{
						Parents: []gatewayapi.RouteParentStatus{
							{
								ControllerName: gatewayapi.GatewayController("another-controller"),
								ParentRef:      gatewayapi.ParentReference{Name: "not-defined-in-spec"},
							},
							{
								ControllerName: GetControllerName(),
								ParentRef:      gatewayapi.ParentReference{Name: "defined-in-spec"},
							},
						},
					},
				},
			},
			expectedStatusesForRefs: []gatewayapi.ParentReference{
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

			actualStatuses := lo.Map(tc.httproute.Status.Parents, func(status gatewayapi.RouteParentStatus, _ int) string {
				return parentReferenceKey(tc.httproute.Namespace, status.ParentRef)
			})
			expectedStatuses := lo.Map(tc.expectedStatusesForRefs, func(ref gatewayapi.ParentReference, _ int) string {
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
		ref      gatewayapi.ParentReference
		expected string
	}{
		{
			name: "only name",
			ref: gatewayapi.ParentReference{
				Name: "foo",
			},
			expected: "route-ns/foo//",
		},
		{
			name: "name with namespace",
			ref: gatewayapi.ParentReference{
				Name:      "foo",
				Namespace: lo.ToPtr(gatewayapi.Namespace("bar")),
			},
			expected: "bar/foo//",
		},
		{
			name: "name with port number",
			ref: gatewayapi.ParentReference{
				Name: "foo",
				Port: lo.ToPtr(gatewayapi.PortNumber(1234)),
			},
			expected: "route-ns/foo//1234",
		},
		{
			name: "name with section name",
			ref: gatewayapi.ParentReference{
				Name:        "foo",
				SectionName: lo.ToPtr(gatewayapi.SectionName("section")),
			},
			expected: "route-ns/foo/section/",
		},
		{
			name: "all fields",
			ref: gatewayapi.ParentReference{
				Name:        "foo",
				Namespace:   lo.ToPtr(gatewayapi.Namespace("bar")),
				Port:        lo.ToPtr(gatewayapi.PortNumber(1234)),
				SectionName: lo.ToPtr(gatewayapi.SectionName("section")),
			},
			expected: "bar/foo/section/1234",
		},
		{
			name: "group and kind are ignored",
			ref: gatewayapi.ParentReference{
				Name:  "foo",
				Group: lo.ToPtr(gatewayapi.Group("group")),
				Kind:  lo.ToPtr(gatewayapi.Kind("kind")),
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

	baseRoute := gatewayapi.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "route",
			Namespace: "default",
		},
		Spec: gatewayapi.HTTPRouteSpec{
			Rules: []gatewayapi.HTTPRouteRule{{
				BackendRefs: []gatewayapi.HTTPBackendRef{{
					BackendRef: gatewayapi.BackendRef{
						BackendObjectReference: gatewayapi.BackendObjectReference{
							Name:  "svc",
							Kind:  util.StringToGatewayAPIKindPtr("Service"),
							Group: util.StringToTypedPtr[*gatewayapi.Group](""),
						},
					},
				}},
			}},
		},
	}

	kongPlugin := configurationv1.KongPlugin{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rate-limit",
			Namespace: "default",
		},
		PluginName: "rate-limiting",
	}

	tests := []struct {
		name               string
		enableRefGrant     bool
		objects            []client.Object
		route              gatewayapi.HTTPRoute
		wantReason         gatewayapi.RouteConditionReason
		wantMessageContain string
	}{
		{
			name:           "extensionRef resolves",
			enableRefGrant: true,
			objects: []client.Object{
				&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "default"}},
				&kongPlugin,
			},
			route: func() gatewayapi.HTTPRoute {
				r := baseRoute.DeepCopy()
				r.Spec.Rules[0].Filters = []gatewayapi.HTTPRouteFilter{{
					Type: gatewayapi.HTTPRouteFilterExtensionRef,
					ExtensionRef: &gatewayapi.LocalObjectReference{
						Group: "configuration.konghq.com",
						Kind:  "KongPlugin",
						Name:  "rate-limit",
					},
				}}
				return *r
			}(),
			wantReason: gatewayapi.RouteReasonResolvedRefs,
		},
		{
			name:           "extensionRef missing",
			enableRefGrant: true,
			objects: []client.Object{
				&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "default"}},
			},
			route: func() gatewayapi.HTTPRoute {
				r := baseRoute.DeepCopy()
				r.Spec.Rules[0].Filters = []gatewayapi.HTTPRouteFilter{{
					Type: gatewayapi.HTTPRouteFilterExtensionRef,
					ExtensionRef: &gatewayapi.LocalObjectReference{
						Group: "configuration.konghq.com",
						Kind:  "KongPlugin",
						Name:  "missing-plugin",
					},
				}}
				return *r
			}(),
			wantReason:         gatewayapi.RouteReasonBackendNotFound,
			wantMessageContain: "extensionRef default/missing-plugin does not exist",
		},
		{
			name:           "extensionRef invalid kind",
			enableRefGrant: true,
			objects: []client.Object{
				&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "default"}},
			},
			route: func() gatewayapi.HTTPRoute {
				r := baseRoute.DeepCopy()
				r.Spec.Rules[0].Filters = []gatewayapi.HTTPRouteFilter{{
					Type: gatewayapi.HTTPRouteFilterExtensionRef,
					ExtensionRef: &gatewayapi.LocalObjectReference{
						Group: "configuration.konghq.com",
						Kind:  "KongClusterPlugin",
						Name:  "rate-limit",
					},
				}}
				return *r
			}(),
			wantReason:         gatewayapi.RouteReasonInvalidKind,
			wantMessageContain: "unsupported type configuration.konghq.com/KongClusterPlugin",
		},
		{
			name:           "annotation plugin missing",
			enableRefGrant: true,
			objects: []client.Object{
				&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "default"}},
			},
			route: func() gatewayapi.HTTPRoute {
				r := baseRoute.DeepCopy()
				r.Annotations = map[string]string{
					metadata.AnnotationKeyPlugins: "missing-plugin",
				}
				return *r
			}(),
			wantReason:         gatewayapi.RouteReasonBackendNotFound,
			wantMessageContain: "referenced KongPlugin default/missing-plugin does not exist",
		},
		{
			name:           "annotation plugin cross-namespace without grant",
			enableRefGrant: true,
			objects: []client.Object{
				&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "default"}},
				&configurationv1.KongPlugin{
					ObjectMeta: metav1.ObjectMeta{Name: "rate-limit", Namespace: "plugins"},
					PluginName: "rate-limiting",
				},
			},
			route: func() gatewayapi.HTTPRoute {
				r := baseRoute.DeepCopy()
				r.Annotations = map[string]string{
					metadata.AnnotationKeyPlugins: "plugins:rate-limit",
				}
				return *r
			}(),
			wantReason:         gatewayapi.RouteReasonRefNotPermitted,
			wantMessageContain: "and no ReferenceGrant allowing reference is configured",
		},
		{
			name:           "annotation plugin cross-namespace with grant",
			enableRefGrant: true,
			objects: []client.Object{
				&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "default"}},
				&configurationv1.KongPlugin{
					ObjectMeta: metav1.ObjectMeta{Name: "rate-limit", Namespace: "plugins"},
					PluginName: "rate-limiting",
				},
				&gatewayapi.ReferenceGrant{
					ObjectMeta: metav1.ObjectMeta{Name: "grant", Namespace: "plugins"},
					Spec: gatewayapi.ReferenceGrantSpec{
						From: []gatewayapi.ReferenceGrantFrom{{
							Group:     gatewayapi.V1Group,
							Kind:      "HTTPRoute",
							Namespace: "default",
						}},
						To: []gatewayapi.ReferenceGrantTo{{
							Group: "configuration.konghq.com",
							Kind:  "KongPlugin",
						}},
					},
				},
			},
			route: func() gatewayapi.HTTPRoute {
				r := baseRoute.DeepCopy()
				r.Annotations = map[string]string{
					metadata.AnnotationKeyPlugins: "plugins:rate-limit",
				}
				return *r
			}(),
			wantReason: gatewayapi.RouteReasonResolvedRefs,
		},
		{
			name:           "annotation plugin cross-namespace without ReferenceGrant CRD",
			enableRefGrant: false,
			objects: []client.Object{
				&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "default"}},
				&configurationv1.KongPlugin{
					ObjectMeta: metav1.ObjectMeta{Name: "rate-limit", Namespace: "plugins"},
					PluginName: "rate-limiting",
				},
			},
			route: func() gatewayapi.HTTPRoute {
				r := baseRoute.DeepCopy()
				r.Annotations = map[string]string{
					metadata.AnnotationKeyPlugins: "plugins:rate-limit",
				}
				return *r
			}(),
			wantReason:         gatewayapi.RouteReasonRefNotPermitted,
			wantMessageContain: "install ReferenceGrant CRD and configure a proper grant",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cl := fakeclient.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(tc.objects...).
				Build()
			reconciler := &HTTPRouteReconciler{
				Client:               cl,
				Log:                  logger,
				enableReferenceGrant: tc.enableRefGrant,
			}

			reason, msg, err := reconciler.getHTTPRouteRuleReason(ctx, tc.route)
			require.NoError(t, err)
			assert.Equal(t, tc.wantReason, reason)
			if tc.wantMessageContain != "" {
				assert.Contains(t, msg, tc.wantMessageContain)
			}
		})
	}
}
