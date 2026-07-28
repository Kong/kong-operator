package watch

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/internal/utils/index"
)

func TestMapHTTPRouteForClientCertSecret(t *testing.T) {
	ctx := context.Background()

	scheme := schemeWithAll()
	require.NoError(t, gatewayv1.Install(scheme))

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "my-cert", Namespace: "ns1"},
	}
	svcWithAnnotation := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc1",
			Namespace: "ns1",
			Annotations: map[string]string{
				"konghq.com/client-cert": "my-cert",
			},
		},
	}
	svcOtherAnnotation := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc2",
			Namespace: "ns1",
			Annotations: map[string]string{
				"konghq.com/client-cert": "other-cert",
			},
		},
	}
	route1 := &gwtypes.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "route1", Namespace: "ns1"},
		Spec: gwtypes.HTTPRouteSpec{
			Rules: []gwtypes.HTTPRouteRule{{
				BackendRefs: []gwtypes.HTTPBackendRef{{
					BackendRef: gwtypes.BackendRef{
						BackendObjectReference: gatewayv1.BackendObjectReference{
							Name: "svc1",
						},
					},
				}},
			}},
		},
	}

	httpRouteIndexer := func(obj client.Object) []string {
		route, ok := obj.(*gwtypes.HTTPRoute)
		if !ok {
			return nil
		}
		var keys []string
		for _, rule := range route.Spec.Rules {
			for _, ref := range rule.BackendRefs {
				keys = append(keys, route.Namespace+"/"+string(ref.Name))
			}
		}
		return keys
	}

	tests := []struct {
		name       string
		input      client.Object
		objects    []client.Object
		setupIndex bool
		wantLen    int
		wantNames  []string
		wantNil    bool
	}{
		{
			name:    "nil input returns nil",
			input:   nil,
			wantNil: true,
		},
		{
			name:    "wrong type returns nil",
			input:   &corev1.Service{},
			wantNil: true,
		},
		{
			name:       "secret not referenced by any service returns empty",
			input:      secret,
			setupIndex: true,
			objects:    []client.Object{},
			wantLen:    0,
		},
		{
			name:       "secret referenced by service with HTTPRoute returns request",
			input:      secret,
			objects:    []client.Object{svcWithAnnotation, route1},
			setupIndex: true,
			wantLen:    1,
			wantNames:  []string{"route1"},
		},
		{
			name:       "service annotation references different secret - no match",
			input:      secret,
			objects:    []client.Object{svcOtherAnnotation},
			setupIndex: true,
			wantLen:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cl client.Client
			if tt.setupIndex {
				cl = fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(tt.objects...).
					WithIndex(&gwtypes.HTTPRoute{}, index.BackendServicesOnHTTPRouteIndex, httpRouteIndexer).
					Build()
			} else {
				cl = fake.NewClientBuilder().WithScheme(scheme).Build()
			}

			mapFn := MapHTTPRouteForClientCertSecret(cl)
			result := mapFn(ctx, tt.input)

			if tt.wantNil {
				require.Nil(t, result)
				return
			}
			require.Len(t, result, tt.wantLen)
			if len(tt.wantNames) > 0 {
				names := make([]string, len(result))
				for i, r := range result {
					names[i] = r.Name
				}
				for _, want := range tt.wantNames {
					require.Contains(t, names, want)
				}
			}
		})
	}
}

func TestMapTLSRouteForClientCertSecret(t *testing.T) {
	ctx := context.Background()

	scheme := schemeWithAll()
	require.NoError(t, gatewayv1.Install(scheme))

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "my-cert", Namespace: "ns1"},
	}
	svcWithAnnotation := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc1",
			Namespace: "ns1",
			Annotations: map[string]string{
				"konghq.com/client-cert": "my-cert",
			},
		},
	}
	tlsRoute1 := &gwtypes.TLSRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "tlsroute1", Namespace: "ns1"},
		Spec: gwtypes.TLSRouteSpec{
			Rules: []gwtypes.TLSRouteRule{{
				BackendRefs: []gwtypes.BackendRef{{
					BackendObjectReference: gatewayv1.BackendObjectReference{
						Name: "svc1",
					},
				}},
			}},
		},
	}

	tlsRouteIndexer := func(obj client.Object) []string {
		route, ok := obj.(*gwtypes.TLSRoute)
		if !ok {
			return nil
		}
		var keys []string
		for _, rule := range route.Spec.Rules {
			for _, ref := range rule.BackendRefs {
				keys = append(keys, route.Namespace+"/"+string(ref.Name))
			}
		}
		return keys
	}

	tests := []struct {
		name       string
		input      client.Object
		objects    []client.Object
		setupIndex bool
		wantLen    int
		wantNames  []string
		wantNil    bool
	}{
		{
			name:    "nil input returns nil",
			input:   nil,
			wantNil: true,
		},
		{
			name:    "wrong type returns nil",
			input:   &corev1.Service{},
			wantNil: true,
		},
		{
			name:       "secret not referenced returns empty",
			input:      secret,
			objects:    []client.Object{},
			setupIndex: true,
			wantLen:    0,
		},
		{
			name:       "secret referenced by service with TLSRoute returns request",
			input:      secret,
			objects:    []client.Object{svcWithAnnotation, tlsRoute1},
			setupIndex: true,
			wantLen:    1,
			wantNames:  []string{"tlsroute1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cl client.Client
			if tt.setupIndex {
				cl = fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(tt.objects...).
					WithIndex(&gwtypes.TLSRoute{}, index.BackendServicesOnTLSRouteIndex, tlsRouteIndexer).
					Build()
			} else {
				cl = fake.NewClientBuilder().WithScheme(scheme).Build()
			}

			mapFn := MapTLSRouteForClientCertSecret(cl)
			result := mapFn(ctx, tt.input)

			if tt.wantNil {
				require.Nil(t, result)
				return
			}
			require.Len(t, result, tt.wantLen)
			if len(tt.wantNames) > 0 {
				names := make([]string, len(result))
				for i, r := range result {
					names[i] = r.Name
				}
				for _, want := range tt.wantNames {
					require.Contains(t, names, want)
				}
			}
		})
	}
}
