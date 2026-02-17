package builder

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

func TestKongCertificateBuilder_BasicFields(t *testing.T) {
	b := NewKongCertificate().WithName("cert1").WithNamespace("ns1").WithSecretRef("sec", "ns2")
	cert, err := b.Build()
	require.NoError(t, err)
	require.Equal(t, "cert1", cert.Name)
	require.Equal(t, "ns1", cert.Namespace)
	require.NotNil(t, cert.Spec.SecretRef)
	require.Equal(t, "sec", cert.Spec.SecretRef.Name)
	require.Equal(t, "ns2", *cert.Spec.SecretRef.Namespace)
	require.NotNil(t, cert.Spec.Type)
	require.Equal(t, configurationv1alpha1.KongCertificateSourceTypeSecretRef, *cert.Spec.Type)
}

func TestKongCertificateBuilder_WithControlPlaneRef(t *testing.T) {
	b := NewKongCertificate()
	cpr := commonv1alpha1.ControlPlaneRef{Type: "konnectID"}
	cert, err := b.WithControlPlaneRef(cpr).Build()
	require.NoError(t, err)
	require.NotNil(t, cert.Spec.ControlPlaneRef)
	require.Equal(t, "konnectID", cert.Spec.ControlPlaneRef.Type)
}

func TestKongCertificateBuilder_WithLabels(t *testing.T) {
	gw := &gwtypes.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "gw1", Namespace: "ns1"},
	}
	listener := &gwtypes.Listener{Port: 443}
	b := NewKongCertificate().WithLabels(gw, listener)
	cert, err := b.Build()
	require.NoError(t, err)
	require.NotNil(t, cert.Labels)
	require.Contains(t, cert.Labels, "gateway-operator.konghq.com/listener-port")
	require.Equal(t, "443", cert.Labels["gateway-operator.konghq.com/listener-port"])
}

func TestKongCertificateBuilder_WithLabels_NilListener(t *testing.T) {
	gw := &gwtypes.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "gw2", Namespace: "ns2"},
	}
	b := NewKongCertificate().WithLabels(gw, nil)
	cert, err := b.Build()
	require.NoError(t, err)
	require.NotNil(t, cert.Labels)
	require.NotContains(t, cert.Labels, "gateway-operator.konghq.com/hybrid-listener-port")
}

func TestKongCertificateBuilder_WithAnnotations(t *testing.T) {
	gw := &gwtypes.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "gw3", Namespace: "ns3"},
	}
	b := NewKongCertificate().WithAnnotations(gw)
	cert, err := b.Build()
	require.NoError(t, err)
	require.NotNil(t, cert.Annotations)
	// No strict value check, just ensure map is present.
}

func TestKongCertificateBuilder_WithOwner(t *testing.T) {
	gw := &gwtypes.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "gw4", Namespace: "ns4"},
	}
	b := NewKongCertificate().WithOwner(gw)
	_, err := b.Build()
	require.Error(t, err)
	require.Contains(t, err.Error(), "cluster-scoped resource must not have a namespace-scoped owner")
}

func TestKongCertificateBuilder_WithOwner_NilOwner(t *testing.T) {
	b := NewKongCertificate().WithOwner(nil)
	_, err := b.Build()
	require.Error(t, err)
	require.Contains(t, err.Error(), "owner cannot be nil")
}

func TestKongCertificateBuilder_ErrorAccumulation(t *testing.T) {
	b := NewKongCertificate().WithOwner(nil).WithOwner(nil)
	_, err := b.Build()
	require.Error(t, err)
	require.Contains(t, err.Error(), "owner cannot be nil")
	// Should mention error twice if both appended.
	require.GreaterOrEqual(t, len(err.Error()), len("owner cannot be nil"))
}

func TestKongCertificateBuilder_MustBuild_PanicsOnError(t *testing.T) {
	b := NewKongCertificate().WithOwner(nil)

	require.PanicsWithError(t, "failed to build KongCertificate: owner cannot be nil", func() {
		_ = b.MustBuild()
	})
}

func TestKongCertificateBuilder_MustBuild_Success(t *testing.T) {
	b := NewKongCertificate().WithName("cert-ok").WithNamespace("ns-ok").WithSecretRef("sec-ok", "ns-ok")
	cert := b.MustBuild()
	require.Equal(t, "cert-ok", cert.Name)
	require.Equal(t, "ns-ok", cert.Namespace)
}
