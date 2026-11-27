package manager

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/v2/controller/konnect/ops"
	mgrconfig "github.com/kong/kong-operator/v2/modules/manager/config"
)

const (
	// KonnectSyncHashKeySecretKey is the Secret data key holding the current
	// key used to compute keyed HMAC-SHA256 sync hashes.
	KonnectSyncHashKeySecretKey = "key"
	// KonnectSyncHashKeySecretPreviousKey is the optional Secret data key
	// holding the key that was current before a rotation. Hashes computed with
	// it are verified and rewritten with the current key without sending the
	// entities to Konnect again.
	KonnectSyncHashKeySecretPreviousKey = "previousKey"
)

// EnsureKonnectSyncHashKeys returns the keys used to compute keyed HMAC-SHA256
// hashes of secret-bearing Konnect entities persisted in their status.
//
// The keys are read from the Secret at key. When the Secret does not exist, it
// is created with a random key. Replicas racing to create it all end up using
// the Secret that was created first. An existing Secret is never overwritten:
// an invalid current key is reported as an error, an invalid previous key is
// ignored. The created Secret carries secretLabelSelector, like the cluster CA
// Secret, so the operator's Secret cache can see it.
func EnsureKonnectSyncHashKeys(
	ctx context.Context,
	log logr.Logger,
	c client.Client,
	key client.ObjectKey,
	secretLabelSelector string,
) (ops.SyncHashKeys, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var secret corev1.Secret
	err := c.Get(ctx, key, &secret)
	if apierrors.IsNotFound(err) {
		material, genErr := ops.GenerateSyncHashKeyMaterial()
		if genErr != nil {
			return ops.SyncHashKeys{}, genErr
		}
		var labels map[string]string
		if secretLabelSelector != "" {
			labels = map[string]string{
				secretLabelSelector: mgrconfig.LabelValueForSelectorInternal,
			}
		}
		secret = corev1.Secret{
			Name:      key.Name,
			Namespace: key.Namespace,
			Labels:    labels,
			Type:      corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				KonnectSyncHashKeySecretKey: material,
			},
		}
		err = c.Create(ctx, &secret)
		switch {
		case err == nil:
			log.Info("created Konnect sync hash key Secret", "namespace", key.Namespace, "name", key.Name)
		case apierrors.IsAlreadyExists(err):
			// Another replica created the Secret between our Get and Create.
			// Use its key instead of overwriting it.
			err = c.Get(ctx, key, &secret)
		}
	}
	if err != nil {
		return ops.SyncHashKeys{}, fmt.Errorf("failed ensuring Konnect sync hash key Secret %s: %w", key, err)
	}

	return syncHashKeysFromSecret(log, key, &secret)
}

func syncHashKeysFromSecret(log logr.Logger, key client.ObjectKey, secret *corev1.Secret) (ops.SyncHashKeys, error) {
	current, err := ops.NewSyncHashKey(secret.Data[KonnectSyncHashKeySecretKey])
	if err != nil {
		return ops.SyncHashKeys{}, fmt.Errorf(
			"invalid %q in Konnect sync hash key Secret %s: %w", KonnectSyncHashKeySecretKey, key, err,
		)
	}
	keys := ops.SyncHashKeys{Current: current}

	values := []any{"namespace", key.Namespace, "name", key.Name, "key_id", current.ID()}
	if material, ok := secret.Data[KonnectSyncHashKeySecretPreviousKey]; ok {
		previous, err := ops.NewSyncHashKey(material)
		if err != nil {
			log.Error(err, "ignoring invalid previous Konnect sync hash key: entities hashed with it are sent to Konnect once more",
				"namespace", key.Namespace, "name", key.Name, "data_key", KonnectSyncHashKeySecretPreviousKey,
			)
		} else if previous.ID() != current.ID() {
			keys.Previous = &previous
			values = append(values, "previous_key_id", previous.ID())
		}
	}
	log.Info("using Konnect sync hash key", values...)
	return keys, nil
}
