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

	certutils "github.com/kong/gateway-operator/controller/dataplane/utils/certificates"
	ossop "github.com/kong/gateway-operator/controller/pkg/op"
	osspatch "github.com/kong/gateway-operator/controller/pkg/patch"
	osslogging "github.com/kong/gateway-operator/modules/manager/logging"
	ossconsts "github.com/kong/gateway-operator/pkg/consts"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"
	ossk8sreduce "github.com/kong/gateway-operator/pkg/utils/kubernetes/reduce"
	k8sresources "github.com/kong/gateway-operator/pkg/utils/kubernetes/resources"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
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
		logger.Error(err, "failed to check if certifiacte CRD installed")
		return false
	}
	return !exist
}

// CreateKonnectCert is a dynamic.Callback that creates a cert-manager certificate request for a DataPlane.
func CreateKonnectCert(ctx context.Context, dataplane *operatorv1beta1.DataPlane, cl client.Client, _ any) error {
	logger := logr.FromContextOrDiscard(ctx)
	// Skip creating Konnect certificate if KonnectCertificateOptions is not specified and certificate CRD is not installed.
	if dataplane.Spec.Network.KonnectCertificateOptions == nil && certificateCRDNotInstalled(logger, cl) {
		logger.V(osslogging.DebugLevel.Value()).Info("skipping because dataplane does not have Konnect certificate options and certificate CRD is not installed",
			"namespace", dataplane.Namespace, "dataplane", dataplane.Name)
		return nil
	}

	logger.V(osslogging.DebugLevel.Value()).Info("running Konnect Certificate provisioning callback",
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
		logger.V(osslogging.DebugLevel.Value()).Info("reducing excess Konnect Certificates",
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
		logger.V(osslogging.DebugLevel.Value()).Info("Konnect Issuer configured, ensuring Certificate",
			"namespace", dataplane.Namespace, "dataplane", dataplane.Name, "issuer", issuer.Name)
		labels[ossconsts.CertPurposeLabel] = KonnectDataPlaneCertPurpose
		generatedCertificate, err = certutils.GenerateCMCertificateForOwner(
			dataplane,
			&types.NamespacedName{Namespace: issuer.Namespace, Name: issuer.Name},
			fmt.Sprintf("%s-%s", dataplane.Name, DataPlaneKonnectClientCertificateName),
			labels,
		)
		if err != nil {
			return fmt.Errorf("could not generate Certificate: %w", err)
		}
	} else {
		logger.V(osslogging.DebugLevel.Value()).Info("no Konnect Issuer configured",
			"namespace", dataplane.Namespace, "dataplane", dataplane.Name)
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
			logger.V(osslogging.DebugLevel.Value()).Info("updated Konnect Certificate",
				"namespace", dataplane.Namespace, "dataplane", dataplane.Name, "Certificate", generatedCertificate.Name)
		case ossop.Noop:
			logger.V(osslogging.DebugLevel.Value()).Info("no update to Konnect Certificate",
				"namespace", dataplane.Namespace, "dataplane", dataplane.Name, "Certificate", generatedCertificate.Name)
		}
		return err
	}

	if err = cl.Create(ctx, generatedCertificate); err != nil {
		return fmt.Errorf("failed creating Certificate for DataPlane %s: %w", dataplane.Name, err)
	}

	return nil
}

// MountAndUseKonnectCert is a dynamic.Callback that looks for an operator-managed Konnect certificate for a DataPlane,
// modifies that DataPlane's Deployment to mount it in the proxy container, and configures the proxy environment to
// authenticate to Konnect using it.
func MountAndUseKonnectCert(ctx context.Context, dataplane *operatorv1beta1.DataPlane, cl client.Client, subj any) error {
	logger := logr.FromContextOrDiscard(ctx)
	// Skip mounting Konnect certificate if KonnectCertificateOptions is not specified and certificate CRD is not installed.
	if dataplane.Spec.Network.KonnectCertificateOptions == nil && certificateCRDNotInstalled(logger, cl) {
		logger.V(osslogging.DebugLevel.Value()).Info("skipping because dataplane does not have Konnect certificate options and certificate CRD is not installed",
			"namespace", dataplane.Namespace, "dataplane", dataplane.Name)
		return nil
	}

	logger.V(osslogging.DebugLevel.Value()).Info("running Konnect certificate mount callback",
		"namespace", dataplane.Namespace, "dataplane", dataplane.Name)

	var issuer *operatorv1beta1.NamespacedName
	if dataplane.Spec.Network.KonnectCertificateOptions != nil {
		issuer = &dataplane.Spec.Network.KonnectCertificateOptions.Issuer
	}
	if issuer == nil {
		logger.V(osslogging.DebugLevel.Value()).Info("no Konnect Issuer configured, skipping certificate mount",
			"namespace", dataplane.Namespace, "dataplane", dataplane.Name)
		return nil
	}

	deployment, ok := subj.(*k8sresources.Deployment)
	if !ok {
		return fmt.Errorf("Invalid subject type for MountAndUseKonnectCert: %T", subj)
	}

	labels := k8sresources.GetManagedLabelForOwner(dataplane)
	labels[ossconsts.CertPurposeLabel] = KonnectDataPlaneCertPurpose
	labels[certutils.ManagerUIDLabel] = string(dataplane.UID)

	secrets, err := certutils.ListCMSecretsForOwner(ctx, cl, dataplane, labels)
	if err != nil {
		return fmt.Errorf("failed listing Secrets for Deployment %s/%s: %w",
			deployment.GetNamespace(), dataplane.GetName(), err)
	}
	if len(secrets) > 1 {
		return fmt.Errorf("too many %s Secrets for Deployment %s/%s",
			labels[ossconsts.CertPurposeLabel], deployment.GetNamespace(), dataplane.GetName())
	}
	if len(secrets) < 1 {
		return fmt.Errorf("no %s Secrets for Deployment %s/%s",
			labels[ossconsts.CertPurposeLabel], deployment.GetNamespace(), dataplane.GetName())
	}
	logger.V(osslogging.DebugLevel.Value()).Info("found Secret for Konnect Certificate",
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

	_ = deployment.WithVolume(konnectCertVolume).
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
	logger.V(osslogging.DebugLevel.Value()).Info("successfully added Konnect Certificate mount",
		"namespace", dataplane.Namespace, "dataplane", dataplane.Name)
	return nil
}
