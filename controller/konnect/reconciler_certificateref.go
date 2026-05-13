package konnect

import (
	"context"
	"fmt"

	"github.com/samber/mo"
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

// getKongCertificateRef gets the reference of KongCertificate.
func getKongCertificateRef[T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T]](
	e TEnt,
) mo.Option[commonv1alpha1.NamespacedRef] {
	switch e := any(e).(type) {
	case *configurationv1alpha1.KongSNI:
		// Return the full CertificateRef including the optional Namespace for cross-namespace refs.
		return mo.Some(e.Spec.CertificateRef)
	case *configurationv1alpha1.KongService:
		if e.Spec.ClientCertificateRef != nil {
			return mo.Some(*e.Spec.ClientCertificateRef)
		}
		return mo.None[commonv1alpha1.NamespacedRef]()
	default:
		return mo.None[commonv1alpha1.NamespacedRef]()
	}
}

func handleKongCertificateRef[T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T]](
	ctx context.Context,
	cl client.Client,
	ent TEnt,
) (ctrl.Result, error) {
	certRef, ok := getKongCertificateRef(ent).Get()
	if !ok {
		if svc, ok := any(ent).(*configurationv1alpha1.KongService); ok && svc.Status.Konnect != nil {
			svc.Status.Konnect.CertificateID = ""
		}
		return ctrl.Result{}, nil
	}

	certNamespace := ent.GetNamespace()
	var crossNamespaceRef bool
	if certRef.Namespace != nil && *certRef.Namespace != "" && *certRef.Namespace != certNamespace {
		certNamespace = *certRef.Namespace
		crossNamespaceRef = true
	}

	if crossNamespaceRef {
		if err := crossnamespace.CheckKongReferenceGrantForResource(
			ctx,
			cl,
			ent.GetNamespace(),
			certNamespace,
			certRef.Name,
			metav1.GroupVersionKind(ent.GetObjectKind().GroupVersionKind()),
			metav1.GroupVersionKind(configurationv1alpha1.GroupVersion.WithKind("KongCertificate")),
		); err != nil {
			if crossnamespace.IsReferenceNotGranted(err) {
				if res, errStatus := patch.StatusWithCondition(
					ctx, cl, ent,
					consts.ConditionType(configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs),
					metav1.ConditionFalse,
					configurationv1alpha1.KongReferenceGrantReasonRefNotPermitted,
					fmt.Sprintf("KongReferenceGrants do not allow access to KongCertificate %s/%s", certNamespace, certRef.Name),
				); errStatus != nil || !res.IsZero() {
					return res, errStatus
				}
				return ctrl.Result{RequeueAfter: ctrlconsts.RequeueWithoutBackoff}, nil
			}
			return ctrl.Result{}, err
		}

		if res, errStatus := patch.StatusWithCondition(
			ctx, cl, ent,
			consts.ConditionType(configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs),
			metav1.ConditionTrue,
			configurationv1alpha1.KongReferenceGrantReasonResolvedRefs,
			"KongReferenceGrants allow access to KongCertificate",
		); errStatus != nil || !res.IsZero() {
			return res, errStatus
		}
	}

	cert := &configurationv1alpha1.KongCertificate{}
	nn := types.NamespacedName{
		Name:      certRef.Name,
		Namespace: certNamespace,
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
		// Don't requeue. The referenced entity's changes will trigger the reconciliation.
		return ctrl.Result{}, nil
	}

	// Save the certificate Konnect ID now (cert is confirmed programmed).
	// We intentionally write it to ent.Status AFTER all patch.StatusWithCondition calls
	// below to avoid clobbering: each merge-patch only carries condition diffs (CertificateID
	// is identical in old and ent at snapshot time), so the server keeps its stored ""
	// and overwrites ent.Status in the response, losing the in-memory value.
	certKonnectID := cert.GetKonnectID()

	// TODO: make this more generic.
	// Initialise ent.Status.Konnect if nil so subsequent patches do not panic.
	switch ent := any(ent).(type) {
	case *configurationv1alpha1.KongSNI:
		if ent.Status.Konnect == nil {
			ent.Status.Konnect = &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndCertificateRefs{}
		}
	case *configurationv1alpha1.KongService:
		if ent.Status.Konnect == nil {
			ent.Status.Konnect = &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndCertificateAndCACertificatesRefs{}
		}
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
	cp, err := controlplane.GetCPForRef(ctx, cl, cpRef, cert.GetNamespace())
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
		fmt.Sprintf("Referenced ControlPlane %s is programmed", nn),
	); errStatus != nil || !res.IsZero() {
		return res, errStatus
	}

	// Set CertificateID after all patches so the server-response from each merge patch
	// cannot clobber it (see comment above near certKonnectID declaration).
	switch ent := any(ent).(type) {
	case *configurationv1alpha1.KongSNI:
		if ent.Status.Konnect == nil {
			ent.Status.Konnect = &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndCertificateRefs{}
		}
		ent.Status.Konnect.CertificateID = certKonnectID
	case *configurationv1alpha1.KongService:
		if ent.Status.Konnect == nil {
			ent.Status.Konnect = &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndCertificateAndCACertificatesRefs{}
		}
		ent.Status.Konnect.CertificateID = certKonnectID
	}
	return ctrl.Result{}, nil
}
