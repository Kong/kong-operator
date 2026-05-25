/*
Copyright 2025 Kong, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package dataplane

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	eventgatewayv1alpha1 "github.com/kong/kong-operator/v2/api/eventgateway/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// ensureCertificateSecret is a PoC stub: instead of issuing an mTLS cert from
// the cluster CA, it creates a Secret with a freshly generated self-signed
// cert+key. Good enough to get the keg pod running.
func (r *Reconciler) ensureCertificateSecret(
	ctx context.Context,
	egdp *eventgatewayv1alpha1.KegDataPlane,
) (op.Result, *corev1.Secret, error) {
	name := egdp.Name + "-keg-cert"

	existing := &corev1.Secret{}
	err := r.Client.Get(ctx, client.ObjectKey{Namespace: egdp.Namespace, Name: name}, existing)
	if err == nil {
		apimeta.SetStatusCondition(&egdp.Status.Conditions, metav1.Condition{
			Type:               string(eventgatewayv1alpha1.CertificateProvisionedType),
			Status:             metav1.ConditionTrue,
			Reason:             string(eventgatewayv1alpha1.CertificateProvisionedReason),
			Message:            "PoC self-signed mTLS Secret present",
			ObservedGeneration: egdp.Generation,
		})
		return op.Noop, existing, nil
	}
	if !apierrors.IsNotFound(err) {
		return op.Noop, nil, fmt.Errorf("failed to get cert Secret: %w", err)
	}

	certPEM, keyPEM, err := generateSelfSignedCert(fmt.Sprintf("%s.%s", egdp.Name, egdp.Namespace))
	if err != nil {
		return op.Noop, nil, fmt.Errorf("failed to generate self-signed cert: %w", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: egdp.Namespace,
			Labels: map[string]string{
				consts.SecretProvisioningLabelKey:         consts.SecretProvisioningAutomaticLabelValue,
				consts.SecretKEGDataPlaneCertificateLabel: "true",
			},
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			corev1.TLSCertKey:       certPEM,
			corev1.TLSPrivateKeyKey: keyPEM,
		},
	}
	k8sutils.SetOwnerForObject(secret, egdp)

	if err := r.Client.Create(ctx, secret); err != nil {
		apimeta.SetStatusCondition(&egdp.Status.Conditions, metav1.Condition{
			Type:               string(eventgatewayv1alpha1.CertificateProvisionedType),
			Status:             metav1.ConditionFalse,
			Reason:             string(eventgatewayv1alpha1.UnableToProvisionReason),
			Message:            fmt.Sprintf("failed to create PoC mTLS Secret: %v", err),
			ObservedGeneration: egdp.Generation,
		})
		return op.Noop, nil, err
	}

	apimeta.SetStatusCondition(&egdp.Status.Conditions, metav1.Condition{
		Type:               string(eventgatewayv1alpha1.CertificateProvisionedType),
		Status:             metav1.ConditionTrue,
		Reason:             string(eventgatewayv1alpha1.CertificateProvisionedReason),
		Message:            "PoC self-signed mTLS Secret created",
		ObservedGeneration: egdp.Generation,
	})
	return op.Created, secret, nil
}

func generateSelfSignedCert(commonName string) (certPEM, keyPEM []byte, err error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, err
	}
	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, nil, err
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM, nil
}
