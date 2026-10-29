/*
Copyright 2026 Kong, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package gwadmin

// The certificate authorities and certificates of a GWAdmin are generated
// here, with the standard library only, so that the GWAdmin controller shares
// no TLS code with the other controllers: changing one can't affect the
// other.

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/kong/kong-operator/v2/internal/utils/pki"
)

// clockSkew is how far certificates are backdated, and how early a Manual
// certificate is accepted, to tolerate clock skew between the operator and
// the peers.
const clockSkew = pki.CertificateBackdate

var certificateSubject = pkix.Name{Organization: []string{"Kong, Inc."}, Country: []string{"US"}}

// generateCA generates a self-signed ECDSA P-256 certificate authority valid
// for ttl from now and returns its certificate and private key, both PEM
// encoded. It only signs certificates, never other certificate authorities.
func generateCA(commonName string, now time.Time, ttl time.Duration) (certPEM, keyPEM []byte, err error) {
	key, keyPEM, err := generateKey()
	if err != nil {
		return nil, nil, err
	}
	template, err := certificateTemplate(commonName, now.Add(-clockSkew), now.Add(ttl))
	if err != nil {
		return nil, nil, err
	}
	template.KeyUsage = x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature
	template.BasicConstraintsValid = true
	template.IsCA = true
	template.MaxPathLenZero = true
	der, err := x509.CreateCertificate(rand.Reader, template, template, key.Public(), key)
	if err != nil {
		return nil, nil, fmt.Errorf("failed creating CA certificate: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), keyPEM, nil
}

// certRequest describes a certificate to issue with issueCertificate.
type certRequest struct {
	commonName string
	dnsNames   []string
	usages     []x509.ExtKeyUsage
	// ttl is the requested validity, capped at the expiry of the CA.
	ttl time.Duration
	// now is when the certificate is issued, the current time when zero.
	now time.Time
}

// issueCertificate issues an ECDSA P-256 certificate for req, signed by the
// certificate authority held in the tls.crt and tls.key of the ca Secret, and
// returns it with its private key, both PEM encoded. The certificate is
// backdated by clockSkew, rounded down to the minute.
func issueCertificate(ca *corev1.Secret, req certRequest) (certPEM, keyPEM []byte, err error) {
	if req.now.IsZero() {
		req.now = time.Now()
	}
	block, _ := pem.Decode(ca.Data[corev1.TLSCertKey])
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, nil, errors.New("tls.crt of the certificate authority holds no certificate")
	}
	caCert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("failed parsing the certificate authority: %w", err)
	}
	caKey, err := parsePrivateKey(ca.Data[corev1.TLSPrivateKeyKey])
	if err != nil {
		return nil, nil, err
	}
	if !req.now.Before(caCert.NotAfter) {
		return nil, nil, fmt.Errorf("the certificate authority expired at %s", caCert.NotAfter.UTC().Format(time.RFC3339))
	}
	key, keyPEM, err := generateKey()
	if err != nil {
		return nil, nil, err
	}
	notBefore := req.now.Truncate(time.Minute).Add(-clockSkew)
	notAfter := notBefore.Add(req.ttl)
	if notAfter.After(caCert.NotAfter) {
		notAfter = caCert.NotAfter
	}
	template, err := certificateTemplate(req.commonName, notBefore, notAfter)
	if err != nil {
		return nil, nil, err
	}
	template.DNSNames = req.dnsNames
	template.KeyUsage = x509.KeyUsageDigitalSignature
	template.ExtKeyUsage = req.usages
	template.BasicConstraintsValid = true
	der, err := x509.CreateCertificate(rand.Reader, template, caCert, key.Public(), caKey)
	if err != nil {
		return nil, nil, fmt.Errorf("failed issuing certificate: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), keyPEM, nil
}

func certificateTemplate(commonName string, notBefore, notAfter time.Time) (*x509.Certificate, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("failed generating serial number: %w", err)
	}
	subject := certificateSubject
	subject.CommonName = commonName
	return &x509.Certificate{SerialNumber: serial, Subject: subject, NotBefore: notBefore, NotAfter: notAfter}, nil
}

// generateKey generates an ECDSA P-256 key and returns it with its PEM
// encoding.
func generateKey() (crypto.Signer, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("failed generating key: %w", err)
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("failed marshaling key: %w", err)
	}
	return key, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}), nil
}

// parsePrivateKey parses the first private key of keyPEM: SEC 1 EC, PKCS #1
// RSA or PKCS #8.
func parsePrivateKey(keyPEM []byte) (crypto.Signer, error) {
	for rest := keyPEM; ; {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			return nil, errors.New("tls.key of the certificate authority holds no private key")
		}
		switch block.Type {
		case "EC PRIVATE KEY":
			return x509.ParseECPrivateKey(block.Bytes)
		case "RSA PRIVATE KEY":
			return x509.ParsePKCS1PrivateKey(block.Bytes)
		case "PRIVATE KEY":
			key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
			if err != nil {
				return nil, err
			}
			signer, ok := key.(crypto.Signer)
			if !ok {
				return nil, fmt.Errorf("unsupported private key type %T", key)
			}
			return signer, nil
		}
	}
}
