package secrets

import (
	"crypto/x509"
	"encoding/pem"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreatePrivateKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                 string
		keyConfig            KeyConfig
		expectErr            bool
		expectedPemBlockType string
	}{
		{
			name: "Generate ECDSA key",
			keyConfig: KeyConfig{
				Type: x509.ECDSA,
			},
			expectErr:            false,
			expectedPemBlockType: "EC PRIVATE KEY",
		},
		{
			name: "Generate RSA key with size 2048",
			keyConfig: KeyConfig{
				Type: x509.RSA,
				Size: 2048,
			},
			expectErr:            false,
			expectedPemBlockType: "RSA PRIVATE KEY",
		},
		{
			name: "Unsupported key type",
			keyConfig: KeyConfig{
				Type: x509.DSA,
			},
			expectErr: true,
		},
		{
			name:      "Empty key type",
			keyConfig: KeyConfig{},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			priv, pemBlock, sigAlg, err := CreatePrivateKey(tt.keyConfig)
			if tt.expectErr {
				require.Error(t, err)
				assert.Nil(t, priv)
				assert.Nil(t, pemBlock)
				assert.Equal(t, x509.UnknownSignatureAlgorithm, sigAlg)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, priv)
			require.NotNil(t, pemBlock)
			require.NotEqual(t, x509.UnknownSignatureAlgorithm, sigAlg)

			assert.Equal(t, tt.expectedPemBlockType, pemBlock.Type)

			// Check if PEM block can be parsed
			p, rest := pem.Decode(pem.EncodeToMemory(pemBlock))
			assert.Empty(t, rest)
			assert.NotNil(t, p)
		})
	}
}

func TestSignatureAlgorithmForKeyType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		keyType         x509.PublicKeyAlgorithm
		expectedSigAlgo x509.SignatureAlgorithm
	}{
		{
			name:            "ECDSA key type",
			keyType:         x509.ECDSA,
			expectedSigAlgo: SignatureAlgorithmForECDSA,
		},
		{
			name:            "RSA key type",
			keyType:         x509.RSA,
			expectedSigAlgo: SignatureAlgorithmForRSA,
		},
		{
			name:            "Unsupported key type",
			keyType:         x509.DSA,
			expectedSigAlgo: x509.UnknownSignatureAlgorithm,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sigAlgo := SignatureAlgorithmForKeyType(tt.keyType)
			assert.Equal(t, tt.expectedSigAlgo, sigAlgo)
		})
	}
}
