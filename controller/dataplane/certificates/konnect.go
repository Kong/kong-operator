package certificates

import (
	"context"
	"fmt"
	"path/filepath"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1beta1"

	certutils "github.com/kong/kong-operator/controller/dataplane/utils/certificates"
	"github.com/kong/kong-operator/controller/pkg/log"
	ossop "github.com/kong/kong-operator/controller/pkg/op"
	osspatch "github.com/kong/kong-operator/controller/pkg/patch"
	ossconsts "github.com/kong/kong-operator/pkg/consts"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
	ossk8sreduce "github.com/kong/kong-operator/pkg/utils/kubernetes/reduce"
	k8sresources "github.com/kong/kong-operator/pkg/utils/kubernetes/resources"
)

//+kubebuilder:rbac:groups=cert-manager.io,resources=certificates,verbs=get;list;create;delete;patch;update;watch

const (
	// KonnectDataPlaneCertPurpose is a ossconsts.CertPurposeLabel value indicating a certificate is by DataPlanes for Konnect
	// authentication.
	KonnectDataPlaneCertPurpose = "konnect-dataplane"

	// DataPlaneKonnectClientCertificateName is the name and label used to identify resources related to the Konnect
	// client certificate, including the Certificate, its Secret, and volume.
	DataPlaneKonnectClientCertificateName = "konnect-cert"
	// DataPlaneKonnectClientCertificatePath is the mount path for the Konnect client certificate.
	DataPlaneKonnectClientCertificatePath = "/var/konnect-client-certificate/"
	// ClusterCertEnvKey is the environment variable name for cluster certificates.
	ClusterCertEnvKey = "KONG_CLUSTER_CERT"
	// ClusterCertKeyEnvKey is the environment variable name for cluster certificate keys.
	ClusterCertKeyEnvKey = "KONG_CLUSTER_CERT_KEY"
)

var certificateGVR = schema.GroupVersionResource{
	Group:    certmanagerv1.SchemeGroupVersion.Group,
	Version:  certmanagerv1.SchemeGroupVersion.Version,
	Resource: "certificates",
}

// certificateCRDNotInstalled returns true if `certmanager.io/v1.certificates` CRD is not installed
// so we can skip the processing of Konnect certificates when KonnectCertificateOptions is missing in DataPlane.
func certificateCRDNotInstalled(logger logr.Logger, cl client.Client) bool {
	checker := k8sutils.CRDChecker{Client: cl}
	exist, err := checker.CRDExists(certificateGVR)
	if err != nil {
		log.Error(logger, err, "failed to check if certificate CRD installed")
		return false
	}
	return !exist
}

// CertOpt modifies generated cerfificates.
type CertOpt func(*certmanagerv1.Certificate)

// WithSecretLabel adds a label "key:value" to the secrets generated for the certificate.
func WithSecretLabel(key, value string) CertOpt {
	return func(cert *certmanagerv1.Certificate) {
		if cert.Spec.SecretTemplate == nil {
			cert.Spec.SecretTemplate = &certmanagerv1.CertificateSecretTemplate{
				Labels: map[string]string{},
			}
		}
		if cert.Spec.SecretTemplate.Labels == nil {
			cert.Spec.SecretTemplate.Labels = map[string]string{}
		}
		cert.Spec.SecretTemplate.Labels[key] = value
	}
}

// CreateKonnectCert creates a cert-manager certificate request for a DataPlane.
func CreateKonnectCert(
	ctx context.Context,
	logger logr.Logger,
	dataplane *operatorv1beta1.DataPlane,
	cl client.Client,
	certOpts ...CertOpt,
) error {
	// Skip creating Konnect certificate if KonnectCertificateOptions is not specified and certificate CRD is not installed.
	if dataplane.Spec.Network.KonnectCertificateOptions == nil && certificateCRDNotInstalled(logger, cl) {
		log.Debug(logger, "skipping because dataplane does not have Konnect certificate options and certificate CRD is not installed",
			"namespace", dataplane.Namespace, "dataplane", dataplane.Name)
		return nil
	}

	log.Debug(logger, "running Konnect Certificate provisioning",
		"namespace", dataplane.Namespace, "dataplane", dataplane.Name)

	labels := k8sresources.GetManagedLabelForOwner(dataplane)
	labels[ossconsts.CertPurposeLabel] = KonnectDataPlaneCertPurpose
	labels[certutils.ManagerUIDLabel] = string(dataplane.UID)

	certs, err := certutils.ListCMCertificatesForOwner(
		ctx,
		cl,
		dataplane.Namespace,
		dataplane.UID,
		labels,
	)
	if err != nil {
		return fmt.Errorf("failed listing Certificates for DataPlane %s/%s: %w", dataplane.Namespace, dataplane.Name, err)
	}

	if len(certs) > 1 {
		log.Debug(logger, "reducing excess Konnect Certificates",
			"namespace", dataplane.Namespace, "dataplane", dataplane.Name)
		if err := certutils.ReduceCMCertificates(ctx, cl, certs, certutils.FilterCMCertificates); err != nil {
			return fmt.Errorf("failed reducing Certificates for DataPlane %s/%s: %w", dataplane.Namespace, dataplane.Name, err)
		}
		// NOTE reducing in a callback is actually an error condition, since callbacks can't independently trigger
		// a new reconcile. To avoid this we could run a reduce callback first, to noop if the size is zero or one,
		// try and reduce if greater than one, and error if that fails. However, this function would then depend on
		// that running first, which doesn't work well with the simple "execute everything in a phase in random order"
		// approach. We could add another phase before, but PrePreDeployment is not great. Priority values like Kong
		// proxy plugins are an option, but learned experience from those is that they're not a fun one. They may
		// be less a concern here given that we don't expect third-party callbacks or reordered callbacks yet, at
		// least so they're maybe less bad here compared to a full systemd-esque wants/requires system
		return fmt.Errorf("reduced Certificates for DataPlane %s/%s: %w", dataplane.Namespace, dataplane.Name, err)
	}

	var generatedCertificate *certmanagerv1.Certificate
	var issuer *operatorv1beta1.NamespacedName
	if dataplane.Spec.Network.KonnectCertificateOptions != nil {
		issuer = &dataplane.Spec.Network.KonnectCertificateOptions.Issuer
	}
	if issuer != nil {
		log.Debug(logger, "Konnect Issuer configured, ensuring Certificate",
			"namespace", dataplane.Namespace, "dataplane", dataplane.Name, "issuer", issuer.Name)
		labels[ossconsts.CertPurposeLabel] = KonnectDataPlaneCertPurpose
		generatedCertificate, err = certutils.GenerateCMCertificateForOwner(
			dataplane,
			&types.NamespacedName{Namespace: issuer.Namespace, Name: issuer.Name},
			fmt.Sprintf("%s-%s", dataplane.Name, DataPlaneKonnectClientCertificateName),
			labels,
		)
		for _, opt := range certOpts {
			opt(generatedCertificate)
		}
		if err != nil {
			return fmt.Errorf("could not generate Certificate: %w", err)
		}
	} else {
		log.Debug(logger, "no Konnect Issuer configured", "namespace", dataplane.Namespace, "dataplane", dataplane.Name)
		// no issuer, check for existing certs to delete
		if len(certs) == 1 {
			if err := certutils.ReduceCMCertificates(ctx, cl, certs, ossk8sreduce.FilterNone); err != nil {
				return fmt.Errorf("failed reducing Certificates for DataPlane %s/%s: %w", dataplane.Namespace, dataplane.Name, err)
			}
		}
		// config disabled, nothing more to do
		return nil
	}

	if len(certs) == 1 {
		var updated bool
		existingCertificate := &certs[0]
		oldExistingCertificate := existingCertificate.DeepCopy()

		// ensure that object metadata is up to date
		updated, existingCertificate.ObjectMeta = k8sutils.EnsureObjectMetaIsUpdated(
			existingCertificate.ObjectMeta, generatedCertificate.ObjectMeta)

		// ensure that certificate configuration is up to date
		if !cmp.Equal(existingCertificate.Spec, generatedCertificate.Spec) {
			existingCertificate.Spec = generatedCertificate.Spec
			updated = true
		}

		// TODO Upstream returns an operation, the end resource, and any errors. It logs whether it did or did not
		// take action. This callback doesn't have any immediate use for the operation or end resource, but adding
		// logging support to the callback primitives probably makes sense. For now, this just discards patch logs.
		op, _, err := osspatch.ApplyPatchIfNotEmpty(ctx, cl, logr.Discard(), existingCertificate, oldExistingCertificate, updated)
		switch op { //nolint:exhaustive
		case ossop.Created, ossop.Updated:
			log.Debug(logger, "updated Konnect Certificate",
				"namespace", dataplane.Namespace, "dataplane", dataplane.Name, "Certificate", generatedCertificate.Name)
		case ossop.Noop:
			log.Debug(logger, "no update to Konnect Certificate",
				"namespace", dataplane.Namespace, "dataplane", dataplane.Name, "Certificate", generatedCertificate.Name)
		}
		return err
	}

	if err = cl.Create(ctx, generatedCertificate); err != nil {
		return fmt.Errorf("failed creating Certificate for DataPlane %s: %w", dataplane.Name, err)
	}

	return nil
}

// MountAndUseKonnectCert looks for an operator-managed Konnect certificate for a DataPlane,
// modifies that DataPlane's Deployment to mount it in the proxy container, and configures
// the proxy environment to authenticate to Konnect using it.
func MountAndUseKonnectCert(ctx context.Context, logger logr.Logger, dataplane *operatorv1beta1.DataPlane, cl client.Client, desiredDeployment *k8sresources.Deployment) error {
	// Skip mounting Konnect certificate if KonnectCertificateOptions is not specified and certificate CRD is not installed.
	if dataplane.Spec.Network.KonnectCertificateOptions == nil && certificateCRDNotInstalled(logger, cl) {
		log.Debug(logger, "skipping because dataplane does not have Konnect certificate options and certificate CRD is not installed",
			"namespace", dataplane.Namespace, "dataplane", dataplane.Name)
		return nil
	}

	log.Debug(logger, "Konnect certificate mount",
		"namespace", dataplane.Namespace, "dataplane", dataplane.Name)

	var issuer *operatorv1beta1.NamespacedName
	if dataplane.Spec.Network.KonnectCertificateOptions != nil {
		issuer = &dataplane.Spec.Network.KonnectCertificateOptions.Issuer
	}
	if issuer == nil {
		log.Debug(logger, "no Konnect Issuer configured, skipping certificate mount",
			"namespace", dataplane.Namespace, "dataplane", dataplane.Name)
		return nil
	}

	labels := k8sresources.GetManagedLabelForOwner(dataplane)
	labels[ossconsts.CertPurposeLabel] = KonnectDataPlaneCertPurpose
	labels[certutils.ManagerUIDLabel] = string(dataplane.UID)

	secrets, err := certutils.ListCMSecretsForOwner(ctx, cl, dataplane, labels)
	if err != nil {
		return fmt.Errorf("failed listing Secrets for Deployment %s/%s: %w",
			desiredDeployment.GetNamespace(), dataplane.GetName(), err)
	}
	if len(secrets) > 1 {
		return fmt.Errorf("too many %s Secrets for Deployment %s/%s",
			labels[ossconsts.CertPurposeLabel], desiredDeployment.GetNamespace(), dataplane.GetName())
	}
	if len(secrets) < 1 {
		return fmt.Errorf("no %s Secrets for Deployment %s/%s",
			labels[ossconsts.CertPurposeLabel], desiredDeployment.GetNamespace(), dataplane.GetName())
	}
	log.Debug(logger, "found Secret for Konnect Certificate",
		"namespace", dataplane.Namespace, "dataplane", dataplane.Name, "secret", secrets[0].Name)

	konnectCertVolume := corev1.Volume{
		Name: DataPlaneKonnectClientCertificateName,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: secrets[0].Name,
			},
		},
	}
	mount := corev1.VolumeMount{
		Name:      DataPlaneKonnectClientCertificateName,
		ReadOnly:  true,
		MountPath: DataPlaneKonnectClientCertificatePath,
	}

	_ = desiredDeployment.WithVolume(konnectCertVolume).
		WithVolumeMount(mount, ossconsts.DataPlaneProxyContainerName).
		WithEnvVar(
			corev1.EnvVar{
				Name:  ClusterCertEnvKey,
				Value: filepath.Join(DataPlaneKonnectClientCertificatePath, corev1.TLSCertKey),
			},
			ossconsts.DataPlaneProxyContainerName,
		).
		WithEnvVar(
			corev1.EnvVar{
				Name:  ClusterCertKeyEnvKey,
				Value: filepath.Join(DataPlaneKonnectClientCertificatePath, corev1.TLSPrivateKeyKey),
			},
			ossconsts.DataPlaneProxyContainerName,
		)
	log.Debug(logger, "successfully added Konnect Certificate mount",
		"namespace", dataplane.Namespace, "dataplane", dataplane.Name)
	return nil
}
