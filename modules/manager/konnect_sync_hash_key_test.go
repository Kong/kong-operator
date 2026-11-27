package manager

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/kong/kong-operator/v2/controller/konnect/ops"
	mgrconfig "github.com/kong/kong-operator/v2/modules/manager/config"
)

func TestEnsureKonnectSyncHashKeys(t *testing.T) {
	key := client.ObjectKey{Namespace: "kong-system", Name: "sync-hash-key"}
	material := func(fill byte) []byte {
		return bytes.Repeat([]byte{fill}, ops.SyncHashKeyMinLength)
	}
	secretWith := func(data map[string][]byte) *corev1.Secret {
		return &corev1.Secret{
			Namespace: key.Namespace,
			Name:      key.Name,
			Data:      data,
		}
	}
	keyID := func(t *testing.T, b []byte) string {
		k, err := ops.NewSyncHashKey(b)
		require.NoError(t, err)
		return k.ID()
	}

	t.Run("creates a missing Secret with a random key", func(t *testing.T) {
		cl := fake.NewClientBuilder().Build()

		keys, err := EnsureKonnectSyncHashKeys(t.Context(), logr.Discard(), cl, key, mgrconfig.DefaultSecretLabelSelector)
		require.NoError(t, err)
		assert.Nil(t, keys.Previous)

		var secret corev1.Secret
		require.NoError(t, cl.Get(t.Context(), key, &secret))
		assert.Equal(t, corev1.SecretTypeOpaque, secret.Type)
		assert.Equal(t, mgrconfig.LabelValueForSelectorInternal, secret.Labels[mgrconfig.DefaultSecretLabelSelector])
		require.Len(t, secret.Data[KonnectSyncHashKeySecretKey], ops.SyncHashKeyMinLength)
		assert.Equal(t, keyID(t, secret.Data[KonnectSyncHashKeySecretKey]), keys.Current.ID())

		// A second call, e.g. after a restart, keeps using the same key.
		again, err := EnsureKonnectSyncHashKeys(t.Context(), logr.Discard(), cl, key, mgrconfig.DefaultSecretLabelSelector)
		require.NoError(t, err)
		assert.Equal(t, keys.Current.ID(), again.Current.ID())
	})

	t.Run("empty label selector leaves the Secret unlabelled", func(t *testing.T) {
		cl := fake.NewClientBuilder().Build()
		_, err := EnsureKonnectSyncHashKeys(t.Context(), logr.Discard(), cl, key, "")
		require.NoError(t, err)
		var secret corev1.Secret
		require.NoError(t, cl.Get(t.Context(), key, &secret))
		assert.Empty(t, secret.Labels)
	})

	t.Run("uses the key of a Secret created concurrently by another replica", func(t *testing.T) {
		winner := secretWith(map[string][]byte{KonnectSyncHashKeySecretKey: material(7)})
		cl := fake.NewClientBuilder().WithInterceptorFuncs(interceptor.Funcs{
			Create: func(ctx context.Context, c client.WithWatch, obj client.Object, _ ...client.CreateOption) error {
				// Another replica wins the race between our Get and Create.
				require.NoError(t, c.Create(ctx, winner.DeepCopy()))
				return apierrors.NewAlreadyExists(schema.GroupResource{Resource: "secrets"}, obj.GetName())
			},
		}).Build()

		keys, err := EnsureKonnectSyncHashKeys(t.Context(), logr.Discard(), cl, key, mgrconfig.DefaultSecretLabelSelector)
		require.NoError(t, err)
		assert.Equal(t, keyID(t, material(7)), keys.Current.ID())
	})

	t.Run("reads the current and previous keys of an existing Secret", func(t *testing.T) {
		cl := fake.NewClientBuilder().WithObjects(secretWith(map[string][]byte{
			KonnectSyncHashKeySecretKey:         material(1),
			KonnectSyncHashKeySecretPreviousKey: material(2),
		})).Build()

		keys, err := EnsureKonnectSyncHashKeys(t.Context(), logr.Discard(), cl, key, mgrconfig.DefaultSecretLabelSelector)
		require.NoError(t, err)
		assert.Equal(t, keyID(t, material(1)), keys.Current.ID())
		require.NotNil(t, keys.Previous)
		assert.Equal(t, keyID(t, material(2)), keys.Previous.ID())
	})

	t.Run("ignores an invalid or duplicate previous key", func(t *testing.T) {
		for name, previous := range map[string][]byte{
			"too short": []byte("short"),
			"duplicate": material(1),
		} {
			t.Run(name, func(t *testing.T) {
				cl := fake.NewClientBuilder().WithObjects(secretWith(map[string][]byte{
					KonnectSyncHashKeySecretKey:         material(1),
					KonnectSyncHashKeySecretPreviousKey: previous,
				})).Build()

				keys, err := EnsureKonnectSyncHashKeys(t.Context(), logr.Discard(), cl, key, mgrconfig.DefaultSecretLabelSelector)
				require.NoError(t, err)
				assert.Equal(t, keyID(t, material(1)), keys.Current.ID())
				assert.Nil(t, keys.Previous)
			})
		}
	})

	t.Run("does not overwrite an existing Secret with an invalid key", func(t *testing.T) {
		for name, data := range map[string]map[string][]byte{
			"missing key":   {"other": material(1)},
			"too short key": {KonnectSyncHashKeySecretKey: []byte("short")},
		} {
			t.Run(name, func(t *testing.T) {
				cl := fake.NewClientBuilder().WithObjects(secretWith(data)).Build()

				_, err := EnsureKonnectSyncHashKeys(t.Context(), logr.Discard(), cl, key, mgrconfig.DefaultSecretLabelSelector)
				require.Error(t, err)

				var secret corev1.Secret
				require.NoError(t, cl.Get(t.Context(), key, &secret))
				assert.Equal(t, data, secret.Data, "an existing Secret must never be overwritten")
			})
		}
	})

	t.Run("reports API errors", func(t *testing.T) {
		cl := fake.NewClientBuilder().WithInterceptorFuncs(interceptor.Funcs{
			Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
				return apierrors.NewForbidden(schema.GroupResource{Resource: "secrets"}, key.Name, errors.New("denied"))
			},
		}).Build()

		_, err := EnsureKonnectSyncHashKeys(t.Context(), logr.Discard(), cl, key, mgrconfig.DefaultSecretLabelSelector)
		require.Error(t, err)
		assert.True(t, apierrors.IsForbidden(err))
	})
}
