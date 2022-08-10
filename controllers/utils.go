package controllers

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"os"
	"reflect"
	"time"

	"github.com/cloudflare/cfssl/config"
	cflog "github.com/cloudflare/cfssl/log"
	"github.com/cloudflare/cfssl/signer"
	"github.com/cloudflare/cfssl/signer/local"
	"github.com/go-logr/logr"
	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	"github.com/kong/gateway-operator/internal/manager/logging"
)

// -----------------------------------------------------------------------------
// Private Vars
// -----------------------------------------------------------------------------

const requeueWithoutBackoff = time.Millisecond * 200

// -----------------------------------------------------------------------------
// Private Functions - Certificate management
// -----------------------------------------------------------------------------

// cfssl uses its own internal logger which will yeet unformatted messages to stderr unless overidden
type loggerShim struct {
	logger logr.Logger
}

func (l loggerShim) Debug(msg string)   { l.logger.V(logging.InfoLevel).Info(msg) }
func (l loggerShim) Info(msg string)    { l.logger.V(logging.InfoLevel).Info(msg) }
func (l loggerShim) Warning(msg string) { l.logger.V(logging.InfoLevel).Info(msg) }
func (l loggerShim) Err(msg string)     { l.logger.V(logging.InfoLevel).Info(msg) }
func (l loggerShim) Crit(msg string)    { l.logger.V(logging.InfoLevel).Info(msg) }
func (l loggerShim) Emerg(msg string)   { l.logger.V(logging.InfoLevel).Info(msg) }

/*
Adapted from the Kubernetes CFSSL signer:
https://github.com/kubernetes/kubernetes/blob/v1.16.15/pkg/controller/certificates/signer/cfssl_signer.go
Modified to handle requests entirely in memory instead of via a controller watching for CertificateSigningRequests
in the API.


Copyright 2016 The Kubernetes Authors.
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

// signCertificate takes a CertificateSigningRequest and a TLS Secret and returns a PEM x.509 certificate
// signed by the certificate in the Secret.
func signCertificate(csr certificatesv1.CertificateSigningRequest, ca *corev1.Secret, logger logr.Logger) ([]byte, error) {
	caKeyBlock, _ := pem.Decode(ca.Data["tls.key"])
	caCertBlock, _ := pem.Decode(ca.Data["tls.crt"])
	priv, err := x509.ParseECPrivateKey(caKeyBlock.Bytes)
	if err != nil {
		return nil, err
	}
	caCert, err := x509.ParseCertificate(caCertBlock.Bytes)
	if err != nil {
		return nil, err
	}

	var usages []string
	for _, usage := range csr.Spec.Usages {
		usages = append(usages, string(usage))
	}

	certExpiryDuration := time.Second * time.Duration(*csr.Spec.ExpirationSeconds)
	durationUntilExpiry := caCert.NotAfter.Sub(time.Now()) //nolint:gosimple
	if durationUntilExpiry <= 0 {
		return nil, fmt.Errorf("the signer has expired: %v", caCert.NotAfter)
	}
	if durationUntilExpiry < certExpiryDuration {
		certExpiryDuration = durationUntilExpiry
	}

	policy := &config.Signing{
		Default: &config.SigningProfile{
			Usage:        usages,
			Expiry:       certExpiryDuration,
			ExpiryString: certExpiryDuration.String(),
		},
	}
	cflog.SetLogger(loggerShim{logger: logger})
	cfs, err := local.NewSigner(priv, caCert, x509.ECDSAWithSHA256, policy)
	if err != nil {
		return nil, err
	}

	certBytes, err := cfs.Sign(signer.SignRequest{Request: string(csr.Spec.Request)})
	if err != nil {
		return nil, err
	}
	return certBytes, nil
}

// maybeCreateCertificateSecret creates a namespace/name Secret for suject signed by the CA in the mtlsCASecretName
// Secret, or does nothing if a namespace/name Secret is already present. It returns a boolean indicating if it
// created a Secret and an error indicating any failures it encountered.
func maybeCreateCertificateSecret(ctx context.Context, subject, namespace, name, mtlsCASecretName string,
	usages []certificatesv1.KeyUsage, k8sClient client.Client,
) (bool, error) {
	logger := log.FromContext(ctx).WithName("MTLSCertificateCreation")
	// check for existing
	cert := &corev1.Secret{}
	err := k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, cert)
	if err == nil {
		return false, nil
	}

	template := x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName:   subject,
			Organization: []string{"Kong, Inc."},
			Country:      []string{"US"},
		},
		SignatureAlgorithm: x509.ECDSAWithSHA256,
		DNSNames:           []string{subject},
	}

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return false, err
	}

	der, err := x509.CreateCertificateRequest(rand.Reader, &template, priv)
	if err != nil {
		return false, err
	}

	// This is effectively a placeholder so long as we handle signing internally. When actually creating CSR resources,
	// this string is used by signers to filter which resources they pay attention to
	signerName := "gateway-operator.konghq.com/mtls"
	// TODO This creates certificates that last for 10 years as an arbitrarily long period for the alpha. A production-
	// ready implementation should use a shorter lifetime and rotate certificates. Rotation requires some mechanism to
	// recognize that certificates have expired (ideally without permissions to read Secrets across the cluster) and
	// to get Deployments to acknowledge them. For Kong, this requires a restart, as there's no way to force a reload
	// of updated files on disk.
	expiration := int32(315400000)

	csr := certificatesv1.CertificateSigningRequest{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: certificatesv1.CertificateSigningRequestSpec{
			Request: pem.EncodeToMemory(&pem.Block{
				Type:  "CERTIFICATE REQUEST",
				Bytes: der,
			}),
			SignerName:        signerName,
			ExpirationSeconds: &expiration,
			Usages:            usages,
		},
	}

	ca := &corev1.Secret{}
	err = k8sClient.Get(ctx, client.ObjectKey{Namespace: os.Getenv("POD_NAMESPACE"), Name: mtlsCASecretName}, ca)
	if err != nil {
		return false, err
	}

	signed, err := signCertificate(csr, ca, logger)
	if err != nil {
		return false, err
	}
	privDer, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return false, err
	}

	signedSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Type: corev1.SecretTypeTLS,
		StringData: map[string]string{
			"ca.crt":  string(ca.Data["tls.crt"]),
			"tls.crt": string(signed),
			"tls.key": string(pem.EncodeToMemory(&pem.Block{
				Type:  "EC PRIVATE KEY",
				Bytes: privDer,
			})),
		},
	}
	err = k8sClient.Create(ctx, signedSecret)
	if err != nil {
		return false, err
	}

	return true, nil
}

// -----------------------------------------------------------------------------
// Private Functions - Logging
// -----------------------------------------------------------------------------

func info(log logr.Logger, msg string, rawOBJ interface{}, keysAndValues ...interface{}) {
	if obj, ok := rawOBJ.(client.Object); ok {
		kvs := append([]interface{}{"namespace", obj.GetNamespace(), "name", obj.GetName()}, keysAndValues...)
		log.V(logging.InfoLevel).Info(msg, kvs...)
	} else if req, ok := rawOBJ.(reconcile.Request); ok {
		kvs := append([]interface{}{"namespace", req.Namespace, "name", req.Name}, keysAndValues...)
		log.V(logging.InfoLevel).Info(msg, kvs...)
	} else {
		log.V(logging.InfoLevel).Info(fmt.Sprintf("unexpected type processed for info logging: %T, this is a bug!", rawOBJ))
	}
}

func debug(log logr.Logger, msg string, rawOBJ interface{}, keysAndValues ...interface{}) {
	if obj, ok := rawOBJ.(client.Object); ok {
		kvs := append([]interface{}{"namespace", obj.GetNamespace(), "name", obj.GetName()}, keysAndValues...)
		log.V(logging.DebugLevel).Info(msg, kvs...)
	} else if req, ok := rawOBJ.(reconcile.Request); ok {
		kvs := append([]interface{}{"namespace", req.Namespace, "name", req.Name}, keysAndValues...)
		log.V(logging.DebugLevel).Info(msg, kvs...)
	} else {
		log.V(logging.DebugLevel).Info(fmt.Sprintf("unexpected type processed for debug logging: %T, this is a bug!", rawOBJ))
	}
}

// -----------------------------------------------------------------------------
// DeploymentOptions - Private Functions - Equality Checks
// -----------------------------------------------------------------------------

func deploymentOptionsDeepEqual(opts1, opts2 *operatorv1alpha1.DeploymentOptions) bool {
	if !reflect.DeepEqual(opts1.ContainerImage, opts2.ContainerImage) {
		return false
	}

	if !reflect.DeepEqual(opts1.Version, opts2.Version) {
		return false
	}

	if !reflect.DeepEqual(opts1.Env, opts2.Env) {
		return false
	}

	if !reflect.DeepEqual(opts1.EnvFrom, opts2.EnvFrom) {
		return false
	}

	return true
}
