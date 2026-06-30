package konnect

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kong/kong-operator/v2/api/common/consts"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	ctrlconsts "github.com/kong/kong-operator/v2/controller/consts"
	"github.com/kong/kong-operator/v2/controller/konnect/constraints"
	"github.com/kong/kong-operator/v2/controller/pkg/patch"
	"github.com/kong/kong-operator/v2/internal/utils/crossnamespace"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

type objectWithParentRef interface {
	k8sutils.ConditionsAwareObject
	client.Object
	GetParentRef() commonv1alpha1.ObjectRef
	SetParentID(string)
	GetStatusConditionTypeParentRefValid() string
	GetStatusConditionReasonParentRefValid() string
	GetStatusConditionReasonParentRefInvalid() string
	GetStatusConditionReasonParentRefNotProgrammed() string
	GetParentGVK() schema.GroupVersionKind
}

// objectExposingAncestorIDs is implemented by a parent entity that can
// publish its transitively-resolved ancestor Konnect IDs (keyed by ancestor
// Kind) so that a child entity can absorb them in a single hop.
type objectExposingAncestorIDs interface {
	GetAncestorIDs() map[string]string
}

// objectAcceptingAncestorIDs is implemented by a child entity that can
// receive ancestor Konnect IDs published by its immediate parent, keyed by
// ancestor Kind.
type objectAcceptingAncestorIDs interface {
	SetAncestorID(kind, id string)
}

func ensureKongReferenceGrantForParentRef[
	T objectWithParentRef,
](
	ctx context.Context,
	cl client.Client,
	ent T,
	ref commonv1alpha1.ObjectRef,
	parentGVK schema.GroupVersionKind,
) (ctrl.Result, error) {
	parentTypeName := parentGVK.Kind
	if ref.Type != commonv1alpha1.ObjectRefTypeNamespacedRef ||
		ref.NamespacedRef == nil ||
		ref.NamespacedRef.Namespace == nil ||
		*ref.NamespacedRef.Namespace == ent.GetNamespace() {
		// The parent ref doesn't require a grant. Only remove the shared
		// ResolvedRefs condition if no other cross-namespace reference of this
		// entity still justifies it, otherwise we'd clobber the condition that
		// the secret ref handler sets (it runs later in Reconcile).
		if !parentRefEntityHasCrossNamespaceRefs(ent) {
			if res, errStatus := patch.StatusWithoutCondition(
				ctx, cl, ent,
				configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs,
			); errStatus != nil || !res.IsZero() {
				return res, errStatus
			}
		}
		return ctrl.Result{}, nil
	}

	targetNamespace := *ref.NamespacedRef.Namespace
	err := crossnamespace.CheckKongReferenceGrantForResource(
		ctx,
		cl,
		ent.GetNamespace(),
		targetNamespace,
		ref.NamespacedRef.Name,
		metav1.GroupVersionKind(ent.GetObjectKind().GroupVersionKind()),
		metav1.GroupVersionKind(parentGVK),
	)

	if crossnamespace.IsReferenceNotGranted(err) {
		if res, errStatus := patch.StatusWithCondition(
			ctx, cl, ent,
			consts.ConditionType(configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs),
			metav1.ConditionFalse,
			configurationv1alpha1.KongReferenceGrantReasonRefNotPermitted,
			fmt.Sprintf(
				"KongReferenceGrants do not allow access to %s %s/%s",
				parentTypeName,
				targetNamespace,
				ref.NamespacedRef.Name,
			),
		); errStatus != nil || !res.IsZero() {
			return res, errStatus
		}
		return ctrl.Result{RequeueAfter: ctrlconsts.RequeueWithoutBackoff}, nil
	}
	if err != nil {
		return ctrl.Result{}, err
	}

	if res, errStatus := patch.StatusWithCondition(
		ctx, cl, ent,
		consts.ConditionType(configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs),
		metav1.ConditionTrue,
		configurationv1alpha1.KongReferenceGrantReasonResolvedRefs,
		fmt.Sprintf(
			"KongReferenceGrants allow access to %s %s/%s",
			parentTypeName,
			targetNamespace,
			ref.NamespacedRef.Name,
		),
	); errStatus != nil || !res.IsZero() {
		return res, errStatus
	}

	return ctrl.Result{}, nil
}

// parentRefEntityHasCrossNamespaceRefs reports whether a parent-ref entity has a
// cross-namespace sensitive-data secret ref. Such refs are the only cross-namespace
// reference type a parent-ref entity carries, and the secret ref handler owns the
// shared ResolvedRefs condition for them. The parent-ref handler must not remove
// ResolvedRefs while one exists, otherwise it would clobber what that handler sets.
func parentRefEntityHasCrossNamespaceRefs(ent client.Object) bool {
	g, ok := any(ent).(sensitiveDataSecretRefsGetter)
	if !ok {
		return false
	}
	ns := ent.GetNamespace()
	for _, r := range g.GetSensitiveDataSecretRefs() {
		if n := r.Ref.Namespace; n != nil && *n != "" && *n != ns {
			return true
		}
	}
	return false
}

type parentRefHandler[
	ParentT parentT,
	ParentTPtr parentTPtr[ParentT],
] struct{}

func (prh parentRefHandler[p, pPTr]) parentTypeName() string {
	return constraints.EntityTypeName[p]()
}

// handleParentRef handles the parent refercence for an entity.
// It ensures that the referenced parent object exists, is programmed, and has a Konnect ID,
// and then sets the parent's Konnect ID on the child entity's status.
func (prh parentRefHandler[p, pPTr]) handleParentRef(
	ctx context.Context,
	cl client.Client,
	obj objectWithParentRef,
) (ctrl.Result, error) {
	parentType := constraints.EntityTypeName[p]()

	parentRef := obj.GetParentRef()
	if res, err := ensureKongReferenceGrantForParentRef(ctx, cl, obj, parentRef, obj.GetParentGVK()); err != nil || !res.IsZero() {
		return res, err
	}

	parent, nn, err := getParentForRef[p, pPTr](ctx, cl, parentRef, obj.GetNamespace())
	if err != nil {
		if res, errStatus := patch.StatusWithCondition(
			ctx, cl, obj,
			consts.ConditionType(obj.GetStatusConditionTypeParentRefValid()),
			metav1.ConditionFalse,
			consts.ConditionReason(obj.GetStatusConditionReasonParentRefInvalid()),
			err.Error(),
		); errStatus != nil || !res.IsZero() {
			return res, errStatus
		}
		return ctrl.Result{}, err
	}

	if delTimestamp := parent.GetDeletionTimestamp(); !delTimestamp.IsZero() {
		msg := fmt.Sprintf("Referenced %s %s is being deleted", parentType, nn)
		if res, errStatus := patch.StatusWithCondition(
			ctx, cl, obj,
			consts.ConditionType(obj.GetStatusConditionTypeParentRefValid()),
			metav1.ConditionFalse,
			consts.ConditionReason(obj.GetStatusConditionReasonParentRefInvalid()),
			msg,
		); errStatus != nil || !res.IsZero() {
			return res, errStatus
		}
		return ctrl.Result{}, ReferencedObjectIsBeingDeletedError{
			Reference:         nn,
			DeletionTimestamp: delTimestamp.Time,
		}
	}

	cond, ok := k8sutils.GetCondition(konnectv1alpha1.KonnectEntityProgrammedConditionType, parent)
	if !ok || cond.Status != metav1.ConditionTrue {
		msg := fmt.Sprintf("Referenced %s %s is not programmed yet", parentType, nn)
		if res, errStatus := patch.StatusWithCondition(
			ctx, cl, obj,
			consts.ConditionType(obj.GetStatusConditionTypeParentRefValid()),
			metav1.ConditionFalse,
			consts.ConditionReason(obj.GetStatusConditionReasonParentRefNotProgrammed()),
			msg,
		); errStatus != nil || !res.IsZero() {
			return res, errStatus
		}
		return ctrl.Result{RequeueAfter: ctrlconsts.RequeueWithoutBackoff}, nil
	}

	if parent.GetKonnectID() == "" {
		err := ReferencedObjectIsInvalidError{
			Reference: nn.String(),
			Msg:       fmt.Sprintf("Referenced %s does not have a Konnect ID yet", parentType),
		}
		if res, errStatus := patch.StatusWithCondition(
			ctx, cl, obj,
			consts.ConditionType(obj.GetStatusConditionTypeParentRefValid()),
			metav1.ConditionFalse,
			consts.ConditionReason(obj.GetStatusConditionReasonParentRefInvalid()),
			err.Error(),
		); errStatus != nil || !res.IsZero() {
			return res, errStatus
		}
		return ctrl.Result{}, err
	}

	old := obj.DeepCopyObject().(objectWithParentRef)
	obj.SetParentID(parent.GetKonnectID())
	_, err = patch.ApplyStatusPatchIfNotEmpty(ctx, cl, ctrllog.FromContext(ctx), obj, old)
	if err != nil {
		if apierrors.IsConflict(err) {
			return ctrl.Result{RequeueAfter: ctrlconsts.RequeueWithoutBackoff}, nil
		}
		return ctrl.Result{}, err
	}

	if pa, ok := any(parent).(objectExposingAncestorIDs); ok {
		if cs, ok := any(obj).(objectAcceptingAncestorIDs); ok {
			ancestors := pa.GetAncestorIDs()
			for _, id := range ancestors {
				if id == "" {
					return ctrl.Result{RequeueAfter: ctrlconsts.RequeueWithoutBackoff}, nil
				}
			}
			old := obj.DeepCopyObject().(objectWithParentRef)
			for kind, id := range ancestors {
				cs.SetAncestorID(kind, id)
			}
			if _, err = patch.ApplyStatusPatchIfNotEmpty(ctx, cl, ctrllog.FromContext(ctx), obj, old); err != nil {
				if apierrors.IsConflict(err) {
					return ctrl.Result{RequeueAfter: ctrlconsts.RequeueWithoutBackoff}, nil
				}
				return ctrl.Result{}, err
			}
		}
	}

	if res, errStatus := patch.StatusWithCondition(
		ctx, cl, obj,
		consts.ConditionType(obj.GetStatusConditionTypeParentRefValid()),
		metav1.ConditionTrue,
		consts.ConditionReason(obj.GetStatusConditionReasonParentRefValid()),
		fmt.Sprintf("Referenced %s %s is programmed", parentType, nn),
	); errStatus != nil || !res.IsZero() {
		return res, errStatus
	}

	return ctrl.Result{}, nil
}

func getParentForRef[
	ParentT parentT,
	ParentTPtr parentTPtr[ParentT],
](
	ctx context.Context,
	cl client.Client,
	ref commonv1alpha1.ObjectRef,
	namespace string,
) (ParentTPtr, types.NamespacedName, error) {
	switch ref.Type {
	case commonv1alpha1.ObjectRefTypeNamespacedRef:
		if ref.NamespacedRef == nil {
			return nil, types.NamespacedName{}, fmt.Errorf("ref.namespacedRef is required when type is namespacedRef")
		}
		nn := types.NamespacedName{
			Name:      ref.NamespacedRef.Name,
			Namespace: namespace,
		}
		// When the namespace in the parentRef field is an empty string, then
		// we are going to use the namespace of the resource that contains the parentRef.
		if ref.NamespacedRef.Namespace != nil && *ref.NamespacedRef.Namespace != "" {
			nn.Namespace = *ref.NamespacedRef.Namespace
		}

		var p ParentT
		var ptr ParentTPtr = &p
		if err := cl.Get(ctx, nn, ptr); err != nil {
			if apierrors.IsNotFound(err) {
				return nil, nn, ReferencedObjectDoesNotExistError{
					Reference: nn,
					Err:       err,
				}
			}
			return nil, nn, fmt.Errorf("failed to get %T %s: %w", ptr, nn, err)
		}
		return ptr, nn, nil

	case commonv1alpha1.ObjectRefTypeKonnectID:
		return nil, types.NamespacedName{}, fmt.Errorf("unsupported ref type %q", ref.Type)

	default:
		return nil, types.NamespacedName{}, fmt.Errorf("unsupported ref type %q", ref.Type)
	}
}
