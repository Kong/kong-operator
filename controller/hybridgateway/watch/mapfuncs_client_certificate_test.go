package watch

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/metadata"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/internal/utils/index"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

func TestMapRouteForClientCertificate(t *testing.T) {
	for _, kind := range []string{kindHTTPRoute, kindGRPCRoute, kindTLSRoute, kindTCPRoute, kindUDPRoute} {
		t.Run(kind, func(t *testing.T) {
			backendRef := gwtypes.BackendRef{Name: "backend", Namespace: new(gatewayv1.Namespace("backends")), Port: new(gatewayv1.PortNumber(443))}
			objects := []client.Object{&corev1.Service{
				Name: "backend", Namespace: "backends",
				Annotations: map[string]string{metadata.ClientCertificateRefAnnotation: "certificates/client"},
			}}
			builder := fake.NewClientBuilder().WithScheme(schemeWithAll()).
				WithIndex(&corev1.Service{}, index.ClientKongCertificateOnBackendServiceIndex, index.ClientKongCertificateOnBackendService)
			switch kind {
			case kindHTTPRoute:
				objects = append(objects, &gwtypes.HTTPRoute{Name: "route", Namespace: "routes",
					Spec: gwtypes.HTTPRouteSpec{Rules: []gwtypes.HTTPRouteRule{{BackendRefs: []gwtypes.HTTPBackendRef{{BackendRef: backendRef}}}}}})
				builder.WithIndex(&gwtypes.HTTPRoute{}, index.BackendServicesOnHTTPRouteIndex, index.BackendServicesOnHTTPRoute)
			case kindGRPCRoute:
				objects = append(objects, &gwtypes.GRPCRoute{Name: "route", Namespace: "routes",
					Spec: gwtypes.GRPCRouteSpec{Rules: []gwtypes.GRPCRouteRule{{BackendRefs: []gwtypes.GRPCBackendRef{{BackendRef: backendRef}}}}}})
				builder.WithIndex(&gwtypes.GRPCRoute{}, index.BackendServicesOnGRPCRouteIndex, index.BackendServicesOnGRPCRoute)
			case kindTLSRoute:
				objects = append(objects, &gwtypes.TLSRoute{Name: "route", Namespace: "routes",
					Spec: gwtypes.TLSRouteSpec{Rules: []gwtypes.TLSRouteRule{{BackendRefs: []gwtypes.BackendRef{backendRef}}}}})
				builder.WithIndex(&gwtypes.TLSRoute{}, index.BackendServicesOnTLSRouteIndex, index.BackendServicesOnTLSRoute)
			case kindTCPRoute:
				objects = append(objects, &gwtypes.TCPRoute{Name: "route", Namespace: "routes",
					Spec: gwtypes.TCPRouteSpec{Rules: []gwtypes.TCPRouteRule{{BackendRefs: []gwtypes.BackendRef{backendRef}}}}})
				builder.WithIndex(&gwtypes.TCPRoute{}, index.BackendServicesOnTCPRouteIndex, index.BackendServicesOnTCPRoute)
			case kindUDPRoute:
				objects = append(objects, &gwtypes.UDPRoute{Name: "route", Namespace: "routes",
					Spec: gwtypes.UDPRouteSpec{Rules: []gwtypes.UDPRouteRule{{BackendRefs: []gwtypes.BackendRef{backendRef}}}}})
				builder.WithIndex(&gwtypes.UDPRoute{}, index.BackendServicesOnUDPRouteIndex, index.BackendServicesOnUDPRoute)
			}
			cl := builder.WithObjects(objects...).Build()
			mapper := MapRouteForClientCertificate(cl, kind)
			want := []reconcile.Request{{Namespace: "routes", Name: "route"}}
			// No generated KongService exists: initial readiness and restoration must still enqueue.
			require.Equal(t, want, mapper(t.Context(), &configurationv1alpha1.KongCertificate{Name: "client", Namespace: "certificates"}))
			require.Empty(t, mapper(t.Context(), &configurationv1alpha1.KongCertificate{Name: "other", Namespace: "certificates"}))
			require.Empty(t, mapper(t.Context(), &configurationv1alpha1.KongCertificate{Name: "client", Namespace: "other"}))
			// Even an emptied/revoked grant must enqueue by target namespace. The actual
			// generated Service's namespace may differ from both route and backend.
			require.Equal(t, want, mapper(t.Context(), &configurationv1alpha1.KongReferenceGrant{Name: "grant", Namespace: "certificates"}))
			require.Empty(t, mapper(t.Context(), &configurationv1alpha1.KongReferenceGrant{Name: "grant", Namespace: "other"}))
			require.Empty(t, mapper(t.Context(), &corev1.Secret{Name: "client", Namespace: "certificates"}))

			owned := &configurationv1alpha1.KongCertificate{
				Name: "legacy", Namespace: "gateways",
				Annotations: map[string]string{metadata.HybridRouteAnnotationKeyForKind(kind): "routes/route"},
				Labels:      map[string]string{consts.GatewayOperatorManagedByLabel: kind},
			}
			require.Equal(t, want, mapper(t.Context(), owned), "keep legacy generated-resource watches")
		})
	}
}
