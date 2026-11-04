package pki

import (
	"crypto/x509"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kong/kong-operator/v2/test/helpers/certificate"
)

func TestParseCertificates(t *testing.T) {
	first, firstKey := certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("first"))
	second, _ := certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("second"))

	certs := ParseCertificates(slices.Concat(first, firstKey, second))
	require.Len(t, certs, 2, "anything but certificates is skipped")
	require.Equal(t, "first", certs[0].Subject.CommonName)
	require.Equal(t, "second", certs[1].Subject.CommonName)
	require.Empty(t, ParseCertificates([]byte("not PEM")))
}

func TestLeafCertificate(t *testing.T) {
	certPEM, keyPEM := certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("leaf"))
	_, otherKey := certificate.MustGenerateCertPEMFormat(certificate.WithCommonName("other"))

	leaf := LeafCertificate(certPEM, keyPEM)
	require.NotNil(t, leaf)
	require.Equal(t, "leaf", leaf.Subject.CommonName)
	require.Nil(t, LeafCertificate(certPEM, otherKey), "the certificate must form a key pair with the key")
	require.Nil(t, LeafCertificate(nil, nil))
}

func TestRenewalTime(t *testing.T) {
	issuedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	require.Equal(t, issuedAt.Add(2*time.Hour), RenewalTime(issuedAt, issuedAt.Add(3*time.Hour)))

	noExpiry := time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)
	require.True(t, RenewalTime(issuedAt, noExpiry).After(issuedAt), "a validity past the range of a Duration must not overflow")
}

func TestRenewalExtends(t *testing.T) {
	require.True(t, RenewalExtends(3*time.Hour, time.Hour))
	require.False(t, RenewalExtends(2*time.Hour, time.Hour), "doubling isn't enough")
	require.False(t, RenewalExtends(time.Hour, time.Hour))
	require.False(t, RenewalExtends(time.Hour, 200*365*24*time.Hour), "a remaining validity past half the range of a Duration must not overflow")
}

func TestWarningTime(t *testing.T) {
	now := time.Now()
	cert := &x509.Certificate{NotBefore: now, NotAfter: now.Add(60 * time.Hour)}
	require.Equal(t, now.Add(50*time.Hour), WarningTime(cert), "once less than a sixth of its validity is left")
}
