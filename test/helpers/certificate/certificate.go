package certificate

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
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

type KeyType string

const (
	RSA   KeyType = "rsa"
	ECDSA KeyType = "ecdsa"
)

type certificateOptions struct {
	CommonName        string
	DNSNames          []string
	IPAddresses       []net.IP
	CATrue            bool
	Expired           bool
	MaxPathLen        int
	ParentCertificate *tls.Certificate
	KeyType           KeyType
}

type certificateOption func(certificateOptions) certificateOptions

// WithCommonName sets the CommonName field of the certificate.
func WithCommonName(commonName string) certificateOption {
	return func(opts certificateOptions) certificateOptions {
		opts.CommonName = commonName
		return opts
	}
}

// WithDNSNames sets DNS names for the certificate.
func WithDNSNames(dnsNames ...string) certificateOption {
	return func(opts certificateOptions) certificateOptions {
		opts.DNSNames = append(opts.DNSNames, dnsNames...)
		return opts
	}
}

// WithIPAdresses sets IP addresses for the certificate.
func WithIPAdresses(ipAddresses ...string) certificateOption {
	return func(opts certificateOptions) certificateOptions {
		for _, ip := range ipAddresses {
			opts.IPAddresses = append(opts.IPAddresses, net.ParseIP(ip))
		}
		return opts
	}
}

// WithCATrue allows to use returned certificate to sign other certificates (uses BasicConstraints extension).
func WithCATrue() certificateOption {
	return func(opts certificateOptions) certificateOptions {
		opts.CATrue = true
		return opts
	}
}

// WithAlreadyExpired sets the certificate to be already expired.
func WithAlreadyExpired() certificateOption {
	return func(opts certificateOptions) certificateOptions {
		opts.Expired = true
		return opts
	}
}

// WithMaxPathLen sets the MaxPathLen constraint in the certificate.
func WithMaxPathLen(maxLen int) certificateOption {
	return func(opts certificateOptions) certificateOptions {
		opts.MaxPathLen = maxLen
		return opts
	}
}

// WithParent allows to sign the certificate with a parent certificate.
// When not provided, the certificate is self-signed.
func WithParent(parent tls.Certificate) certificateOption {
	return func(opts certificateOptions) certificateOptions {
		opts.ParentCertificate = &parent
		return opts
	}
}

// WithKeyType sets the key type for certificate generation (RSA or ECDSA). Defaults to RSA.
func WithKeyType(keyType KeyType) certificateOption {
	return func(opts certificateOptions) certificateOptions {
		opts.KeyType = keyType
		return opts
	}
}

// MustGenerateCert generates a [tls.Certificate] struct to be used in TLS client/listener configurations.
// If no parent certificate is passed using WithParent option, the certificate is self-signed thus
// returned cert can be used as CA for it. Default is RSA key type unless overridden using WithKeyType option.
func MustGenerateCert(options ...certificateOption) tls.Certificate {
	var certOptions certificateOptions
	certOptions.KeyType = RSA
	for _, option := range options {
		certOptions = option(certOptions)
	}

	// Generate private key based on key type
	var privateKey crypto.PrivateKey
	switch certOptions.KeyType {
	case ECDSA:
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			panic(fmt.Sprintf("Failed to generate ECDSA key: %s", err))
		}
		privateKey = key
	default:
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			panic(fmt.Sprintf("Failed to generate RSA key: %s", err))
		}
		privateKey = key
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
		MaxPathLen:            certOptions.MaxPathLen,
	}

	// If ParentCertificate is not provided, create a self-signed certificate.
	var (
		parent     = template
		signingKey = privateKey
	)
	if certOptions.ParentCertificate != nil {
		var err error
		parent, err = x509.ParseCertificate(certOptions.ParentCertificate.Certificate[0])
		if err != nil {
			panic(fmt.Sprintf("Failed to parse parent certificate: %s", err))
		}
		signingKey = certOptions.ParentCertificate.PrivateKey
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, template, parent, getPublicKey(privateKey), signingKey)
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

func getPublicKey(privateKey crypto.PrivateKey) crypto.PublicKey {
	switch key := privateKey.(type) {
	case *rsa.PrivateKey:
		return &key.PublicKey
	case *ecdsa.PrivateKey:
		return &key.PublicKey
	default:
		panic("Unsupported private key type")
	}
}

// MustGenerateCertPEMFormat generates a certificate and returns certificate and key in PEM format.
// If no parent certificate is passed using WithParent option, the certificate is self-signed thus
// returned cert can be used as CA for it. Default is RSA key type unless overridden using WithKeyType option.
func MustGenerateCertPEMFormat(opts ...certificateOption) (cert []byte, key []byte) {
	return CertToPEMFormat(MustGenerateCert(opts...))
}

// CertToPEMFormat converts a [tls.Certificate] to PEM format.
func CertToPEMFormat(tlsCert tls.Certificate) (cert []byte, key []byte) {
	certBlock := &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: tlsCert.Certificate[0],
	}

	var keyBlock *pem.Block
	switch privateKey := tlsCert.PrivateKey.(type) {
	case *rsa.PrivateKey:
		keyBlock = &pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
		}
	case *ecdsa.PrivateKey:
		keyBytes, err := x509.MarshalECPrivateKey(privateKey)
		if err != nil {
			panic(fmt.Sprintf("Failed to marshal ECDSA private key: %s", err))
		}
		keyBlock = &pem.Block{
			Type:  "EC PRIVATE KEY",
			Bytes: keyBytes,
		}
	default:
		panic("Unsupported private key type")
	}

	return pem.EncodeToMemory(certBlock), pem.EncodeToMemory(keyBlock)
}
