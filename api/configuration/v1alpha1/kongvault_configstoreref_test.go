package v1alpha1_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
)

func configStoreRefScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, konnectv1alpha1.AddToScheme(s))
	require.NoError(t, configurationv1alpha1.AddToScheme(s))
	return s
}

func configStore(namespace, konnectID string) *konnectv1alpha1.KonnectConfigStore {
	cs := &konnectv1alpha1.KonnectConfigStore{
		ObjectMeta: metav1.ObjectMeta{Name: "tls-cert-keys", Namespace: namespace},
	}
	cs.Status.ID = konnectID
	return cs
}

func konnectVaultWithRef(ref *configurationv1alpha1.KonnectConfigStoreRef) *configurationv1alpha1.KongVault {
	return &configurationv1alpha1.KongVault{
		ObjectMeta: metav1.ObjectMeta{Name: "certvault"},
		Spec: configurationv1alpha1.KongVaultSpec{
			Backend:        "konnect",
			Prefix:         "certvault",
			ConfigStoreRef: ref,
		},
	}
}

func TestKongVaultResolveConfigStoreID(t *testing.T) {
	t.Parallel()

	ref := &configurationv1alpha1.KonnectConfigStoreRef{
		Name:      "tls-cert-keys",
		Namespace: "kong",
	}

	testCases := []struct {
		name        string
		vault       *configurationv1alpha1.KongVault
		objects     []client.Object
		expectedID  string
		assertError func(*testing.T, error)
	}{
		{
			name:       "no ref resolves to an empty ID",
			vault:      konnectVaultWithRef(nil),
			expectedID: "",
		},
		{
			name:       "no ref leaves config_store_id in spec.config alone",
			vault:      vaultWithRawConfig(t, nil, `{"config_store_id":"manually-copied-id"}`),
			expectedID: "",
		},
		{
			name:       "ref resolves to the Konnect ID of the referenced config store",
			vault:      konnectVaultWithRef(ref),
			objects:    []client.Object{configStore("kong", "konnect-config-store-id")},
			expectedID: "konnect-config-store-id",
		},
		{
			name:    "ref resolves alongside unrelated spec.config entries",
			vault:   vaultWithRawConfig(t, ref, `{"ttl":300}`),
			objects: []client.Object{configStore("kong", "konnect-config-store-id")},

			expectedID: "konnect-config-store-id",
		},
		{
			name:  "missing config store is reported as not found",
			vault: konnectVaultWithRef(ref),
			assertError: func(t *testing.T, err error) {
				var target configurationv1alpha1.ReferenceNotFoundError
				require.ErrorAs(t, err, &target)
				assert.Equal(t, "KonnectConfigStore", target.Kind)
				assert.Equal(t, "kong", target.Namespace)
				assert.Equal(t, "tls-cert-keys", target.Name)
			},
		},
		{
			name:    "config store without a Konnect ID is reported as not programmed",
			vault:   konnectVaultWithRef(ref),
			objects: []client.Object{configStore("kong", "")},
			assertError: func(t *testing.T, err error) {
				var target configurationv1alpha1.ReferenceNotProgrammedError
				require.ErrorAs(t, err, &target)
				assert.Equal(t, "KonnectConfigStore", target.Kind)
			},
		},
		{
			name:    "config store in another namespace is not resolved",
			vault:   konnectVaultWithRef(ref),
			objects: []client.Object{configStore("other", "konnect-config-store-id")},
			assertError: func(t *testing.T, err error) {
				var target configurationv1alpha1.ReferenceNotFoundError
				require.ErrorAs(t, err, &target)
			},
		},
		{
			name: "ref with a non-konnect backend is invalid",
			vault: func() *configurationv1alpha1.KongVault {
				v := konnectVaultWithRef(ref)
				v.Spec.Backend = "aws"
				return v
			}(),
			objects: []client.Object{configStore("kong", "konnect-config-store-id")},
			assertError: func(t *testing.T, err error) {
				var target configurationv1alpha1.ConfigStoreRefInvalidError
				require.ErrorAs(t, err, &target)
				assert.Contains(t, err.Error(), `spec.backend is "aws"`)
			},
		},
		{
			name:    "ref conflicting with config_store_id in spec.config is invalid",
			vault:   vaultWithRawConfig(t, ref, `{"config_store_id":"manually-copied-id"}`),
			objects: []client.Object{configStore("kong", "konnect-config-store-id")},
			assertError: func(t *testing.T, err error) {
				var target configurationv1alpha1.ConfigStoreRefInvalidError
				require.ErrorAs(t, err, &target)
				assert.Contains(t, err.Error(), "mutually exclusive")
			},
		},
		{
			name:    "ref with a malformed spec.config is invalid",
			vault:   vaultWithRawConfig(t, ref, `not-json`),
			objects: []client.Object{configStore("kong", "konnect-config-store-id")},
			assertError: func(t *testing.T, err error) {
				var target configurationv1alpha1.ConfigStoreRefInvalidError
				require.ErrorAs(t, err, &target)
				assert.Contains(t, err.Error(), "not a valid JSON object")
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cl := fake.NewClientBuilder().
				WithScheme(configStoreRefScheme(t)).
				WithObjects(tc.objects...).
				Build()

			id, err := tc.vault.ResolveConfigStoreID(t.Context(), cl)
			if tc.assertError != nil {
				require.Error(t, err)
				tc.assertError(t, err)
				assert.Empty(t, id, "no ID must be returned along with an error")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expectedID, id)
		})
	}
}

// vaultWithRawConfig returns a Konnect backed KongVault with the given raw
// spec.config JSON, which may be intentionally malformed.
func vaultWithRawConfig(t *testing.T, ref *configurationv1alpha1.KonnectConfigStoreRef, rawConfig string) *configurationv1alpha1.KongVault {
	t.Helper()
	v := konnectVaultWithRef(ref)
	v.Spec.Config = apiextensionsv1.JSON{Raw: []byte(rawConfig)}
	return v
}

func TestKongVaultGetConfigStoreRefNamespacedName(t *testing.T) {
	t.Parallel()

	t.Run("unset ref", func(t *testing.T) {
		t.Parallel()
		nn, ok := konnectVaultWithRef(nil).GetConfigStoreRefNamespacedName()
		assert.False(t, ok)
		assert.Empty(t, nn)
	})

	t.Run("set ref", func(t *testing.T) {
		t.Parallel()
		nn, ok := konnectVaultWithRef(&configurationv1alpha1.KonnectConfigStoreRef{
			Name:      "tls-cert-keys",
			Namespace: "kong",
		}).GetConfigStoreRefNamespacedName()
		require.True(t, ok)
		assert.Equal(t, "kong/tls-cert-keys", nn.String())
	})
}

func TestConfigStoreRefInvalidErrorIsMatchable(t *testing.T) {
	t.Parallel()

	// The reconciler distinguishes an invalid reference from a not-yet-resolvable
	// one by error type, so the error must stay matchable once wrapped.
	err := fmt.Errorf("wrapped: %w", configurationv1alpha1.ConfigStoreRefInvalidError{Reason: "because"})
	var target configurationv1alpha1.ConfigStoreRefInvalidError
	require.ErrorAs(t, err, &target)
	assert.Equal(t, "because", target.Reason)
}
