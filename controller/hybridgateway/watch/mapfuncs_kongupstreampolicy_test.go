package watch

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kong-operator/v2/api/configuration/v1beta1"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	index "github.com/kong/kong-operator/v2/internal/utils/index"
)

func TestMapHTTPRouteForKongUpstreamPolicy(t *testing.T) {
	ctx := context.Background()

	httpRouteRuleForService := func(svcName string) []gwtypes.HTTPRouteRule {
		return []gwtypes.HTTPRouteRule{
			{
				BackendRefs: []gwtypes.HTTPBackendRef{
					{
						BackendRef: gwtypes.BackendRef{
							BackendObjectReference: gwtypes.BackendObjectReference{
								Name: gwtypes.ObjectName(svcName),
							},
						},
					},
				},
			},
		}
	}

	tests := []struct {
		name      string
		setup     []client.Object
		input     client.Object
		wantNil   bool
		wantLen   int
		wantNames []string
	}{
		{
			name:    "nil object returns nil",
			input:   nil,
			wantNil: true,
		},
		{
			name:    "wrong type returns nil",
			input:   &corev1.Service{},
			wantNil: true,
		},
		{
			name: "policy with no referencing services returns nil",
			input: &configurationv1beta1.KongUpstreamPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: "my-policy", Namespace: "ns1"},
			},
			setup: []client.Object{
				&configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: metav1.ObjectMeta{Name: "my-policy", Namespace: "ns1"},
				},
			},
			wantNil: true,
		},
		{
			name: "policy with service referencing it but no HTTPRoute returns nil",
			input: &configurationv1beta1.KongUpstreamPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: "my-policy", Namespace: "ns1"},
			},
			setup: []client.Object{
				&configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: metav1.ObjectMeta{Name: "my-policy", Namespace: "ns1"},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name: "svc1", Namespace: "ns1",
						Annotations: map[string]string{
							configurationv1beta1.KongUpstreamPolicyAnnotationKey: "my-policy",
						},
					},
				},
			},
			wantNil: true,
		},
		{
			name: "policy with service referencing it with HTTPRoute returns requests",
			input: &configurationv1beta1.KongUpstreamPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: "my-policy", Namespace: "ns1"},
			},
			setup: []client.Object{
				&configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: metav1.ObjectMeta{Name: "my-policy", Namespace: "ns1"},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name: "svc1", Namespace: "ns1",
						Annotations: map[string]string{
							configurationv1beta1.KongUpstreamPolicyAnnotationKey: "my-policy",
						},
					},
				},
				&gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name: "route1", Namespace: "ns1",
						Annotations: map[string]string{
							"gateway-operator.konghq.com/hybrid-routes": "ns1/route1",
						},
					},
					Spec: gwtypes.HTTPRouteSpec{
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{Name: gwtypes.ObjectName("gw1")},
							},
						},
						Rules: httpRouteRuleForService("svc1"),
					},
				},
				&configurationv1alpha1.KongUpstream{
					ObjectMeta: metav1.ObjectMeta{
						Name: "upstream1", Namespace: "ns1",
						Annotations: map[string]string{
							"gateway-operator.konghq.com/hybrid-gateways": "ns1/gw1",
							"gateway-operator.konghq.com/hybrid-routes":   "ns1/route1",
						},
					},
				},
			},
			wantLen:   1,
			wantNames: []string{"route1"},
		},
		{
			name: "policy with multiple services referencing it returns multiple route requests",
			input: &configurationv1beta1.KongUpstreamPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: "my-policy", Namespace: "ns1"},
			},
			setup: []client.Object{
				&configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: metav1.ObjectMeta{Name: "my-policy", Namespace: "ns1"},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name: "svc1", Namespace: "ns1",
						Annotations: map[string]string{
							configurationv1beta1.KongUpstreamPolicyAnnotationKey: "my-policy",
						},
					},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name: "svc2", Namespace: "ns1",
						Annotations: map[string]string{
							configurationv1beta1.KongUpstreamPolicyAnnotationKey: "my-policy",
						},
					},
				},
				&gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name: "route1", Namespace: "ns1",
						Annotations: map[string]string{
							"gateway-operator.konghq.com/hybrid-routes": "ns1/route1",
						},
					},
					Spec: gwtypes.HTTPRouteSpec{
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{Name: gwtypes.ObjectName("gw1")},
							},
						},
						Rules: httpRouteRuleForService("svc1"),
					},
				},
				&gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name: "route2", Namespace: "ns1",
						Annotations: map[string]string{
							"gateway-operator.konghq.com/hybrid-routes": "ns1/route2",
						},
					},
					Spec: gwtypes.HTTPRouteSpec{
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{Name: gwtypes.ObjectName("gw1")},
							},
						},
						Rules: httpRouteRuleForService("svc2"),
					},
				},
				&configurationv1alpha1.KongUpstream{
					ObjectMeta: metav1.ObjectMeta{
						Name: "upstream1", Namespace: "ns1",
						Annotations: map[string]string{
							"gateway-operator.konghq.com/hybrid-gateways": "ns1/gw1",
							"gateway-operator.konghq.com/hybrid-routes":   "ns1/route1",
						},
					},
				},
				&configurationv1alpha1.KongUpstream{
					ObjectMeta: metav1.ObjectMeta{
						Name: "upstream2", Namespace: "ns1",
						Annotations: map[string]string{
							"gateway-operator.konghq.com/hybrid-gateways": "ns1/gw1",
							"gateway-operator.konghq.com/hybrid-routes":   "ns1/route2",
						},
					},
				},
			},
			wantLen:   2,
			wantNames: []string{"route1", "route2"},
		},
		{
			name: "policy in different namespace than service",
			input: &configurationv1beta1.KongUpstreamPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: "my-policy", Namespace: "ns1"},
			},
			setup: []client.Object{
				&configurationv1beta1.KongUpstreamPolicy{
					ObjectMeta: metav1.ObjectMeta{Name: "my-policy", Namespace: "ns1"},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name: "svc1", Namespace: "ns2",
						Annotations: map[string]string{
							configurationv1beta1.KongUpstreamPolicyAnnotationKey: "my-policy",
						},
					},
				},
			},
			wantNil: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			scheme := schemeWithAll()
			cl := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tc.setup...).
				WithIndex(&gwtypes.HTTPRoute{}, index.BackendServicesOnHTTPRouteIndex, func(obj client.Object) []string {
					httpRoute, ok := obj.(*gwtypes.HTTPRoute)
					if !ok {
						return nil
					}
					var keys []string
					for _, rule := range httpRoute.Spec.Rules {
						for _, ref := range rule.BackendRefs {
							keys = append(keys, httpRoute.Namespace+"/"+string(ref.BackendRef.Name))
						}
					}
					return keys
				}).
				Build()
			mapFn := MapHTTPRouteForKongUpstreamPolicy(cl)

			result := mapFn(ctx, tc.input)

			if tc.wantNil {
				require.Nil(t, result)
				return
			}
			require.NotNil(t, result)
			if tc.wantLen >= 0 {
				require.Len(t, result, tc.wantLen)
			}
			if len(tc.wantNames) > 0 {
				names := make([]string, 0, len(result))
				for _, r := range result {
					names = append(names, r.Name)
				}
				for _, want := range tc.wantNames {
					require.Contains(t, names, want)
				}
			}
		})
	}
}

// schemeWithAll builds a scheme with all required types.
func schemeWithAll() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	_ = configurationv1.AddToScheme(s)
	_ = configurationv1alpha1.AddToScheme(s)
	_ = configurationv1beta1.AddToScheme(s)
	_ = gatewayv1.Install(s)
	return s
}
