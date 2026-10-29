package pki

import (
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
}

func TestRenewalExtends(t *testing.T) {
	require.True(t, RenewalExtends(3*time.Hour, time.Hour))
	require.False(t, RenewalExtends(2*time.Hour, time.Hour), "doubling isn't enough")
	require.False(t, RenewalExtends(time.Hour, time.Hour))
}
