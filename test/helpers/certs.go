package helpers

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// -----------------------------------------------------------------------------
// Certificate test helper functions and types
// -----------------------------------------------------------------------------

type Cert struct {
	Cert    *x509.Certificate
	CertPEM *bytes.Buffer
	Key     *ecdsa.PrivateKey
	KeyPEM  *bytes.Buffer
}

func createKey(t *testing.T) *ecdsa.PrivateKey {
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	return privKey
}

func randomBigInt(t *testing.T) *big.Int {
	n, err := rand.Int(rand.Reader, big.NewInt(10000))
	require.NoError(t, err)
	return n
}

// CreateCA creates a CA that can be used in tests.
func CreateCA(t *testing.T) Cert {
	caCertTemplate := &x509.Certificate{
		SerialNumber: randomBigInt(t),
		Subject: pkix.Name{
			Organization:  []string{"Company, INC."},
			Country:       []string{"US"},
			Province:      []string{""},
			Locality:      []string{"San Francisco"},
			StreetAddress: []string{"Golden Gate Bridge"},
			PostalCode:    []string{"11111"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(1, 0, 0),
		IsCA:                  true,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}

	caPrivKey := createKey(t)
	keyPEM := encodeKeyToPEM(t, caPrivKey)

	caBytes, err := x509.CreateCertificate(rand.Reader, caCertTemplate, caCertTemplate, &caPrivKey.PublicKey, caPrivKey)
	require.NoError(t, err)
	caPEM := encodeCertToPEM(t, caBytes)

	return Cert{
		Cert:    caCertTemplate,
		CertPEM: caPEM,
		Key:     caPrivKey,
		KeyPEM:  keyPEM,
	}
}

// CreateCert creates a certificates using the provided CA and its private key.
func CreateCert(t *testing.T, name string, caCert *x509.Certificate, caPrivKey *ecdsa.PrivateKey) Cert {
	certTemplate := &x509.Certificate{
		SerialNumber: randomBigInt(t),
		Subject: pkix.Name{
			Organization:  []string{"Company, INC."},
			Country:       []string{"US"},
			Province:      []string{""},
			Locality:      []string{"San Francisco"},
			StreetAddress: []string{"Golden Gate Bridge"},
			PostalCode:    []string{"11111"},
			CommonName:    name,
		},
		DNSNames:    []string{name},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().AddDate(1, 0, 0),
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:    x509.KeyUsageDigitalSignature,
	}

	certPrivKey := createKey(t)
	keyPEM := encodeKeyToPEM(t, certPrivKey)

	certBytes, err := x509.CreateCertificate(rand.Reader, certTemplate, caCert, &certPrivKey.PublicKey, caPrivKey)
	require.NoError(t, err)
	caPEM := encodeCertToPEM(t, certBytes)

	return Cert{
		Cert:    certTemplate,
		CertPEM: caPEM,
		Key:     certPrivKey,
		KeyPEM:  keyPEM,
	}
}

func encodeKeyToPEM(t *testing.T, key *ecdsa.PrivateKey) *bytes.Buffer {
	var buff bytes.Buffer
	ecKey, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	require.NoError(t,
		pem.Encode(&buff, &pem.Block{
			Type:  "ECDSA PRIVATE KEY",
			Bytes: ecKey,
		}),
	)

	return &buff
}

func encodeCertToPEM(t *testing.T, cert []byte) *bytes.Buffer {
	var buff bytes.Buffer
	require.NoError(t,
		pem.Encode(&buff, &pem.Block{
			Type:  "CERTIFICATE",
			Bytes: cert,
		}),
	)

	return &buff
}

// TLSSecretData creates TLS secret data that can be then used as Secret.Data field
// when using certificates secrets in tests.
func TLSSecretData(t *testing.T, ca Cert, c Cert) map[string][]byte {
	require.NotNil(t, ca.CertPEM)
	require.NotNil(t, c.CertPEM)
	require.NotNil(t, c.KeyPEM)

	return map[string][]byte{
		"ca.crt":  ca.CertPEM.Bytes(),
		"tls.crt": c.CertPEM.Bytes(),
		"tls.key": c.KeyPEM.Bytes(),
	}
}
