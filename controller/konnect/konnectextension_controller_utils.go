package konnect

import (
	"context"
	"fmt"
	"sort"
	"strings"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"github.com/samber/lo"
	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/controller/konnect/ops"
	sdkops "github.com/kong/gateway-operator/controller/konnect/ops/sdk"
	extensionserrors "github.com/kong/gateway-operator/controller/pkg/extensions/errors"
	"github.com/kong/gateway-operator/controller/pkg/log"
	"github.com/kong/gateway-operator/controller/pkg/op"
	"github.com/kong/gateway-operator/controller/pkg/patch"
	"github.com/kong/gateway-operator/controller/pkg/secrets"
	"github.com/kong/gateway-operator/pkg/consts"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/api/common/v1alpha1"
	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
	"github.com/kong/kubernetes-configuration/api/konnect"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

// getKonnectControlPlane retrieves the Konnect Control Plane based on the provided KonnectExtension specification.
// It supports two types of ControlPlaneRef: KonnectNamespacedRef and KonnectID.
//
// Returns:
// - cp: The retrieved Konnect Control Plane.
// - res: The result of the controller reconciliation.
// - err: An error if the retrieval fails.
func (r *KonnectExtensionReconciler) getKonnectControlPlane(
	ctx context.Context,
	logger logr.Logger,
	sdk sdkops.ControlPlaneSDK,
	ext konnectv1alpha1.KonnectExtension,
	dependingConditions ...metav1.Condition,
) (cp *sdkkonnectcomp.ControlPlane, res ctrl.Result, err error) {
	controlPlaneRefValidCond := metav1.Condition{
		Type:    konnectv1alpha1.ControlPlaneRefValidConditionType,
		Status:  metav1.ConditionTrue,
		Reason:  konnectv1alpha1.ControlPlaneRefReasonValid,
		Message: "ControlPlaneRef is valid",
	}

	konnectCPID, res, err := r.getKonnectControlPlaneID(ctx, ext, dependingConditions...)
	if err != nil || !res.IsZero() {
		return nil, res, err
	}

	// get the Konnect Control Plane from Konnect
	konnectCP, err := ops.GetControlPlaneByID(ctx, sdk, konnectCPID)
	// set the controlPlaneRefValidCond to false in case the Control Plane is not found in Konnect
	if err != nil {
		controlPlaneRefValidCond.Status = metav1.ConditionFalse
		controlPlaneRefValidCond.Reason = konnectv1alpha1.ControlPlaneRefReasonInvalid
		controlPlaneRefValidCond.Message = err.Error()
		if res, _, errPatch := patch.StatusWithConditions(
			ctx,
			r.Client,
			&ext,
			append(dependingConditions, controlPlaneRefValidCond)...,
		); errPatch != nil || !res.IsZero() {
			return nil, res, errPatch
		}
		log.Debug(logger, "ControlPlane retrieval failed in Konnect")
		// Setting "Requeue: true" along with RequeueAfter makes the controller bulletproof, as
		// if the syncPeriod is set to zero, the controller won't requeue.
		return nil, ctrl.Result{Requeue: true, RequeueAfter: r.SyncPeriod}, nil
	}

	// set the controlPlaneRefValidCond to true in case the Control Plane is found in Konnect
	if res, _, errPatch := patch.StatusWithConditions(
		ctx,
		r.Client,
		&ext,
		controlPlaneRefValidCond,
	); errPatch != nil || !res.IsZero() {
		return nil, res, errPatch
	}

	return konnectCP, ctrl.Result{}, err
}

func (r *KonnectExtensionReconciler) getKonnectControlPlaneID(
	ctx context.Context,
	ext konnectv1alpha1.KonnectExtension,
	dependingConditions ...metav1.Condition,
) (id string, res ctrl.Result, err error) {
	var (
		konnectCPID string
		// init the controlPlaneRefValidCond with the assumption that the ControlPlaneRef is valid
		controlPlaneRefValidCond = metav1.Condition{
			Type:    konnectv1alpha1.ControlPlaneRefValidConditionType,
			Status:  metav1.ConditionTrue,
			Reason:  konnectv1alpha1.ControlPlaneRefReasonValid,
			Message: "ControlPlaneRef is valid",
		}
	)

	switch ext.Spec.Konnect.ControlPlane.Ref.Type {
	case commonv1alpha1.ControlPlaneRefKonnectNamespacedRef:
		// in case the ControlPlaneRef is a KonnectNamespacedRef, we fetch the KonnectGatewayControlPlane
		// and get the KonnectID from `status.konnectID`.
		cpRef := ext.Spec.Konnect.ControlPlane.Ref.KonnectNamespacedRef
		cpNamepace := ext.Namespace
		// TODO: get namespace from cpRef.Namespace when allowed to reference CP from another namespace.
		kgcp := &konnectv1alpha1.KonnectGatewayControlPlane{}
		err := r.Get(ctx, client.ObjectKey{
			Namespace: cpNamepace,
			Name:      cpRef.Name,
		}, kgcp)

		// set the controlPlaneRefValidCond to false in case the KonnectGatewayControlPlane is not found
		if err != nil {
			controlPlaneRefValidCond.Status = metav1.ConditionFalse
			controlPlaneRefValidCond.Reason = konnectv1alpha1.ControlPlaneRefReasonInvalid
			controlPlaneRefValidCond.Message = err.Error()
			if res, _, errPatch := patch.StatusWithConditions(
				ctx,
				r.Client,
				&ext,
				append(dependingConditions, controlPlaneRefValidCond)...,
			); errPatch != nil || !res.IsZero() {
				return "", res, errPatch
			}
			return "", ctrl.Result{}, err
		}

		// set the controlPlaneRefValidCond to false in case the KonnectGatewayControlPlane is not programmed yet
		if !lo.ContainsBy(kgcp.Status.Conditions, func(cond metav1.Condition) bool {
			return cond.Type == konnectv1alpha1.KonnectEntityProgrammedConditionType &&
				cond.Status == metav1.ConditionTrue
		}) {
			controlPlaneRefValidCond.Status = metav1.ConditionFalse
			controlPlaneRefValidCond.Reason = konnectv1alpha1.ControlPlaneRefReasonInvalid
			controlPlaneRefValidCond.Message = fmt.Sprintf("Konnect control plane %s/%s not programmed yet", cpNamepace, cpRef.Name)
			if res, _, errPatch := patch.StatusWithConditions(
				ctx,
				r.Client,
				&ext,
				append(dependingConditions, controlPlaneRefValidCond)...,
			); errPatch != nil || !res.IsZero() {
				return "", res, errPatch
			}
			return "", ctrl.Result{}, extensionserrors.ErrKonnectGatewayControlPlaneNotProgrammed
		}
		konnectCPID = kgcp.GetKonnectID()
	case commonv1alpha1.ControlPlaneRefKonnectID:
		// in case the ControlPlaneRef is a KonnectID, we use it directly.
		konnectCPID = string(*ext.Spec.Konnect.ControlPlane.Ref.KonnectID)
	}

	return konnectCPID, ctrl.Result{}, nil
}

// ensureExtendablesReferencesInStatus ensures that the KonnectExtension references to DataPlane and ControlPlane are up-to-date.
// Only DataPlanes and ControlPlanes with the condition KonnectExtensionApplied=True are added to the status.
func (r *KonnectExtensionReconciler) ensureExtendablesReferencesInStatus(
	ctx context.Context,
	ext *konnectv1alpha1.KonnectExtension,
	dps operatorv1beta1.DataPlaneList,
	cps operatorv1beta1.ControlPlaneList,
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

func getKonnectAPIAuthRefNN(ctx context.Context, cl client.Client, ext *konnectv1alpha1.KonnectExtension) (types.NamespacedName, error) {
	if ext.Spec.Konnect.Configuration != nil {
		// TODO: handle cross namespace refs when supported.
		return types.NamespacedName{
			Namespace: ext.Namespace,
			Name:      ext.Spec.Konnect.Configuration.APIAuthConfigurationRef.Name,
		}, nil
	}

	// In case the KonnectConfiguration is not set, we fetch the KonnectGatewayControlPlane
	// and get the KonnectConfiguration from `spec.konnectControlPlane.controlPlane.konnectNamespacedRef`.
	// KonnectGatewayControlPlane reference and KonnectConfiguration
	// are mutually exclusive in the KonnectExtension API.
	cpRef := ext.Spec.Konnect.ControlPlane.Ref.KonnectNamespacedRef
	kgcp := &konnectv1alpha1.KonnectGatewayControlPlane{}
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
	usages := []certificatesv1.KeyUsage{
		certificatesv1.UsageKeyEncipherment,
		certificatesv1.UsageDigitalSignature,
		certificatesv1.UsageClientAuth,
	}
	matchingLabels := client.MatchingLabels{
		consts.SecretProvisioningLabelKey:      consts.SecretProvisioningAutomaticLabelValue,
		SecretKonnectDataPlaneCertificateLabel: "true",
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

func (r *KonnectExtensionReconciler) getCertificateSecret(ctx context.Context, ext konnectv1alpha1.KonnectExtension, cleanup bool) (op.Result, *corev1.Secret, error) {
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
	case *ext.Spec.ClientAuth.CertificateSecret.Provisioning == konnectv1alpha1.ManualSecretProvisioning:
		// No need to check CertificateSecretRef is nil, as it is enforced at the CRD level.
		err = r.Get(ctx, types.NamespacedName{
			Namespace: ext.Namespace,
			Name:      ext.Spec.ClientAuth.CertificateSecret.CertificateSecretRef.Name,
		}, certificateSecret)
	case *ext.Spec.ClientAuth.CertificateSecret.Provisioning == konnectv1alpha1.AutomaticSecretProvisioning:
		res, certificateSecret, err = r.ensureCertificateSecret(ctx, &ext)
	}
	return res, certificateSecret, err
}

func konnectClusterTypeToCRDClusterType(clusterType sdkkonnectcomp.ControlPlaneClusterType) konnectv1alpha1.KonnectExtensionClusterType {
	switch clusterType {
	case sdkkonnectcomp.ControlPlaneClusterTypeClusterTypeControlPlane:
		return konnectv1alpha1.ClusterTypeControlPlane
	case sdkkonnectcomp.ControlPlaneClusterTypeClusterTypeK8SIngressController:
		return konnectv1alpha1.ClusterTypeK8sIngressController
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
