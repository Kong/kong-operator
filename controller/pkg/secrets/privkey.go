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

const (
	// SignatureAlgorithmForECDSA is the default signature algorithm for ECDSA keys.
	SignatureAlgorithmForECDSA x509.SignatureAlgorithm = x509.ECDSAWithSHA256
	// SignatureAlgorithmForRSA is the default signature algorithm for RSA keys.
	SignatureAlgorithmForRSA x509.SignatureAlgorithm = x509.SHA256WithRSA
)

// SignatureAlgorithmForKeyType returns the default signature algorithm for the provided key type.
func SignatureAlgorithmForKeyType(keyType x509.PublicKeyAlgorithm) x509.SignatureAlgorithm {
	switch keyType {
	case x509.ECDSA:
		return SignatureAlgorithmForECDSA
	case x509.RSA:
		return SignatureAlgorithmForRSA
	default:
		return x509.UnknownSignatureAlgorithm
	}
}

// CreatePrivateKey generates a private key based on the provided keyConfig.
func CreatePrivateKey(
	keyConfig KeyConfig,
) (crypto.Signer, *pem.Block, x509.SignatureAlgorithm, error) {
	switch keyConfig.Type {

	case x509.ECDSA:
		ecdsa, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, nil, SignatureAlgorithmForECDSA, err
		}
		privDer, err := x509.MarshalECPrivateKey(ecdsa)
		if err != nil {
			return nil, nil, SignatureAlgorithmForECDSA, err
		}
		pemBlock := &pem.Block{
			Type:  "EC PRIVATE KEY",
			Bytes: privDer,
		}
		return ecdsa, pemBlock, SignatureAlgorithmForECDSA, nil

	case x509.RSA:
		rsa, err := rsa.GenerateKey(rand.Reader, keyConfig.Size)
		if err != nil {
			return nil, nil, SignatureAlgorithmForRSA, err
		}
		pemBlock := &pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(rsa),
		}
		return rsa, pemBlock, SignatureAlgorithmForRSA, nil

	default:
		return nil, nil, x509.UnknownSignatureAlgorithm, fmt.Errorf("unsupported key type: %s", keyConfig.Type)
	}
}

// ParseKey parses a private key from a PEM block based on the provided keyType.
func ParseKey(
	keyType x509.PublicKeyAlgorithm,
	pemBlock *pem.Block,
) (crypto.Signer, error) {
	switch keyType {
	case x509.ECDSA:
		return x509.ParseECPrivateKey(pemBlock.Bytes)
	case x509.RSA:
		// RSA can be in PKCS1 or PKCS8 format so let's try both.
		key, err := x509.ParsePKCS1PrivateKey(pemBlock.Bytes)
		if err == nil {
			return key, nil
		}
		pkcs8Key, err := x509.ParsePKCS8PrivateKey(pemBlock.Bytes)
		if err != nil {
			return nil, err
		}
		if rsaKey, ok := pkcs8Key.(*rsa.PrivateKey); ok {
			return rsaKey, nil
		}
		return nil, fmt.Errorf("parsed PKCS8 key is not an RSA private key")
	default:
		return nil, fmt.Errorf("unsupported key type: %v", keyType)
	}
}
