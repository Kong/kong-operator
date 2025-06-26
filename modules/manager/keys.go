package manager

import (
	"crypto/x509"
	"fmt"

	mgrconfig "github.com/kong/kong-operator/modules/manager/config"
)

// KeyTypeToX509PublicKeyAlgorithm converts a KeyType to an x509.PublicKeyAlgorithm.
func KeyTypeToX509PublicKeyAlgorithm(keyType mgrconfig.KeyType) (x509.PublicKeyAlgorithm, error) {
	switch keyType {
	case mgrconfig.RSA:
		return x509.RSA, nil
	case mgrconfig.ECDSA:
		return x509.ECDSA, nil
	default:
		return x509.UnknownPublicKeyAlgorithm, fmt.Errorf("unsupported key type: %s", keyType)
	}
}
