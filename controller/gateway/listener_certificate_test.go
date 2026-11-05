package gateway

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/namegen"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	referencegranthelpers "github.com/kong/kong-operator/v2/test/helpers/referencegrant"
)

func TestListenerTLSPassthroughIgnoresCertificateRefs(t *testing.T) {
	secretRef := gatewayv1.SecretObjectReference{Name: "missing-secret"}
	certificateRef := gatewayv1.SecretObjectReference{
		Group: new(gatewayv1.Group(configurationv1alpha1.GroupVersion.Group)), Kind: new(gatewayv1.Kind("KongCertificate")),
		Name: "missing-certificate", Namespace: new(gatewayv1.Namespace("certificates")),
	}
	for _, tt := range []struct {
		name string
		refs []gatewayv1.SecretObjectReference
	}{
		{name: "no certificate references"},
		{name: "missing Secret", refs: []gatewayv1.SecretObjectReference{secretRef}},
		{name: "cross-namespace KongCertificate without grants", refs: []gatewayv1.SecretObjectReference{certificateRef}},
		{name: "multiple certificate references", refs: []gatewayv1.SecretObjectReference{secretRef, certificateRef}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			gateway := &gwtypes.Gateway{
				Name: "gateway", Namespace: "gateways", Generation: 3,
				Spec: gatewayv1.GatewaySpec{Listeners: []gatewayv1.Listener{{
					Name: "tls", Protocol: gatewayv1.TLSProtocolType, Port: 443,
					TLS: &gatewayv1.ListenerTLSConfig{Mode: new(gatewayv1.TLSModePassthrough), CertificateRefs: tt.refs},
				}}},
			}
			cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithInterceptorFuncs(interceptor.Funcs{
				Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					t.Errorf("TLS passthrough must not fetch certificate resources: %T", obj)
					return cl.Get(ctx, key, obj, opts...)
				},
				List: func(ctx context.Context, cl client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					t.Errorf("TLS passthrough must not resolve reference grants: %T", list)
					return cl.List(ctx, list, opts...)
				},
			}).Build()
			kinds, condition, err := getSupportedKindsWithResolvedRefsCondition(t.Context(), cl, referencegranthelpers.V1(),
				*gateway, gateway.Generation, gateway.Spec.Listeners[0])
			require.NoError(t, err)
			require.Equal(t, []gatewayv1.RouteGroupKind{{Group: new(gatewayv1.Group(gatewayv1.GroupName)), Kind: "TLSRoute"}}, kinds)
			require.Equal(t, metav1.ConditionTrue, condition.Status)
			require.Equal(t, string(gatewayv1.ListenerReasonResolvedRefs), condition.Reason)
			require.Equal(t, gateway.Generation, condition.ObservedGeneration)

			aware := gatewayConditionsAndListenersAware(gateway)
			aware.initListenersStatus()
			gateway.Status.Listeners[0].Conditions = []metav1.Condition{condition}
			aware.setListenersStatus(metav1.ConditionTrue)
			aware.setProgrammed(metav1.ConditionTrue)
			status := gateway.DeepCopy().Status
			require.NoError(t, aware.setKongCertificateListenersStatus(t.Context(), cl))
			require.Equal(t, status, gateway.Status)
			require.True(t, k8sutils.IsProgrammed(aware))
			require.True(t, k8sutils.IsProgrammed(listenerConditionsAware(&gateway.Status.Listeners[0])))
		})
	}
}

func TestKongCertificateListenerProgrammed(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mutate func(*configurationv1alpha1.KongSNI)
		ready  bool
	}{
		{name: "current binding programmed", ready: true},
		{name: "binding not programmed", mutate: func(s *configurationv1alpha1.KongSNI) { s.Status.Conditions = nil }},
		{name: "stale binding status", mutate: func(s *configurationv1alpha1.KongSNI) { s.Generation++ }},
		{name: "old certificate ID", mutate: func(s *configurationv1alpha1.KongSNI) { s.Status.Konnect.CertificateID = "old-id" }},
		{name: "missing SNI ID", mutate: func(s *configurationv1alpha1.KongSNI) { s.Status.Konnect.ID = "" }},
		{name: "wrong ControlPlane", mutate: func(s *configurationv1alpha1.KongSNI) { s.Status.Konnect.ControlPlaneID = "other-cp" }},
		{name: "wrong reference", mutate: func(s *configurationv1alpha1.KongSNI) { s.Spec.CertificateRef.Name = "other" }},
		{name: "wrong namespace", mutate: func(s *configurationv1alpha1.KongSNI) { s.Spec.CertificateRef.Namespace = new("other") }},
		{name: "wrong hostname", mutate: func(s *configurationv1alpha1.KongSNI) { s.Spec.Name = "other.example.test" }},
		{name: "different owner", mutate: func(s *configurationv1alpha1.KongSNI) { s.OwnerReferences[0].UID = "other-uid" }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			gateway := &gwtypes.Gateway{
				Name: "gateway", Namespace: "gateways", UID: "gateway-uid",
				Spec: gatewayv1.GatewaySpec{Listeners: []gatewayv1.Listener{{
					Name: "https", Port: 443, Hostname: new(gatewayv1.Hostname("api.example.test")),
					TLS: &gatewayv1.ListenerTLSConfig{CertificateRefs: []gatewayv1.SecretObjectReference{{
						Group: new(gatewayv1.Group(configurationv1alpha1.GroupVersion.Group)), Kind: new(gatewayv1.Kind("KongCertificate")),
						Name: "certificate", Namespace: new(gatewayv1.Namespace("certificates")),
					}}},
				}}},
			}
			aware := gatewayConditionsAndListenersAware(gateway)
			aware.initListenersStatus()
			gateway.Status.Listeners[0].Conditions = []metav1.Condition{{
				Type: string(gatewayv1.ListenerConditionResolvedRefs), Status: metav1.ConditionTrue,
			}}
			aware.setListenersStatus(metav1.ConditionTrue)
			cert := &configurationv1alpha1.KongCertificate{
				Name: "certificate", Namespace: "certificates",
				Status: configurationv1alpha1.KongCertificateStatus{
					Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{ID: "certificate-id", ControlPlaneID: "cp-id"},
				},
			}
			sni := &configurationv1alpha1.KongSNI{
				Name: namegen.NewKongCertificateName(gateway.Name, "443", "https"), Namespace: gateway.Namespace, Generation: 1,
				OwnerReferences: []metav1.OwnerReference{{UID: gateway.UID}},
				Spec: configurationv1alpha1.KongSNISpec{
					Name:           "api.example.test",
					CertificateRef: commonv1alpha1.NamespacedRef{Name: cert.Name, Namespace: new(cert.Namespace)},
				},
				Status: configurationv1alpha1.KongSNIStatus{
					Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndCertificateRefs{
						ID: "sni-id", CertificateID: cert.GetKonnectID(), ControlPlaneID: "cp-id",
					},
					Conditions: []metav1.Condition{{
						Type: konnectv1alpha1.KonnectEntityProgrammedConditionType, Status: metav1.ConditionTrue, ObservedGeneration: 1,
					}},
				},
			}
			if tt.mutate != nil {
				tt.mutate(sni)
			}
			cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(cert, sni).Build()
			require.NoError(t, aware.setKongCertificateListenersStatus(t.Context(), cl))
			condition := gateway.Status.Listeners[0].Conditions[1]
			require.Equal(t, string(gatewayv1.ListenerConditionProgrammed), condition.Type)
			require.Equal(t, tt.ready, condition.Status == metav1.ConditionTrue)
		})
	}
}

func TestListenerKongCertificateInvalidStatus(t *testing.T) {
	ref := gatewayv1.SecretObjectReference{
		Group: new(gatewayv1.Group(configurationv1alpha1.GroupVersion.Group)), Kind: new(gatewayv1.Kind("KongCertificate")), Name: "missing",
	}
	cl := fake.NewClientBuilder().WithScheme(scheme.Get()).Build()
	_, condition, err := getSupportedKindsWithResolvedRefsCondition(t.Context(), cl, referencegranthelpers.V1(),
		gwtypes.Gateway{Namespace: "gateways"}, 3, gatewayv1.Listener{
			Protocol: gatewayv1.HTTPSProtocolType, TLS: &gatewayv1.ListenerTLSConfig{CertificateRefs: []gatewayv1.SecretObjectReference{ref}},
		})
	require.NoError(t, err)
	require.Equal(t, metav1.ConditionFalse, condition.Status)
	require.Equal(t, string(gatewayv1.ListenerReasonInvalidCertificateRef), condition.Reason)
	require.Contains(t, condition.Message, "KongCertificate gateways/missing does not exist")
	require.EqualValues(t, 3, condition.ObservedGeneration)
	var certificates configurationv1alpha1.KongCertificateList
	require.NoError(t, cl.List(t.Context(), &certificates, client.InNamespace("gateways")))
	require.Empty(t, certificates.Items)
}
