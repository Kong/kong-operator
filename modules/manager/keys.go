package manager

import (
	"crypto/x509"
	"fmt"

	mgrconfig "github.com/kong/gateway-operator/modules/manager/config"
)

func keyTypeToX509PublicKeyAlgorithm(keyType mgrconfig.KeyType) (x509.PublicKeyAlgorithm, error) {
	switch keyType {
	case mgrconfig.RSA:
		return x509.RSA, nil
	case mgrconfig.ECDSA:
		return x509.ECDSA, nil
	default:
		return x509.UnknownPublicKeyAlgorithm, fmt.Errorf("unsupported key type: %s", keyType)
	}
}
