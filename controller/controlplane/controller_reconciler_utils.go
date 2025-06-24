package controlplane

import (
	"context"
	"fmt"

	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/controller/pkg/op"
	"github.com/kong/gateway-operator/controller/pkg/secrets"
	"github.com/kong/gateway-operator/pkg/consts"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"

	kcfgcontrolplane "github.com/kong/kubernetes-configuration/api/gateway-operator/controlplane"
	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
	operatorv2alpha1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v2alpha1"
)

// -----------------------------------------------------------------------------
// Reconciler - Status Management
// -----------------------------------------------------------------------------

func (r *Reconciler) ensureIsMarkedScheduled(
	cp *ControlPlane,
) bool {
	_, present := k8sutils.GetCondition(kcfgcontrolplane.ConditionTypeProvisioned, cp)
	if !present {
		condition := k8sutils.NewCondition(
			kcfgcontrolplane.ConditionTypeProvisioned,
			metav1.ConditionFalse,
			kcfgcontrolplane.ConditionReasonPodsNotReady,
			"ControlPlane resource is scheduled for provisioning",
		)

		k8sutils.SetCondition(condition, cp)
		return true
	}

	return false
}

// ensureDataPlaneStatus ensures that the dataplane is in the correct state
// to carry on with the controlplane deployments reconciliation.
// Information about the missing dataplane is stored in the controlplane status.
func (r *Reconciler) ensureDataPlaneStatus(
	cp *ControlPlane,
	dataplane *operatorv1beta1.DataPlane,
) (dataplaneIsSet bool, err error) {
	switch cp.Spec.DataPlane.Type {
	case operatorv2alpha1.ControlPlaneDataPlaneTargetRefType:
		dataplaneIsSet = cp.Spec.DataPlane.Ref != nil && cp.Spec.DataPlane.Ref.Name == dataplane.Name

		var newCondition metav1.Condition
		if dataplaneIsSet {
			newCondition = k8sutils.NewCondition(
				kcfgcontrolplane.ConditionTypeProvisioned,
				metav1.ConditionFalse,
				kcfgcontrolplane.ConditionReasonPodsNotReady,
				"DataPlane was set, ControlPlane resource is scheduled for provisioning",
			)
		} else {
			newCondition = k8sutils.NewCondition(
				kcfgcontrolplane.ConditionTypeProvisioned,
				metav1.ConditionFalse,
				kcfgcontrolplane.ConditionReasonNoDataPlane,
				"DataPlane is not set",
			)
		}

		condition, present := k8sutils.GetCondition(kcfgcontrolplane.ConditionTypeProvisioned, cp)
		if !present || condition.Status != newCondition.Status || condition.Reason != newCondition.Reason {
			k8sutils.SetCondition(newCondition, cp)
		}
		return dataplaneIsSet, nil

	// TODO(pmalek): implement DataPlane URL type

	default:
		return false, fmt.Errorf("unsupported ControlPlane's DataPlane type: %s", cp.Spec.DataPlane.Type)
	}
}

// ensureAdminMTLSCertificateSecret ensures that a Secret is created with the certificate for mTLS
// communication between the ControlPlane and the DataPlane.
func (r *Reconciler) ensureAdminMTLSCertificateSecret(
	ctx context.Context,
	cp *operatorv2alpha1.ControlPlane,
) (
	op.Result,
	*corev1.Secret,
	error,
) {
	usages := []certificatesv1.KeyUsage{
		certificatesv1.UsageKeyEncipherment,
		certificatesv1.UsageDigitalSignature,
		certificatesv1.UsageClientAuth,
	}
	matchingLabels := client.MatchingLabels{
		consts.SecretUsedByServiceLabel: consts.ControlPlaneServiceKindAdmin,
	}
	// this subject is arbitrary. data planes only care that client certificates are signed by the trusted CA, and will
	// accept a certificate with any subject
	return secrets.EnsureCertificate(ctx,
		cp,
		fmt.Sprintf("%s.%s", cp.Name, cp.Namespace),
		k8stypes.NamespacedName{
			Namespace: r.ClusterCASecretNamespace,
			Name:      r.ClusterCASecretName,
		},
		usages,
		r.ClusterCAKeyConfig,
		r.Client,
		matchingLabels,
	)
}
