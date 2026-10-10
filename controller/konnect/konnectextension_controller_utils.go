package konnect

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"sort"
	"strings"
	"time"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/google/go-cmp/cmp"
	"github.com/samber/lo"
	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	"github.com/kong/kong-operator/v2/api/konnect"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	extensionserrors "github.com/kong/kong-operator/v2/controller/pkg/extensions/errors"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	"github.com/kong/kong-operator/v2/controller/pkg/patch"
	"github.com/kong/kong-operator/v2/controller/pkg/secrets"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// getGatewayKonnectControlPlane retrieves the Konnect Control Plane from K8s cluster
// based on the provided KonnectExtension specification.
// It supports one type of ControlPlaneRef: KonnectNamespacedRef.
//
// Returns:
// - cp: The retrieved Konnect Control Plane.
// - res: The result of the controller reconciliation.
// - err: An error if the retrieval fails.
func (r *KonnectExtensionReconciler) getGatewayKonnectControlPlane(
	ctx context.Context,
	ext konnectv1alpha2.KonnectExtension,
	dependingConditions ...metav1.Condition,
) (cp *konnectv1alpha2.KonnectGatewayControlPlane, res ctrl.Result, err error) {
	// Get respective KonnectGatewayControlPlane from K8s cluster.
	var errGetFromK8s error
	// TODO: get namespace from cpRef.Namespace when allowed to reference CP from another namespace.
	cpNN := client.ObjectKey{
		Name:      ext.Spec.Konnect.ControlPlane.Ref.KonnectNamespacedRef.Name,
		Namespace: ext.Namespace,
	}
	kgcp := &konnectv1alpha2.KonnectGatewayControlPlane{}
	// Set the controlPlaneRefValidCond to false in case the KonnectGatewayControlPlane is not found.
	if err := r.Get(ctx, cpNN, kgcp); err != nil {
		if apierrors.IsNotFound(err) {
			errGetFromK8s = err
		} else {
			return nil, ctrl.Result{}, err
		}
	}
	cp = kgcp

	controlPlaneRefValidCond := metav1.Condition{
		Type:    konnectv1alpha1.ControlPlaneRefValidConditionType,
		Status:  metav1.ConditionTrue,
		Reason:  konnectv1alpha1.ControlPlaneRefReasonValid,
		Message: "ControlPlaneRef is valid",
	}

	// Check if the KonnectGatewayControlPlane has been found.
	if errGetFromK8s != nil {
		controlPlaneRefValidCond.Status = metav1.ConditionFalse
		controlPlaneRefValidCond.Reason = konnectv1alpha1.ControlPlaneRefReasonInvalid
		controlPlaneRefValidCond.Message = errGetFromK8s.Error()
		if ext.Status.Konnect != nil {
			ext.Status.Konnect.ControlPlaneID = ""
		}
		if res, _, errPatch := patch.StatusWithConditions(
			ctx,
			r.Client,
			&ext,
			append(dependingConditions, controlPlaneRefValidCond)...,
		); errPatch != nil || !res.IsZero() {
			return nil, res, errPatch
		}
		return nil, ctrl.Result{}, errGetFromK8s
	}

	// Set the controlPlaneRefValidCond to false in case the KonnectGatewayControlPlane is not programmed yet.
	// Use NotProgrammed reason to differentiate from permanent Invalid (e.g., CP not found).
	if !k8sutils.HasConditionTrue(konnectv1alpha1.KonnectEntityProgrammedConditionType, cp) {
		controlPlaneRefValidCond.Status = metav1.ConditionFalse
		controlPlaneRefValidCond.Reason = konnectv1alpha1.ControlPlaneRefReasonNotProgrammed
		controlPlaneRefValidCond.Message = fmt.Sprintf("Konnect control plane %s/%s not programmed yet", cp.Name, cp.Namespace)
		if res, _, errPatch := patch.StatusWithConditions(
			ctx,
			r.Client,
			&ext,
			append(dependingConditions, controlPlaneRefValidCond)...,
		); errPatch != nil || !res.IsZero() {
			return nil, res, errPatch
		}
		return nil, ctrl.Result{}, extensionserrors.ErrKonnectGatewayControlPlaneNotProgrammed
	}

	// Set the controlPlaneRefValidCond to true in case the ControlPlane is configured properly.
	if res, _, errPatch := patch.StatusWithConditions(
		ctx,
		r.Client,
		&ext,
		controlPlaneRefValidCond,
	); errPatch != nil || !res.IsZero() {
		return nil, res, errPatch
	}

	return cp, ctrl.Result{}, nil
}

// ensureExtendablesReferencesInStatus ensures that the KonnectExtension references to DataPlane and ControlPlane are up-to-date.
// Only DataPlanes and ControlPlanes with the condition KonnectExtensionApplied=True are added to the status.
func (r *KonnectExtensionReconciler) ensureExtendablesReferencesInStatus(
	ctx context.Context,
	ext *konnectv1alpha2.KonnectExtension,
	dps []operatorv1beta1.DataPlane,
	cps []gwtypes.ControlPlane,
) (ctrl.Result, error) {
	sortRefs := func(refs []commonv1alpha1.NamespacedRef) {
		refToStr := func(ref commonv1alpha1.NamespacedRef) string {
			// We can safely assume that the namespace is not nil, as we fill it when mapping refs.
			return fmt.Sprintf("%s/%s", *ref.Namespace, ref.Name)
		}
		sort.Slice(refs, func(i, j int) bool {
			return refToStr(refs[i]) < refToStr(refs[j])
		})
	}
	hasExtensionAppliedCondition := func(conditions []metav1.Condition) bool {
		return lo.ContainsBy(conditions, func(cond metav1.Condition) bool {
			return cond.Type == string(konnect.KonnectExtensionAppliedType) &&
				cond.Status == metav1.ConditionTrue
		})
	}

	extOld := ext.DeepCopy()

	// Ensure DataPlaneRefs are up-to-date.
	var dpRefs []commonv1alpha1.NamespacedRef
	for _, dp := range dps {
		// Only add DataPlanes with the KonnectExtensionApplied condition set to true.
		if !hasExtensionAppliedCondition(dp.Status.Conditions) {
			continue
		}
		dpRefs = append(dpRefs, commonv1alpha1.NamespacedRef{
			Name:      dp.Name,
			Namespace: &dp.Namespace,
		})
	}
	sortRefs(dpRefs)
	ext.Status.DataPlaneRefs = dpRefs

	// Ensure ControlPlaneRefs are up-to-date.
	var cpRefs []commonv1alpha1.NamespacedRef
	for _, cp := range cps {
		// Only add ControlPlanes with the KonnectExtensionApplied condition set to true.
		if !hasExtensionAppliedCondition(cp.Status.Conditions) {
			continue
		}
		cpRefs = append(cpRefs, commonv1alpha1.NamespacedRef{
			Name:      cp.Name,
			Namespace: &cp.Namespace,
		})
	}
	sortRefs(cpRefs)
	ext.Status.ControlPlaneRefs = cpRefs

	if shouldUpdate := !cmp.Equal(ext.Status, extOld.Status); !shouldUpdate {
		return ctrl.Result{}, nil
	}

	if err := r.Client.Status().Update(ctx, ext); err != nil {
		if apierrors.IsConflict(err) {
			// Gracefully requeue in case of conflict.
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to update KonnectExtension ControlPlane and DataPlane references in status: %w", err)
	}
	return ctrl.Result{Requeue: true}, nil
}

func getKonnectAPIAuthRefNN(cp *konnectv1alpha2.KonnectGatewayControlPlane, ext *konnectv1alpha2.KonnectExtension) (types.NamespacedName, error) {
	var authRef konnectv1alpha2.ControlPlaneKonnectAPIAuthConfigurationRef

	if ext.Status.Konnect != nil && ext.Status.Konnect.AuthRef != nil {
		// Use the AuthRef from the status if available.
		authRef = *ext.Status.Konnect.AuthRef
	} else {
		if cp == nil {
			return types.NamespacedName{}, fmt.Errorf("cannot determine KonnectAPIAuthConfiguration reference")
		}
		// Fallback to the CP spec reference.
		authRef = cp.Spec.KonnectConfiguration.APIAuthConfigurationRef
		if authRef.Namespace == nil {
			authRef.Namespace = &cp.Namespace
		}
	}

	return types.NamespacedName{
		Name:      authRef.Name,
		Namespace: *authRef.Namespace,
	}, nil
}

func (r *KonnectExtensionReconciler) ensureCertificateSecret(ctx context.Context, ext *konnectv1alpha2.KonnectExtension) (op.Result, *corev1.Secret, error) {
	usages := []certificatesv1.KeyUsage{
		certificatesv1.UsageKeyEncipherment,
		certificatesv1.UsageDigitalSignature,
		certificatesv1.UsageClientAuth,
	}
	matchingLabels := client.MatchingLabels{
		consts.SecretProvisioningLabelKey:      consts.SecretProvisioningAutomaticLabelValue,
		SecretKonnectDataPlaneCertificateLabel: "true",
	}
	if r.SecretLabelSelector != "" {
		matchingLabels[r.SecretLabelSelector] = "true"
	}
	owned, err := r.listOwnedCertificateSecrets(ctx, ext)
	if err != nil {
		return op.Noop, nil, err
	}
	var candidates []corev1.Secret
	issuedAt := make(map[string]time.Time)
	for _, secret := range owned {
		if secret.Labels[consts.SecretProvisioningLabelKey] == consts.SecretProvisioningAutomaticLabelValue &&
			secret.DeletionTimestamp.IsZero() {
			candidates = append(candidates, secret)
			issuedAt[secret.Name] = secret.CreationTimestamp.Time
			if value := secret.Annotations[automaticCertificateIssuedAtAnnotation]; value != "" {
				issued, err := time.Parse(time.RFC3339Nano, value)
				if err != nil {
					return op.Noop, nil, fmt.Errorf("invalid issuance time on certificate Secret %s/%s: %w", secret.Namespace, secret.Name, err)
				}
				issuedAt[secret.Name] = issued
			}
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if issuedAt[candidates[i].Name].Equal(issuedAt[candidates[j].Name]) {
			return candidates[i].Name > candidates[j].Name
		}
		return issuedAt[candidates[i].Name].After(issuedAt[candidates[j].Name])
	})
	if len(candidates) > 0 {
		current := &candidates[0]
		pair, err := tls.X509KeyPair(current.Data[corev1.TLSCertKey], current.Data[corev1.TLSPrivateKeyKey])
		if err == nil && pair.Leaf == nil {
			pair.Leaf, err = x509.ParseCertificate(pair.Certificate[0])
		}
		switch {
		case err != nil:
			ctrl.LoggerFrom(ctx).Error(err, "Replacing invalid automatically provisioned certificate", "secret", client.ObjectKeyFromObject(current))
		case pair.Leaf.Subject.CommonName != fmt.Sprintf("%s.%s", ext.Name, ext.Namespace):
			ctrl.LoggerFrom(ctx).Info("Replacing automatically provisioned certificate with unexpected subject", "secret", client.ObjectKeyFromObject(current))
		default:
			renewAt := pair.Leaf.NotAfter.Add(-r.CertExpirationMargin)
			if current.Annotations[automaticCertificateIssuedAtAnnotation] != "" &&
				!current.CreationTimestamp.IsZero() && !renewAt.After(current.CreationTimestamp.Time) {
				return op.Noop, nil, fmt.Errorf("certificate Secret %s/%s has no renewal interval: increase --cert-ttl or decrease --cert-expiration-margin",
					current.Namespace, current.Name)
			}
			// Finish publishing and migrating a pending generation before issuing
			// another. Old Secrets are deliberately retained during the rollout.
			if time.Now().Before(renewAt) || len(candidates) > 1 ||
				(ext.Status.DataPlaneClientAuth != nil &&
					ext.Status.DataPlaneClientAuth.CertificateSecretRef != nil &&
					ext.Status.DataPlaneClientAuth.CertificateSecretRef.Name != current.Name) {
				if current.Immutable == nil || !*current.Immutable {
					current.Immutable = new(true)
					if err := r.Update(ctx, current); err != nil {
						return op.Noop, nil, err
					}
					return op.Updated, current, nil
				}
				return op.Noop, current, nil
			}
		}
	}
	return secrets.GenerateCertificate(ctx,
		ext,
		fmt.Sprintf("%s.%s", ext.Name, ext.Namespace),
		types.NamespacedName{
			Namespace: r.ClusterCASecretNamespace,
			Name:      r.ClusterCASecretName,
		},
		usages,
		r.Client,
		matchingLabels,
		r.CertTTL,
		func(secret *corev1.Secret) {
			secret.Immutable = new(true)
			secret.Annotations = map[string]string{
				automaticCertificateIssuedAtAnnotation: time.Now().UTC().Format(time.RFC3339Nano),
			}
		},
	)
}

func (r *KonnectExtensionReconciler) getCertificateSecret(ctx context.Context, ext konnectv1alpha2.KonnectExtension) (op.Result, *corev1.Secret, error) {
	var (
		certificateSecret  = &corev1.Secret{}
		err                error
		res                = op.Noop
		manualProvisioning = ext.Spec.ClientAuth != nil &&
			ext.Spec.ClientAuth.CertificateSecret.Provisioning != nil &&
			*ext.Spec.ClientAuth.CertificateSecret.Provisioning == konnectv1alpha2.ManualSecretProvisioning
	)

	switch {
	case manualProvisioning:
		// No need to check CertificateSecretRef is nil, as it is enforced at the CRD level.
		err = r.Get(ctx, types.NamespacedName{
			Namespace: ext.Namespace,
			Name:      ext.Spec.ClientAuth.CertificateSecret.CertificateSecretRef.Name,
		}, certificateSecret)
		if err == nil {
			return r.ensureCertificateSnapshot(ctx, &ext, certificateSecret)
		}
	default:
		res, certificateSecret, err = r.ensureCertificateSecret(ctx, &ext)
	}

	return res, certificateSecret, err
}

func (r *KonnectExtensionReconciler) certificateRenewalTime(secret *corev1.Secret) (time.Time, error) {
	block, _ := pem.Decode(secret.Data[corev1.TLSCertKey])
	if block == nil {
		return time.Time{}, fmt.Errorf("invalid certificate in Secret %s/%s", secret.Namespace, secret.Name)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid certificate in Secret %s/%s: %w", secret.Namespace, secret.Name, err)
	}
	return cert.NotAfter.Add(-r.CertExpirationMargin), nil
}

func enforceKonnectExtensionStatus(
	cp *konnectv1alpha2.KonnectGatewayControlPlane,
	apiAuthRef konnectv1alpha2.ControlPlaneKonnectAPIAuthConfigurationRef,
	certificateSecret corev1.Secret,
	ext *konnectv1alpha2.KonnectExtension,
) bool {
	var toUpdate bool

	// The Status schema requires non-empty endpoints; updating with empty values triggers validation errors.
	// When endpoints are unavailable, clear the entire Konnect status struct.
	if cp == nil ||
		cp.Status.Endpoints == nil ||
		cp.Status.Endpoints.ControlPlaneEndpoint == "" ||
		cp.Status.Endpoints.TelemetryEndpoint == "" {
		if ext.Status.Konnect != nil {
			ext.Status.Konnect = nil
			toUpdate = true
		}
	} else {
		expectedKonnectStatus := &konnectv1alpha2.KonnectExtensionControlPlaneStatus{
			ControlPlaneID: cp.Status.ID,
			ClusterType:    konnectClusterTypeToCRDClusterType(cp.Status.ClusterType),
			AuthRef:        &apiAuthRef,
			Endpoints: konnectv1alpha2.KonnectEndpoints{
				ControlPlaneEndpoint: cp.Status.Endpoints.ControlPlaneEndpoint,
				TelemetryEndpoint:    cp.Status.Endpoints.TelemetryEndpoint,
			},
		}

		if !cmp.Equal(ext.Status.Konnect, expectedKonnectStatus) {
			ext.Status.Konnect = expectedKonnectStatus
			toUpdate = true
		}
	}

	expectedDataPlaneClientAuth := &konnectv1alpha2.DataPlaneClientAuthStatus{
		CertificateSecretRef: &konnectv1alpha2.SecretRef{
			Name: certificateSecret.Name,
		},
	}
	if !cmp.Equal(ext.Status.DataPlaneClientAuth, expectedDataPlaneClientAuth) {
		ext.Status.DataPlaneClientAuth = expectedDataPlaneClientAuth
		toUpdate = true
	}

	return toUpdate
}

func konnectClusterTypeToCRDClusterType(clusterType sdkkonnectcomp.ControlPlaneClusterType) konnectv1alpha2.KonnectExtensionClusterType {
	switch clusterType {
	// When it's not specified by the caller (left empty) in Konnect it's set
	// to CLUSTER_TYPE_CONTROL_PLANE.
	case "",
		sdkkonnectcomp.ControlPlaneClusterTypeClusterTypeControlPlane:
		return konnectv1alpha2.ClusterTypeControlPlane
	case sdkkonnectcomp.ControlPlaneClusterTypeClusterTypeK8SIngressController:
		return konnectv1alpha2.ClusterTypeK8sIngressController
	case sdkkonnectcomp.ControlPlaneClusterTypeClusterTypeControlPlaneGroup:
		return konnectv1alpha2.ClusterTypeControlPlaneGroup
	default:
		// Simply translate the SDK cluster type to the CRD cluster type.
		// This way bubble up any unsupported cluster types to be handled at the CRD validation level,
		// instead of silently ignored here.
		return konnectv1alpha2.KonnectExtensionClusterType(clusterType)
	}
}

func sanitizeCert(cert string) string {
	newCert := strings.TrimSuffix(cert, "\n")
	newCert = strings.ReplaceAll(newCert, "\r", "")
	return newCert
}

// dataPlaneClientCertificateName returns the name of the KongDataPlaneClientCertificate registering certData.
// It is the KonnectExtension name, unless an existing KongDataPlaneClientCertificate with that name registers
// another certificate (spec.cert is immutable): then a digest of certData is appended, so that both can exist
// until consumers migrate to the new generation.
func dataPlaneClientCertificateName(
	extName string,
	existing []configurationv1alpha1.KongDataPlaneClientCertificate,
	certData string,
) string {
	for _, c := range existing {
		if c.DeletionTimestamp.IsZero() && sanitizeCert(c.Spec.Cert) == sanitizeCert(certData) {
			return c.Name
		}
	}
	for _, c := range existing {
		if c.Name == extName && sanitizeCert(c.Spec.Cert) != sanitizeCert(certData) {
			digest := sha256.Sum256([]byte(sanitizeCert(certData)))
			const maxPrefixLength = validation.DNS1123SubdomainMaxLength - 9
			if len(extName) > maxPrefixLength {
				// Include the full name so truncation cannot merge two extensions' names.
				digest = sha256.Sum256([]byte(extName + "\x00" + sanitizeCert(certData)))
				extName = strings.TrimRight(extName[:maxPrefixLength], ".-")
			}
			return fmt.Sprintf("%s-%x", extName, digest[:4])
		}
	}
	return extName
}
