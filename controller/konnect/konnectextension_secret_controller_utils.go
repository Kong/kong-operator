package konnect

import (
	"context"
	"errors"
	"fmt"

	"github.com/kong/gateway-operator/controller/pkg/op"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"
	k8sreduce "github.com/kong/gateway-operator/pkg/utils/kubernetes/reduce"
	k8sresources "github.com/kong/gateway-operator/pkg/utils/kubernetes/resources"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func ensureKongDataPlaneCertificate(
	ctx context.Context,
	cl client.Client,
	secret *corev1.Secret,
	extension *konnectv1alpha1.KonnectExtension,
	matchingLabels client.HasLabels,
) (res op.Result, cert *configurationv1alpha1.KongDataPlaneClientCertificate, err error) {
	// Get the KongDataPlaneCertificate from the secret
	certificates, err := k8sutils.ListKongDataPlaneClientCertificateForOwner(
		ctx,
		cl,
		secret.UID,
		client.InNamespace(secret.Namespace),
		matchingLabels,
	)
	if err != nil {
		return op.Noop, nil, fmt.Errorf("failed listing KongDataPlaneClientCertificates for Secret %s/%s: %w", secret.Namespace, secret.Name, err)
	}

	count := len(certificates)
	if count > 1 {
		if err := k8sreduce.ReduceKongDataPlaneClientCertificates(ctx, cl, certificates); err != nil {
			return op.Noop, nil, err
		}
		return op.Noop, nil, errors.New("number of KongDataPlaneClientCertificates reduced")
	}

	generatedCert := k8sresources.GenerateKongDataPlaneClientCertificatesForSecret(secret, extension)

	if count == 1 {
		var updated bool
		existingCertificate := &certificates[0]
		updated, existingCertificate.ObjectMeta = k8sutils.EnsureObjectMetaIsUpdated(existingCertificate.ObjectMeta, generatedCert.ObjectMeta)

		if existingCertificate.Spec.Cert != generatedCert.Spec.Cert {
			existingCertificate.Spec.Cert = generatedCert.Spec.Cert
			updated = true
		}

		if updated {
			if err := cl.Update(ctx, existingCertificate); err != nil {
				return op.Noop, existingCertificate, fmt.Errorf("failed updating KongDataPlaneClientCertificate %s: %w", existingCertificate.Name, err)
			}
			return op.Updated, existingCertificate, nil
		}
		return op.Noop, existingCertificate, nil
	}

	if err = cl.Create(ctx, generatedCert); err != nil {
		return op.Noop, nil, fmt.Errorf("failed creating KongDataPlaneClientCertificate for secret %s: %w", secret.Name, err)
	}

	return op.Created, generatedCert, nil
}
