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

// getKongCertificateRef gets the reference of KongCertificate.
func getKongCertificateRef[T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T]](
	e TEnt,
) mo.Option[configurationv1alpha1.KongObjectRef] {
	switch e := any(e).(type) {
	case *configurationv1alpha1.KongSNI:
		// Since certificateRef is required for KongSNI, we directly return spec.CertificateRef here.
		return mo.Some(e.Spec.CertificateRef)
	default:
		return mo.None[configurationv1alpha1.KongObjectRef]()
	}
}

func handleKongCertificateRef[T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T]](
	ctx context.Context,
	cl client.Client,
	ent TEnt,
) (ctrl.Result, error) {
	certRef, ok := getKongCertificateRef(ent).Get()
	if !ok {
		return ctrl.Result{}, nil
	}

	cert := &configurationv1alpha1.KongCertificate{}
	nn := types.NamespacedName{
		Name: certRef.Name,
		// TODO: handle cross namespace refs
		Namespace: ent.GetNamespace(),
	}
	err := cl.Get(ctx, nn, cert)
	if err != nil {
		if res, errStatus := updateStatusWithCondition(
			ctx, cl, ent,
			conditions.KongCertificateRefValidConditionType,
			metav1.ConditionFalse,
			conditions.KongCertificateRefReasonInvalid,
			err.Error(),
		); errStatus != nil || res.Requeue {
			return res, errStatus
		}

		return ctrl.Result{}, ReferencedKongCertificateDoesNotExist{
			Reference: nn,
			Err:       err,
		}
	}

	// If referenced KongCertificate is being deleted, return an error so that we
	// can remove the entity from Konnect first.
	if delTimestamp := cert.GetDeletionTimestamp(); !delTimestamp.IsZero() {
		return ctrl.Result{}, ReferencedKongCertificateIsBeingDeleted{
			Reference:         nn,
			DeletionTimestamp: delTimestamp.Time,
		}
	}

	// requeue it if referenced KongCertificate is not programmed yet so we cannot do the following work.
	cond, ok := k8sutils.GetCondition(conditions.KonnectEntityProgrammedConditionType, cert)
	if !ok || cond.Status != metav1.ConditionTrue {
		ent.SetKonnectID("")
		if res, err := updateStatusWithCondition(
			ctx, cl, ent,
			conditions.KongCertificateRefValidConditionType,
			metav1.ConditionFalse,
			conditions.KongCertificateRefReasonInvalid,
			fmt.Sprintf("Referenced KongCertificate %s is not programmed yet", nn),
		); err != nil || res.Requeue {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Set owner reference of referenced KongCertificate and the reconciled entity.
	old := ent.DeepCopyObject().(TEnt)
	if err := controllerutil.SetOwnerReference(cert, ent, cl.Scheme(), controllerutil.WithBlockOwnerDeletion(true)); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to set owner reference: %w", err)
	}
	if err := cl.Patch(ctx, ent, client.MergeFrom(old)); err != nil {
		if k8serrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to update status: %w", err)
	}

	// TODO: make this more generic.
	if sni, ok := any(ent).(*configurationv1alpha1.KongSNI); ok {
		if sni.Status.Konnect == nil {
			sni.Status.Konnect = &konnectv1alpha1.KonnectEntityStatusWithControlPlaneAndCertificateRefs{}
		}
		sni.Status.Konnect.CertificateID = cert.GetKonnectID()
	}

	if res, errStatus := updateStatusWithCondition(
		ctx, cl, ent,
		conditions.KongCertificateRefValidConditionType,
		metav1.ConditionTrue,
		conditions.KongCertificateRefReasonValid,
		fmt.Sprintf("Referenced KongCertificate %s programmed", nn),
	); errStatus != nil || res.Requeue {
		return res, errStatus
	}

	cpRef, ok := getControlPlaneRef(cert).Get()
	// TODO: ignore the entity if referenced KongCertificate does not have a Konnect control plane reference
	// because this situation is likely to mean that they are not controlled by us:
	// https://github.com/Kong/gateway-operator/issues/629
	if !ok {
		return ctrl.Result{}, fmt.Errorf(
			"%T references a KongCertificate %s which does not have a ControlPlane ref",
			ent, client.ObjectKeyFromObject(cert),
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
			return ctrl.Result{}, ReferencedControlPlaneDoesNotExistError{
				Reference: types.NamespacedName{
					Namespace: ent.GetNamespace(),
					Name:      cpRef.KonnectNamespacedRef.Name,
				},
				Err: err,
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
