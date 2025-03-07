package konnect

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

const (
	// SecretKonnectDataPlaneCertificateLabel is the label to mark that the secret is used as a Konnect DP certificate.
	// A secret must have the label to be watched by the KonnectExtension reconciler.
	SecretKonnectDataPlaneCertificateLabel = "konghq.com/konnect-dp-cert" //nolint:gosec
)

func listKonnectExtensionsBySecret(ctx context.Context, cl client.Client, s *corev1.Secret) ([]konnectv1alpha1.KonnectExtension, error) {
	l := &konnectv1alpha1.KonnectExtensionList{}
	err := cl.List(
		ctx, l,
		client.InNamespace(s.Namespace),
		client.MatchingFields{
			IndexFieldKonnectExtensionOnSecrets: s.Name,
		},
	)
	if err != nil {
		return nil, err
	}

	return l.Items, nil

}

func enqueueKonnectExtensionsForSecret(cl client.Client) func(context.Context, client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		s, ok := obj.(*corev1.Secret)
		if !ok {
			return nil
		}
		konnectExtensions, err := listKonnectExtensionsBySecret(ctx, cl, s)
		if err != nil {
			return nil
		}

		reqs := make([]reconcile.Request, 0, len(konnectExtensions))
		for _, ke := range konnectExtensions {
			if ke.Spec.ClientAuth != nil &&
				ke.Spec.ClientAuth.CertificateSecret.CertificateSecretRef != nil &&
				ke.Spec.ClientAuth.CertificateSecret.CertificateSecretRef.Name == obj.GetName() {
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
