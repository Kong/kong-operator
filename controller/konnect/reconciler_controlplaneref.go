package konnect

import (
	"context"
	"fmt"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kong/kong-operator/api/common/consts"
	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
	ctrlconsts "github.com/kong/kong-operator/controller/consts"
	"github.com/kong/kong-operator/controller/konnect/constraints"
	"github.com/kong/kong-operator/controller/pkg/controlplane"
	"github.com/kong/kong-operator/controller/pkg/patch"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
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

		return ctrl.Result{}, err
	}

	// Do not continue reconciling of the control plane has incompatible cluster type to prevent repeated failure of creation.
	// Only CLUSTER_TYPE_CONTROL_PLANE is supported.
	// The configuration in control plane group type are read only so they are unsupported to attach entities to them:
	// https://docs.konghq.com/konnect/gateway-manager/control-plane-groups/#limitations
	if cp.GetKonnectClusterType() != nil &&
		*cp.GetKonnectClusterType() == sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlaneGroup {
		if res, errStatus := patch.StatusWithCondition(
			ctx, cl, ent,
			konnectv1alpha1.ControlPlaneRefValidConditionType,
			metav1.ConditionFalse,
			konnectv1alpha1.ControlPlaneRefReasonInvalid,
			fmt.Sprintf("Attaching to ControlPlane %s with cluster type %s is not supported", cpRef.String(), *cp.GetKonnectClusterType()),
		); errStatus != nil || !res.IsZero() {
			return res, errStatus
		}
		return ctrl.Result{}, nil
	}

	cond, ok := k8sutils.GetCondition(konnectv1alpha1.KonnectEntityProgrammedConditionType, cp)
	if !ok || cond.Status != metav1.ConditionTrue || cond.ObservedGeneration != cp.GetGeneration() {
		if res, errStatus := patch.StatusWithCondition(
			ctx, cl, ent,
			konnectv1alpha1.ControlPlaneRefValidConditionType,
			metav1.ConditionFalse,
			konnectv1alpha1.ControlPlaneRefReasonInvalid,
			fmt.Sprintf("Referenced ControlPlane %s is not programmed yet", cpRef.String()),
		); errStatus != nil || !res.IsZero() {
			return res, errStatus
		}

		return ctrl.Result{Requeue: true}, nil
	}

	if resource, ok := any(ent).(EntityWithControlPlaneRef); ok {
		old := ent.DeepCopyObject().(TEnt)
		resource.SetControlPlaneID(cp.Status.ID)
		_, err := patch.ApplyStatusPatchIfNotEmpty(ctx, cl, ctrllog.FromContext(ctx), ent, old)
		if err != nil {
			if k8serrors.IsConflict(err) {
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
		fmt.Sprintf("Referenced ControlPlane %s is programmed", cpRef.String()),
	); errStatus != nil || !res.IsZero() {
		return res, errStatus
	}
	return ctrl.Result{}, nil
}

func conditionMessageReferenceKonnectAPIAuthConfigurationInvalid(apiAuthRef types.NamespacedName) string {
	return fmt.Sprintf("referenced KonnectAPIAuthConfiguration %s is invalid", apiAuthRef)
}

func conditionMessageReferenceKonnectAPIAuthConfigurationValid(apiAuthRef types.NamespacedName) string {
	return fmt.Sprintf("referenced KonnectAPIAuthConfiguration %s is valid", apiAuthRef)
}

func kongReferenceGrantsAllowsCPRef(
	krgs []configurationv1alpha1.KongReferenceGrant,
	obj client.Object,
	cpRef commonv1alpha1.KonnectNamespacedRef,
) bool {
	var (
		gvk       = obj.GetObjectKind().GroupVersionKind()
		fromKind  = gvk.Kind
		fromGroup = gvk.Group
		fromNs    = obj.GetNamespace()
	)

	for _, krg := range krgs {
		// This is already enforced at the call site but in case this is used elsewhere, double check.
		if krg.Namespace != cpRef.Namespace {
			continue
		}

		var (
			allowedFrom bool
			allowedTo   bool
		)
		for _, from := range krg.Spec.From {
			if string(from.Namespace) == fromNs &&
				string(from.Kind) == fromKind &&
				string(from.Group) == fromGroup {
				allowedFrom = true
				break
			}
		}
		if !allowedFrom {
			continue
		}

		for _, to := range krg.Spec.To {
			if (to.Name == nil || cpRef.Name == string(*to.Name)) &&
				string(to.Kind) == "KonnectGatewayControlPlane" &&
				string(to.Group) == konnectv1alpha2.SchemeGroupVersion.Group {
				allowedTo = true
				break
			}
		}
		if !allowedTo {
			continue
		}

		return true
	}

	return false
}

func ensureKongReferenceGrant[
	T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T],
](
	ctx context.Context,
	cl client.Client,
	ent TEnt,
	cpRef commonv1alpha1.ControlPlaneRef,
) (ctrl.Result, error) {
	if cpRef.Type != commonv1alpha1.ControlPlaneRefKonnectNamespacedRef ||
		cpRef.KonnectNamespacedRef.Namespace == "" ||
		cpRef.KonnectNamespacedRef.Name == ent.GetNamespace() {
		if res, errStatus := patch.StatusWithoutCondition(
			ctx, cl, ent,
			configurationv1alpha1.KongReferenceGrantConditionType,
		); errStatus != nil || !res.IsZero() {
			return res, errStatus
		}
		return ctrl.Result{}, nil
	}

	// Only check KongReferenceGrants for namespaced resources.
	gvk := ent.GetObjectKind().GroupVersionKind()
	mapping, err := cl.RESTMapper().RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get REST mapping for %s: %w", gvk.String(), err)
	}
	if mapping.Scope.Name() != meta.RESTScopeNameNamespace {
		if res, errStatus := patch.StatusWithoutCondition(
			ctx, cl, ent,
			configurationv1alpha1.KongReferenceGrantConditionType,
		); errStatus != nil || !res.IsZero() {
			return res, errStatus
		}
		return ctrl.Result{}, nil
	}

	nn := types.NamespacedName{
		Name:      cpRef.KonnectNamespacedRef.Name,
		Namespace: cpRef.KonnectNamespacedRef.Namespace,
	}
	var krgs configurationv1alpha1.KongReferenceGrantList
	if err := cl.List(ctx, &krgs, client.InNamespace(nn.Namespace)); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to list KongReferenceGrants in namespace %s: %w", nn.Namespace, err)
	}

	isAllowed := kongReferenceGrantsAllowsCPRef(krgs.Items, ent, *cpRef.KonnectNamespacedRef)
	if !isAllowed {
		if res, errStatus := patch.StatusWithCondition(
			ctx, cl, ent,
			consts.ConditionType(configurationv1alpha1.KongReferenceGrantConditionType),
			metav1.ConditionFalse,
			configurationv1alpha1.KongReferenceGrantReasonInvalid,
			fmt.Sprintf("KongReferenceGrant %s does not allow access to KonnectGatewayControlPlane %s", nn, cpRef),
		); errStatus != nil || !res.IsZero() {
			return res, errStatus
		}
		return ctrl.Result{RequeueAfter: ctrlconsts.RequeueWithoutBackoff}, nil
	}

	if res, errStatus := patch.StatusWithCondition(
		ctx, cl, ent,
		consts.ConditionType(configurationv1alpha1.KongReferenceGrantConditionType),
		metav1.ConditionTrue,
		configurationv1alpha1.KongReferenceGrantReasonValid,
		fmt.Sprintf("KongReferenceGrant %s allows access to KonnectGatewayControlPlane %s", nn, cpRef),
	); errStatus != nil || !res.IsZero() {
		return res, errStatus
	}

	return ctrl.Result{}, nil
}
