package controlplane

import (
	"context"
	"errors"
	"fmt"

	"github.com/samber/lo"
	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kcfgcontrolplane "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/controlplane"
	operatorv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1alpha1"
	operatorv1beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1beta1"

	operatorv2beta1 "github.com/kong/kong-operator/apis/v2beta1"
	"github.com/kong/kong-operator/controller/pkg/op"
	"github.com/kong/kong-operator/controller/pkg/secrets"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/pkg/consts"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
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
			kcfgcontrolplane.ConditionReasonProvisioningInProgress,
			"ControlPlane resource is scheduled for provisioning",
		)

		k8sutils.SetCondition(condition, cp)
		return true
	}

	return false
}

// ensureDataPlaneStatus ensures that the dataplane is in the correct state
// to carry on with the controlplane reconciliation.
// Information about the missing dataplane is stored in the controlplane status.
func (r *Reconciler) ensureDataPlaneStatus(
	cp *ControlPlane,
	dataplane *operatorv1beta1.DataPlane,
) (dataplaneIsSet bool, err error) {
	switch cp.Spec.DataPlane.Type {
	case gwtypes.ControlPlaneDataPlaneTargetRefType:
		dataplaneIsSet = cp.Spec.DataPlane.Ref != nil && cp.Spec.DataPlane.Ref.Name == dataplane.Name

	case gwtypes.ControlPlaneDataPlaneTargetManagedByType:
		dataplaneIsSet = cp.Status.DataPlane != nil && cp.Status.DataPlane.Name == dataplane.Name

	default:
		return false, fmt.Errorf("unsupported ControlPlane's DataPlane type: %s", cp.Spec.DataPlane.Type)
	}

	var newCondition metav1.Condition
	if dataplaneIsSet {
		newCondition = k8sutils.NewCondition(
			kcfgcontrolplane.ConditionTypeProvisioned,
			metav1.ConditionFalse,
			kcfgcontrolplane.ConditionReasonProvisioningInProgress,
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
	cp.Status.DataPlane = &gwtypes.ControlPlaneDataPlaneStatus{
		Name: dataplane.Name,
	}

	return dataplaneIsSet, nil
}

// ensureAdminMTLSCertificateSecret ensures that a Secret is created with the certificate for mTLS
// communication between the ControlPlane and the DataPlane.
func (r *Reconciler) ensureAdminMTLSCertificateSecret(
	ctx context.Context,
	cp *gwtypes.ControlPlane,
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
	if r.SecretLabelSelector != "" {
		matchingLabels[r.SecretLabelSelector] = "true"
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

func (r *Reconciler) validateWatchNamespaceGrants(
	ctx context.Context,
	cp *ControlPlane,
) ([]string, error) {
	if cp.Spec.WatchNamespaces == nil {
		return nil, errors.New("spec.watchNamespaces cannot be empty")
	}

	switch cp.Spec.WatchNamespaces.Type {
	// NOTE: We currentlty do not require any ReferenceGrants or other permission
	// granting resources for the "All" case.
	case operatorv2beta1.WatchNamespacesTypeAll:
		return nil, nil
	// No special permissions are required to watch the controlplane's own namespace.
	case operatorv2beta1.WatchNamespacesTypeOwn:
		return []string{cp.Namespace}, nil
	case operatorv2beta1.WatchNamespacesTypeList:
		var nsList []string
		for _, ns := range cp.Spec.WatchNamespaces.List {
			if err := ensureWatchNamespaceGrantsForNamespace(ctx, r.Client, cp, ns); err != nil {
				return nsList, err
			}
			nsList = append(nsList, ns)
		}
		// Add ControlPlane's own namespace as it will add it anyway because
		// that's where the default "publish service" exists.
		// We add it here as we do not require a ReferenceGrant for own namespace
		// so there's no validation whether a grant exists.
		nsList = append(nsList, cp.Namespace)

		return nsList, nil
	default:
		return nil, fmt.Errorf("unexpected watchNamespaces.type: %q", cp.Spec.WatchNamespaces.Type)
	}
}

// +kubebuilder:rbac:groups=gateway-operator.konghq.com,resources=watchnamespacegrants,verbs=list

// ensureWatchNamespaceGrantsForNamespace ensures that a WatchNamespaceGrant exists for the
// given namespace and ControlPlane.
// It returns an error if a WatchNamespaceGrant is missing.
func ensureWatchNamespaceGrantsForNamespace(
	ctx context.Context,
	cl client.Client,
	cp *ControlPlane,
	ns string,
) error {
	var grants operatorv1alpha1.WatchNamespaceGrantList
	if err := cl.List(ctx, &grants, client.InNamespace(ns)); err != nil {
		return fmt.Errorf("failed listing WatchNamespaceGrants in namespace %s: %w", ns, err)
	}
	for _, grant := range grants.Items {
		if watchNamespaceGrantContainsControlPlaneFrom(grant, cp) {
			// return nil if there is one grant allows the CP to watch the namespace.
			return nil
		}
	}
	return fmt.Errorf("WatchNamespaceGrant in Namespace %s to ControlPlane in Namespace %s not found", ns, cp.Namespace)
}

func watchNamespaceGrantContainsControlPlaneFrom(
	grant operatorv1alpha1.WatchNamespaceGrant,
	cp *ControlPlane,
) bool {
	for _, from := range grant.Spec.From {
		if from.Group == operatorv2beta1.SchemeGroupVersion.Group &&
			from.Kind == "ControlPlane" &&
			from.Namespace == cp.Namespace {
			return true
		}
	}
	return false
}

// validateWatchNamespaces validates that the operator's watch namespaces are compatible
// with the ControlPlane's watch namespaces configuration.
func validateWatchNamespaces(
	cp *ControlPlane,
	watchNamespaces []string,
) error {
	if cp.Spec.WatchNamespaces == nil {
		return nil
	}

	// If ControlPlane is configured to watch all namespaces, operator should
	// not be configured to watch any specific namespaces: it should have the watchNamespaces
	// list empty, indicating that it will watch all namespaces.
	switch cp.Spec.WatchNamespaces.Type {
	case operatorv2beta1.WatchNamespacesTypeAll:
		if len(watchNamespaces) > 0 {
			return fmt.Errorf(
				"ControlPlane's watchNamespaces is set to 'All', but operator is only allowed on: %v",
				watchNamespaces,
			)
		}

	case operatorv2beta1.WatchNamespacesTypeOwn:
		// NOTE: In case the operator Pod does not have watch namespaces flag/environment variable set
		// to include the ControlPlane's namespace, it will not be able to watch
		// the ControlPlane's own namespace, which is required for the operator to function properly.
		if len(watchNamespaces) > 0 && !lo.Contains(watchNamespaces, cp.Namespace) {
			return fmt.Errorf(
				"ControlPlane's watchNamespaces is set to 'Own' (current ControlPlane namespace: %v), but operator is only allowed on: %v",
				cp.Namespace, watchNamespaces,
			)
		}

	case operatorv2beta1.WatchNamespacesTypeList:
		if len(watchNamespaces) == 0 {
			return nil
		}

		if !lo.Every(watchNamespaces, cp.Spec.WatchNamespaces.List) {
			return fmt.Errorf(
				"ControlPlane's watchNamespaces requests %v, but operator is only allowed on: %v",
				cp.Spec.WatchNamespaces.List, watchNamespaces,
			)
		}
	}

	return nil
}
