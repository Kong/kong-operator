package secrets

import (
	"context"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math"
	"math/big"
	"time"

	"github.com/avast/retry-go/v4"
	"github.com/go-logr/logr"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CreateClusterCACertificate creates a cluster CA certificate Secret.
func CreateClusterCACertificate(ctx context.Context, logger logr.Logger, cl client.Client, secretNN types.NamespacedName, secretLabels map[string]string, keyConfig KeyConfig) error {
	serial, err := rand.Int(rand.Reader, big.NewInt(math.MaxInt64))
	if err != nil {
		return err
	}

	priv, pemBlock, signatureAlgorithm, err := CreatePrivateKey(keyConfig)
	if err != nil {
		return err
	}

	template := x509.Certificate{
		Subject: pkix.Name{
			CommonName:   "Kong Operator CA",
			Organization: []string{"Kong, Inc."},
			Country:      []string{"US"},
		},
		SerialNumber:          serial,
		SignatureAlgorithm:    signatureAlgorithm,
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Second * 315400000),
		KeyUsage:              x509.KeyUsageCertSign + x509.KeyUsageKeyEncipherment + x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	der, err := x509.CreateCertificate(rand.Reader, &template, &template, priv.Public(), priv)
	if err != nil {
		return err
	}

	signedSecret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: secretNN.Namespace,
			Name:      secretNN.Name,
			Labels:    secretLabels,
		},
		Type: v1.SecretTypeTLS,
		StringData: map[string]string{
			"tls.crt": string(pem.EncodeToMemory(&pem.Block{
				Type:  "CERTIFICATE",
				Bytes: der,
			})),

			"tls.key": string(pem.EncodeToMemory(pemBlock)),
		},
	}
	if err := retry.Do(
		func() error {
			err := cl.Create(ctx, signedSecret)
			if err != nil {
				if errS := (&k8serrors.StatusError{}); errors.As(err, &errS) {
					if errS.ErrStatus.Code == 409 &&
						errS.ErrStatus.Reason == metav1.StatusReasonAlreadyExists {
						// If it's a 409 status code then the Secret already exists.
						return nil
					}
				}
				return err
			}
			return nil
		},
		retry.Context(ctx),
		retry.Attempts(0),
		retry.MaxDelay(3*time.Second),
		retry.DelayType(retry.BackOffDelay),
		retry.LastErrorOnly(true),
		retry.OnRetry(func(n uint, err error) {
			logger.Info(
				"failed to create CA cluster secret for MTLs communication with Kong Gateway, retrying...",
				"error", err,
			)
		}),
	); err != nil {
		return err
	}
	return nil
}
