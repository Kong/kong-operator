package hybridgateway

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clientgoevents "k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	finalizerconst "github.com/kong/kong-operator/v2/controller/hybridgateway/const/finalizers"
	routeconst "github.com/kong/kong-operator/v2/controller/hybridgateway/const/route"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/events"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/metadata"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

func TestReconcileInvalidClientCertificateCleansOldBindings(t *testing.T) {
	for _, state := range []string{"missing", "unready", "revoked grant", "conflict", "malformed", "unrelated rule unready", "API failure"} {
		t.Run(state, func(t *testing.T) {
			route := &gwtypes.HTTPRoute{
				Name: "route", Namespace: "default", Kind: "HTTPRoute", APIVersion: gatewayv1.GroupVersion.String(),
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: []gwtypes.ParentReference{{Name: "gateway"}}},
					Rules:           []gwtypes.HTTPRouteRule{{BackendRefs: []gwtypes.HTTPBackendRef{{Name: "backend", Port: new(gatewayv1.PortNumber(443))}}}},
				},
			}
			route.Finalizers = []string{finalizerconst.GetFinalizerForType(*route)}
			if state == "unrelated rule unready" {
				route.Spec.Rules = append(route.Spec.Rules, gwtypes.HTTPRouteRule{
					BackendRefs: []gwtypes.HTTPBackendRef{{Name: "unrelated", Port: new(gatewayv1.PortNumber(80))}},
				})
			}
			cpRef := commonv1alpha1.ControlPlaneRef{
				Type:                 commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{Name: "cp"},
			}
			gateway := &gwtypes.Gateway{
				Name: "gateway", Namespace: route.Namespace, UID: "gateway-uid",
				Spec: gatewayv1.GatewaySpec{Listeners: []gatewayv1.Listener{{
					Name: "http", Protocol: gatewayv1.HTTPProtocolType, Port: 80,
					AllowedRoutes: &gatewayv1.AllowedRoutes{Namespaces: &gatewayv1.RouteNamespaces{From: new(gatewayv1.NamespacesFromSame)}},
				}}},
				Status: gatewayv1.GatewayStatus{Listeners: []gatewayv1.ListenerStatus{{
					Name: "http", Conditions: []metav1.Condition{{Type: "Accepted", Status: metav1.ConditionTrue}},
				}}},
			}
			extension := &konnectv1alpha2.KonnectExtension{
				Name: "extension", Namespace: gateway.Namespace,
				Labels:          map[string]string{consts.GatewayOperatorManagedByLabel: "gateway"},
				OwnerReferences: []metav1.OwnerReference{{APIVersion: gatewayv1.GroupVersion.String(), Kind: "Gateway", Name: gateway.Name, UID: gateway.UID}},
				Spec: konnectv1alpha2.KonnectExtensionSpec{Konnect: konnectv1alpha2.KonnectExtensionKonnectSpec{
					ControlPlane: konnectv1alpha2.KonnectExtensionControlPlane{Ref: commonv1alpha1.KonnectExtensionControlPlaneRef(cpRef)},
				}},
			}
			backend := &corev1.Service{
				Name: "backend", Namespace: route.Namespace,
				Annotations: map[string]string{metadata.ClientCertificateRefAnnotation: "external", "konghq.com/protocol": "https"},
				Spec:        corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 443}}},
			}
			cert := &configurationv1alpha1.KongCertificate{
				Name: "external", Namespace: route.Namespace, Generation: 1,
				Spec: configurationv1alpha1.KongCertificateSpec{
					Type: new(configurationv1alpha1.KongCertificateSourceTypeInline),
					Cert: "{vault://upstream/client/certificate}", Key: "{vault://upstream/client/key}", ControlPlaneRef: &cpRef,
				},
				Status: configurationv1alpha1.KongCertificateStatus{
					Konnect:    &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{ID: "external-id", ControlPlaneID: "cp-id"},
					Conditions: []metav1.Condition{{Type: "Programmed", Status: metav1.ConditionFalse, ObservedGeneration: 1}},
				},
			}
			switch state {
			case "missing":
				backend.Annotations[metadata.ClientCertificateRefAnnotation] = "missing"
			case "revoked grant":
				backend.Annotations[metadata.ClientCertificateRefAnnotation] = "certificates/external"
			case "conflict":
				backend.Annotations["konghq.com/client-cert"] = "legacy-secret"
			case "malformed":
				backend.Annotations[metadata.ClientCertificateRefAnnotation] = "invalid/ref/value"
			}
			cp := &konnectv1alpha2.KonnectGatewayControlPlane{
				Name: "cp", Namespace: route.Namespace,
				Status: konnectv1alpha2.KonnectGatewayControlPlaneStatus{
					KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{ID: "cp-id"},
					Conditions:          []metav1.Condition{{Type: "Programmed", Status: metav1.ConditionTrue}},
				},
			}
			oldService := &configurationv1alpha1.KongService{
				Name: "old-service", Namespace: route.Namespace,
				Labels: metadata.BuildLabels(route, nil), Annotations: metadata.BuildAnnotations(route, &gwtypes.ParentReference{Name: "gateway"}),
				Spec:   configurationv1alpha1.KongServiceSpec{ControlPlaneRef: &cpRef, ClientCertificateRef: &commonv1alpha1.NamespacedRef{Name: cert.Name}},
				Status: configurationv1alpha1.KongServiceStatus{Conditions: []metav1.Condition{{Type: "Programmed", Status: metav1.ConditionTrue}}},
			}
			oldRoute := &configurationv1alpha1.KongRoute{
				Name: "old-route", Namespace: route.Namespace,
				Labels: metadata.BuildLabels(route, nil), Annotations: metadata.BuildAnnotations(route, &gwtypes.ParentReference{Name: "gateway"}),
			}
			cl := fake.NewClientBuilder().WithScheme(scheme.Get()).
				WithObjects(route, gateway, extension, backend, cp, cert, oldService, oldRoute, &corev1.Namespace{Name: route.Namespace}).
				WithObjects(&corev1.Service{Name: "unrelated", Namespace: route.Namespace, Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 80}}}}).
				WithStatusSubresource(&gwtypes.HTTPRoute{}).
				WithInterceptorFuncs(interceptor.Funcs{Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if _, ok := obj.(*configurationv1alpha1.KongCertificate); ok && state == "API failure" {
						return apierrors.NewServiceUnavailable("test failure")
					}
					return cl.Get(ctx, key, obj, opts...)
				}}).Build()
			reconciler := &HybridGatewayReconciler[gwtypes.HTTPRoute, *gwtypes.HTTPRoute]{
				Client: cl, referenceGrantVersion: schema.GroupVersion{Group: gatewayv1.GroupName, Version: "v1"},
				eventRecorder: events.NewTypedEventRecorder(clientgoevents.NewFakeRecorder(100)),
			}
			for range 6 {
				require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(route), route))
				route.SetGroupVersionKind(schema.GroupVersionKind{Group: gatewayv1.GroupName, Version: "v1", Kind: "HTTPRoute"})
				_, err := reconciler.Reconcile(t.Context(), route)
				if state == "API failure" {
					require.Error(t, err)
					break
				}
				require.NoError(t, err)
				if apierrors.IsNotFound(cl.Get(t.Context(), client.ObjectKeyFromObject(oldService), &configurationv1alpha1.KongService{})) {
					break
				}
			}
			var service configurationv1alpha1.KongService
			var binding configurationv1alpha1.KongRoute
			if state == "API failure" {
				require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(oldService), &service))
				require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(oldRoute), &binding))
				return
			}
			require.True(t, apierrors.IsNotFound(cl.Get(t.Context(), client.ObjectKeyFromObject(oldService), &service)), "route status: %+v", route.Status)
			require.True(t, apierrors.IsNotFound(cl.Get(t.Context(), client.ObjectKeyFromObject(oldRoute), &binding)))
			var external configurationv1alpha1.KongCertificate
			require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(cert), &external))
			require.Equal(t, cert.Spec, external.Spec)
			require.Empty(t, external.Annotations)
			require.Empty(t, external.OwnerReferences)
			require.Len(t, route.Status.Parents, 1)
			var invalid, unprogrammed bool
			for _, condition := range route.Status.Parents[0].Conditions {
				invalid = invalid || condition.Type == routeconst.ConditionTypeKongConfigurationValid && condition.Status == metav1.ConditionFalse
				unprogrammed = unprogrammed || condition.Type == routeconst.ConditionTypeKongServiceProgrammed && condition.Status == metav1.ConditionFalse
			}
			require.True(t, invalid)
			require.True(t, unprogrammed)
		})
	}
}

func TestDesiredClientCertificateBindingsReady(t *testing.T) {
	for _, state := range []string{"old certificate still bound", "new certificate bound", "stale service status", "removed ref still bound", "removed ref unbound", "removed ref unbound with stale status"} {
		t.Run(state, func(t *testing.T) {
			certificate := &configurationv1alpha1.KongCertificate{
				Name: "client", Namespace: "certificates",
				Status: configurationv1alpha1.KongCertificateStatus{Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{ID: "new-id"}},
			}
			service := &configurationv1alpha1.KongService{
				Name: "service", Namespace: "gateways", Generation: 2,
				Status: configurationv1alpha1.KongServiceStatus{
					Konnect:    &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndCertificateAndCACertificatesRefs{CertificateID: "old-id"},
					Conditions: []metav1.Condition{{Type: "Programmed", Status: metav1.ConditionTrue, ObservedGeneration: 2}},
				},
			}
			desired := newUnstructured(service.Namespace, service.Name, configurationv1alpha1.GroupVersion.WithKind("KongService"), nil)
			if state != "removed ref still bound" && state != "removed ref unbound" && state != "removed ref unbound with stale status" {
				require.NoError(t, unstructured.SetNestedField(desired.Object, certificate.Name, "spec", "clientCertificateRef", "name"))
				require.NoError(t, unstructured.SetNestedField(desired.Object, certificate.Namespace, "spec", "clientCertificateRef", "namespace"))
			}
			switch state {
			case "new certificate bound", "stale service status":
				service.Status.Konnect.CertificateID = "new-id"
			case "removed ref unbound", "removed ref unbound with stale status":
				service.Status.Konnect.CertificateID = ""
			}
			if state == "stale service status" || state == "removed ref unbound with stale status" {
				service.Status.Conditions[0].ObservedGeneration = 1
			}
			cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(service, certificate).Build()
			conv := &fakeHTTPRouteConverter{desired: []unstructured.Unstructured{desired}, gvks: []schema.GroupVersionKind{}}
			ready, err := desiredClientCertificateBindingsReady(t.Context(), cl, logr.Discard(), conv)
			require.NoError(t, err)
			require.Equal(t, state == "new certificate bound" || state == "removed ref unbound", ready)
		})
	}
}
