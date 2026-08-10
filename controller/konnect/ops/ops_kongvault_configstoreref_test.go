package ops

import (
	"net/http"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/Kong/sdk-konnect-go/test/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
)

func configStoreRefScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, konnectv1alpha1.AddToScheme(scheme))
	return scheme
}

// programmedConfigStore builds a KonnectConfigStore that already carries a Konnect
// ID, i.e. a reference target that resolves successfully.
func programmedConfigStore(name, namespace, konnectID string) *konnectv1alpha1.KonnectConfigStore {
	cs := &konnectv1alpha1.KonnectConfigStore{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
	}
	cs.Status.ID = konnectID
	return cs
}

// konnectBackedVault builds a Konnect backed KongVault attached to a Control Plane,
// with the given raw spec.config (empty means spec.config is unset).
func konnectBackedVault(rawConfig string, ref *configurationv1alpha1.KonnectConfigStoreRef) *configurationv1alpha1.KongVault {
	v := &configurationv1alpha1.KongVault{
		ObjectMeta: metav1.ObjectMeta{Name: "certvault"},
		Spec: configurationv1alpha1.KongVaultSpec{
			Backend:        "konnect",
			Prefix:         "certvault",
			ConfigStoreRef: ref,
		},
		Status: configurationv1alpha1.KongVaultStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
				ControlPlaneID: "cp-id",
			},
		},
	}
	if rawConfig != "" {
		v.Spec.Config = apiextensionsv1.JSON{Raw: []byte(rawConfig)}
	}
	return v
}

func TestKongVaultToVaultInputConfigStoreRef(t *testing.T) {
	ref := &configurationv1alpha1.KonnectConfigStoreRef{
		Name:      "tls-cert-keys",
		Namespace: "kong",
	}

	testCases := []struct {
		name           string
		vault          *configurationv1alpha1.KongVault
		objects        []client.Object
		expectedConfig map[string]any
		expectedErr    string
	}{
		{
			name:           "configStoreRef alone produces config_store_id",
			vault:          konnectBackedVault("", ref),
			objects:        []client.Object{programmedConfigStore("tls-cert-keys", "kong", "cs-id")},
			expectedConfig: map[string]any{"config_store_id": "cs-id"},
		},
		{
			name:           "configStoreRef is merged into the existing config",
			vault:          konnectBackedVault(`{"ttl":300}`, ref),
			objects:        []client.Object{programmedConfigStore("tls-cert-keys", "kong", "cs-id")},
			expectedConfig: map[string]any{"config_store_id": "cs-id", "ttl": float64(300)},
		},
		{
			name:           "config_store_id set directly in spec.config is passed through unchanged",
			vault:          konnectBackedVault(`{"config_store_id":"manually-copied-id"}`, nil),
			expectedConfig: map[string]any{"config_store_id": "manually-copied-id"},
		},
		{
			name:           "an unset config produces an empty config",
			vault:          konnectBackedVault("", nil),
			expectedConfig: map[string]any{},
		},
		{
			name:        "an unresolvable configStoreRef fails the conversion",
			vault:       konnectBackedVault("", ref),
			expectedErr: "failed to resolve spec.configStoreRef",
		},
		{
			name:           "a resolved configStoreRef is reused from the reconciliation context",
			vault:          konnectBackedVault("", ref),
			expectedConfig: map[string]any{"config_store_id": "resolved-id"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cl := fake.NewClientBuilder().
				WithScheme(configStoreRefScheme(t)).
				WithObjects(tc.objects...).
				Build()

			ctx := t.Context()
			if tc.name == "a resolved configStoreRef is reused from the reconciliation context" {
				ctx = WithResolvedConfigStoreID(ctx, "resolved-id")
			}
			input, err := kongVaultToVaultInput(ctx, cl, tc.vault)
			if tc.expectedErr != "" {
				require.ErrorContains(t, err, tc.expectedErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expectedConfig, input.Config)
			assert.Equal(t, "konnect", input.Name)
			assert.Equal(t, "certvault", input.Prefix)
		})
	}
}

func TestCreateKongVaultWithConfigStoreRef(t *testing.T) {
	vault := konnectBackedVault("", &configurationv1alpha1.KonnectConfigStoreRef{
		Name:      "tls-cert-keys",
		Namespace: "kong",
	})
	cl := fake.NewClientBuilder().
		WithScheme(configStoreRefScheme(t)).
		WithObjects(programmedConfigStore("tls-cert-keys", "kong", "cs-id")).
		Build()

	sdk := mocks.NewMockVaultsSDK(t)
	// Assert on the payload directly rather than through the converter, so that a
	// change dropping config_store_id from the request is caught.
	sdk.EXPECT().
		CreateVault(mock.Anything, "cp-id", mock.MatchedBy(func(v any) bool {
			input, ok := v.(sdkkonnectcomp.Vault)
			return ok && input.Config["config_store_id"] == "cs-id"
		})).
		Return(&sdkkonnectops.CreateVaultResponse{
			Vault:      &sdkkonnectcomp.Vault{ID: new("vault-id")},
			StatusCode: http.StatusCreated,
		}, nil)

	require.NoError(t, createVault(t.Context(), cl, sdk, vault))
	assert.Equal(t, "vault-id", vault.GetKonnectStatus().GetKonnectID())
}

func TestCreateKongVaultWithUnresolvableConfigStoreRef(t *testing.T) {
	vault := konnectBackedVault("", &configurationv1alpha1.KonnectConfigStoreRef{
		Name:      "tls-cert-keys",
		Namespace: "kong",
	})
	cl := fake.NewClientBuilder().WithScheme(configStoreRefScheme(t)).Build()

	// No CreateVault expectation is registered: the mock fails the test if the
	// vault is pushed to Konnect without its Config Store ID resolved.
	sdk := mocks.NewMockVaultsSDK(t)

	err := createVault(t.Context(), cl, sdk, vault)
	require.ErrorContains(t, err, "failed to resolve spec.configStoreRef")
	assert.Empty(t, vault.GetKonnectStatus().GetKonnectID())
}
