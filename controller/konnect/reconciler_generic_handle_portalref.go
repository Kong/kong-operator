package konnect

// TODO: this file contains manually maintained reference handling for generated Konnect types.
// This is a temporary solution until we have a more generic way of handling
// references for generated types, e.g. by generating reference handling code in the future with crd-from-oas.

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	ctrlconsts "github.com/kong/kong-operator/v2/controller/consts"
	"github.com/kong/kong-operator/v2/controller/pkg/patch"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// handlePortalRef handles the PortalRef.
func handlePortalRef(
	ctx context.Context,
	cl client.Client,
	obj k8sutils.ConditionsAwareObject,
) (ctrl.Result, error) {
	// TODO: refactor this to be more generic and reusable for other types of references.
	type TObj interface {
		k8sutils.ConditionsAwareObject
		portalRefAccessor
		GetTypeName() string
		GetPortalID() string
		SetPortalID(id string)
	}

	o, ok := any(obj).(TObj)
	if !ok {
		return ctrl.Result{}, &UnsupportedGeneratedReferenceTypeError{
			TypeName: fmt.Sprintf("%T", obj),
		}
	}

	portalRef := o.GetPortalRef()
	if res, err := ensureKongReferenceGrantForParentRef(ctx, cl, o, portalRef); err != nil || !res.IsZero() {
		return res, err
	}

	portal, nn, err := getPortalForRef(ctx, cl, portalRef, o.GetNamespace())
	if err != nil {
		if res, errStatus := patch.StatusWithCondition(
			ctx, cl, o,
			konnectv1alpha1.PortalRefValidConditionType,
			metav1.ConditionFalse,
			konnectv1alpha1.PortalRefReasonInvalid,
			err.Error(),
		); errStatus != nil || !res.IsZero() {
			return res, errStatus
		}
		return ctrl.Result{}, err
	}

	if delTimestamp := portal.GetDeletionTimestamp(); !delTimestamp.IsZero() {
		msg := fmt.Sprintf("Referenced Portal %s is being deleted", nn)
		if res, errStatus := patch.StatusWithCondition(
			ctx, cl, o,
			konnectv1alpha1.PortalRefValidConditionType,
			metav1.ConditionFalse,
			konnectv1alpha1.PortalRefReasonInvalid,
			msg,
		); errStatus != nil || !res.IsZero() {
			return res, errStatus
		}
		return ctrl.Result{}, ReferencedObjectIsBeingDeletedError{
			Reference:         nn,
			DeletionTimestamp: delTimestamp.Time,
		}
	}

	cond, ok := k8sutils.GetCondition(konnectv1alpha1.KonnectEntityProgrammedConditionType, portal)
	if !ok || cond.Status != metav1.ConditionTrue {
		if res, errStatus := patch.StatusWithCondition(
			ctx, cl, o,
			konnectv1alpha1.PortalRefValidConditionType,
			metav1.ConditionFalse,
			konnectv1alpha1.PortalRefReasonNotProgrammed,
			fmt.Sprintf("Referenced Portal %s is not programmed yet", nn),
		); errStatus != nil || !res.IsZero() {
			return res, errStatus
		}
		return ctrl.Result{RequeueAfter: ctrlconsts.RequeueWithoutBackoff}, nil
	}

	if portal.GetKonnectID() == "" {
		err := ReferencedObjectIsInvalidError{
			Reference: nn.String(),
			Msg:       "Referenced Portal does not have a Konnect ID yet",
		}
		if res, errStatus := patch.StatusWithCondition(
			ctx, cl, o,
			konnectv1alpha1.PortalRefValidConditionType,
			metav1.ConditionFalse,
			konnectv1alpha1.PortalRefReasonInvalid,
			err.Error(),
		); errStatus != nil || !res.IsZero() {
			return res, errStatus
		}
		return ctrl.Result{}, err
	}

	old := o.DeepCopyObject().(TObj)
	o.SetPortalID(portal.GetKonnectID())
	_, err = patch.ApplyStatusPatchIfNotEmpty(ctx, cl, ctrllog.FromContext(ctx), o, old)
	if err != nil {
		if apierrors.IsConflict(err) {
			return ctrl.Result{RequeueAfter: ctrlconsts.RequeueWithoutBackoff}, nil
		}
		return ctrl.Result{}, err
	}

	if res, errStatus := patch.StatusWithCondition(
		ctx, cl, o,
		konnectv1alpha1.PortalRefValidConditionType,
		metav1.ConditionTrue,
		konnectv1alpha1.PortalRefReasonValid,
		fmt.Sprintf("Referenced Portal %s is programmed", nn),
	); errStatus != nil || !res.IsZero() {
		return res, errStatus
	}

	return ctrl.Result{}, nil
}

func getPortalForRef(
	ctx context.Context,
	cl client.Client,
	ref commonv1alpha1.ObjectRef,
	namespace string,
) (*konnectv1alpha1.Portal, types.NamespacedName, error) {
	switch ref.Type {
	case commonv1alpha1.ObjectRefTypeNamespacedRef:
		if ref.NamespacedRef == nil {
			return nil, types.NamespacedName{}, fmt.Errorf("portalRef.namespacedRef is required when type is namespacedRef")
		}
		nn := types.NamespacedName{
			Name:      ref.NamespacedRef.Name,
			Namespace: namespace,
		}
		if ref.NamespacedRef.Namespace != nil && *ref.NamespacedRef.Namespace != "" {
			nn.Namespace = *ref.NamespacedRef.Namespace
		}

		var portal konnectv1alpha1.Portal
		if err := cl.Get(ctx, nn, &portal); err != nil {
			if apierrors.IsNotFound(err) {
				return nil, nn, ReferencedObjectDoesNotExistError{
					Reference: nn,
					Err:       err,
				}
			}
			return nil, nn, fmt.Errorf("failed to get Portal %s: %w", nn, err)
		}
		return &portal, nn, nil

	case commonv1alpha1.ObjectRefTypeKonnectID:
		return nil, types.NamespacedName{}, fmt.Errorf("unsupported portalRef type %q", ref.Type)

	default:
		return nil, types.NamespacedName{}, fmt.Errorf("unsupported portalRef type %q", ref.Type)
	}
}
