package konnect

import (
	"context"
	"fmt"

	"github.com/samber/mo"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/controller/konnect/constraints"
	"github.com/kong/kong-operator/controller/pkg/controlplane"
	"github.com/kong/kong-operator/controller/pkg/patch"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
)

// getKongUpstreamRef gets the reference of KongUpstream.
func getKongUpstreamRef[T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T]](
	e TEnt,
) mo.Option[commonv1alpha1.NameRef] {
	switch e := any(e).(type) {
	case *configurationv1alpha1.KongTarget:
		// Since upstreamRef is required for KongTarget, we directly return spec.UpstreamRef here.
		return mo.Some(e.Spec.UpstreamRef)
	default:
		return mo.None[commonv1alpha1.NameRef]()
	}
}

// handleKongUpstreamRef handles KongUpstream reference if the entity references a KongUpstream.
// Now applies to KongTarget.
func handleKongUpstreamRef[T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T]](
	ctx context.Context,
	cl client.Client,
	ent TEnt,
) (ctrl.Result, error) {
	upstreamRef, ok := getKongUpstreamRef(ent).Get()
	if !ok {
		return ctrl.Result{}, nil
	}

	kongUpstream := &configurationv1alpha1.KongUpstream{}
	nn := types.NamespacedName{
		Name: upstreamRef.Name,
		// TODO: handle cross namespace refs
		Namespace: ent.GetNamespace(),
	}
	err := cl.Get(ctx, nn, kongUpstream)
	if err != nil {
		if res, errStatus := patch.StatusWithCondition(
			ctx, cl, ent,
			konnectv1alpha1.KongUpstreamRefValidConditionType,
			metav1.ConditionFalse,
			konnectv1alpha1.KongUpstreamRefReasonInvalid,
			err.Error(),
		); errStatus != nil || !res.IsZero() {
			return res, errStatus
		}

		return ctrl.Result{}, ReferencedKongUpstreamDoesNotExist{
			Reference: nn,
			Err:       err,
		}
	}

	// If referenced KongUpstream is being deleted, return an error so that we
	// can remove the entity from Konnect first.
	if delTimestamp := kongUpstream.GetDeletionTimestamp(); !delTimestamp.IsZero() {
		return ctrl.Result{}, ReferencedKongUpstreamIsBeingDeleted{
			Reference:         nn,
			DeletionTimestamp: delTimestamp.Time,
		}
	}

	// requeue it if referenced KongUpstream is not programmed yet so we cannot do the following work.
	cond, ok := k8sutils.GetCondition(konnectv1alpha1.KonnectEntityProgrammedConditionType, kongUpstream)
	if !ok || cond.Status != metav1.ConditionTrue {
		ent.SetKonnectID("")
		if res, err := patch.StatusWithCondition(
			ctx, cl, ent,
			konnectv1alpha1.KongUpstreamRefValidConditionType,
			metav1.ConditionFalse,
			konnectv1alpha1.KongUpstreamRefReasonInvalid,
			fmt.Sprintf("Referenced KongUpstream %s is not programmed yet", nn),
		); err != nil || !res.IsZero() {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// TODO: make this more generic.
	if target, ok := any(ent).(*configurationv1alpha1.KongTarget); ok {
		if target.Status.Konnect == nil {
			target.Status.Konnect = &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndUpstreamRefs{}
		}
		target.Status.Konnect.UpstreamID = kongUpstream.GetKonnectID()
	}

	if res, errStatus := patch.StatusWithCondition(
		ctx, cl, ent,
		konnectv1alpha1.KongUpstreamRefValidConditionType,
		metav1.ConditionTrue,
		konnectv1alpha1.KongUpstreamRefReasonValid,
		fmt.Sprintf("Referenced KongUpstream %s programmed", nn),
	); errStatus != nil || !res.IsZero() {
		return res, errStatus
	}

	cpRef, ok := controlplane.GetControlPlaneRef(kongUpstream).Get()
	// TODO: ignore the entity if referenced KongUpstream does not have a Konnect control plane reference
	// because this situation is likely to mean that they are not controlled by us:
	// https://github.com/kong/kong-operator/issues/629
	if !ok {
		return ctrl.Result{}, fmt.Errorf(
			"%T references a KongUpstream %s which does not have a ControlPlane ref",
			ent, client.ObjectKeyFromObject(kongUpstream),
		)
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
		if k8serrors.IsNotFound(err) {
			return ctrl.Result{}, controlplane.ReferencedControlPlaneDoesNotExistError{
				Reference: cpRef,
				Err:       err,
			}
		}
		return ctrl.Result{}, err
	}

	cond, ok = k8sutils.GetCondition(konnectv1alpha1.KonnectEntityProgrammedConditionType, cp)
	if !ok || cond.Status != metav1.ConditionTrue || cond.ObservedGeneration != cp.GetGeneration() {
		if res, errStatus := patch.StatusWithCondition(
			ctx, cl, ent,
			konnectv1alpha1.ControlPlaneRefValidConditionType,
			metav1.ConditionFalse,
			konnectv1alpha1.ControlPlaneRefReasonInvalid,
			fmt.Sprintf("Referenced ControlPlane %s is not programmed yet", nn),
		); errStatus != nil || !res.IsZero() {
			return res, errStatus
		}

		return ctrl.Result{Requeue: true}, nil
	}

	if resource, ok := any(ent).(EntityWithControlPlaneRef); ok {
		resource.SetControlPlaneID(cp.Status.ID)
	}

	if res, errStatus := patch.StatusWithCondition(
		ctx, cl, ent,
		konnectv1alpha1.ControlPlaneRefValidConditionType,
		metav1.ConditionTrue,
		konnectv1alpha1.ControlPlaneRefReasonValid,
		fmt.Sprintf("Referenced ControlPlane %s is programmed", nn),
	); errStatus != nil || !res.IsZero() {
		return res, errStatus
	}

	return ctrl.Result{}, nil
}
