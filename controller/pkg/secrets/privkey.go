package secrets

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
)

// KeyConfig is the configuration for generating a private key.
type KeyConfig struct {
	// Type is the type of the key to generate
	Type x509.PublicKeyAlgorithm

	// Size is the size of the key to generate in bits.
	// This is only used for RSA keys.
	Size int
}

// CreatePrivateKey generates a private key based on the provided keyConfig.
func CreatePrivateKey(
	keyConfig KeyConfig,
) (crypto.Signer, *pem.Block, x509.SignatureAlgorithm, error) {
	switch keyConfig.Type {

	case x509.ECDSA:
		ecdsa, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, nil, x509.ECDSAWithSHA256, err
		}
		privDer, err := x509.MarshalECPrivateKey(ecdsa)
		if err != nil {
			return nil, nil, x509.ECDSAWithSHA256, err
		}
		pemBlock := &pem.Block{
			Type:  "EC PRIVATE KEY",
			Bytes: privDer,
		}
		return ecdsa, pemBlock, x509.ECDSAWithSHA256, nil

	case x509.RSA:
		rsa, err := rsa.GenerateKey(rand.Reader, keyConfig.Size)
		if err != nil {
			return nil, nil, x509.SHA256WithRSA, err
		}
		pemBlock := &pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(rsa),
		}
		return rsa, pemBlock, x509.SHA256WithRSA, nil

	default:
		return nil, nil, x509.UnknownSignatureAlgorithm, fmt.Errorf("unsupported key type: %s", keyConfig.Type)
	}
}
