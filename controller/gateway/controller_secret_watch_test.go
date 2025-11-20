package gateway

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	gwtypes "github.com/kong/kong-operator/internal/types"
)

func gwWithListenerCertRef(name string, ref gatewayv1.SecretObjectReference) *gwtypes.Gateway {
	gw := &gwtypes.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "ns1",
			Name:      name,
		},
		Spec: gwtypes.GatewaySpec{
			Listeners: []gwtypes.Listener{
				{
					Name:     "https",
					Port:     443,
					Protocol: gwtypes.HTTPSProtocolType,
					TLS: &gatewayv1.GatewayTLSConfig{
						Mode:            gatewayTLSModePtr(gatewayv1.TLSModeTerminate),
						CertificateRefs: []gatewayv1.SecretObjectReference{ref},
					},
				},
			},
		},
	}
	return gw
}

func gatewayTLSModePtr(m gatewayv1.TLSModeType) *gatewayv1.TLSModeType { return &m }

func Test_secretReferencedByGateway_SameNamespace_DefaultGroupKind(t *testing.T) {
	// No Group/Kind set => treated as core/v1 Secret
	ref := gatewayv1.SecretObjectReference{
		Name: "cert",
		// Namespace nil -> same namespace as Gateway
	}
	gw := gwWithListenerCertRef("gw1", ref)

	if !secretReferencedByGateway(gw, "ns1", "cert") {
		t.Fatalf("expected match for same-namespace Secret reference with default group/kind")
	}
}

func Test_secretReferencedByGateway_CrossNamespace_ExplicitCoreSecret(t *testing.T) {
	// Explicit Group/Kind of core/Secret and explicit namespace
	ns := gatewayv1.Namespace("other")
	kind := gatewayv1.Kind("Secret")
	group := gatewayv1.Group(corev1.GroupName)
	ref := gatewayv1.SecretObjectReference{
		Group:     &group,
		Kind:      &kind,
		Name:      "cert2",
		Namespace: &ns,
	}
	gw := gwWithListenerCertRef("gw1", ref)

	if !secretReferencedByGateway(gw, "other", "cert2") {
		t.Fatalf("expected match for cross-namespace explicit core/Secret reference")
	}
}

func Test_secretReferencedByGateway_IgnoresNonSecretOrNonCore(t *testing.T) {
	// Non-core group => should be ignored
	ns := gatewayv1.Namespace("ns1")
	badGroup := gatewayv1.Group("example.com")
	kindSecret := gatewayv1.Kind("Secret")
	refNonCore := gatewayv1.SecretObjectReference{Group: &badGroup, Kind: &kindSecret, Name: "cert", Namespace: &ns}
	gw := gwWithListenerCertRef("gw1", refNonCore)
	if secretReferencedByGateway(gw, "ns1", "cert") {
		t.Fatalf("did not expect match for non-core group")
	}

	// Non-Secret kind => should be ignored
	groupCore := gatewayv1.Group(corev1.GroupName)
	badKind := gatewayv1.Kind("ConfigMap")
	refNonSecret := gatewayv1.SecretObjectReference{Group: &groupCore, Kind: &badKind, Name: "cert"}
	gw2 := gwWithListenerCertRef("gw2", refNonSecret)
	if secretReferencedByGateway(gw2, "ns1", "cert") {
		t.Fatalf("did not expect match for non-Secret kind")
	}
}

func Test_secretReferencedByGateway_NoTLSOrNoRefs(t *testing.T) {
	gw := &gwtypes.Gateway{Spec: gwtypes.GatewaySpec{}}
	if secretReferencedByGateway(gw, "ns", "name") {
		t.Fatalf("did not expect match when no listeners/tls refs present")
	}
}
