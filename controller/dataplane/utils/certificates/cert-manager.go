package certificates

import (
	"context"
	"crypto/sha256"
	"fmt"
	"maps"
	"strings"

	"github.com/cert-manager/cert-manager/pkg/apis/certmanager"
	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ossconsts "github.com/kong/kong-operator/pkg/consts"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
	k8sresources "github.com/kong/kong-operator/pkg/utils/kubernetes/resources"
)

const (
	// ManagerUIDLabel is a label indicating the owner ID when references are not possible.
	ManagerUIDLabel = ossconsts.OperatorLabelPrefix + "owner-id"
)

// ListCMCertificatesForOwner retrieves cert-manager certificate resources owned by a resource with a given UID.
func ListCMCertificatesForOwner(
	ctx context.Context,
	c client.Client,
	namespace string,
	uid types.UID,
	listOpts ...client.ListOption,
) ([]certmanagerv1.Certificate, error) {
	certList := &certmanagerv1.CertificateList{}

	err := c.List(
		ctx,
		certList,
		append(
			[]client.ListOption{client.InNamespace(namespace)},
			listOpts...,
		)...,
	)
	if err != nil {
		return nil, err
	}

	certs := make([]certmanagerv1.Certificate, 0)
	for _, cert := range certList.Items {
		if k8sutils.IsOwnedByRefUID(&cert, uid) {
			certs = append(certs, cert)
		}
	}

	return certs, nil
}

// ReduceCMCertificates detects the best Certificate in the set and deletes all the others.
func ReduceCMCertificates(
	ctx context.Context,
	k8sClient client.Client,
	certs []certmanagerv1.Certificate,
	filter cmCertificateFilterer,
) error {
	for _, cert := range filter(certs) {
		if err := k8sClient.Delete(ctx, &cert); err != nil {
			return err
		}
	}
	return nil
}

type cmCertificateFilterer func(certs []certmanagerv1.Certificate) []certmanagerv1.Certificate

// FilterCMCertificates filters out the Certificates to be kept and returns all
// the Certificates to be deleted.
// The filtered-out Certificates is decided as follows:
// 1. creationTimestamp (older is better)
func FilterCMCertificates(certs []certmanagerv1.Certificate) []certmanagerv1.Certificate {
	if len(certs) < 2 {
		return []certmanagerv1.Certificate{}
	}

	best := 0
	for i, cert := range certs {
		if cert.CreationTimestamp.Before(&certs[best].CreationTimestamp) {
			best = i
		}
	}

	res := make([]certmanagerv1.Certificate, 0, len(certs)-1)
	res = append(res, certs[:best]...)
	return append(res, certs[best+1:]...)
}

// GenerateCMCertificateForOwner generate a cert-manager Certificate for the given client.Object to be provisioned by
// the given Issuer.
func GenerateCMCertificateForOwner(
	owner client.Object,
	issuer *types.NamespacedName,
	secretName string,
	additionalLabels client.MatchingLabels,
) (
	*certmanagerv1.Certificate, error,
) {
	labels := k8sresources.GetManagedLabelForOwner(owner)
	labels["app"] = owner.GetName()
	// Because Certificate Secrets are owned by cert-manager (if references are enabled--they aren't by default)
	// they won't have an ownerRef to the original owner, and the usual secret by owner lookup function can't find
	// them. This is an alternative means of expressing the same information.
	labels[ManagerUIDLabel] = string(owner.GetUID())
	maps.Copy(labels, additionalLabels)

	issuerKind := certmanagerv1.ClusterIssuerKind
	if issuer.Namespace != "" {
		issuerKind = certmanagerv1.IssuerKind
	}

	kind := strings.ToLower(owner.GetObjectKind().GroupVersionKind().Kind)
	cn := sha256.Sum256(fmt.Appendf([]byte{}, "%s.%s", owner.GetName(), owner.GetNamespace()))
	cert := &certmanagerv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: k8sutils.TrimGenerateName(fmt.Sprintf("%s-%s-", kind, owner.GetName())),
			Namespace:    owner.GetNamespace(),
			Labels:       labels,
		},
		Spec: certmanagerv1.CertificateSpec{
			SecretName: secretName,
			// TODO TRC 1222 we're labeling this but not cleaning it up. Unsure if we should. By default cert-manager
			// does not delete old Secrets for deleted Certificates, but you can configure this:
			// https://cert-manager.io/docs/usage/certificate/#cleaning-up-secrets-when-certificates-are-deleted
			// Unsure if we should leave that up to the user. May not be desirable because it's a global CM setting.
			SecretTemplate: &certmanagerv1.CertificateSecretTemplate{
				Labels: labels,
			},
			// TODO TRC 1222 CN pattern is vov. owner NSN is at least guaranteed to be a valid hostname
			// namespace + name is actually too long in tests with UUIDs for both. Konnect UI generates
			// "konnect-<control plane name>" (yes, this is a dataplane, but the UI that generates "helm install"
			// commands or (presumably, in the future) DataPlane resources is called "New Control Plane", don't ask
			// me how we use terminology, I no longer know) CNs, so roughly following that pattern with some unique
			// garbage. DNS Names have something more readable.
			CommonName: fmt.Sprintf("konnect-%x", cn[:16]),
			DNSNames:   []string{fmt.Sprintf("%s.%s.%s.konnect", owner.GetName(), owner.GetNamespace(), kind)},
			IssuerRef: cmmeta.ObjectReference{
				Name:  issuer.Name,
				Group: certmanager.GroupName,
				Kind:  issuerKind,
			},
		},
	}

	k8sutils.SetOwnerForObject(cert, owner)

	return cert, nil
}

// because we don't get owner references (cert-manager doesn't add them), lookups by Secret need a non-standard
// lookup mechanism. Alternative is to look up the Certificate by owner ref and then look up its Secret from status.

// ListCMSecretsForOwner lists Secrets with a given owner label, for cert-manager Secrets with no ownership information
// or a Certificate owner.
func ListCMSecretsForOwner(
	ctx context.Context,
	c client.Client,
	owner client.Object,
	listOpts ...client.ListOption,
) ([]corev1.Secret, error) {
	secretList := &corev1.SecretList{}

	err := c.List(
		ctx,
		secretList,
		listOpts...,
	)
	if err != nil {
		return nil, err
	}

	secrets := make([]corev1.Secret, 0)
	for _, secret := range secretList.Items {
		if secret.Labels[ManagerUIDLabel] == string(owner.GetUID()) {
			secrets = append(secrets, secret)
		}
	}

	return secrets, nil
}
