package konnect

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kong/kong-operator/internal/utils/index"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"

	konnectv1alpha2 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha2"
)

const (
	// SecretKonnectDataPlaneCertificateLabel is the label to mark that the secret is used as a Konnect DP certificate.
	// A secret must have the label to be watched by the KonnectExtension reconciler.
	SecretKonnectDataPlaneCertificateLabel = "konghq.com/konnect-dp-cert" //nolint:gosec
)

func listKonnectExtensionsBySecret(ctx context.Context, cl client.Client, s *corev1.Secret) ([]konnectv1alpha2.KonnectExtension, error) {

	// Get all the secrets explicitly referenced by KonnectExtensions in the spec.
	l := &konnectv1alpha2.KonnectExtensionList{}
	err := cl.List(
		ctx, l,
		client.InNamespace(s.Namespace),
		client.MatchingFields{
			index.IndexFieldKonnectExtensionOnSecrets: s.Name,
		},
	)
	if err != nil {
		return nil, err
	}

	// Add all the konnectExtensions that own the secret.
	for _, ownerRef := range s.GetOwnerReferences() {
		if ownerRef.Controller != nil &&
			*ownerRef.Controller &&
			ownerRef.Kind == konnectv1alpha2.KonnectExtensionKind &&
			ownerRef.APIVersion == konnectv1alpha2.GroupVersion.String() {
			owner := &konnectv1alpha2.KonnectExtension{}
			err := cl.Get(ctx, k8stypes.NamespacedName{
				Namespace: s.Namespace,
				Name:      ownerRef.Name,
			}, owner)
			if err != nil {
				return nil, err
			}
			l.Items = append(l.Items, *owner)
		}
	}

	return l.Items, nil

}

func enqueueKonnectExtensionsForSecret(cl client.Client) func(context.Context, client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		secret, ok := obj.(*corev1.Secret)
		if !ok {
			return nil
		}
		konnectExtensions, err := listKonnectExtensionsBySecret(ctx, cl, secret)
		if err != nil {
			return nil
		}

		reqs := make([]reconcile.Request, 0, len(konnectExtensions))
		for _, ke := range konnectExtensions {
			if (ke.Spec.ClientAuth != nil &&
				ke.Spec.ClientAuth.CertificateSecret.CertificateSecretRef != nil &&
				ke.Spec.ClientAuth.CertificateSecret.CertificateSecretRef.Name == obj.GetName()) ||
				k8sutils.IsOwnedByRefUID(secret, ke.UID) {
				reqs = append(reqs, reconcile.Request{
					NamespacedName: k8stypes.NamespacedName{
						Namespace: ke.Namespace,
						Name:      ke.Name,
					},
				})
			}
		}
		return reqs
	}
}
