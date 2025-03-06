package konnect

import (
	"context"
	"errors"
	"fmt"
	"strings"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/go-logr/logr"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/controller/konnect/ops"
	sdkops "github.com/kong/gateway-operator/controller/konnect/ops/sdk"
	"github.com/kong/gateway-operator/controller/pkg/log"
	"github.com/kong/gateway-operator/controller/pkg/patch"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/api/common/v1alpha1"
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

	switch ext.Spec.KonnectControlPlane.ControlPlaneRef.Type {
	case commonv1alpha1.ControlPlaneRefKonnectNamespacedRef:
		// in case the ControlPlaneRef is a KonnectNamespacedRef, we fetch the KonnectGatewayControlPlane
		// and get the KonnectID from `status.konnectID`.
		cpRef := ext.Spec.KonnectControlPlane.ControlPlaneRef.KonnectNamespacedRef
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
				return nil, res, errPatch
			}
			return nil, ctrl.Result{}, nil
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
				return nil, res, errPatch
			}
			return nil, ctrl.Result{}, nil
		}
		konnectCPID = kgcp.GetKonnectID()
	case commonv1alpha1.ControlPlaneRefKonnectID:
		// in case the ControlPlaneRef is a KonnectID, we use it directly.
		konnectCPID = *ext.Spec.KonnectControlPlane.ControlPlaneRef.KonnectID
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
		return nil, ctrl.Result{RequeueAfter: r.syncPeriod}, nil
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

func getKonnectAPIAuthRefNN(ctx context.Context, cl client.Client, ext *konnectv1alpha1.KonnectExtension) (types.NamespacedName, error) {
	if ext.Spec.KonnectConfiguration != nil {
		// TODO: handle cross namespace refs when supported.
		return types.NamespacedName{
			Namespace: ext.Namespace,
			Name:      ext.Spec.KonnectConfiguration.APIAuthConfigurationRef.Name,
		}, nil
	}

	// In case the KonnectConfiguration is not set, we fetch the KonnectGatewayControlPlane
	// and get the KonnectConfiguration from `spec.konnectControlPlane.controlPlane.konnectNamespacedRef`.
	// KonnectGatewayControlPlane reference and KonnectConfiguration
	// are mutually exclusive in the KonnectExtension API.
	cpRef := ext.Spec.KonnectControlPlane.ControlPlaneRef.KonnectNamespacedRef
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

func getCertificateSecret(ctx context.Context, cl client.Client, ext konnectv1alpha1.KonnectExtension) (*corev1.Secret, error) {
	var certificateSecret corev1.Secret
	switch *ext.Spec.DataPlaneClientAuth.CertificateSecret.Provisioning {
	case konnectv1alpha1.ManualSecretProvisioning:
		// No need to check CertificateSecretRef is nil, as it is enforced at the CRD level.
		if err := cl.Get(ctx, types.NamespacedName{
			Namespace: ext.Namespace,
			Name:      ext.Spec.DataPlaneClientAuth.CertificateSecret.CertificateSecretRef.Name,
		}, &certificateSecret); err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("automatic secret provisioning not supported yet")
	}
	return &certificateSecret, nil
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
