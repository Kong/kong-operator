package gwadmin

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	"github.com/kong/kong-operator/v2/internal/utils/pki"
)

func TestGenerateCAAndIssueCertificate(t *testing.T) {
	now := time.Date(2026, 10, 19, 10, 30, 42, 0, time.UTC)
	caPEM, keyPEM, err := generateCA("test-ca", now, 24*time.Hour)
	require.NoError(t, err)
	ca := pki.ParseCertificates(caPEM)[0]
	assert.True(t, ca.IsCA)
	assert.True(t, ca.MaxPathLenZero, "it never signs other certificate authorities")
	assert.Equal(t, "test-ca", ca.Subject.CommonName)
	assert.Equal(t, x509.ECDSA, ca.PublicKeyAlgorithm)
	assert.Equal(t, now.Add(-clockSkew), ca.NotBefore)
	assert.Equal(t, now.Add(24*time.Hour), ca.NotAfter)
	caSecret := &corev1.Secret{Data: map[string][]byte{corev1.TLSCertKey: caPEM, corev1.TLSPrivateKeyKey: keyPEM}}

	t.Run("server certificate", func(t *testing.T) {
		certPEM, leafKeyPEM, err := issueCertificate(caSecret, certRequest{
			commonName: "svc", dnsNames: []string{"svc", "svc.ns"}, usages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
			ttl: time.Hour, now: now,
		})
		require.NoError(t, err)
		leaf := leafCertificate(&corev1.Secret{Data: map[string][]byte{corev1.TLSCertKey: certPEM, corev1.TLSPrivateKeyKey: leafKeyPEM}})
		require.NotNil(t, leaf, "a key pair")
		assert.Equal(t, "svc", leaf.Subject.CommonName)
		assert.Equal(t, []string{"svc", "svc.ns"}, leaf.DNSNames)
		assert.Equal(t, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, leaf.ExtKeyUsage)
		assert.Equal(t, x509.KeyUsageDigitalSignature, leaf.KeyUsage)
		assert.False(t, leaf.IsCA)
		assert.Equal(t, now.Truncate(time.Minute).Add(-clockSkew), leaf.NotBefore, "backdated from the minute")
		assert.Equal(t, leaf.NotBefore.Add(time.Hour), leaf.NotAfter)
		roots := x509.NewCertPool()
		roots.AddCert(ca)
		_, err = leaf.Verify(x509.VerifyOptions{
			Roots: roots, DNSName: "svc.ns", CurrentTime: now, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		})
		require.NoError(t, err)
	})

	t.Run("validity capped at the expiry of the CA", func(t *testing.T) {
		certPEM, _, err := issueCertificate(caSecret, certRequest{commonName: "c", ttl: 48 * time.Hour, now: now})
		require.NoError(t, err)
		assert.Equal(t, ca.NotAfter, pki.ParseCertificates(certPEM)[0].NotAfter)
	})

	t.Run("a chain in tls.crt signs with the first certificate", func(t *testing.T) {
		otherPEM, _, err := generateCA("other", now, time.Hour)
		require.NoError(t, err)
		chained := &corev1.Secret{Data: map[string][]byte{
			corev1.TLSCertKey: append(append([]byte{}, caPEM...), otherPEM...), corev1.TLSPrivateKeyKey: keyPEM,
		}}
		certPEM, _, err := issueCertificate(chained, certRequest{commonName: "c", ttl: time.Hour, now: now})
		require.NoError(t, err)
		require.NoError(t, pki.ParseCertificates(certPEM)[0].CheckSignatureFrom(ca))
	})

	t.Run("an expired CA issues nothing", func(t *testing.T) {
		_, _, err := issueCertificate(caSecret, certRequest{commonName: "c", ttl: time.Hour, now: ca.NotAfter})
		require.ErrorContains(t, err, "expired")
	})

	t.Run("zero time is the current time", func(t *testing.T) {
		live, liveKey, err := generateCA("live", time.Now(), time.Hour)
		require.NoError(t, err)
		certPEM, _, err := issueCertificate(&corev1.Secret{Data: map[string][]byte{corev1.TLSCertKey: live, corev1.TLSPrivateKeyKey: liveKey}},
			certRequest{commonName: "c", ttl: time.Minute})
		require.NoError(t, err)
		assert.WithinDuration(t, time.Now().Add(-clockSkew), pki.ParseCertificates(certPEM)[0].NotBefore, 2*time.Minute)
	})
}

func TestParsePrivateKey(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	ecKey, _, err := generateKey()
	require.NoError(t, err)
	pkcs8, err := x509.MarshalPKCS8PrivateKey(rsaKey)
	require.NoError(t, err)

	for name, keyPEM := range map[string][]byte{
		"PKCS #1 RSA": pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(rsaKey)}),
		"PKCS #8":     pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8}),
	} {
		t.Run(name, func(t *testing.T) {
			k, err := parsePrivateKey(keyPEM)
			require.NoError(t, err)
			assert.IsType(t, &rsa.PrivateKey{}, k)
		})
	}

	ecDER, err := x509.MarshalECPrivateKey(ecKey.(*ecdsa.PrivateKey))
	require.NoError(t, err)
	params := pem.EncodeToMemory(&pem.Block{Type: "EC PARAMETERS", Bytes: []byte{0x06, 0x08, 0x2a, 0x86, 0x48, 0xce, 0x3d, 0x03, 0x01, 0x07}})
	k, err := parsePrivateKey(append(params, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: ecDER})...))
	require.NoError(t, err, "blocks other than keys are skipped")
	assert.IsType(t, &ecdsa.PrivateKey{}, k)

	_, err = parsePrivateKey([]byte("nothing"))
	require.Error(t, err)
}

// TestIssueCertificateFromRSA checks a certificate authority with an RSA key
// issues, as a Manual one in tests does.
func TestIssueCertificateFromRSA(t *testing.T) {
	now := time.Now()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "rsa"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	ca := &corev1.Secret{Data: map[string][]byte{
		corev1.TLSCertKey:       pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		corev1.TLSPrivateKeyKey: pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}),
	}}
	certPEM, _, err := issueCertificate(ca, certRequest{commonName: "c", ttl: time.Minute, now: now})
	require.NoError(t, err)
	require.NoError(t, pki.ParseCertificates(certPEM)[0].CheckSignatureFrom(pki.ParseCertificates(ca.Data[corev1.TLSCertKey])[0]))
}
