package konnect

import (
	"context"
	"fmt"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/samber/mo"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kong/gateway-operator/controller/konnect/constraints"
	"github.com/kong/gateway-operator/controller/pkg/patch"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/api/common/v1alpha1"
	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kubernetes-configuration/api/configuration/v1beta1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

func getControlPlaneRef[T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T]](
	e TEnt,
) mo.Option[commonv1alpha1.ControlPlaneRef] {
	none := mo.None[commonv1alpha1.ControlPlaneRef]()
	switch e := any(e).(type) {
	case *configurationv1.KongConsumer:
		if e.Spec.ControlPlaneRef == nil {
			return none
		}
		return mo.Some(*e.Spec.ControlPlaneRef)
	case *configurationv1beta1.KongConsumerGroup:
		if e.Spec.ControlPlaneRef == nil {
			return none
		}
		return mo.Some(*e.Spec.ControlPlaneRef)
	case *configurationv1alpha1.KongRoute:
		if e.Spec.ControlPlaneRef == nil {
			return none
		}
		return mo.Some(*e.Spec.ControlPlaneRef)
	case *configurationv1alpha1.KongService:
		if e.Spec.ControlPlaneRef == nil {
			return none
		}
		return mo.Some(*e.Spec.ControlPlaneRef)
	case *configurationv1alpha1.KongPluginBinding:
		return mo.Some(e.Spec.ControlPlaneRef)
	case *configurationv1alpha1.KongUpstream:
		if e.Spec.ControlPlaneRef == nil {
			return none
		}
		return mo.Some(*e.Spec.ControlPlaneRef)
	case *configurationv1alpha1.KongCACertificate:
		if e.Spec.ControlPlaneRef == nil {
			return none
		}
		return mo.Some(*e.Spec.ControlPlaneRef)
	case *configurationv1alpha1.KongCertificate:
		if e.Spec.ControlPlaneRef == nil {
			return none
		}
		return mo.Some(*e.Spec.ControlPlaneRef)
	case *configurationv1alpha1.KongVault:
		if e.Spec.ControlPlaneRef == nil {
			return none
		}
		return mo.Some(*e.Spec.ControlPlaneRef)
	case *configurationv1alpha1.KongKey:
		if e.Spec.ControlPlaneRef == nil {
			return none
		}
		return mo.Some(*e.Spec.ControlPlaneRef)
	case *configurationv1alpha1.KongKeySet:
		if e.Spec.ControlPlaneRef == nil {
			return none
		}
		return mo.Some(*e.Spec.ControlPlaneRef)
	case *configurationv1alpha1.KongDataPlaneClientCertificate:
		if e.Spec.ControlPlaneRef == nil {
			return none
		}
		return mo.Some(*e.Spec.ControlPlaneRef)
	default:
		return none
	}
}

// handleControlPlaneRef handles the ControlPlaneRef for the given entity.
// It sets the owner reference to the referenced ControlPlane and updates the
// status of the entity based on the referenced ControlPlane status.
func handleControlPlaneRef[T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T]](
	ctx context.Context,
	cl client.Client,
	ent TEnt,
) (ctrl.Result, error) {
	cpRef, ok := getControlPlaneRef(ent).Get()
	if !ok {
		return ctrl.Result{}, nil
	}

	cp, err := getCPForRef(ctx, cl, cpRef, ent.GetNamespace())
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
	if cp.Spec.ClusterType != nil &&
		*cp.Spec.ClusterType == sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlaneGroup {
		if res, errStatus := patch.StatusWithCondition(
			ctx, cl, ent,
			konnectv1alpha1.ControlPlaneRefValidConditionType,
			metav1.ConditionFalse,
			konnectv1alpha1.ControlPlaneRefReasonInvalid,
			fmt.Sprintf("Attaching to ControlPlane %s with cluster type %s is not supported", cpRef.String(), *cp.Spec.ClusterType),
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
