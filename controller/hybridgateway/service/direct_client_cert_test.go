package service

import (
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	hgerrors "github.com/kong/kong-operator/v2/controller/hybridgateway/errors"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/metadata"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/vars"
)

func TestServiceForRule_DirectClientCertificate(t *testing.T) {
	for _, kind := range []string{"HTTPRoute", "GRPCRoute", "TLSRoute", "TCPRoute", "UDPRoute"} {
		t.Run(kind, func(t *testing.T) {
			cp, cert, objects := directClientCertificateFixtures()
			cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(objects...).Build()
			pRef := &gwtypes.ParentReference{Name: "gateway", Namespace: new(gatewayv1.Namespace("gateways"))}
			parents := []gwtypes.RouteParentStatus{{ParentRef: *pRef, ControllerName: gwtypes.GatewayController(vars.ControllerName())}}
			backend := gwtypes.BackendRef{Name: "backend", Port: new(gatewayv1.PortNumber(443))}
			var service *configurationv1alpha1.KongService
			var generatedCert *configurationv1alpha1.KongCertificate
			var generatedGrant *configurationv1alpha1.KongReferenceGrant
			var err error
			switch kind {
			case "HTTPRoute":
				route := &gwtypes.HTTPRoute{
					Name: "route", Namespace: "backends", Kind: kind, APIVersion: gatewayv1.GroupVersion.String(),
					Status: gwtypes.HTTPRouteStatus{RouteStatus: gatewayv1.RouteStatus{Parents: parents}},
				}
				rule := gwtypes.HTTPRouteRule{BackendRefs: []gwtypes.HTTPBackendRef{{BackendRef: backend}}}
				service, generatedCert, generatedGrant, err = ServiceForRule(t.Context(), logr.Discard(), cl, route, rule, pRef, cp, "upstream")
			case "GRPCRoute":
				route := &gwtypes.GRPCRoute{
					Name: "route", Namespace: "backends", Kind: kind, APIVersion: gatewayv1.GroupVersion.String(),
					Status: gatewayv1.GRPCRouteStatus{RouteStatus: gatewayv1.RouteStatus{Parents: parents}},
				}
				rule := gwtypes.GRPCRouteRule{BackendRefs: []gwtypes.GRPCBackendRef{{BackendRef: backend}}}
				service, generatedCert, generatedGrant, err = ServiceForRule(t.Context(), logr.Discard(), cl, route, rule, pRef, cp, "upstream")
			case "TLSRoute":
				route := &gwtypes.TLSRoute{
					Name: "route", Namespace: "backends", Kind: kind, APIVersion: gatewayv1.GroupVersion.String(),
					Status: gwtypes.TLSRouteStatus{RouteStatus: gatewayv1.RouteStatus{Parents: parents}},
				}
				rule := gwtypes.TLSRouteRule{BackendRefs: []gwtypes.BackendRef{backend}}
				service, generatedCert, generatedGrant, err = ServiceForRule(t.Context(), logr.Discard(), cl, route, rule, pRef, cp, "upstream")
			case "TCPRoute":
				route := &gwtypes.TCPRoute{
					Name: "route", Namespace: "backends", Kind: kind, APIVersion: gatewayv1alpha2.GroupVersion.String(),
					Status: gwtypes.TCPRouteStatus{RouteStatus: gatewayv1.RouteStatus{Parents: parents}},
				}
				rule := gwtypes.TCPRouteRule{BackendRefs: []gwtypes.BackendRef{backend}}
				service, generatedCert, generatedGrant, err = ServiceForRule(t.Context(), logr.Discard(), cl, route, rule, pRef, cp, "upstream")
			case "UDPRoute":
				route := &gwtypes.UDPRoute{
					Name: "route", Namespace: "backends", Kind: kind, APIVersion: gatewayv1alpha2.GroupVersion.String(),
					Status: gwtypes.UDPRouteStatus{RouteStatus: gatewayv1.RouteStatus{Parents: parents}},
				}
				rule := gwtypes.UDPRouteRule{BackendRefs: []gwtypes.BackendRef{backend}}
				service, generatedCert, generatedGrant, err = ServiceForRule(t.Context(), logr.Discard(), cl, route, rule, pRef, cp, "upstream")
			}
			require.NoError(t, err)
			require.NotNil(t, service)
			require.Equal(t, "gateways", service.Namespace)
			require.Equal(t, &commonv1alpha1.NamespacedRef{Name: cert.Name, Namespace: new(cert.Namespace)}, service.Spec.ClientCertificateRef)
			require.Nil(t, generatedCert, "must not create a duplicate or adopt the external certificate")
			require.Nil(t, generatedGrant, "must not generate tenant authorization")
			var current configurationv1alpha1.KongCertificate
			require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(cert), &current))
			require.Equal(t, cert.Spec, current.Spec)
			require.Empty(t, current.OwnerReferences)
			require.Empty(t, current.Annotations)
			require.Empty(t, current.Labels)
		})
	}
}

func TestDirectClientCertificateSelectionErrors(t *testing.T) {
	for _, tt := range []struct {
		name        string
		annotations []map[string]string
		wantError   bool
	}{
		{name: "same resolved reference", annotations: []map[string]string{
			{metadata.ClientCertificateRefAnnotation: "client"}, {metadata.ClientCertificateRefAnnotation: "backends/client"},
		}},
		{name: "different references", wantError: true, annotations: []map[string]string{
			{metadata.ClientCertificateRefAnnotation: "client"}, {metadata.ClientCertificateRefAnnotation: "other/client"},
		}},
		{name: "mixed legacy and direct", wantError: true, annotations: []map[string]string{
			{metadata.ClientCertificateRefAnnotation: "client"}, {"konghq.com/client-cert": "client"},
		}},
		{name: "legacy before direct must not short circuit", wantError: true, annotations: []map[string]string{
			{"konghq.com/client-cert": "client"}, {metadata.ClientCertificateRefAnnotation: "client"},
		}},
		{name: "legacy before malformed direct must not fall back", wantError: true, annotations: []map[string]string{
			{"konghq.com/client-cert": "client"}, {metadata.ClientCertificateRefAnnotation: "a/b/c"},
		}},
		{name: "legacy before empty direct must not fall back", wantError: true, annotations: []map[string]string{
			{"konghq.com/client-cert": "client"}, {metadata.ClientCertificateRefAnnotation: ""},
		}},
		{name: "multiple legacy selections before direct remain a conflict", wantError: true, annotations: []map[string]string{
			{"konghq.com/client-cert": "first"}, {"konghq.com/client-cert": "second"},
			{metadata.ClientCertificateRefAnnotation: "client"},
		}},
		{name: "both on one service", wantError: true, annotations: []map[string]string{
			{metadata.ClientCertificateRefAnnotation: "client", "konghq.com/client-cert": "secret"},
		}},
		{name: "malformed reference", wantError: true, annotations: []map[string]string{
			{metadata.ClientCertificateRefAnnotation: "a/b/c"},
		}},
		{name: "empty reference", wantError: true, annotations: []map[string]string{
			{metadata.ClientCertificateRefAnnotation: ""},
		}},
		{name: "unannotated mixed with direct", wantError: true, annotations: []map[string]string{
			{}, {metadata.ClientCertificateRefAnnotation: "client"},
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var objects []client.Object
			var backendRefs []gwtypes.BackendRef
			for i, annotations := range tt.annotations {
				name := fmt.Sprintf("backend-%d", i)
				objects = append(objects, &corev1.Service{Name: name, Namespace: "backends", Annotations: annotations})
				backendRefs = append(backendRefs, gwtypes.BackendRef{Name: gatewayv1.ObjectName(name)})
			}
			cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(objects...).Build()
			selection, err := resolveClientCertFromBackendRefs(t.Context(), cl, "backends", backendRefs)
			require.Equal(t, tt.wantError, hgerrors.OnlyClientCertificateRefErrors(err), "%v", err)
			if !tt.wantError {
				require.NoError(t, err)
				require.Equal(t, &commonv1alpha1.NamespacedRef{Name: "client", Namespace: new("backends")}, selection.certificateRef)
			}
		})
	}
}

func TestServiceForRule_DirectClientCertificateFailsClosed(t *testing.T) {
	for _, state := range []string{"missing", "unready", "non-TLS protocol", "revoked grant", "wrong CP"} {
		t.Run(state, func(t *testing.T) {
			cp, _, objects := directClientCertificateFixtures()
			cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(objects...).Build()
			var backend corev1.Service
			require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Namespace: "backends", Name: "backend"}, &backend))
			switch state {
			case "non-TLS protocol":
				backend.Annotations["konghq.com/protocol"] = "http"
				require.NoError(t, cl.Update(t.Context(), &backend))
			case "missing":
				backend.Annotations[metadata.ClientCertificateRefAnnotation] = "missing"
				require.NoError(t, cl.Update(t.Context(), &backend))
			case "unready", "wrong CP":
				var cert configurationv1alpha1.KongCertificate
				require.NoError(t, cl.Get(t.Context(), client.ObjectKey{Namespace: "backends", Name: "client"}, &cert))
				if state == "unready" {
					cert.Status.Conditions[0].ObservedGeneration = 0
				} else {
					cert.Status.Konnect.ControlPlaneID = "wrong-cp"
				}
				require.NoError(t, cl.Update(t.Context(), &cert))
			case "revoked grant":
				require.NoError(t, cl.Delete(t.Context(), &configurationv1alpha1.KongReferenceGrant{Name: "allow-service", Namespace: "backends"}))
			}
			route := &gwtypes.HTTPRoute{Name: "route", Namespace: "backends", Kind: "HTTPRoute"}
			rule := gwtypes.HTTPRouteRule{BackendRefs: []gwtypes.HTTPBackendRef{{Name: "backend", Port: new(gatewayv1.PortNumber(443))}}}
			service, cert, grant, err := ServiceForRule(t.Context(), logr.Discard(), cl, route, rule,
				&gwtypes.ParentReference{Name: "gateway", Namespace: new(gatewayv1.Namespace("gateways"))}, cp, "upstream")
			require.True(t, hgerrors.OnlyClientCertificateRefErrors(err), "%v", err)
			require.Nil(t, service, "must not silently remove requested client authentication")
			require.Nil(t, cert)
			require.Nil(t, grant)
		})
	}
}

func directClientCertificateFixtures() (*commonv1alpha1.ControlPlaneRef, *configurationv1alpha1.KongCertificate, []client.Object) {
	cpRef := &commonv1alpha1.ControlPlaneRef{
		Type:                 commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
		KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{Name: "cp"},
	}
	cp := &konnectv1alpha2.KonnectGatewayControlPlane{
		Name: "cp", Namespace: "gateways", Generation: 1,
		Status: konnectv1alpha2.KonnectGatewayControlPlaneStatus{
			KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{ID: "remote-cp"},
			Conditions:          []metav1.Condition{{Type: "Programmed", Status: metav1.ConditionTrue, ObservedGeneration: 1}},
		},
	}
	cert := &configurationv1alpha1.KongCertificate{
		Name: "client", Namespace: "backends", Generation: 1,
		Spec: configurationv1alpha1.KongCertificateSpec{
			Type: new(configurationv1alpha1.KongCertificateSourceTypeInline),
			Cert: "{vault://upstream/client/certificate}", Key: "{vault://upstream/client/key}",
			ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
				Type:                 commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{Name: cp.Name, Namespace: cp.Namespace},
			},
		},
		Status: configurationv1alpha1.KongCertificateStatus{
			Konnect:    &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{ID: "external-cert", ControlPlaneID: cp.Status.ID},
			Conditions: []metav1.Condition{{Type: "Programmed", Status: metav1.ConditionTrue, ObservedGeneration: 1}},
		},
	}
	backend := &corev1.Service{
		Name: "backend", Namespace: "backends",
		Annotations: map[string]string{
			metadata.ClientCertificateRefAnnotation: cert.Name, "konghq.com/protocol": "https", "konghq.com/tags": "backend-tag",
		},
	}
	grant := &configurationv1alpha1.KongReferenceGrant{
		Name: "allow-service", Namespace: cert.Namespace,
		Spec: configurationv1alpha1.KongReferenceGrantSpec{
			From: []configurationv1alpha1.ReferenceGrantFrom{{
				Group: configurationv1alpha1.Group(configurationv1alpha1.GroupVersion.Group), Kind: "KongService", Namespace: "gateways",
			}},
			To: []configurationv1alpha1.ReferenceGrantTo{{
				Group: configurationv1alpha1.Group(configurationv1alpha1.GroupVersion.Group), Kind: "KongCertificate",
				Name: new(configurationv1alpha1.ObjectName(cert.Name)),
			}},
		},
	}
	return cpRef, cert, []client.Object{cp, cert, backend, grant}
}
