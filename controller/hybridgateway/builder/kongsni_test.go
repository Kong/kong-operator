package builder

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

func TestKongSNIBuilder_BasicFields(t *testing.T) {
	b := NewKongSNI().WithName("sni1").WithNamespace("ns1").WithSNIName("example.com").WithCertificateRef("cert1")
	sni, err := b.Build()
	require.NoError(t, err)
	require.Equal(t, "sni1", sni.Name)
	require.Equal(t, "ns1", sni.Namespace)
	require.Equal(t, "example.com", sni.Spec.Name)
	require.Equal(t, "cert1", sni.Spec.CertificateRef.Name)
}

func TestKongSNIBuilder_WithLabels(t *testing.T) {
	gw := &gwtypes.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "gw1", Namespace: "ns1"},
	}
	listener := &gwtypes.Listener{Port: 443}
	b := NewKongSNI().WithLabels(gw, listener)
	sni, err := b.Build()
	require.NoError(t, err)
	require.NotNil(t, sni.Labels)
	require.Contains(t, sni.Labels, "gateway-operator.konghq.com/listener-port")
	require.Equal(t, "443", sni.Labels["gateway-operator.konghq.com/listener-port"])
}

func TestKongSNIBuilder_WithLabels_NilListener(t *testing.T) {
	gw := &gwtypes.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "gw2", Namespace: "ns2"},
	}
	b := NewKongSNI().WithLabels(gw, nil)
	sni, err := b.Build()
	require.NoError(t, err)
	require.NotNil(t, sni.Labels)
	require.NotContains(t, sni.Labels, "gateway-operator.konghq.com/listener-port")
}

func TestKongSNIBuilder_WithAnnotations(t *testing.T) {
	gw := &gwtypes.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "gw3", Namespace: "ns3"},
	}
	b := NewKongSNI().WithAnnotations(gw)
	sni, err := b.Build()
	require.NoError(t, err)
	require.NotNil(t, sni.Annotations)
}

func TestKongSNIBuilder_WithOwner(t *testing.T) {
	gw := &gwtypes.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "gw4", Namespace: "ns4"},
	}
	b := NewKongSNI().WithOwner(gw)
	_, err := b.Build()
	require.Error(t, err)
	require.Contains(t, err.Error(), "cluster-scoped resource must not have a namespace-scoped owner")
}

func TestKongSNIBuilder_WithOwner_NilOwner(t *testing.T) {
	b := NewKongSNI().WithOwner(nil)
	_, err := b.Build()
	require.Error(t, err)
	require.Contains(t, err.Error(), "owner cannot be nil")
}

func TestKongSNIBuilder_ErrorAccumulation(t *testing.T) {
	b := NewKongSNI().WithOwner(nil).WithOwner(nil)
	_, err := b.Build()
	require.Error(t, err)
	require.Contains(t, err.Error(), "owner cannot be nil")
}

func TestKongSNIBuilder_MustBuild_PanicsOnError(t *testing.T) {
	b := NewKongSNI().WithOwner(nil)
	defer func() {
		r := recover()
		require.NotNil(t, r)
		require.Contains(t, r.(error).Error(), "failed to build KongSNI")
	}()
	_ = b.MustBuild()
}

func TestKongSNIBuilder_MustBuild_Success(t *testing.T) {
	b := NewKongSNI().WithName("sni-ok").WithNamespace("ns-ok").WithSNIName("ok.com").WithCertificateRef("cert-ok")
	sni := b.MustBuild()
	require.Equal(t, "sni-ok", sni.Name)
	require.Equal(t, "ns-ok", sni.Namespace)
	require.Equal(t, "ok.com", sni.Spec.Name)
	require.Equal(t, "cert-ok", sni.Spec.CertificateRef.Name)
}
