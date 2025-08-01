package konnect

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/google/go-cmp/cmp"
	"github.com/samber/lo"
	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/common/v1alpha1"
	operatorv1beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1beta1"
	"github.com/kong/kubernetes-configuration/v2/api/konnect"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha2"

	extensionserrors "github.com/kong/kong-operator/controller/pkg/extensions/errors"
	"github.com/kong/kong-operator/controller/pkg/op"
	"github.com/kong/kong-operator/controller/pkg/patch"
	"github.com/kong/kong-operator/controller/pkg/secrets"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/internal/utils/index"
	"github.com/kong/kong-operator/pkg/consts"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
	commonv1alpha1 "github.com/kong/kubernetes-configuration/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
	"github.com/kong/kubernetes-configuration/api/konnect"

	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// getGatewayKonnectControlPlane retrieves the Konnect Control Plane from K8s cluster
// based on the provided KonnectExtension specification.
// It supports two types of ControlPlaneRef: KonnectNamespacedRef and KonnectID.
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
		if k8serrors.IsNotFound(err) {
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
	if !k8sutils.HasConditionTrue(konnectv1alpha1.KonnectEntityProgrammedConditionType, cp) {
		controlPlaneRefValidCond.Status = metav1.ConditionFalse
		controlPlaneRefValidCond.Reason = konnectv1alpha1.ControlPlaneRefReasonInvalid
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
	dps operatorv1beta1.DataPlaneList,
	cps gwtypes.ControlPlaneList,
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
	for _, dp := range dps.Items {
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
	for _, cp := range cps.Items {
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
		if k8serrors.IsConflict(err) {
			// Gracefully requeue in case of conflict.
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to update KonnectExtension ControlPlane and DataPlane references in status: %w", err)
	}
	return ctrl.Result{Requeue: true}, nil
}

func getKonnectAPIAuthRefNN(ctx context.Context, cl client.Client, ext *konnectv1alpha2.KonnectExtension) (types.NamespacedName, error) {
	cpRef := ext.Spec.Konnect.ControlPlane.Ref.KonnectNamespacedRef
	kgcp := &konnectv1alpha2.KonnectGatewayControlPlane{}
	err := cl.Get(ctx, client.ObjectKey{
		// TODO: handle cross namespace refs to KonnectGatewayControlPlane when referencing CP from another namespace is supported.
		Namespace: ext.Namespace,
		Name:      cpRef.Name,
	}, kgcp)
	if err != nil {
		return types.NamespacedName{}, err
	}
	return types.NamespacedName{
		Namespace: kgcp.Namespace,
		Name:      kgcp.Spec.KonnectConfiguration.APIAuthConfigurationRef.Name,
	}, nil
}

func (r *KonnectExtensionReconciler) ensureCertificateSecret(ctx context.Context, ext *konnectv1alpha1.KonnectExtension) (op.Result, *corev1.Secret, error) {
	hasCPRef := ext.Spec.Konnect.ControlPlane.Ref.Type == configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef

	usages := []certificatesv1.KeyUsage{
		certificatesv1.UsageKeyEncipherment,
		certificatesv1.UsageDigitalSignature,
		certificatesv1.UsageClientAuth,
	}
	matchingLabels := client.MatchingLabels{
		consts.SecretProvisioningLabelKey:                consts.SecretProvisioningAutomaticLabelValue,
		SecretKonnectDataPlaneCertificateLabel:           "true",
		SecretKonnectDataPlaneCertificateReconcilerLabel: strconv.FormatBool(hasCPRef),
	}
	if r.SecretLabelSelector != "" {
		matchingLabels[r.SecretLabelSelector] = "true"
	}
	return secrets.EnsureCertificate(ctx,
		ext,
		fmt.Sprintf("%s.%s", ext.Name, ext.Namespace),
		types.NamespacedName{
			Namespace: r.ClusterCASecretNamespace,
			Name:      r.ClusterCASecretName,
		},
		usages,
		r.ClusterCAKeyConfig,
		r.Client,
		matchingLabels,
	)
}

func (r *KonnectExtensionReconciler) getCertificateSecret(ctx context.Context, ext konnectv1alpha2.KonnectExtension, cleanup bool) (op.Result, *corev1.Secret, error) {
	var (
		certificateSecret = &corev1.Secret{}
		err               error
		res               = op.Noop
	)

	switch {
	case cleanup:
		if ext.Status.DataPlaneClientAuth != nil && ext.Status.DataPlaneClientAuth.CertificateSecretRef != nil {
			err = r.Get(ctx, types.NamespacedName{
				Namespace: ext.Namespace,
				Name:      ext.Status.DataPlaneClientAuth.CertificateSecretRef.Name,
			}, certificateSecret)
		}
	case *ext.Spec.ClientAuth.CertificateSecret.Provisioning == konnectv1alpha2.ManualSecretProvisioning:
		// No need to check CertificateSecretRef is nil, as it is enforced at the CRD level.
		err = r.Get(ctx, types.NamespacedName{
			Namespace: ext.Namespace,
			Name:      ext.Spec.ClientAuth.CertificateSecret.CertificateSecretRef.Name,
		}, certificateSecret)
	case *ext.Spec.ClientAuth.CertificateSecret.Provisioning == konnectv1alpha2.AutomaticSecretProvisioning:
		res, certificateSecret, err = r.ensureCertificateSecret(ctx, &ext)
	}
	return res, certificateSecret, err
}

func enforceKonnectExtensionStatus(cp konnectv1alpha2.KonnectGatewayControlPlane, certificateSecret corev1.Secret, ext *konnectv1alpha2.KonnectExtension) bool {
	var toUpdate bool
	expectedKonnectStatus := &konnectv1alpha2.KonnectExtensionControlPlaneStatus{
		ControlPlaneID: cp.Status.ID,
		ClusterType: konnectClusterTypeToCRDClusterType(
			sdkkonnectcomp.ControlPlaneClusterType(lo.FromPtrOr(cp.GetKonnectClusterType(), "")),
		),
	}

	if cp.Status.Endpoints != nil {
		expectedKonnectStatus.Endpoints = konnectv1alpha2.KonnectEndpoints{
			ControlPlaneEndpoint: cp.Status.Endpoints.ControlPlaneEndpoint,
			TelemetryEndpoint:    cp.Status.Endpoints.TelemetryEndpoint,
		}
	}

	if !cmp.Equal(ext.Status.Konnect, expectedKonnectStatus) {
		ext.Status.Konnect = expectedKonnectStatus
		toUpdate = true
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
	// When it's not specified by the caller (left empty) in Konnect it's set to CLUSTER_TYPE_CONTROL_PLANE.
	case sdkkonnectcomp.ControlPlaneClusterTypeClusterTypeControlPlane, "":
		return konnectv1alpha2.ClusterTypeControlPlane
	case sdkkonnectcomp.ControlPlaneClusterTypeClusterTypeK8SIngressController:
		return konnectv1alpha2.ClusterTypeK8sIngressController
	default:
		// default never happens as the validation is at the CRD level
		return ""
	}
}

func sanitizeCert(cert string) string {
	newCert := strings.TrimSuffix(cert, "\n")
	newCert = strings.ReplaceAll(newCert, "\r", "")
	return newCert
}

const (
	// SecretKonnectDataPlaneCertificateLabel is the label to mark that the secret is used as a Konnect DP certificate.
	// A secret must have the label to be watched by the KonnectExtension reconciler.
	SecretKonnectDataPlaneCertificateLabel = "konghq.com/konnect-dp-cert" //nolint:gosec

	// TODO: comment
	SecretKonnectDataPlaneCertificateReconcilerLabel = "konghq.com/konnect-dp-cert-reconciler"
)

func listKonnectExtensionsBySecret(ctx context.Context, cl client.Client, s *corev1.Secret) ([]konnectv1alpha1.KonnectExtension, error) {
	// Get all the secrets explicitly referenced by KonnectExtensions in the spec.
	l := &konnectv1alpha1.KonnectExtensionList{}
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
			ownerRef.Kind == konnectv1alpha1.KonnectExtensionKind &&
			ownerRef.APIVersion == konnectv1alpha1.GroupVersion.String() {
			owner := &konnectv1alpha1.KonnectExtension{}
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
