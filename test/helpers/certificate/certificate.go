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

type SelfSignedCertificateOption func(selfSignedCertificateOptions) selfSignedCertificateOptions

func WithCommonName(commonName string) SelfSignedCertificateOption {
	return func(opts selfSignedCertificateOptions) selfSignedCertificateOptions {
		opts.CommonName = commonName
		return opts
	}
}

func WithDNSNames(dnsNames ...string) SelfSignedCertificateOption {
	return func(opts selfSignedCertificateOptions) selfSignedCertificateOptions {
		opts.DNSNames = append(opts.DNSNames, dnsNames...)
		return opts
	}
}

func WithIPAdresses(ipAddresses ...string) SelfSignedCertificateOption {
	return func(opts selfSignedCertificateOptions) selfSignedCertificateOptions {
		for _, ip := range ipAddresses {
			opts.IPAddresses = append(opts.IPAddresses, net.ParseIP(ip))
		}
		return opts
	}
}

// WithCATrue allows to use returned certificate to sign other certificates (uses BasicConstraints extension).
func WithCATrue() SelfSignedCertificateOption {
	return func(opts selfSignedCertificateOptions) selfSignedCertificateOptions {
		opts.CATrue = true
		return opts
	}
}

func WithAlreadyExpired() SelfSignedCertificateOption {
	return func(opts selfSignedCertificateOptions) selfSignedCertificateOptions {
		opts.Expired = true
		return opts
	}
}

// MustGenerateSelfSignedCert generates a tls.Certificate struct to be used in TLS client/listener configurations.
// Certificate is self-signed thus returned cert can be used as CA for it.
func MustGenerateSelfSignedCert(decorators ...SelfSignedCertificateOption) tls.Certificate {
	// Generate a new RSA private key.
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(fmt.Sprintf("Failed to generate RSA key: %s", err))
	}

	options := selfSignedCertificateOptions{
		CommonName: "",
		DNSNames:   []string{},
	}

	for _, decorator := range decorators {
		options = decorator(options)
	}

	notBefore := time.Now()
	notAfter := notBefore.AddDate(1, 0, 0)
	if options.Expired {
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
			CommonName:    options.CommonName,
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		DNSNames:              options.DNSNames,
		IPAddresses:           options.IPAddresses,
		BasicConstraintsValid: true,
		IsCA:                  options.CATrue,
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
func MustGenerateSelfSignedCertPEMFormat(decorators ...SelfSignedCertificateOption) (cert []byte, key []byte) {
	tlsCert := MustGenerateSelfSignedCert(decorators...)

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
