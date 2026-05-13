package konnect

import (
	"context"
	"errors"
	"fmt"

	"github.com/samber/mo"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

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

// getKongUpstreamRef gets the reference of KongUpstream.
func getKongUpstreamRef[T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T]](
	e TEnt,
) mo.Option[commonv1alpha1.NamespacedRef] {
	switch e := any(e).(type) {
	case *configurationv1alpha1.KongTarget:
		// Since upstreamRef is required for KongTarget, we directly return spec.UpstreamRef here.
		return mo.Some(e.Spec.UpstreamRef)
	default:
		return mo.None[commonv1alpha1.NamespacedRef]()
	}
}

// handleKongUpstreamRef handles KongUpstream reference if the entity references a KongUpstream.
// Now applies to KongTarget.
func handleKongUpstreamRef[T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T]](
	ctx context.Context,
	cl client.Client,
	ent TEnt,
) (result ctrl.Result, err error) {
	upstreamRef, ok := getKongUpstreamRef(ent).Get()
	if !ok {
		return ctrl.Result{}, nil
	}

	// Snapshot current status. All condition writes below are in-memory only.
	// A single status patch is issued on exit regardless of exit path.
	old := ent.DeepCopyObject().(TEnt)
	defer func() {
		if _, patchErr := patch.ApplyStatusPatchIfNotEmpty(ctx, cl, ctrllog.FromContext(ctx), ent, old); patchErr != nil {
			err = errors.Join(err, patchErr)
		}
	}()

	upstreamNamespace := ent.GetNamespace()
	if upstreamRef.Namespace != nil && *upstreamRef.Namespace != "" {
		upstreamNamespace = *upstreamRef.Namespace
	}

	nn := types.NamespacedName{
		Name:      upstreamRef.Name,
		Namespace: upstreamNamespace,
	}

	crossNamespaceRef := upstreamNamespace != ent.GetNamespace()
	if crossNamespaceRef {
		if err := crossnamespace.CheckKongReferenceGrantForResource(
			ctx,
			cl,
			ent.GetNamespace(),
			upstreamNamespace,
			upstreamRef.Name,
			metav1.GroupVersionKind(ent.GetObjectKind().GroupVersionKind()),
			metav1.GroupVersionKind(configurationv1alpha1.GroupVersion.WithKind("KongUpstream")),
		); err != nil {
			if crossnamespace.IsReferenceNotGranted(err) {
				msg := fmt.Sprintf("KongReferenceGrants do not allow access to KongUpstream %s/%s", upstreamNamespace, upstreamRef.Name)
				_ = patch.SetStatusWithConditionIfDifferent(ent,
					konnectv1alpha1.KongUpstreamRefValidConditionType,
					metav1.ConditionFalse,
					konnectv1alpha1.KongUpstreamRefReasonRefNotPermitted,
					msg,
				)
				return ctrl.Result{RequeueAfter: ctrlconsts.RequeueWithoutBackoff}, nil
			}
			return ctrl.Result{}, err
		}
	}

	kongUpstream := &configurationv1alpha1.KongUpstream{}
	if err := cl.Get(ctx, nn, kongUpstream); err != nil {
		_ = patch.SetStatusWithConditionIfDifferent(ent,
			konnectv1alpha1.KongUpstreamRefValidConditionType,
			metav1.ConditionFalse,
			konnectv1alpha1.KongUpstreamRefReasonInvalid,
			err.Error(),
		)
		return ctrl.Result{}, ReferencedKongUpstreamDoesNotExistError{
			Reference: nn,
			Err:       err,
		}
	}

	// If referenced KongUpstream is being deleted, return an error so that we
	// can remove the entity from Konnect first.
	if delTimestamp := kongUpstream.GetDeletionTimestamp(); !delTimestamp.IsZero() {
		return ctrl.Result{}, ReferencedKongUpstreamIsBeingDeletedError{
			Reference:         nn,
			DeletionTimestamp: delTimestamp.Time,
		}
	}

	cond, ok := k8sutils.GetCondition(konnectv1alpha1.KonnectEntityProgrammedConditionType, kongUpstream)
	if !ok || cond.Status != metav1.ConditionTrue {
		ent.SetKonnectID("")
		notProgrammedMsg := fmt.Sprintf("Referenced KongUpstream %s is not programmed yet", nn)
		_ = patch.SetStatusWithConditionIfDifferent(ent,
			konnectv1alpha1.KongUpstreamRefValidConditionType,
			metav1.ConditionFalse,
			konnectv1alpha1.KongUpstreamRefReasonInvalid,
			notProgrammedMsg,
		)
		// Don't requeue. The referenced entity's changes will trigger the reconciliation.
		return ctrl.Result{}, nil
	}

	// TODO: make this more generic.
	if target, ok := any(ent).(*configurationv1alpha1.KongTarget); ok {
		if target.Status.Konnect == nil {
			target.Status.Konnect = &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndUpstreamRefs{}
		}
		target.Status.Konnect.UpstreamID = kongUpstream.GetKonnectID()
	}

	upstreamValidMsg := fmt.Sprintf("Referenced KongUpstream %s programmed", nn)
	_ = patch.SetStatusWithConditionIfDifferent(ent,
		konnectv1alpha1.KongUpstreamRefValidConditionType,
		metav1.ConditionTrue,
		konnectv1alpha1.KongUpstreamRefReasonValid,
		upstreamValidMsg,
	)

	// Check and handle the ControlPlaneRef of the referenced KongUpstream.
	cpRef, ok := controlplane.GetControlPlaneRef(kongUpstream).Get()
	if !ok {
		return ctrl.Result{}, fmt.Errorf(
			"KongTarget references a KongUpstream %s which does not have a ControlPlane ref",
			client.ObjectKeyFromObject(kongUpstream),
		)
	}

	cp, err := controlplane.GetCPForRef(ctx, cl, cpRef, kongUpstream.GetNamespace())
	if err != nil {
		_ = patch.SetStatusWithConditionIfDifferent(ent,
			konnectv1alpha1.ControlPlaneRefValidConditionType,
			metav1.ConditionFalse,
			konnectv1alpha1.ControlPlaneRefReasonInvalid,
			err.Error(),
		)
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, controlplane.ReferencedControlPlaneDoesNotExistError{
				Reference: cpRef,
				Err:       err,
			}
		}
		return ctrl.Result{}, err
	}

	cond, ok = k8sutils.GetCondition(konnectv1alpha1.KonnectEntityProgrammedConditionType, cp)
	if !ok || cond.Status != metav1.ConditionTrue || cond.ObservedGeneration != cp.GetGeneration() {
		msg := fmt.Sprintf("Referenced ControlPlane %s is not programmed yet", nn)
		_ = patch.SetStatusWithConditionIfDifferent(ent,
			konnectv1alpha1.ControlPlaneRefValidConditionType,
			metav1.ConditionFalse,
			konnectv1alpha1.ControlPlaneRefReasonInvalid,
			msg,
		)
		return ctrl.Result{Requeue: true}, nil
	}

	if resource, ok := any(ent).(EntityWithControlPlaneRef); ok {
		resource.SetControlPlaneID(cp.Status.ID)
	}

	cpValidMsg := fmt.Sprintf("Referenced ControlPlane %s is programmed", nn)
	_ = patch.SetStatusWithConditionIfDifferent(ent,
		konnectv1alpha1.ControlPlaneRefValidConditionType,
		metav1.ConditionTrue,
		konnectv1alpha1.ControlPlaneRefReasonValid,
		cpValidMsg,
	)
	return ctrl.Result{}, nil
}
