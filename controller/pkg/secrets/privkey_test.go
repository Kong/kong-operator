package secrets

import (
	"crypto/x509"
	"encoding/pem"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mgrconfig "github.com/kong/gateway-operator/modules/manager/config"
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
				Type: mgrconfig.ECDSA,
			},
			expectErr:            false,
			expectedPemBlockType: "EC PRIVATE KEY",
		},
		{
			name: "Generate RSA key with size 2048",
			keyConfig: KeyConfig{
				Type: mgrconfig.RSA,
				Size: 2048,
			},
			expectErr:            false,
			expectedPemBlockType: "RSA PRIVATE KEY",
		},
		{
			name: "Unsupported key type",
			keyConfig: KeyConfig{
				Type: "unsupported",
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
				assert.Error(t, err)
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
