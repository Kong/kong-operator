package ops

import (
	"fmt"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/Kong/sdk-konnect-go/test/mocks"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

func newVaultForTest(t *testing.T, name, cpID, storeID, prefix string) *configurationv1alpha1.KongVault {
	t.Helper()
	return &configurationv1alpha1.KongVault{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: configurationv1alpha1.KongVaultSpec{
			Backend: configurationv1alpha1.KongVaultBackendKonnect,
			Prefix:  prefix,
			Config:  apiextensionsv1.JSON{Raw: fmt.Appendf(nil, `{"config_store_id":%q}`, storeID)},
		},
		Status: configurationv1alpha1.KongVaultStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
				ControlPlaneID: cpID,
			},
		},
	}
}

func TestVaultSecretKeyName(t *testing.T) {
	// Namespace "foo-bar"/name "baz" and namespace "foo"/name "bar-baz" would
	// collide under a naive "<namespace>-<name>" join, since both produce
	// "foo-bar-baz". The length prefixes must keep them distinct.
	a := vaultSecretKeyName(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "foo-bar", Name: "baz"}})
	b := vaultSecretKeyName(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "foo", Name: "bar-baz"}})
	require.NotEqual(t, a, b)
}

func TestPushKeyToVault(t *testing.T) {
	ctx := t.Context()
	scheme := runtime.NewScheme()
	require.NoError(t, configurationv1alpha1.AddToScheme(scheme))

	unmarkedSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "plain-tls", Namespace: "ns"},
	}

	markedSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-cert-tls",
			Namespace: "ns",
			Annotations: map[string]string{
				consts.VaultSecretAnnotation: "certvault",
			},
		},
	}
	const markedSecretKeyName = "k8s-2-ns-11-my-cert-tls-tls-key"

	t.Run("unmarked secret is a no-op", func(t *testing.T) {
		vaultRef, status, marked, err := pushKeyToVault(ctx, fake.NewClientBuilder().WithScheme(scheme).Build(), nil, unmarkedSecret, "the-real-key")
		require.NoError(t, err)
		require.False(t, marked)
		require.Empty(t, vaultRef)
		require.Nil(t, status)
	})

	t.Run("referenced KongVault does not exist", func(t *testing.T) {
		vaultRef, status, marked, err := pushKeyToVault(ctx, fake.NewClientBuilder().WithScheme(scheme).Build(), nil, markedSecret, "the-real-key")
		require.Error(t, err)
		require.Contains(t, err.Error(), `KongVault "certvault"`)
		require.True(t, marked)
		require.Empty(t, vaultRef)
		require.Nil(t, status)
	})

	t.Run("creates the secret when it does not exist yet", func(t *testing.T) {
		vault := newVaultForTest(t, "certvault", "cp-1", "store-1", "certvault")
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(vault).Build()
		sdk := mocks.NewMockConfigStoreSecretsSDK(t)
		sdk.EXPECT().GetConfigStoreSecret(mock.Anything, sdkkonnectops.GetConfigStoreSecretRequest{
			ControlPlaneID: "cp-1",
			ConfigStoreID:  "store-1",
			Key:            markedSecretKeyName,
		}).Return(nil, &sdkkonnecterrs.NotFoundError{})
		sdk.EXPECT().CreateConfigStoreSecret(mock.Anything, sdkkonnectops.CreateConfigStoreSecretRequest{
			ControlPlaneID: "cp-1",
			ConfigStoreID:  "store-1",
			CreateConfigStoreSecret: sdkkonnectcomp.CreateConfigStoreSecret{
				Key:   markedSecretKeyName,
				Value: "the-real-key",
			},
		}).Return(&sdkkonnectops.CreateConfigStoreSecretResponse{}, nil)

		vaultRef, status, marked, err := pushKeyToVault(ctx, cl, sdk, markedSecret, "the-real-key")
		require.NoError(t, err)
		require.True(t, marked)
		require.Equal(t, fmt.Sprintf("{vault://certvault/%s}", markedSecretKeyName), vaultRef)
		require.Equal(t, &configurationv1alpha1.KongCertificateVaultSecretStatus{
			ControlPlaneID: "cp-1",
			ConfigStoreID:  "store-1",
			Key:            markedSecretKeyName,
		}, status)
	})

	t.Run("updates the secret when it already exists", func(t *testing.T) {
		vault := newVaultForTest(t, "certvault", "cp-1", "store-1", "certvault")
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(vault).Build()
		sdk := mocks.NewMockConfigStoreSecretsSDK(t)
		sdk.EXPECT().GetConfigStoreSecret(mock.Anything, sdkkonnectops.GetConfigStoreSecretRequest{
			ControlPlaneID: "cp-1",
			ConfigStoreID:  "store-1",
			Key:            markedSecretKeyName,
		}).Return(&sdkkonnectops.GetConfigStoreSecretResponse{}, nil)
		sdk.EXPECT().UpdateConfigStoreSecret(mock.Anything, sdkkonnectops.UpdateConfigStoreSecretRequest{
			ControlPlaneID: "cp-1",
			ConfigStoreID:  "store-1",
			Key:            markedSecretKeyName,
			UpdateConfigStoreSecret: sdkkonnectcomp.UpdateConfigStoreSecret{
				Value: "the-rotated-key",
			},
		}).Return(&sdkkonnectops.UpdateConfigStoreSecretResponse{}, nil)

		vaultRef, status, marked, err := pushKeyToVault(ctx, cl, sdk, markedSecret, "the-rotated-key")
		require.NoError(t, err)
		require.True(t, marked)
		require.Equal(t, fmt.Sprintf("{vault://certvault/%s}", markedSecretKeyName), vaultRef)
		require.Equal(t, "store-1", status.ConfigStoreID)
	})

	t.Run("vault not yet programmed", func(t *testing.T) {
		vault := newVaultForTest(t, "certvault", "", "store-1", "certvault")
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(vault).Build()

		_, status, marked, err := pushKeyToVault(ctx, cl, nil, markedSecret, "the-real-key")
		require.Error(t, err)
		require.Contains(t, err.Error(), "not programmed")
		require.True(t, marked)
		require.Nil(t, status)
	})
}

func TestDeleteVaultSecret(t *testing.T) {
	ctx := t.Context()
	vs := &configurationv1alpha1.KongCertificateVaultSecretStatus{
		ControlPlaneID: "cp-1",
		ConfigStoreID:  "store-1",
		Key:            "k8s-2-ns-11-my-cert-tls-tls-key",
	}

	t.Run("deletes the secret", func(t *testing.T) {
		sdk := mocks.NewMockConfigStoreSecretsSDK(t)
		sdk.EXPECT().DeleteConfigStoreSecret(mock.Anything, sdkkonnectops.DeleteConfigStoreSecretRequest{
			ControlPlaneID: "cp-1",
			ConfigStoreID:  "store-1",
			Key:            vs.Key,
		}).Return(&sdkkonnectops.DeleteConfigStoreSecretResponse{}, nil)

		require.NoError(t, deleteVaultSecret(ctx, sdk, vs))
	})

	t.Run("already deleted is not an error", func(t *testing.T) {
		sdk := mocks.NewMockConfigStoreSecretsSDK(t)
		sdk.EXPECT().DeleteConfigStoreSecret(mock.Anything, mock.Anything).Return(nil, &sdkkonnecterrs.NotFoundError{})

		require.NoError(t, deleteVaultSecret(ctx, sdk, vs))
	})

	t.Run("propagates other errors", func(t *testing.T) {
		sdk := mocks.NewMockConfigStoreSecretsSDK(t)
		sdk.EXPECT().DeleteConfigStoreSecret(mock.Anything, mock.Anything).Return(nil, fmt.Errorf("konnect API error"))

		require.Error(t, deleteVaultSecret(ctx, sdk, vs))
	})
}
