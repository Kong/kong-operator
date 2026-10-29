// Package pki contains helpers to inspect the certificates the operator
// issues, shared by the controllers that issue and renew them.
package pki

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"time"
)

const (
	// CertificateBackdate is how far the signers of the operator backdate the
	// NotBefore of the certificates they issue, against clock skew.
	CertificateBackdate = 5 * time.Minute
	// MaxRenewalRequeue caps the delay until the certificates the operator
	// issues are checked again, so that long-lived ones are re-validated at
	// least once a day.
	MaxRenewalRequeue = 24 * time.Hour
)

// ParseCertificates parses the PEM encoded certificates, skipping anything else.
func ParseCertificates(data []byte) []*x509.Certificate {
	var certs []*x509.Certificate
	for rest := data; ; {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			return certs
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		if cert, err := x509.ParseCertificate(block.Bytes); err == nil {
			certs = append(certs, cert)
		}
	}
}

// LeafCertificate returns the first certificate of certPEM, or nil when there
// is none or it doesn't form a key pair with keyPEM.
func LeafCertificate(certPEM, keyPEM []byte) *x509.Certificate {
	if _, err := tls.X509KeyPair(certPEM, keyPEM); err != nil {
		return nil
	}
	certs := ParseCertificates(certPEM)
	if len(certs) == 0 {
		return nil
	}
	return certs[0]
}

// RenewalTime returns when a certificate valid from issuedAt until notAfter
// is due for renewal: once two thirds of that validity have elapsed.
func RenewalTime(issuedAt, notAfter time.Time) time.Time {
	return issuedAt.Add(notAfter.Sub(issuedAt) * 2 / 3)
}

// RenewalExtends reports whether a certificate issued again with the fresh
// validity would at least double the remaining validity of the current one.
// A certificate capped at the expiry of its certificate authority can't be
// extended: renewing it would only roll its consumers ever more often as that
// expiry nears.
func RenewalExtends(fresh, remaining time.Duration) bool {
	return fresh > 2*remaining
}

// Fingerprint returns the SHA-256 fingerprint of the certificate, in hex.
func Fingerprint(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(sum[:])
}
