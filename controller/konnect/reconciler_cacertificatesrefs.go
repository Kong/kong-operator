package konnect

import (
	"context"
	"errors"
	"fmt"

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
	"github.com/kong/kong-operator/v2/controller/konnect/constraints"
	"github.com/kong/kong-operator/v2/controller/pkg/patch"
	"github.com/kong/kong-operator/v2/internal/utils/crossnamespace"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// getKongCACertificateRefs returns the list of KongCACertificate references from the entity's spec.
func getKongCACertificateRefs[T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T]](
	e TEnt,
) []commonv1alpha1.NamespacedRef {
	switch e := any(e).(type) {
	case *configurationv1alpha1.KongService:
		return e.Spec.CACertificateRefs
	default:
		return nil
	}
}

// handleKongCACertificateRefs resolves a list of KongCACertificate references from a KongService spec,
// validates them (including cross-namespace via KongReferenceGrant), and stores the resolved Konnect IDs
// in the entity's status.
func handleKongCACertificateRefs[T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T]](
	ctx context.Context,
	cl client.Client,
	ent TEnt,
) (result ctrl.Result, err error) {
	refs := getKongCACertificateRefs[T, TEnt](ent)

	svc, isSvc := any(ent).(*configurationv1alpha1.KongService)
	if !isSvc {
		// Only KongService supports CA certificate refs for now.
		return ctrl.Result{}, nil
	}

	if len(refs) == 0 {
		if svc.Status.Konnect != nil {
			svc.Status.Konnect.CACertificateIDs = nil
		}
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

	var collectedIDs []string
	for _, ref := range refs {
		nn := types.NamespacedName{
			Name:      ref.Name,
			Namespace: ent.GetNamespace(),
		}
		if ref.Namespace != nil && *ref.Namespace != ent.GetNamespace() {
			// cross-namespace: check KongReferenceGrant
			if grantErr := crossnamespace.CheckKongReferenceGrantForResource(ctx, cl, ent.GetNamespace(), *ref.Namespace, ref.Name,
				metav1.GroupVersionKind(ent.GetObjectKind().GroupVersionKind()),
				metav1.GroupVersionKind(configurationv1alpha1.GroupVersion.WithKind("KongCACertificate")),
			); grantErr != nil {
				return ctrl.Result{}, grantErr
			}
			_ = patch.SetStatusWithConditionIfDifferent(ent,
				consts.ConditionType(configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs),
				metav1.ConditionTrue,
				configurationv1alpha1.KongReferenceGrantReasonResolvedRefs,
				"KongReferenceGrants allow access to KongCACertificate",
			)
			nn.Namespace = *ref.Namespace
		}

		cacert := &configurationv1alpha1.KongCACertificate{}
		if getErr := cl.Get(ctx, nn, cacert); getErr != nil {
			_ = patch.SetStatusWithConditionIfDifferent(ent,
				konnectv1alpha1.KongCACertificateRefsValidConditionType,
				metav1.ConditionFalse,
				konnectv1alpha1.KongCACertificateRefsReasonInvalid,
				getErr.Error(),
			)
			return ctrl.Result{}, ReferencedKongCACertificateDoesNotExistError{Reference: nn, Err: getErr}
		}

		if delTimestamp := cacert.GetDeletionTimestamp(); !delTimestamp.IsZero() {
			_ = patch.SetStatusWithConditionIfDifferent(ent,
				konnectv1alpha1.KongCACertificateRefsValidConditionType,
				metav1.ConditionFalse,
				konnectv1alpha1.KongCACertificateRefsReasonInvalid,
				fmt.Sprintf("Referenced KongCACertificate %s is being deleted", nn),
			)
			return ctrl.Result{}, ReferencedObjectIsBeingDeletedError{
				Reference:         nn,
				DeletionTimestamp: delTimestamp.Time,
			}
		}

		cond, ok := k8sutils.GetCondition(konnectv1alpha1.KonnectEntityProgrammedConditionType, cacert)
		if !ok || cond.Status != metav1.ConditionTrue {
			_ = patch.SetStatusWithConditionIfDifferent(ent,
				konnectv1alpha1.KongCACertificateRefsValidConditionType,
				metav1.ConditionFalse,
				konnectv1alpha1.KongCACertificateRefsReasonInvalid,
				fmt.Sprintf("Referenced KongCACertificate %s is not programmed yet", nn),
			)
			// Don't requeue. The referenced entity's changes will trigger the reconciliation.
			return ctrl.Result{}, nil
		}

		// Verify the KongCACertificate belongs to the same ControlPlane as the KongService.
		if cacert.Status.Konnect != nil && svc.GetControlPlaneID() != "" &&
			cacert.Status.Konnect.ControlPlaneID != svc.GetControlPlaneID() {
			_ = patch.SetStatusWithConditionIfDifferent(ent,
				konnectv1alpha1.KongCACertificateRefsValidConditionType,
				metav1.ConditionFalse,
				konnectv1alpha1.KongCACertificateRefsReasonInvalid,
				fmt.Sprintf("Referenced KongCACertificate %s belongs to a different ControlPlane", nn),
			)
			return ctrl.Result{}, ReferencedKongCACertificateBelongsToWrongControlPlaneError{
				Reference:   nn,
				CertCPID:    cacert.Status.Konnect.ControlPlaneID,
				ServiceCPID: svc.GetControlPlaneID(),
			}
		}

		id := cacert.GetKonnectID()
		if id == "" {
			_ = patch.SetStatusWithConditionIfDifferent(ent,
				konnectv1alpha1.KongCACertificateRefsValidConditionType,
				metav1.ConditionFalse,
				konnectv1alpha1.KongCACertificateRefsReasonInvalid,
				fmt.Sprintf("Referenced KongCACertificate %s has no Konnect ID yet", nn),
			)
			return ctrl.Result{}, nil
		}
		collectedIDs = append(collectedIDs, id)
	}

	// All refs resolved successfully.
	if svc.Status.Konnect == nil {
		svc.Status.Konnect = &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndCertificateAndCACertificatesRefs{}
	}
	svc.Status.Konnect.CACertificateIDs = collectedIDs

	_ = patch.SetStatusWithConditionIfDifferent(ent,
		konnectv1alpha1.KongCACertificateRefsValidConditionType,
		metav1.ConditionTrue,
		konnectv1alpha1.KongCACertificateRefsReasonValid,
		"All referenced KongCACertificates are programmed",
	)

	return ctrl.Result{}, nil
}
