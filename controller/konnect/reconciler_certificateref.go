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
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/controller/konnect/constraints"
	"github.com/kong/kong-operator/controller/pkg/controlplane"
	"github.com/kong/kong-operator/controller/pkg/patch"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
)

// getKongCertificateRef gets the reference of KongCertificate.
func getKongCertificateRef[T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T]](
	e TEnt,
) mo.Option[commonv1alpha1.NameRef] {
	switch e := any(e).(type) {
	case *configurationv1alpha1.KongSNI:
		// Since certificateRef is required for KongSNI, we directly return spec.CertificateRef here.
		return mo.Some(e.Spec.CertificateRef)
	default:
		return mo.None[commonv1alpha1.NameRef]()
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
		if res, errStatus := patch.StatusWithCondition(
			ctx, cl, ent,
			konnectv1alpha1.KongCertificateRefValidConditionType,
			metav1.ConditionFalse,
			konnectv1alpha1.KongCertificateRefReasonInvalid,
			err.Error(),
		); errStatus != nil || !res.IsZero() {
			return res, errStatus
		}

		return ctrl.Result{}, ReferencedKongCertificateDoesNotExistError{
			Reference: nn,
			Err:       err,
		}
	}

	// If referenced KongCertificate is being deleted, return an error so that we
	// can remove the entity from Konnect first.
	if delTimestamp := cert.GetDeletionTimestamp(); !delTimestamp.IsZero() {
		return ctrl.Result{}, ReferencedKongCertificateIsBeingDeletedError{
			Reference:         nn,
			DeletionTimestamp: delTimestamp.Time,
		}
	}

	// requeue it if referenced KongCertificate is not programmed yet so we cannot do the following work.
	cond, ok := k8sutils.GetCondition(konnectv1alpha1.KonnectEntityProgrammedConditionType, cert)
	if !ok || cond.Status != metav1.ConditionTrue {
		ent.SetKonnectID("")
		if res, err := patch.StatusWithCondition(
			ctx, cl, ent,
			konnectv1alpha1.KongCertificateRefValidConditionType,
			metav1.ConditionFalse,
			konnectv1alpha1.KongCertificateRefReasonInvalid,
			fmt.Sprintf("Referenced KongCertificate %s is not programmed yet", nn),
		); err != nil || !res.IsZero() {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// TODO: make this more generic.
	if sni, ok := any(ent).(*configurationv1alpha1.KongSNI); ok {
		if sni.Status.Konnect == nil {
			sni.Status.Konnect = &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndCertificateRefs{}
		}
		sni.Status.Konnect.CertificateID = cert.GetKonnectID()
	}

	if res, errStatus := patch.StatusWithCondition(
		ctx, cl, ent,
		konnectv1alpha1.KongCertificateRefValidConditionType,
		metav1.ConditionTrue,
		konnectv1alpha1.KongCertificateRefReasonValid,
		fmt.Sprintf("Referenced KongCertificate %s programmed", nn),
	); errStatus != nil || !res.IsZero() {
		return res, errStatus
	}

	cpRef, ok := controlplane.GetControlPlaneRef(cert).Get()
	// TODO: ignore the entity if referenced KongCertificate does not have a Konnect control plane reference
	// because this situation is likely to mean that they are not controlled by us:
	// https://github.com/kong/kong-operator/issues/629
	if !ok {
		return ctrl.Result{}, fmt.Errorf(
			"%T references a KongCertificate %s which does not have a ControlPlane ref",
			ent, client.ObjectKeyFromObject(cert),
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
		fmt.Sprintf("Referenced ControlPlane %s is programmed", nn),
	); errStatus != nil || !res.IsZero() {
		return res, errStatus
	}

	return ctrl.Result{}, nil
}
