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
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/kong/gateway-operator/controller/konnect/conditions"
	"github.com/kong/gateway-operator/controller/konnect/constraints"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

// getKongUpstreamRef gets the reference of KongUpstream.
func getKongUpstreamRef[T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T]](
	e TEnt,
) mo.Option[configurationv1alpha1.TargetRef] {
	switch e := any(e).(type) {
	case *configurationv1alpha1.KongTarget:
		// Since upstreamRef is required for KongTarget, we directly return spec.UpstreamRef here.
		return mo.Some(e.Spec.UpstreamRef)
	default:
		return mo.None[configurationv1alpha1.TargetRef]()
	}
}

// handleKongUpstreamRef handles KongUpstram reference if the entity referenced some KongUpstream.
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
		if res, errStatus := updateStatusWithCondition(
			ctx, cl, ent,
			conditions.KongUpstreamRefValidConditionType,
			metav1.ConditionFalse,
			conditions.KongUpstreamRefReasonInvalid,
			err.Error(),
		); errStatus != nil || res.Requeue {
			return res, errStatus
		}

		return ctrl.Result{}, fmt.Errorf("can't get the referenced KongUpstream %s: %w", nn, err)
	}

	// If referenced KongUpstream is being deleted, return an error so that we
	// can remove the entity from Konnect first.
	if delTimestamp := kongUpstream.GetDeletionTimestamp(); !delTimestamp.IsZero() {
		return ctrl.Result{}, ReferencedKongUpstreamIsBeingDeleted{
			Reference: nn,
		}
	}

	// requeue it if referenced KongUpstream is not programmed yet so we cannot do the following work.
	cond, ok := k8sutils.GetCondition(conditions.KonnectEntityProgrammedConditionType, kongUpstream)
	if !ok || cond.Status != metav1.ConditionTrue {
		ent.SetKonnectID("")
		if res, err := updateStatusWithCondition(
			ctx, cl, ent,
			conditions.KongUpstreamRefValidConditionType,
			metav1.ConditionFalse,
			conditions.KongUpstreamRefReasonInvalid,
			fmt.Sprintf("Referenced KongUpstream %s is not programmed yet", nn),
		); err != nil || res.Requeue {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Set owner reference of referenced KongUpstream and the reconciled entity.
	old := ent.DeepCopyObject().(TEnt)
	if err := controllerutil.SetOwnerReference(kongUpstream, ent, cl.Scheme(), controllerutil.WithBlockOwnerDeletion(true)); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to set owner reference: %w", err)
	}
	if err := cl.Patch(ctx, ent, client.MergeFrom(old)); err != nil {
		if k8serrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to update status: %w", err)
	}

	// TODO: make this more generic.
	if target, ok := any(ent).(*configurationv1alpha1.KongTarget); ok {
		if target.Status.Konnect == nil {
			target.Status.Konnect = &konnectv1alpha1.KonnectEntityStatusWithControlPlaneAndUpstreamRefs{}
		}
		target.Status.Konnect.UpstreamID = kongUpstream.GetKonnectID()
	}

	if res, errStatus := updateStatusWithCondition(
		ctx, cl, ent,
		conditions.KongUpstreamRefValidConditionType,
		metav1.ConditionTrue,
		conditions.KongUpstreamRefReasonValid,
		fmt.Sprintf("Referenced KongUpstream %s programmed", nn),
	); errStatus != nil || res.Requeue {
		return res, errStatus
	}

	cpRef, ok := getControlPlaneRef(kongUpstream).Get()
	// REVIEW: the logic will cause KongTarget referencing to a KongUpstream that is not controlled by Konnect entity reconcilers
	// fall into endless reconcile backoff (so do KongRoute).
	// In such case, should we just ignore the KongTarget and treat it as not controlled?
	if !ok {
		return ctrl.Result{}, fmt.Errorf(
			"%T references a KongUpstream %s which does not have a ControlPlane ref",
			ent, client.ObjectKeyFromObject(kongUpstream),
		)
	}
	cp, err := getCPForRef(ctx, cl, cpRef, ent.GetNamespace())
	if err != nil {
		if res, errStatus := updateStatusWithCondition(
			ctx, cl, ent,
			conditions.ControlPlaneRefValidConditionType,
			metav1.ConditionFalse,
			conditions.ControlPlaneRefReasonInvalid,
			err.Error(),
		); errStatus != nil || res.Requeue {
			return res, errStatus
		}
		if k8serrors.IsNotFound(err) {
			// REVIEW: `getCPForRef` generates a new error but does not wrap the original error from client.Get so this is never reached.
			// Should we change that?
			return ctrl.Result{}, ReferencedControlPlaneDoesNotExistError{
				Reference: nn,
				Err:       err,
			}
		}
		return ctrl.Result{}, err
	}

	cond, ok = k8sutils.GetCondition(conditions.KonnectEntityProgrammedConditionType, cp)
	if !ok || cond.Status != metav1.ConditionTrue || cond.ObservedGeneration != cp.GetGeneration() {
		if res, errStatus := updateStatusWithCondition(
			ctx, cl, ent,
			conditions.ControlPlaneRefValidConditionType,
			metav1.ConditionFalse,
			conditions.ControlPlaneRefReasonInvalid,
			fmt.Sprintf("Referenced ControlPlane %s is not programmed yet", nn),
		); errStatus != nil || res.Requeue {
			return res, errStatus
		}

		return ctrl.Result{Requeue: true}, nil
	}

	if resource, ok := any(ent).(EntityWithControlPlaneRef); ok {
		resource.SetControlPlaneID(cp.Status.ID)
	}

	if res, errStatus := updateStatusWithCondition(
		ctx, cl, ent,
		conditions.ControlPlaneRefValidConditionType,
		metav1.ConditionTrue,
		conditions.ControlPlaneRefReasonValid,
		fmt.Sprintf("Referenced ControlPlane %s is programmed", nn),
	); errStatus != nil || res.Requeue {
		return res, errStatus
	}

	return ctrl.Result{}, nil
}
