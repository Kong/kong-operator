package secrets

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/cloudflare/cfssl/config"
	cflog "github.com/cloudflare/cfssl/log"
	"github.com/cloudflare/cfssl/signer"
	"github.com/cloudflare/cfssl/signer/local"
	"github.com/go-logr/logr"
	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/kong/kong-operator/controller/pkg/dataplane"
	"github.com/kong/kong-operator/controller/pkg/op"
	"github.com/kong/kong-operator/modules/manager/logging"
	"github.com/kong/kong-operator/pkg/consts"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
	k8sreduce "github.com/kong/kong-operator/pkg/utils/kubernetes/reduce"
	k8sresources "github.com/kong/kong-operator/pkg/utils/kubernetes/resources"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
)

var caLoggerInit sync.Once

// SetCALogger sets the logger for the CFSSL signer. Call it once at the start
// of the program to ensure that CFSSL logs are captured by the operator's logger.
// Subsequent calls to this function will have no effect.
func SetCALogger(logger logr.Logger) {
	caLoggerInit.Do(func() {
		cflog.SetLogger(loggerShim{logger: logger.WithName("cfssl")})
	})
}

// -----------------------------------------------------------------------------
// Private Functions - Certificate management
// -----------------------------------------------------------------------------

// cfssl uses its own internal logger which will yet unformatted messages to stderr unless overridden.
type loggerShim struct {
	logger logr.Logger
}

// Debug logs on debug level.
func (l loggerShim) Debug(msg string) { l.logger.V(logging.DebugLevel.Value()).Info(msg) }

// Info logs on info level.
func (l loggerShim) Info(msg string) { l.logger.V(logging.DebugLevel.Value()).Info(msg) }

// Warning logs on warning level.
func (l loggerShim) Warning(msg string) { l.logger.V(logging.InfoLevel.Value()).Info(msg) }

// Err logs on error level.
func (l loggerShim) Err(msg string) { l.logger.V(logging.InfoLevel.Value()).Info(msg) }

// Crit logs on critical level.
func (l loggerShim) Crit(msg string) { l.logger.V(logging.InfoLevel.Value()).Info(msg) }

// Emerg logs on emergency level.
func (l loggerShim) Emerg(msg string) { l.logger.V(logging.InfoLevel.Value()).Info(msg) }

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
func signCertificate(
	csr certificatesv1.CertificateSigningRequest,
	ca *corev1.Secret,
) ([]byte, error) {
	caCertBlock, _ := pem.Decode(ca.Data["tls.crt"])
	if caCertBlock == nil {
		return nil, fmt.Errorf("failed decoding 'tls.crt' data from secret %s", ca.Name)
	}
	caCert, err := x509.ParseCertificate(caCertBlock.Bytes)
	if err != nil {
		return nil, err
	}

	usages := make([]string, 0, len(csr.Spec.Usages))
	for _, usage := range csr.Spec.Usages {
		usages = append(usages, string(usage))
	}

	certExpiryDuration := time.Second * time.Duration(*csr.Spec.ExpirationSeconds)
	durationUntilExpiry := time.Until(caCert.NotAfter)
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

	caKeyBlock, _ := pem.Decode(ca.Data["tls.key"])
	if caKeyBlock == nil {
		return nil, fmt.Errorf("failed decoding 'tls.key' data from secret %s", ca.Name)
	}

	priv, signatureAlgorithm, err := ParsePrivateKey(caKeyBlock)
	if err != nil {
		return nil, err
	}

	cfs, err := local.NewSigner(priv, caCert, signatureAlgorithm, policy)
	if err != nil {
		return nil, err
	}

	certBytes, err := cfs.Sign(signer.SignRequest{Request: string(csr.Spec.Request)})
	if err != nil {
		return nil, err
	}
	return certBytes, nil
}

// IsTLSSecretValid checks if a Secret contains a valid TLS certificate and key.
func IsTLSSecretValid(secret *corev1.Secret) bool {
	var ok bool
	var crt, key []byte
	if crt, ok = secret.Data["tls.crt"]; !ok {
		return false
	}
	if key, ok = secret.Data["tls.key"]; !ok {
		return false
	}
	if p, _ := pem.Decode(crt); p == nil {
		return false
	}
	if p, _ := pem.Decode(key); p == nil {
		return false
	}
	return true
}

// EnsureCertificate creates a namespace/name Secret for subject signed by the CA in the
// mtlsCASecretNamespace/mtlsCASecretName Secret, or does nothing if a namespace/name Secret is
// already present. It returns a boolean indicating if it created a Secret and an error indicating
// any failures it encountered.
func EnsureCertificate[
	T interface {
		k8sresources.ControlPlaneOrDataPlaneOrKonnectExtension
		client.Object
	},
](
	ctx context.Context,
	owner T,
	subject string,
	mtlsCASecretNN types.NamespacedName,
	usages []certificatesv1.KeyUsage,
	keyConfig KeyConfig,
	cl client.Client,
	additionalMatchingLabels client.MatchingLabels,
) (op.Result, *corev1.Secret, error) {
	// Get the Secrets for the DataPlane using new labels.
	matchingLabels := k8sresources.GetManagedLabelForOwner(owner)
	maps.Copy(matchingLabels, additionalMatchingLabels)

	secrets, err := k8sutils.ListSecretsForOwner(ctx, cl, owner.GetUID(), matchingLabels)
	if err != nil {
		return op.Noop, nil, fmt.Errorf("failed listing Secrets for %T %s/%s: %w", owner, owner.GetNamespace(), owner.GetName(), err)
	}

	count := len(secrets)
	if count > 1 {
		if err := k8sreduce.ReduceSecrets(ctx, cl, secrets, getPreDeleteHooks(owner)...); err != nil {
			return op.Noop, nil, err
		}
		return op.Noop, nil, errors.New("number of secrets reduced")
	}

	secretOpts := append(getSecretOpts(owner), matchingLabelsToSecretOpt(matchingLabels))
	generatedSecret := k8sresources.GenerateNewTLSSecret(owner, secretOpts...)

	// If there are no secrets yet, then create one.
	if count == 0 {
		return generateTLSDataSecret(ctx, generatedSecret, owner, subject, mtlsCASecretNN, usages, keyConfig, cl)
	}

	// Otherwise there is already 1 certificate matching specified selectors.
	existingSecret := &secrets[0]

	block, _ := pem.Decode(existingSecret.Data["tls.crt"])
	if block == nil {
		// The existing secret has a broken certificate, delete it and recreate it.
		if err := cl.Delete(ctx, existingSecret); err != nil {
			return op.Noop, nil, err
		}

		return generateTLSDataSecret(ctx, generatedSecret, owner, subject, mtlsCASecretNN, usages, keyConfig, cl)
	}

	// Check if existing certificate is for a different subject.
	// If that's the case, delete the old certificate and create a new one.
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return op.Noop, nil, err
	}
	if cert.Subject.CommonName != subject {
		if err := cl.Delete(ctx, existingSecret); err != nil {
			return op.Noop, nil, err
		}

		return generateTLSDataSecret(ctx, generatedSecret, owner, subject, mtlsCASecretNN, usages, keyConfig, cl)
	}

	var updated bool
	updated, existingSecret.ObjectMeta = k8sutils.EnsureObjectMetaIsUpdated(existingSecret.ObjectMeta, generatedSecret.ObjectMeta)
	if updated {
		if err := cl.Update(ctx, existingSecret); err != nil {
			return op.Noop, existingSecret, fmt.Errorf("failed updating secret %s: %w", existingSecret.Name, err)
		}
		return op.Updated, existingSecret, nil
	}
	return op.Noop, existingSecret, nil
}

func matchingLabelsToSecretOpt(ml client.MatchingLabels) k8sresources.SecretOpt {
	return func(a *corev1.Secret) {
		if a.Labels == nil {
			a.Labels = make(map[string]string)
		}
		maps.Copy(a.Labels, ml)
	}
}

// getPreDeleteHooks returns a list of pre-delete hooks for the given object type.
func getPreDeleteHooks[T interface {
	k8sresources.ControlPlaneOrDataPlaneOrKonnectExtension
	client.Object
},
](obj T,
) []k8sreduce.PreDeleteHook {
	switch any(obj).(type) {
	case *operatorv1beta1.DataPlane:
		return []k8sreduce.PreDeleteHook{dataplane.OwnedObjectPreDeleteHook}
	default:
		return nil
	}
}

// getSecretOpts returns a list of SecretOpt for the given object type.
func getSecretOpts[T interface {
	k8sresources.ControlPlaneOrDataPlaneOrKonnectExtension
	client.Object
},
](obj T,
) []k8sresources.SecretOpt {
	switch any(obj).(type) {
	case *operatorv1beta1.DataPlane:
		withDataPlaneOwnedFinalizer := func(s *corev1.Secret) {
			controllerutil.AddFinalizer(s, consts.DataPlaneOwnedWaitForOwnerFinalizer)
		}
		return []k8sresources.SecretOpt{withDataPlaneOwnedFinalizer}
	default:
		return nil
	}
}

// generateTLSDataSecret generates a TLS certificate data, fills the provided secret with
// that data and creates it using the k8s client.
// It returns a boolean indicating whether the secret has been created, the secret
// itself and an error.
func generateTLSDataSecret(
	ctx context.Context,
	generatedSecret *corev1.Secret,
	owner client.Object,
	subject string,
	mtlsCASecret types.NamespacedName,
	usages []certificatesv1.KeyUsage,
	keyConfig KeyConfig,
	k8sClient client.Client,
) (op.Result, *corev1.Secret, error) {
	priv, pemBlock, signatureAlgorithm, err := CreatePrivateKey(keyConfig)
	if err != nil {
		return op.Noop, nil, err
	}

	template := x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName:   subject,
			Organization: []string{"Kong, Inc."},
			Country:      []string{"US"},
		},
		SignatureAlgorithm: signatureAlgorithm,
		DNSNames:           []string{subject},
	}

	der, err := x509.CreateCertificateRequest(rand.Reader, &template, priv)
	if err != nil {
		return op.Noop, nil, err
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
			Namespace: owner.GetNamespace(),
			Name:      owner.GetName(),
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

	var ca corev1.Secret
	err = k8sClient.Get(ctx, mtlsCASecret, &ca)
	if err != nil {
		return op.Noop, nil, err
	}

	signed, err := signCertificate(csr, &ca)
	if err != nil {
		return op.Noop, nil, err
	}

	generatedSecret.Data = map[string][]byte{
		"ca.crt":  ca.Data["tls.crt"],
		"tls.crt": signed,
		"tls.key": pem.EncodeToMemory(pemBlock),
	}

	err = k8sClient.Create(ctx, generatedSecret)
	if err != nil {
		return op.Noop, nil, err
	}

	return op.Created, generatedSecret, nil
}

// GetManagedLabelForServiceSecret returns a label selector for the ServiceSecret.
func GetManagedLabelForServiceSecret(svcNN types.NamespacedName) client.MatchingLabels {
	return client.MatchingLabels{
		consts.ServiceSecretLabel: svcNN.Name,
	}
}

// -----------------------------------------------------------------------------
// Private Functions - Container Images
// -----------------------------------------------------------------------------

// ensureContainerImageUpdated ensures that the provided container is
// configured with a container image consistent with the image and
// image version provided. The image and version can be provided as
// nil when not wanted.
func ensureContainerImageUpdated(container *corev1.Container, imageVersionStr string) (updated bool, err error) {
	// Can't update a with an empty image.
	if imageVersionStr == "" {
		return false, fmt.Errorf("can't update container image with an empty image")
	}

	imageParts := strings.Split(container.Image, ":")
	if len(imageParts) > 3 {
		return false, fmt.Errorf("invalid container image found: %s", container.Image)
	}

	// This is a special case for registries that specify a non default port,
	// e.g. localhost:5000 or myregistry.io:8000. We do look for '/' since the
	// container.Image will contain it as a separator between the registry+image
	// and the version.
	if len(imageParts) == 3 {
		if !strings.Contains(container.Image, "/") {
			return false, fmt.Errorf("invalid container image found: %s", container.Image)
		}

		containerImageURL := imageParts[0] + imageParts[1]
		u, err := url.Parse(containerImageURL)
		if err != nil {
			return false, fmt.Errorf("invalid registry URL %s: %w", containerImageURL, err)
		}
		containerImageURL = u.String()
		container.Image = containerImageURL + ":" + imageParts[2]
		updated = true
	}

	if imageVersionStr != container.Image {
		container.Image = imageVersionStr
		updated = true
	}

	return updated, nil
}

// ParsePrivateKey parses a PEM block and returns a crypto.Signer and x509.SignatureAlgorithm.
func ParsePrivateKey(pemBlock *pem.Block) (crypto.Signer, x509.SignatureAlgorithm, error) {
	var (
		signatureAlgorithm = x509.UnknownSignatureAlgorithm
		priv               crypto.Signer
		err                error
	)
	switch pemBlock.Type {

	case "EC PRIVATE KEY", "ECDSA PRIVATE KEY":
		priv, err = x509.ParseECPrivateKey(pemBlock.Bytes)
		if err != nil {
			return nil, signatureAlgorithm, err
		}
		return priv, x509.ECDSAWithSHA256, nil

	case "RSA PRIVATE KEY":
		priv, err = x509.ParsePKCS1PrivateKey(pemBlock.Bytes)
		if err != nil {
			return nil, signatureAlgorithm, err
		}
		return priv, x509.SHA256WithRSA, nil

	default:
		return nil, signatureAlgorithm, fmt.Errorf("unsupported key type: %s", pemBlock.Type)
	}
}
