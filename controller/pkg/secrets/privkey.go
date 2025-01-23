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
	var (
		signatureAlgorithm x509.SignatureAlgorithm = x509.UnknownSignatureAlgorithm
		priv               crypto.Signer
		pemBlock           *pem.Block
	)
	switch keyConfig.Type {
	case x509.ECDSA:
		signatureAlgorithm = x509.ECDSAWithSHA256
		ecdsa, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, nil, signatureAlgorithm, err
		}
		privDer, err := x509.MarshalECPrivateKey(ecdsa)
		if err != nil {
			return nil, nil, signatureAlgorithm, err
		}
		priv = ecdsa
		pemBlock = &pem.Block{
			Type:  "EC PRIVATE KEY",
			Bytes: privDer,
		}
	case x509.RSA:
		signatureAlgorithm = x509.SHA256WithRSA
		rsa, err := rsa.GenerateKey(rand.Reader, keyConfig.Size)
		if err != nil {
			return nil, nil, signatureAlgorithm, err
		}
		privDer := x509.MarshalPKCS1PrivateKey(rsa)
		priv = rsa
		pemBlock = &pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: privDer,
		}
	default:
		return nil, nil, signatureAlgorithm, fmt.Errorf("unsupported key type: %s", keyConfig.Type)
	}

	return priv, pemBlock, signatureAlgorithm, nil
}
