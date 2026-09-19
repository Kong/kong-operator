package konnect

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	k8stypes "k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	ctrlconsts "github.com/kong/kong-operator/v2/controller/consts"
	"github.com/kong/kong-operator/v2/controller/pkg/patch"
	"github.com/kong/kong-operator/v2/internal/utils/index"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

const (
	// SecretKonnectDataPlaneCertificateLabel is the label to mark that the secret is used as a Konnect DP certificate.
	// A secret must have the label to be watched by the KonnectExtension reconciler.
	SecretKonnectDataPlaneCertificateLabel = "konghq.com/konnect-dp-cert" //nolint:gosec
)

// certificateSecretUsage checks other extensions before releasing a shared Secret.
// Live reads prevent handing cleanup to an extension that has already disappeared
// or started deleting while the controller cache still reports it as active.
func (r *KonnectExtensionReconciler) certificateSecretUsage(
	ctx context.Context,
	ext *konnectv1alpha2.KonnectExtension,
	secret *corev1.Secret,
) (activeUser *konnectv1alpha2.KonnectExtension, pendingCleanup bool, err error) {
	reader := r.apiReader
	if reader == nil {
		reader = r.Client
	}
	var extensions konnectv1alpha2.KonnectExtensionList
	if err := reader.List(ctx, &extensions, client.InNamespace(secret.Namespace)); err != nil {
		return nil, false, err
	}
	pendingOwners := make(map[k8stypes.UID]struct{})
	for _, other := range extensions.Items {
		if other.Name == ext.Name {
			continue
		}
		specReferences := other.Spec.ClientAuth != nil &&
			other.Spec.ClientAuth.CertificateSecret.CertificateSecretRef != nil &&
			other.Spec.ClientAuth.CertificateSecret.CertificateSecretRef.Name == secret.Name
		statusReferences := other.Status.DataPlaneClientAuth != nil &&
			other.Status.DataPlaneClientAuth.CertificateSecretRef != nil &&
			other.Status.DataPlaneClientAuth.CertificateSecretRef.Name == secret.Name
		ownsSecret := k8sutils.IsOwnedByRefUID(secret, other.UID)
		if !specReferences && !statusReferences && !ownsSecret {
			continue
		}
		if other.DeletionTimestamp.IsZero() || controllerutil.ContainsFinalizer(&other, consts.ExtensionInUseFinalizer) {
			if specReferences {
				return &other, false, nil
			}
		}
		pendingOwners[other.UID] = struct{}{}
	}
	if len(pendingOwners) == 0 {
		return nil, false, nil
	}

	// Status-only references, stale owner references, and deleting references
	// keep the Secret protected only while their certificate still uses its
	// contents. This lets an old Secret go after a peer switches certificates,
	// even if the peer cannot advance its status or the automatic Secret keeps
	// a stale owner reference.
	var certificates configurationv1alpha1.KongDataPlaneClientCertificateList
	if err := reader.List(ctx, &certificates, client.InNamespace(secret.Namespace)); err != nil {
		return nil, false, err
	}
	certData := sanitizeCert(string(secret.Data[consts.TLSCRT]))
	for _, certificate := range certificates.Items {
		if sanitizeCert(certificate.Spec.Cert) != certData {
			continue
		}
		for _, owner := range certificate.OwnerReferences {
			if _, found := pendingOwners[owner.UID]; found {
				return nil, true, nil
			}
		}
	}
	return nil, false, nil
}

// finishCertificateSecretCleanup is called after this extension's certificates
// have been removed. Shared Secret finalizers must outlive every remaining user.
func (r *KonnectExtensionReconciler) finishCertificateSecretCleanup(
	ctx context.Context,
	ext *konnectv1alpha2.KonnectExtension,
	secret *corev1.Secret,
) (ctrl.Result, error) {
	requeue, err := r.finishOwnedCertificateSecretCleanup(ctx, ext, secret)
	if err != nil {
		return ctrl.Result{}, err
	}
	if requeue != nil {
		return *requeue, nil
	}

	ownedSecrets, err := r.listOwnedCertificateSecrets(ctx, ext)
	if err != nil {
		return ctrl.Result{}, err
	}
	for i := range ownedSecrets {
		ownedSecret := &ownedSecrets[i]
		if ownedSecret.Name == secret.Name {
			continue
		}
		requeue, err := r.finishOwnedCertificateSecretCleanup(ctx, ext, ownedSecret)
		if err != nil {
			return ctrl.Result{}, err
		}
		if requeue != nil {
			return *requeue, nil
		}
	}

	// This extension has finished its own cleanup. An active peer keeps the
	// shared Secret protected without blocking deletion of this extension.
	_, res, err := patch.WithoutFinalizer(ctx, r.Client, ext, KonnectCleanupFinalizer)
	return res, client.IgnoreNotFound(err)
}

// finishOwnedCertificateSecretCleanup finishes cleanup for one Secret. When an
// active peer still uses an automatically provisioned Secret, ownership moves
// to that peer so garbage collection does not start deleting the shared Secret
// with the current extension.
func (r *KonnectExtensionReconciler) finishOwnedCertificateSecretCleanup(
	ctx context.Context,
	ext *konnectv1alpha2.KonnectExtension,
	secret *corev1.Secret,
) (*ctrl.Result, error) {
	activeUser, pendingCleanup, err := r.certificateSecretUsage(ctx, ext, secret)
	if err != nil {
		return nil, err
	}
	if pendingCleanup {
		return &ctrl.Result{RequeueAfter: ctrlconsts.RequeueWithBackoff}, nil
	}

	updated := secret.DeepCopy()
	if activeUser != nil {
		for i := range updated.OwnerReferences {
			owner := &updated.OwnerReferences[i]
			if owner.UID != ext.UID {
				continue
			}
			owner.Name = activeUser.Name
			owner.UID = activeUser.UID
			if err := r.Patch(ctx, updated, client.MergeFromWithOptions(secret, client.MergeFromWithOptimisticLock{})); err != nil && !apierrors.IsNotFound(err) {
				if apierrors.IsConflict(err) {
					return &ctrl.Result{RequeueAfter: ctrlconsts.RequeueWithoutBackoff}, nil
				}
				return nil, err
			}
			return &ctrl.Result{RequeueAfter: ctrlconsts.RequeueWithoutBackoff}, nil
		}
		return nil, nil
	}

	removedInUse := controllerutil.RemoveFinalizer(updated, consts.KonnectExtensionSecretInUseFinalizer)
	removedCleanup := controllerutil.RemoveFinalizer(updated, KonnectCleanupFinalizer)
	if !removedInUse && !removedCleanup {
		return nil, nil
	}
	if err := r.Patch(ctx, updated, client.MergeFromWithOptions(secret, client.MergeFromWithOptimisticLock{})); err != nil && !apierrors.IsNotFound(err) {
		if apierrors.IsConflict(err) {
			return &ctrl.Result{RequeueAfter: ctrlconsts.RequeueWithoutBackoff}, nil
		}
		return nil, err
	}
	// Re-list owned Secrets before releasing the extension: automatic
	// provisioning can leave more than one Secret pending cleanup.
	return &ctrl.Result{RequeueAfter: ctrlconsts.RequeueWithoutBackoff}, nil
}

func (r *KonnectExtensionReconciler) listOwnedCertificateSecrets(
	ctx context.Context,
	ext *konnectv1alpha2.KonnectExtension,
) ([]corev1.Secret, error) {
	reader := r.apiReader
	if reader == nil {
		reader = r.Client
	}
	return k8sutils.ListSecretsForOwner(
		ctx,
		reader,
		ext.UID,
		client.InNamespace(ext.Namespace),
		client.MatchingLabels{SecretKonnectDataPlaneCertificateLabel: "true"},
	)
}

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
					Namespace: ke.Namespace,
					Name:      ke.Name,
				})
			}
		}
		return reqs
	}
}
