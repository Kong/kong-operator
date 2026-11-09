package admission

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// writeCert writes a self-signed certificate and key into dir and returns the
// DER-encoded certificate.
func writeCert(t *testing.T, dir string) []byte {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "webhook-test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		IsCA:                  true,
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	require.NoError(t, err)

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tls.crt"), certPEM, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tls.key"), keyPEM, 0o600))
	return der
}

// dialTLS performs a TLS handshake against addr, trusting only caPEM.
func dialTLS(addr string, caPEM []byte) error {
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return os.ErrInvalid
	}
	conn, err := tls.DialWithDialer(
		&net.Dialer{Timeout: time.Second},
		"tcp", addr,
		&tls.Config{RootCAs: roots, ServerName: "localhost"},
	)
	if err != nil {
		return err
	}
	return conn.Close()
}

func TestMakeTLSServerReadsCertFromConfiguredDir(t *testing.T) {
	handler := http.NewServeMux()

	t.Run("empty directory is rejected", func(t *testing.T) {
		_, err := MakeTLSServer(0, handler, "")
		require.Error(t, err)
		require.Contains(t, err.Error(), "must not be empty")
	})

	t.Run("directory without certificate is rejected", func(t *testing.T) {
		_, err := MakeTLSServer(0, handler, t.TempDir())
		require.Error(t, err)
	})

	t.Run("server serves the certificate from the configured directory", func(t *testing.T) {
		dir := t.TempDir()
		der := writeCert(t, dir)
		caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

		// Reserve a free port for the server.
		ln, err := net.Listen("tcp", "127.0.0.1:0") //nolint:gosec // test only
		require.NoError(t, err)
		port := ln.Addr().(*net.TCPAddr).Port
		require.NoError(t, ln.Close())

		srv, err := MakeTLSServer(int32(port), handler, dir)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go func() { _ = srv.Start(ctx) }()

		addr := net.JoinHostPort("localhost", strconv.Itoa(port))
		require.Eventually(t, func() bool {
			return dialTLS(addr, caPEM) == nil
		}, 5*time.Second, 100*time.Millisecond)
	})
}
