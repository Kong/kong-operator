package license

import (
	"context"
	"fmt"
	"maps"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/labels"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/license"
)

const (
	// licenseResourceNamePrefix is the prefix of the secret name storing the konnect license.
	licenseResourceNamePrefix = "konnect-license-"
	// secretKeyPayload is the key to store the payload of the license in the secret.
	secretKeyPayload = "payload"
	// secretKeyID is the key to store the ID of the license.
	secretKeyID = "id"
	// secretKeyUpdatedAt is the key to store updated time of the license.
	secretKeyUpdatedAt = "updated_at"
)

// Storer is used to store license fetched from Konnect or to load it from said storage.
type Storer interface {
	Store(context.Context, license.KonnectLicense) error
	Load(context.Context) (license.KonnectLicense, error)
}

// SecretLicenseStore is the storage used to store the Konnect license. This store uses
// the CP ID, a predefined prefix and the provided namespace to designate the target Secret
// which will be used for storage.
type SecretLicenseStore struct {
	cl                  client.Client
	namespace           string
	controlPlaneID      string
	secretLabelSelector map[string]string
}

var _ Storer = &SecretLicenseStore{}

//+kubebuilder:rbac:groups="",resources=secrets,verbs=create;get;update

// NewSecretLicenseStore creates a storage to store Konnect license to a secret.
func NewSecretLicenseStore(
	cl client.Client,
	namespace, controlPlaneID string,
	secretLabelSelector map[string]string,
) *SecretLicenseStore {
	return &SecretLicenseStore{
		cl:                  cl,
		namespace:           namespace,
		controlPlaneID:      controlPlaneID,
		secretLabelSelector: secretLabelSelector,
	}
}

// Store stores license to the secret `konnect-license-<cpid>`.
func (s *SecretLicenseStore) Store(ctx context.Context, l license.KonnectLicense) error {
	var secret corev1.Secret
	err := s.cl.Get(ctx, k8stypes.NamespacedName{
		Namespace: s.namespace,
		Name:      licenseResourceNamePrefix + s.controlPlaneID,
	}, &secret)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return err
		}
		// Create the secret in case that the secret is not found.
		secret.Name = licenseResourceNamePrefix + s.controlPlaneID
		secret.Namespace = s.namespace
		s.setSecretLabels(&secret)
		secret.StringData = licenseToSecretData(l)

		if err := s.cl.Create(ctx, &secret); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				return err
			}
			toDelete := secret.DeepCopy()
			// Attempt to delete the existing secret.
			// This is a fallback mechanism to enforce for example the secret label
			// selector in the Secret. When selector is configured operator can't
			// perform an update on the Secret because the existing Secret doesn't
			// match the selector, but after deletion and creation it will be
			// recreated with the correct labels.
			if err := s.cl.Delete(ctx, toDelete); err != nil {
				return fmt.Errorf("failed to delete existing secret when creating license secret: %w", err)
			}
			return s.cl.Create(ctx, &secret)
		}
		return nil
	}

	s.setSecretLabels(&secret)
	secret.StringData = licenseToSecretData(l)
	return s.cl.Update(ctx, &secret)
}

// Load loads the license from the secret from secret `konnect-license-<cpid>`.
func (s *SecretLicenseStore) Load(
	ctx context.Context,
) (license.KonnectLicense, error) {
	secret := &corev1.Secret{}
	err := s.cl.Get(ctx, k8stypes.NamespacedName{
		Namespace: s.namespace,
		Name:      licenseResourceNamePrefix + s.controlPlaneID,
	}, secret)
	if err != nil {
		return license.KonnectLicense{}, err
	}

	return konnectLicenseFromSecret(secret)
}

func (s *SecretLicenseStore) setSecretLabels(secret *corev1.Secret) {
	if secret == nil {
		return
	}

	if secret.Labels == nil {
		secret.Labels = map[string]string{}
	}
	// Add label to mark that the secret is managed by KIC.
	secret.Labels[labels.ManagedByLabel] = labels.ManagedByLabelValueIngressController
	maps.Copy(secret.Labels, s.secretLabelSelector)
}

var requiredSecretKeys = []string{secretKeyPayload, secretKeyID, secretKeyUpdatedAt}

func konnectLicenseFromSecret(secret *corev1.Secret) (license.KonnectLicense, error) {
	if secret == nil || secret.Data == nil {
		return license.KonnectLicense{},
			fmt.Errorf("secret %s doesn't contain data", secret.Name)
	}

	missingKeys := []string{}
	for _, key := range requiredSecretKeys {
		if v, ok := secret.Data[key]; !ok || len(v) == 0 {
			missingKeys = append(missingKeys, key)
		}
	}
	if len(missingKeys) > 0 {
		return license.KonnectLicense{},
			fmt.Errorf(
				"missing required key(s): %s in secret %s",
				strings.Join(missingKeys, ", "), secret.Name,
			)
	}

	payload := string(secret.Data[secretKeyPayload])
	decodedID := string(secret.Data[secretKeyID])
	decodedUpdateAt := string(secret.Data[secretKeyUpdatedAt])
	updateAt, err := strconv.ParseInt(decodedUpdateAt, 10, 64)
	if err != nil {
		return license.KonnectLicense{},
			fmt.Errorf(
				"failed to parse updated_at as timestamp of license stored in secret %s: %w",
				secret.Name, err,
			)
	}
	return license.KonnectLicense{
		Payload:   payload,
		UpdatedAt: time.Unix(updateAt, 0),
		ID:        decodedID,
	}, nil
}

func licenseToSecretData(l license.KonnectLicense) map[string]string {
	return map[string]string{
		secretKeyPayload:   l.Payload,
		secretKeyUpdatedAt: strconv.FormatInt(l.UpdatedAt.Unix(), 10),
		secretKeyID:        l.ID,
	}
}
