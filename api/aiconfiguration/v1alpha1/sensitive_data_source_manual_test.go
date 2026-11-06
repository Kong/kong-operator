package v1alpha1

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSensitiveDataSource_UnmarshalJSON guards backward compatibility with
// objects stored before a field became a SensitiveDataSource: a bare string
// must decode as an inline value, or the operator's caches fail to list those
// objects. See https://github.com/Kong/kong-operator/pull/6433.
func TestSensitiveDataSource_UnmarshalJSON(t *testing.T) {
	t.Parallel()

	t.Run("bare string decodes as inline", func(t *testing.T) {
		t.Parallel()

		var got SensitiveDataSource
		require.NoError(t, json.Unmarshal([]byte(`"legacy-password"`), &got))
		require.Equal(t, SensitiveDataSourceTypeInline, got.Type)
		require.Nil(t, got.SecretRef)
		require.Equal(t, "legacy-password", got.GetValue())
	})

	t.Run("object shape still decodes", func(t *testing.T) {
		t.Parallel()

		var got SensitiveDataSource
		require.NoError(t, json.Unmarshal(
			[]byte(`{"type":"secretRef","secretRef":{"name":"creds","key":"password"}}`),
			&got,
		))
		require.Equal(t, SensitiveDataSourceTypeSecretRef, got.Type)
		require.Nil(t, got.Value)
		require.NotNil(t, got.SecretRef)
		require.Equal(t, "creds", got.SecretRef.Name)
		require.Equal(t, "password", got.SecretRef.Key)
	})

	t.Run("legacy string inside a field decodes as inline", func(t *testing.T) {
		t.Parallel()

		var got AIGWOpenIDConnectGeneratedConfigRedis
		require.NoError(t, json.Unmarshal([]byte(`{"password":"legacy-password"}`), &got))
		require.Equal(t, "legacy-password", got.Password.GetValue())
	})
}
