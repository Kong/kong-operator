package konnect

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	ctrlconsts "github.com/kong/kong-operator/v2/controller/consts"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	"github.com/kong/kong-operator/v2/controller/pkg/patch"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	k8sresources "github.com/kong/kong-operator/v2/pkg/utils/kubernetes/resources"
)

const automaticCertificateIssuedAtAnnotation = "konghq.com/konnect-dp-cert-issued-at"

func (r *KonnectExtensionReconciler) certificateReader() client.Reader {
	if r.apiReader != nil {
		return r.apiReader
	}
	return r.Client
}

// A private snapshot prevents an external issuer's in-place renewal from
// changing the identity mounted by Pods before Konnect trusts it.
func (r *KonnectExtensionReconciler) ensureCertificateSnapshot(
	ctx context.Context,
	ext *konnectv1alpha2.KonnectExtension,
	source *corev1.Secret,
) (op.Result, *corev1.Secret, error) {
	if !source.DeletionTimestamp.IsZero() {
		return op.Noop, nil, fmt.Errorf("certificate Secret %s/%s is being deleted", source.Namespace, source.Name)
	}
	if len(source.Data[corev1.TLSCertKey]) == 0 || len(source.Data[corev1.TLSPrivateKeyKey]) == 0 {
		return op.Noop, nil, fmt.Errorf("certificate Secret %s/%s must contain tls.crt and tls.key", source.Namespace, source.Name)
	}
	digest := sha256.Sum256([]byte(string(ext.UID) + "\x00" + string(source.Data[corev1.TLSCertKey]) +
		"\x00" + string(source.Data[corev1.TLSPrivateKeyKey])))
	const suffixLength = 1 + 32
	prefix := ext.Name
	if len(prefix) > validation.DNS1123SubdomainMaxLength-suffixLength {
		prefix = strings.TrimRight(prefix[:validation.DNS1123SubdomainMaxLength-suffixLength], ".-")
	}
	name := fmt.Sprintf("%s-%x", prefix, digest[:16])
	existing := &corev1.Secret{}
	err := r.certificateReader().Get(ctx, client.ObjectKey{Namespace: ext.Namespace, Name: name}, existing)
	if err == nil {
		if !metav1.IsControlledBy(existing, ext) ||
			string(existing.Data[corev1.TLSCertKey]) != string(source.Data[corev1.TLSCertKey]) ||
			string(existing.Data[corev1.TLSPrivateKeyKey]) != string(source.Data[corev1.TLSPrivateKeyKey]) ||
			!existing.DeletionTimestamp.IsZero() {
			return op.Noop, nil, fmt.Errorf("certificate snapshot Secret %s/%s conflicts with the desired generation", ext.Namespace, name)
		}
		if existing.Immutable == nil || !*existing.Immutable {
			existing.Immutable = new(true)
			if err := r.Update(ctx, existing); err != nil {
				return op.Noop, nil, err
			}
			return op.Updated, existing, nil
		}
		return op.Noop, existing, nil
	}
	if !apierrors.IsNotFound(err) {
		return op.Noop, nil, err
	}
	labels := k8sresources.GetManagedLabelForOwner(ext)
	labels[SecretKonnectDataPlaneCertificateLabel] = "true"
	if r.SecretLabelSelector != "" {
		labels[r.SecretLabelSelector] = "true"
	}
	snapshot := &corev1.Secret{
		Name: name, Namespace: ext.Namespace, Labels: labels,
		Type: corev1.SecretTypeTLS, Immutable: new(true), Data: source.DeepCopy().Data,
	}
	if err := controllerutil.SetControllerReference(ext, snapshot, r.Scheme()); err != nil {
		return op.Noop, nil, err
	}
	if err := r.Create(ctx, snapshot); err != nil {
		return op.Noop, nil, err
	}
	return op.Created, snapshot, nil
}

// Use live reads: cached rollout status or a cached absence of an old Pod must
// never authorize removal of the identity that Pod still needs to reconnect.
func (r *KonnectExtensionReconciler) certificateConsumersMigrated(
	ctx context.Context,
	ext *konnectv1alpha2.KonnectExtension,
	secretName string,
) (bool, error) {
	reader := r.certificateReader()
	var dps operatorv1beta1.DataPlaneList
	var cps gwtypes.ControlPlaneList
	if err := reader.List(ctx, &dps, client.InNamespace(ext.Namespace)); err != nil {
		return false, err
	}
	if err := reader.List(ctx, &cps, client.InNamespace(ext.Namespace)); err != nil {
		return false, err
	}
	owners := make(map[types.UID]struct{})
	for _, dp := range dps.Items {
		for _, ref := range listExtendableReferencedExtensions[*operatorv1beta1.DataPlane](ctx, &dp) {
			if ref.Name == ext.Name {
				owners[dp.UID] = struct{}{}
			}
		}
	}
	for _, cp := range cps.Items {
		for _, ref := range listExtendableReferencedExtensions[*gwtypes.ControlPlane](ctx, &cp) {
			if ref.Name == ext.Name &&
				(!cp.DeletionTimestamp.IsZero() ||
					cp.Annotations[consts.KonnectClientCertificateSecretAnnotation] != secretName) {
				return false, nil
			}
		}
	}
	var deployments appsv1.DeploymentList
	if err := reader.List(ctx, &deployments, client.InNamespace(ext.Namespace)); err != nil {
		return false, err
	}
	for _, deployment := range deployments.Items {
		owner := metav1.GetControllerOf(&deployment)
		if owner == nil {
			continue
		}
		if _, found := owners[owner.UID]; !found {
			continue
		}
		if deployment.Spec.Replicas != nil && *deployment.Spec.Replicas == 0 {
			continue
		}
		if !deployment.DeletionTimestamp.IsZero() ||
			deployment.Spec.Paused ||
			!k8sutils.DeploymentRolloutComplete(&deployment) ||
			!usesClientCertificate(deployment.Spec.Template.Spec, secretName) {
			return false, nil
		}
	}
	// A rollout can be complete while an old Pod is still terminating. Check
	// mounts directly, including legacy Secrets and blue/green Deployments.
	var pods corev1.PodList
	if err := reader.List(ctx, &pods, client.InNamespace(ext.Namespace)); err != nil {
		return false, err
	}
	oldSecrets, err := r.listOwnedCertificateSecrets(ctx, ext)
	if err != nil {
		return false, err
	}
	oldNames := make(map[string]bool)
	for _, secret := range oldSecrets {
		if secret.Name != secretName {
			oldNames[secret.Name] = true
		}
	}
	if ext.Spec.ClientAuth != nil && ext.Spec.ClientAuth.CertificateSecret.CertificateSecretRef != nil {
		oldNames[ext.Spec.ClientAuth.CertificateSecret.CertificateSecretRef.Name] = true
	}
	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		for _, volume := range pod.Spec.Volumes {
			if volume.Name == consts.KongClusterCertVolume &&
				volume.Secret != nil && oldNames[volume.Secret.SecretName] {
				return false, nil
			}
		}
	}
	return true, nil
}

func usesClientCertificate(spec corev1.PodSpec, secretName string) bool {
	for _, volume := range spec.Volumes {
		if volume.Name == consts.KongClusterCertVolume && volume.Secret != nil {
			return volume.Secret.SecretName == secretName
		}
	}
	return false
}

func (r *KonnectExtensionReconciler) cleanupCertificateResources(ctx context.Context, ext *konnectv1alpha2.KonnectExtension) (ctrl.Result, error) {
	var certificates configurationv1alpha1.KongDataPlaneClientCertificateList
	if err := r.certificateReader().List(ctx, &certificates, client.InNamespace(ext.Namespace)); err != nil {
		return ctrl.Result{}, err
	}
	pending := false
	for i := range certificates.Items {
		cert := &certificates.Items[i]
		if !k8sutils.IsOwnedByRefUID(cert, ext.UID) {
			continue
		}
		pending = true
		if cert.DeletionTimestamp.IsZero() {
			if err := r.Delete(ctx, cert); client.IgnoreNotFound(err) != nil {
				return ctrl.Result{}, err
			}
		}
	}
	if pending {
		return ctrl.Result{RequeueAfter: ctrlconsts.RequeueWithoutBackoff}, nil
	}
	owned, err := r.listOwnedCertificateSecrets(ctx, ext)
	if err != nil {
		return ctrl.Result{}, err
	}
	names := make(map[string]bool)
	for _, secret := range owned {
		names[secret.Name] = true
	}
	if ext.Spec.ClientAuth != nil && ext.Spec.ClientAuth.CertificateSecret.CertificateSecretRef != nil {
		names[ext.Spec.ClientAuth.CertificateSecret.CertificateSecretRef.Name] = true
	}
	if ext.Status.DataPlaneClientAuth != nil && ext.Status.DataPlaneClientAuth.CertificateSecretRef != nil {
		names[ext.Status.DataPlaneClientAuth.CertificateSecretRef.Name] = true
	}
	for name := range names {
		var secret corev1.Secret
		if err := r.certificateReader().Get(ctx, client.ObjectKey{Namespace: ext.Namespace, Name: name}, &secret); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return ctrl.Result{}, err
		}
		if result, err := r.finishOwnedCertificateSecretCleanup(ctx, ext, &secret); err != nil || result != nil {
			if result != nil {
				return *result, err
			}
			return ctrl.Result{}, err
		}
	}
	_, result, err := patch.WithoutFinalizer(ctx, r.Client, ext, KonnectCleanupFinalizer)
	return result, client.IgnoreNotFound(err)
}

func (r *KonnectExtensionReconciler) retireCertificateGenerations(
	ctx context.Context,
	ext *konnectv1alpha2.KonnectExtension,
	current *corev1.Secret,
) (bool, error) {
	var certificates configurationv1alpha1.KongDataPlaneClientCertificateList
	if err := r.certificateReader().List(ctx, &certificates, client.InNamespace(ext.Namespace)); err != nil {
		return false, err
	}
	pending := false
	for i := range certificates.Items {
		cert := &certificates.Items[i]
		if !k8sutils.IsOwnedByRefUID(cert, ext.UID) ||
			sanitizeCert(cert.Spec.Cert) == sanitizeCert(string(current.Data[corev1.TLSCertKey])) {
			continue
		}
		pending = true
		if cert.DeletionTimestamp.IsZero() {
			if err := r.Delete(ctx, cert); client.IgnoreNotFound(err) != nil {
				return false, err
			}
		}
	}
	if pending {
		return true, nil
	}
	owned, err := r.listOwnedCertificateSecrets(ctx, ext)
	if err != nil {
		return false, err
	}
	for i := range owned {
		secret := &owned[i]
		if secret.Name == current.Name {
			continue
		}
		active, usagePending, err := r.certificateSecretUsage(ctx, ext, secret)
		if err != nil {
			return false, err
		}
		if active != nil || usagePending {
			if _, err := r.finishOwnedCertificateSecretCleanup(ctx, ext, secret); err != nil {
				return false, err
			}
			pending = true
			continue
		}
		result, err := r.finishOwnedCertificateSecretCleanup(ctx, ext, secret)
		if err != nil {
			return false, err
		}
		if result != nil {
			pending = true
			continue
		}
		if err := r.Delete(ctx, secret); client.IgnoreNotFound(err) != nil {
			return false, err
		}
		pending = true
	}
	return pending, nil
}
