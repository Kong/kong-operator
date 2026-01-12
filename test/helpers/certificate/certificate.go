package certificate

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"time"
)

// NOTE: Copy-pasted from https://github.com/Kong/kubernetes-ingress-controller/blob/81c651b285337c03d9249db611fb924e0da3ef4a/test/helpers/certificate/certificate.go
// Enhanced with WithIPAdresses option. Helpers for dealing with certificates in KGO in tests should be merged into a single package and that package can be used
// in both KIC and KGO tests.

type selfSignedCertificateOptions struct {
	CommonName  string
	DNSNames    []string
	IPAddresses []net.IP
	CATrue      bool
	Expired     bool
}

type selfSignedCertificateOption func(selfSignedCertificateOptions) selfSignedCertificateOptions

// WithCommonName sets the CommonName field of the certificate.
func WithCommonName(commonName string) selfSignedCertificateOption {
	return func(opts selfSignedCertificateOptions) selfSignedCertificateOptions {
		opts.CommonName = commonName
		return opts
	}
}

// WithDNSNames sets DNS names for the certificate.
func WithDNSNames(dnsNames ...string) selfSignedCertificateOption {
	return func(opts selfSignedCertificateOptions) selfSignedCertificateOptions {
		opts.DNSNames = append(opts.DNSNames, dnsNames...)
		return opts
	}
}

// WithIPAdresses sets IP addresses for the certificate.
func WithIPAdresses(ipAddresses ...string) selfSignedCertificateOption {
	return func(opts selfSignedCertificateOptions) selfSignedCertificateOptions {
		for _, ip := range ipAddresses {
			opts.IPAddresses = append(opts.IPAddresses, net.ParseIP(ip))
		}
		return opts
	}
}

// WithCATrue allows to use returned certificate to sign other certificates (uses BasicConstraints extension).
func WithCATrue() selfSignedCertificateOption {
	return func(opts selfSignedCertificateOptions) selfSignedCertificateOptions {
		opts.CATrue = true
		return opts
	}
}

// WithAlreadyExpired sets the certificate to be already expired.
func WithAlreadyExpired() selfSignedCertificateOption {
	return func(opts selfSignedCertificateOptions) selfSignedCertificateOptions {
		opts.Expired = true
		return opts
	}
}

// MustGenerateSelfSignedCert generates a [tls.Certificate] struct to be used in TLS client/listener configurations.
// Certificate is self-signed thus returned cert can be used as CA for it.
func MustGenerateSelfSignedCert(options ...selfSignedCertificateOption) tls.Certificate {
	// Generate a new RSA private key.
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(fmt.Sprintf("Failed to generate RSA key: %s", err))
	}

	var certOptions selfSignedCertificateOptions
	for _, option := range options {
		certOptions = option(certOptions)
	}

	notBefore := time.Now()
	notAfter := notBefore.AddDate(1, 0, 0)
	if certOptions.Expired {
		notBefore = notBefore.AddDate(-2, 0, 0)
		notAfter = notAfter.AddDate(-2, 0, 0)
	}

	// Create a self-signed X.509 certificate.
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization:  []string{"Kong HQ"},
			Country:       []string{"US"},
			Province:      []string{"California"},
			Locality:      []string{"San Francisco"},
			StreetAddress: []string{"150 Spear Street, Suite 1600"},
			PostalCode:    []string{"94105"},
			CommonName:    certOptions.CommonName,
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		DNSNames:              certOptions.DNSNames,
		IPAddresses:           certOptions.IPAddresses,
		BasicConstraintsValid: true,
		IsCA:                  certOptions.CATrue,
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		panic(fmt.Sprintf("Failed to create x509 certificate: %s", err))
	}

	// Create a tls.Certificate from the generated private key and certificate.
	certificate := tls.Certificate{
		Certificate: [][]byte{derBytes},
		PrivateKey:  privateKey,
	}

	return certificate
}

// MustGenerateSelfSignedCertPEMFormat generates self-signed certificate
// and returns certificate and key in PEM format. Certificate is self-signed
// thus returned cert can be used as CA for it.
func MustGenerateSelfSignedCertPEMFormat(options ...selfSignedCertificateOption) (cert []byte, key []byte) {
	tlsCert := MustGenerateSelfSignedCert(options...)

	certBlock := &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: tlsCert.Certificate[0],
	}

	privateKey, ok := tlsCert.PrivateKey.(*rsa.PrivateKey)
	if !ok {
		panic("Private Key should be convertible to *rsa.PrivateKey")
	}
	keyBlock := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	}

	return pem.EncodeToMemory(certBlock), pem.EncodeToMemory(keyBlock)
}
