package secrets

import (
	"context"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math"
	"math/big"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CreateClusterCACertificate creates a cluster CA certificate Secret.
func CreateClusterCACertificate(ctx context.Context, cl client.Client, secretNN types.NamespacedName, secretLabels map[string]string, keyConfig KeyConfig) error {
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
	return cl.Create(ctx, signedSecret)
}
