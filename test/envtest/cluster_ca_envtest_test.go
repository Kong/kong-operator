package envtest

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/v2/controller/pkg/secrets"
	kgomanager "github.com/kong/kong-operator/v2/modules/manager"
)

func TestEnsureClusterCASecret(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	scheme := Scheme(t)
	envcfg, ns := Setup(t, ctx, scheme, WithInstallKongCRDs(false))
	cl, err := client.New(envcfg, client.Options{Scheme: scheme})
	require.NoError(t, err)

	keyFor := func(name string) client.ObjectKey {
		return client.ObjectKey{Namespace: ns.Name, Name: name}
	}

	t.Run("creates a missing secret", func(t *testing.T) {
		key := keyFor("missing-ca")

		require.NoError(t, kgomanager.EnsureClusterCASecret(ctx, logr.Discard(), cl, key, time.Hour))

		var ca corev1.Secret
		require.NoError(t, cl.Get(ctx, key, &ca))
		require.Equal(t, corev1.SecretTypeTLS, ca.Type)
		require.Equal(t, "internal", ca.Labels["konghq.com/secret"])
		require.True(t, secrets.IsTLSSecretValid(&ca))

		block, _ := pem.Decode(ca.Data[corev1.TLSCertKey])
		require.NotNil(t, block)
		cert, err := x509.ParseCertificate(block.Bytes)
		require.NoError(t, err)
		require.True(t, cert.IsCA)
		require.Equal(t, "Kong Operator CA", cert.Subject.CommonName)
		require.WithinDuration(t, time.Now().Add(time.Hour), cert.NotAfter, 2*time.Minute)
	})

	t.Run("keeps an existing secret unchanged", func(t *testing.T) {
		key := keyFor("existing-ca")
		require.NoError(t, kgomanager.EnsureClusterCASecret(ctx, logr.Discard(), cl, key, time.Hour))

		var before corev1.Secret
		require.NoError(t, cl.Get(ctx, key, &before))

		require.NoError(t, kgomanager.EnsureClusterCASecret(ctx, logr.Discard(), cl, key, time.Hour))

		var after corev1.Secret
		require.NoError(t, cl.Get(ctx, key, &after))
		require.Equal(t, before.Data, after.Data)
	})

	t.Run("rejects an invalid existing secret without overwriting it", func(t *testing.T) {
		key := keyFor("invalid-ca")
		invalid := corev1.Secret{
			Name: key.Name, Namespace: key.Namespace,
			Data: map[string][]byte{corev1.TLSCertKey: []byte("not a certificate")},
		}
		require.NoError(t, cl.Create(ctx, &invalid))

		err := kgomanager.EnsureClusterCASecret(ctx, logr.Discard(), cl, key, time.Hour)
		require.ErrorContains(t, err, "is invalid")

		var after corev1.Secret
		require.NoError(t, cl.Get(ctx, key, &after))
		require.Equal(t, invalid.Data, after.Data)
	})

	t.Run("uses a secret created concurrently", func(t *testing.T) {
		key := keyFor("concurrent-ca")
		concurrentClient := alreadyExistsClient{Client: cl}

		require.NoError(t, kgomanager.EnsureClusterCASecret(ctx, logr.Discard(), concurrentClient, key, time.Hour))

		var ca corev1.Secret
		require.NoError(t, cl.Get(ctx, key, &ca))
		require.True(t, secrets.IsTLSSecretValid(&ca))
	})
}

// alreadyExistsClient creates the object but reports AlreadyExists, as when
// another operator replica wins the race between Get and Create.
type alreadyExistsClient struct {
	client.Client
}

func (c alreadyExistsClient) Create(
	ctx context.Context, obj client.Object, opts ...client.CreateOption,
) error {
	if err := c.Client.Create(ctx, obj, opts...); err != nil {
		return err
	}
	return apierrors.NewAlreadyExists(corev1.Resource("secrets"), obj.GetName())
}
