package konnect

import (
	"context"
	"fmt"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kong/kong-operator/v2/api/common/consts"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	ctrlconsts "github.com/kong/kong-operator/v2/controller/consts"
	"github.com/kong/kong-operator/v2/controller/konnect/constraints"
	"github.com/kong/kong-operator/v2/controller/pkg/controlplane"
	"github.com/kong/kong-operator/v2/controller/pkg/patch"
	"github.com/kong/kong-operator/v2/internal/utils/crossnamespace"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// EntityWithControlPlaneRef is an interface for entities that have a ControlPlaneRef.
type EntityWithControlPlaneRef interface {
	SetControlPlaneID(string)
	GetControlPlaneID() string
}

// handleControlPlaneRef handles the ControlPlaneRef for the given entity.
// It sets the owner reference to the referenced ControlPlane and updates the
// status of the entity based on the referenced ControlPlane status.
func handleControlPlaneRef[T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T]](
	ctx context.Context,
	cl client.Client,
	ent TEnt,
) (ctrl.Result, error) {
	cpRef, ok := controlplane.GetControlPlaneRef(ent).Get()
	if !ok {
		return ctrl.Result{}, nil
	}

	if res, err := ensureKongReferenceGrant(ctx, cl, ent, cpRef); err != nil || !res.IsZero() {
		return res, err
	}

	cp, err := controlplane.GetCPForRef(ctx, cl, cpRef, ent.GetNamespace())
	if err != nil {
		if res, errStatus := patch.StatusWithCondition(
			ctx, cl, ent,
			konnectv1alpha1.ControlPlaneRefValidConditionType,
			metav1.ConditionFalse,
			konnectv1alpha1.ControlPlaneRefReasonInvalid,
			err.Error(),
		); errStatus != nil || !res.IsZero() {
			return res, errStatus
		}
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, controlplane.ReferencedControlPlaneDoesNotExistError{
				Reference: cpRef,
				Err:       err,
			}
		}

		return ctrl.Result{}, err
	}

	cpnn := types.NamespacedName{
		Name:      cp.Name,
		Namespace: cp.Namespace,
	}

	// Do not continue reconciling of the control plane has incompatible cluster type to prevent repeated failure of creation.
	// Only CLUSTER_TYPE_CONTROL_PLANE is supported.
	// The configuration in control plane group type are read only so they are unsupported to attach entities to them:
	// https://docs.konghq.com/konnect/gateway-manager/control-plane-groups/#limitations
	if cp.GetKonnectClusterType() != nil &&
		(*cp.GetKonnectClusterType() == sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlaneGroup &&
			// We don't allow attaching to control plane group type as they are read only
			// and don't have the configuration that can be used by the entities,
			// but we want to allow attaching KongDataPlaneClientCertificate to them
			// as they are used for CP/DP mTLS.
			ent.GetObjectKind().GroupVersionKind().GroupKind().Kind != "KongDataPlaneClientCertificate") {
		if res, errStatus := patch.StatusWithCondition(
			ctx, cl, ent,
			konnectv1alpha1.ControlPlaneRefValidConditionType,
			metav1.ConditionFalse,
			konnectv1alpha1.ControlPlaneRefReasonInvalid,
			fmt.Sprintf("Attaching to ControlPlane %s with cluster type %s is not supported", cpnn, *cp.GetKonnectClusterType()),
		); errStatus != nil || !res.IsZero() {
			return res, errStatus
		}
		return ctrl.Result{}, nil
	}

	cond, ok := k8sutils.GetCondition(konnectv1alpha1.KonnectEntityProgrammedConditionType, cp)
	if !ok || cond.Status != metav1.ConditionTrue {
		if res, errStatus := patch.StatusWithCondition(
			ctx, cl, ent,
			konnectv1alpha1.ControlPlaneRefValidConditionType,
			metav1.ConditionFalse,
			konnectv1alpha1.ControlPlaneRefReasonNotProgrammed,
			fmt.Sprintf("Referenced ControlPlane %s is not programmed yet", cpnn),
		); errStatus != nil || !res.IsZero() {
			return res, errStatus
		}

		// Don't requeue. The referenced entity's changes will trigger the reconciliation.
		return ctrl.Result{}, nil
	}

	if resource, ok := any(ent).(EntityWithControlPlaneRef); ok {
		old := ent.DeepCopyObject().(TEnt)
		resource.SetControlPlaneID(cp.Status.ID)
		_, err := patch.ApplyStatusPatchIfNotEmpty(ctx, cl, ctrllog.FromContext(ctx), ent, old)
		if err != nil {
			if apierrors.IsConflict(err) {
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, err
		}
	}

	if res, errStatus := patch.StatusWithCondition(
		ctx, cl, ent,
		konnectv1alpha1.ControlPlaneRefValidConditionType,
		metav1.ConditionTrue,
		konnectv1alpha1.ControlPlaneRefReasonValid,
		fmt.Sprintf("Referenced ControlPlane %s is programmed", cpnn),
	); errStatus != nil || !res.IsZero() {
		return res, errStatus
	}
	return ctrl.Result{}, nil
}

// entityHasCrossNamespaceRefs reports whether ent has any cross-namespace
// reference (Secret, KongCACertificate, KongCertificate/ClientCertificate) whose
// shared ResolvedRefs condition is owned by a dedicated handler. The
// control-plane-ref handler must not remove the ResolvedRefs condition while any
// such reference exists, otherwise it would clobber what those handlers set
// (they run later in Reconcile).
func entityHasCrossNamespaceRefs[
	T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T],
](ent TEnt) bool {
	ns := ent.GetNamespace()
	isCross := func(n *string) bool { return n != nil && *n != "" && *n != ns }

	for _, r := range getSecretRefs(ent) {
		if isCross(r.Namespace) {
			return true
		}
	}
	for _, r := range getKongCACertificateRefs(ent) {
		if isCross(r.Namespace) {
			return true
		}
	}
	if r, ok := getKongCertificateRef(ent).Get(); ok && isCross(r.Namespace) {
		return true
	}
	return false
}

func conditionMessageReferenceKonnectAPIAuthConfigurationInvalid(apiAuthRef types.NamespacedName) string {
	return fmt.Sprintf("referenced KonnectAPIAuthConfiguration %s is invalid", apiAuthRef)
}

func conditionMessageReferenceKonnectAPIAuthConfigurationValid(apiAuthRef types.NamespacedName) string {
	return fmt.Sprintf("referenced KonnectAPIAuthConfiguration %s is valid", apiAuthRef)
}

func ensureKongReferenceGrant[
	T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T],
](
	ctx context.Context,
	cl client.Client,
	ent TEnt,
	cpRef commonv1alpha1.ControlPlaneRef,
) (ctrl.Result, error) {
	fromNamespace := ent.GetNamespace()
	toNamespace := ""
	if cpRef.KonnectNamespacedRef != nil {
		toNamespace = cpRef.KonnectNamespacedRef.Namespace
	}

	if cpRef.Type != commonv1alpha1.ControlPlaneRefKonnectNamespacedRef ||
		toNamespace == "" ||
		toNamespace == fromNamespace {
		// The ControlPlane ref doesn't require a grant. Only remove the shared
		// ResolvedRefs condition if no other cross-namespace reference of this
		// entity still justifies it, otherwise we'd clobber the condition that
		// the secret/certificate ref handlers set (they run later in Reconcile).

		// TODO: Unify the place of setting `ResolvedRefs` condition for entities
		// having multiple references to other resources, so that we don't have to check for each of them here:
		// https://github.com/Kong/kong-operator/issues/4711
		if !entityHasCrossNamespaceRefs(ent) {
			if res, errStatus := patch.StatusWithoutCondition(
				ctx, cl, ent,
				configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs,
			); errStatus != nil || !res.IsZero() {
				return res, errStatus
			}
		}
		return ctrl.Result{}, nil
	}

	err := crossnamespace.CheckKongReferenceGrantForResource(
		ctx,
		cl,
		fromNamespace,
		toNamespace,
		cpRef.KonnectNamespacedRef.Name,
		metav1.GroupVersionKind(ent.GetObjectKind().GroupVersionKind()),
		metav1.GroupVersionKind(konnectv1alpha2.SchemeGroupVersion.WithKind("KonnectGatewayControlPlane")),
	)
	if crossnamespace.IsReferenceNotGranted(err) {
		if res, errStatus := patch.StatusWithCondition(
			ctx, cl, ent,
			consts.ConditionType(configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs),
			metav1.ConditionFalse,
			configurationv1alpha1.KongReferenceGrantReasonRefNotPermitted,
			fmt.Sprintf("KongReferenceGrants do not allow access to KonnectGatewayControlPlane %s", cpRef.String()),
		); errStatus != nil || !res.IsZero() {
			return res, errStatus
		}
		return ctrl.Result{RequeueAfter: ctrlconsts.RequeueWithoutBackoff}, nil
	}

	if res, errStatus := patch.StatusWithCondition(
		ctx, cl, ent,
		consts.ConditionType(configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs),
		metav1.ConditionTrue,
		configurationv1alpha1.KongReferenceGrantReasonResolvedRefs,
		fmt.Sprintf("KongReferenceGrants allow access to KonnectGatewayControlPlane %s", cpRef.String()),
	); errStatus != nil || !res.IsZero() {
		return res, errStatus
	}

	return ctrl.Result{}, nil
}
