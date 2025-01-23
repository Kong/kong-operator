// This package serves to break the cyclic dependency between the manager and
// packages that import the config types.

package config

import (
	"fmt"
)

// KeyType is the type of private key.
type KeyType string

const (
	// ECDSA is the key type for ECDSA keys.
	ECDSA KeyType = "ecdsa"
	// RSA is the key type for RSA keys.
	RSA KeyType = "rsa"
)

// Set implements flag.Value.
func (kt *KeyType) Set(v string) error {
	switch v {
	case string(ECDSA), string(RSA):
		*kt = KeyType(v)
	case "":
		// Default to ECDSA.
		*kt = ECDSA
	default:
		return fmt.Errorf("invalid value %q for key type", v)
	}
	return nil
}

// String implements the String() method from flag.Value interface.
func (kt *KeyType) String() string {
	return string(*kt)
}
