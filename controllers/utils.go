package controllers

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"net/url"
	"reflect"
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
	ctrlruntimelog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	"github.com/kong/gateway-operator/internal/consts"
	"github.com/kong/gateway-operator/internal/manager/logging"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
	k8sreduce "github.com/kong/gateway-operator/internal/utils/kubernetes/reduce"
	k8sresources "github.com/kong/gateway-operator/internal/utils/kubernetes/resources"
)

// -----------------------------------------------------------------------------
// Private Consts
// -----------------------------------------------------------------------------

const requeueWithoutBackoff = time.Millisecond * 200

// -----------------------------------------------------------------------------
// Private Functions - Certificate management
// -----------------------------------------------------------------------------

// cfssl uses its own internal logger which will yeet unformatted messages to stderr unless overidden
type loggerShim struct {
	logger logr.Logger
}

func (l loggerShim) Debug(msg string)   { l.logger.V(logging.DebugLevel.Value()).Info(msg) }
func (l loggerShim) Info(msg string)    { l.logger.V(logging.DebugLevel.Value()).Info(msg) }
func (l loggerShim) Warning(msg string) { l.logger.V(logging.InfoLevel.Value()).Info(msg) }
func (l loggerShim) Err(msg string)     { l.logger.V(logging.InfoLevel.Value()).Info(msg) }
func (l loggerShim) Crit(msg string)    { l.logger.V(logging.InfoLevel.Value()).Info(msg) }
func (l loggerShim) Emerg(msg string)   { l.logger.V(logging.InfoLevel.Value()).Info(msg) }

var caLoggerInit sync.Once

func setCALogger(logger logr.Logger) {
	caLoggerInit.Do(func() {
		cflog.SetLogger(loggerShim{logger: logger})
	})
}

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
func signCertificate(csr certificatesv1.CertificateSigningRequest, ca *corev1.Secret) ([]byte, error) {
	caKeyBlock, _ := pem.Decode(ca.Data["tls.key"])
	if caKeyBlock == nil {
		return nil, fmt.Errorf("failed decoding 'tls.key' data from secret %s", ca.Name)
	}
	priv, err := x509.ParseECPrivateKey(caKeyBlock.Bytes)
	if err != nil {
		return nil, err
	}

	caCertBlock, _ := pem.Decode(ca.Data["tls.crt"])
	if caCertBlock == nil {
		return nil, fmt.Errorf("failed decoding 'tls.crt' data from secret %s", ca.Name)
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

// maybeCreateCertificateSecret creates a namespace/name Secret for subject signed by the CA in the
// mtlsCASecretNamespace/mtlsCASecretName Secret, or does nothing if a namespace/name Secret is
// already present. It returns a boolean indicating if it created a Secret and an error indicating
// any failures it encountered.
func maybeCreateCertificateSecret(
	ctx context.Context,
	owner client.Object,
	subject string,
	mtlsCASecretNN types.NamespacedName,
	usages []certificatesv1.KeyUsage,
	k8sClient client.Client,
) (bool, *corev1.Secret, error) {
	setCALogger(ctrlruntimelog.Log)

	selectorKey, selectorValue := getManagedLabelForOwner(owner)
	secrets, err := k8sutils.ListSecretsForOwner(
		ctx,
		k8sClient,
		selectorKey,
		selectorValue,
		owner.GetUID(),
	)
	if err != nil {
		return false, nil, err
	}

	count := len(secrets)
	if count > 1 {
		if err := k8sreduce.ReduceSecrets(ctx, k8sClient, secrets); err != nil {
			return false, nil, err
		}
		return false, nil, errors.New("number of secrets reduced")
	}

	ownerPrefix := getPrefixForOwner(owner)
	generatedSecret := k8sresources.GenerateNewTLSSecret(owner.GetNamespace(), owner.GetName(), ownerPrefix)
	k8sutils.SetOwnerForObject(generatedSecret, owner)
	addLabelForOwner(generatedSecret, owner)

	// If there are no secrets yet, then create one.
	if count == 0 {
		return generateTLSDataSecret(ctx, generatedSecret, owner, subject, mtlsCASecretNN, usages, k8sClient)
	}

	// Otherwise there is already 1 certificate matching specified selectors.
	existingSecret := &secrets[0]

	// Check if existing certificate is for a different subject.
	// If that's the case, delete the old certificate and create a new one.
	block, _ := pem.Decode(existingSecret.Data["tls.crt"])
	if block == nil {
		// The existing secret has a broken certificate, delete it and recreate it.
		if err := k8sClient.Delete(ctx, existingSecret); err != nil {
			return false, nil, err
		}

		return generateTLSDataSecret(ctx, generatedSecret, owner, subject, mtlsCASecretNN, usages, k8sClient)
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return false, nil, err
	}
	if cert.Subject.CommonName != subject {
		if err := k8sClient.Delete(ctx, existingSecret); err != nil {
			return false, nil, err
		}

		return generateTLSDataSecret(ctx, generatedSecret, owner, subject, mtlsCASecretNN, usages, k8sClient)
	}

	var updated bool
	updated, existingSecret.ObjectMeta = k8sutils.EnsureObjectMetaIsUpdated(existingSecret.ObjectMeta, generatedSecret.ObjectMeta)
	if updated {
		return true, existingSecret, k8sClient.Update(ctx, existingSecret)
	}
	return false, existingSecret, nil
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
	k8sClient client.Client,
) (bool, *corev1.Secret, error) {
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
		return false, nil, err
	}

	der, err := x509.CreateCertificateRequest(rand.Reader, &template, priv)
	if err != nil {
		return false, nil, err
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

	ca := &corev1.Secret{}
	err = k8sClient.Get(ctx, mtlsCASecret, ca)
	if err != nil {
		return false, nil, err
	}

	signed, err := signCertificate(csr, ca)
	if err != nil {
		return false, nil, err
	}
	privDer, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return false, nil, err
	}

	generatedSecret.StringData = map[string]string{
		"ca.crt":  string(ca.Data["tls.crt"]),
		"tls.crt": string(signed),
		"tls.key": string(pem.EncodeToMemory(&pem.Block{
			Type:  "EC PRIVATE KEY",
			Bytes: privDer,
		})),
	}

	err = k8sClient.Create(ctx, generatedSecret)
	if err != nil {
		return false, nil, err
	}

	return true, generatedSecret, nil
}

// -----------------------------------------------------------------------------
// Private Functions - Logging
// -----------------------------------------------------------------------------

func info[T any](log logr.Logger, msg string, rawObj T, keysAndValues ...interface{}) {
	_log(log, logging.InfoLevel, msg, rawObj, keysAndValues...)
}

func debug[T any](log logr.Logger, msg string, rawObj T, keysAndValues ...interface{}) {
	_log(log, logging.DebugLevel, msg, rawObj, keysAndValues...)
}

func trace[T any](log logr.Logger, msg string, rawObj T, keysAndValues ...interface{}) {
	_log(log, logging.TraceLevel, msg, rawObj, keysAndValues...)
}

type nameNamespacer interface {
	GetName() string
	GetNamespace() string
}

func keyValuesFromObj[T any](rawObj T) []interface{} {
	if obj, ok := any(rawObj).(nameNamespacer); ok {
		return []interface{}{
			"namespace", obj.GetNamespace(),
			"name", obj.GetName(),
		}
	} else if obj, ok := any((&rawObj)).(nameNamespacer); ok {
		return []interface{}{
			"namespace", obj.GetNamespace(),
			"name", obj.GetName(),
		}
	} else if req, ok := any(rawObj).(reconcile.Request); ok {
		return []interface{}{
			"namespace", req.Namespace,
			"name", req.Name,
		}
	}

	return nil
}

func _log[T any](log logr.Logger, level logging.Level, msg string, rawObj T, keysAndValues ...interface{}) {
	kvs := keyValuesFromObj(rawObj)
	if kvs == nil {
		log.V(level.Value()).Info(
			fmt.Sprintf("unexpected type processed for %s logging: %T, this is a bug!",
				level.String(), rawObj,
			),
		)
		return
	}

	log.V(level.Value()).Info(msg, append(kvs, keysAndValues...)...)
}

func getLogger(ctx context.Context, controllerName string, developmentMode bool) logr.Logger {
	// if development mode is active, do not add the context to the log line, as we want
	// to have a lighter logging structure
	if developmentMode {
		return ctrlruntimelog.Log.WithName(controllerName)
	}
	return ctrlruntimelog.FromContext(ctx).WithName("controlplane")
}

// -----------------------------------------------------------------------------
// DeploymentOptions - Private Functions - Equality Checks
// -----------------------------------------------------------------------------

func deploymentOptionsDeepEqual(opts1, opts2 *operatorv1alpha1.DeploymentOptions, envVarsToIgnore ...string) bool {
	if !reflect.DeepEqual(opts1.ContainerImage, opts2.ContainerImage) {
		return false
	}

	if !reflect.DeepEqual(opts1.Version, opts2.Version) {
		return false
	}

	if !reflect.DeepEqual(opts1.EnvFrom, opts2.EnvFrom) {
		return false
	}

	// envVarsToIgnore contains all the env vars to not consider when checking the opts equality
	if len(opts1.Env) != len(opts2.Env)+len(envVarsToIgnore) {
		return false
	}
	env2i := 0
	for _, env1 := range opts1.Env {
		ignored := false
		for _, envIgn := range envVarsToIgnore {
			if envIgn == env1.Name {
				ignored = true
				break
			}
		}
		if ignored {
			continue
		}
		if env1.Name != opts2.Env[env2i].Name || env1.Value != opts2.Env[env2i].Value {
			return false
		}
		env2i += 1
	}

	return true
}

// -----------------------------------------------------------------------------
// Owner based metadata getters - Private Functions
// -----------------------------------------------------------------------------

func getPrefixForOwner(owner client.Object) string {
	switch owner.(type) {
	case *operatorv1alpha1.ControlPlane:
		return consts.ControlPlanePrefix
	case *operatorv1alpha1.DataPlane:
		return consts.DataPlanePrefix
	}
	return ""
}

func getManagedLabelForOwner(owner client.Object) (key string, value string) {
	switch owner.(type) {
	case *operatorv1alpha1.ControlPlane:
		return consts.GatewayOperatorControlledLabel, consts.ControlPlaneManagedLabelValue
	case *operatorv1alpha1.DataPlane:
		return consts.GatewayOperatorControlledLabel, consts.DataPlaneManagedLabelValue
	}
	return "", ""
}

// -----------------------------------------------------------------------------
// Owner based objects mutators - Private Functions
// -----------------------------------------------------------------------------

func addLabelForOwner(obj client.Object, owner client.Object) {
	switch owner.(type) {
	case *operatorv1alpha1.ControlPlane:
		addLabelForControlPlane(obj)
	case *operatorv1alpha1.DataPlane:
		addLabelForDataplane(obj)
	}
}

// -----------------------------------------------------------------------------
// Private Functions - Container Images
// -----------------------------------------------------------------------------

// ensureContainerImageUpdated ensures that the provided container is
// configured with a container image consistent with the image and
// image version provided. The image and version can be provided as
// nil when not wanted.
func ensureContainerImageUpdated(container *corev1.Container, image *string, version *string) (updated bool, err error) {
	imageParts := strings.Split(container.Image, ":")
	if len(imageParts) > 3 {
		err = fmt.Errorf("invalid container image found: %s", container.Image)
		return
	}

	containerImageURL := imageParts[0]
	// This is a special case for registries that specify a non default port,
	// e.g. localhost:5000 or myregistry.io:8000. We do look for '/' since the
	// contianer.Image will contain it as a separator between the registry+image
	// and the version.
	if len(imageParts) == 3 {
		if !strings.Contains(container.Image, "/") {
			return false, fmt.Errorf("invalid container image found: %s", container.Image)
		}

		containerImageURL = imageParts[0] + imageParts[1]
		u, err := url.Parse(containerImageURL)
		if err != nil {
			return false, fmt.Errorf("invalid registry URL %s: %w", containerImageURL, err)
		}
		containerImageURL = u.String()
	}

	switch {
	// if both the image and the version were provided, we expect the
	// container's image to match exactly.
	case image != nil && *image != "" && version != nil && *version != "":
		expectedImageAndVersion := fmt.Sprintf("%s:%s", *image, *version)
		if container.Image != expectedImageAndVersion {
			container.Image = expectedImageAndVersion
			updated = true
		}
	// if only the image was provided we expect the image to match but we don't
	// worry about what the version is.
	case image != nil && *image != "":
		expectedImage := *image
		if len(imageParts) == 2 {
			if containerImageURL != expectedImage {
				container.Image = fmt.Sprintf("%s:%s", expectedImage, imageParts[1])
				updated = true
			}
		} else {
			if container.Image != expectedImage {
				container.Image = expectedImage
				updated = true
			}
		}
	// if only the image version was provided we expect the tag to match but we
	// don't worry about what the base image is.
	case version != nil && *version != "":
		expectedVersion := *version
		if len(imageParts) == 2 {
			if imageParts[1] != expectedVersion {
				container.Image = fmt.Sprintf("%s:%s", containerImageURL, expectedVersion)
				updated = true
			}
		} else {
			container.Image = fmt.Sprintf("%s:%s", containerImageURL, expectedVersion)
			updated = true
		}
	}

	return
}
