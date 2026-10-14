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
	refs, ok := sensitiveDataSecretRefs(ent)
	if !ok {
		return false
	}
	ns := ent.GetNamespace()
	for _, r := range refs {
		if n := r.Namespace; n != nil && *n != "" && *n != ns {
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

	// The parent may be outside the Konnect reconciler's scope entirely (e.g. an
	// AIGatewayConsumer targeting an OnPremAIGateway). This entity is then not
	// Konnect-managed either: clear the conditions the Konnect reconciler set on
	// it and stop before any Konnect interaction. Returning the sentinel error
	// (instead of a zero result) is what makes handleRefResult stop the
	// reconciliation.
	// The check covers the parent's ancestors too: an EventGatewayVirtualClusterPolicy
	// is out of scope when its virtual cluster's backend cluster targets an
	// OnPremEventGateway. It runs before the KongReferenceGrant check, which
	// only applies to Konnect-managed entities.
	parent, nn, err := getParentForRef[p, pPTr](ctx, cl, parentRef, obj.GetNamespace())
	if err == nil && skipsKonnectReconciliation(ctx, cl, parent) {
		if res, errStatus := removeKonnectConditions(ctx, cl, obj); errStatus != nil || !res.IsZero() {
			return res, errStatus
		}
		return ctrl.Result{}, ReferencedObjectNotKonnectManagedError{
			Reference: nn,
			TypeName:  parentType,
		}
	}

	if res, err := ensureKongReferenceGrantForParentRef(ctx, cl, obj, parentRef, obj.GetParentGVK()); err != nil || !res.IsZero() {
		return res, err
	}

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

// maxAncestorDepth bounds the walk of skipsKonnectReconciliation. The deepest
// chain is a policy, its virtual cluster, the backend cluster and the gateway.
const maxAncestorDepth = 4

// objectWithParentGVK is an entity that can name its parent.
type objectWithParentGVK interface {
	GetParentRef() commonv1alpha1.ObjectRef
	GetParentGVK() schema.GroupVersionKind
}

// skipsKonnectReconciliation reports whether obj, or one of its ancestors, is
// not managed by the Konnect reconciler, e.g. an EventGatewayBackendCluster
// targeting an OnPremEventGateway. The walk stops at the first ancestor that
// cannot be resolved, e.g. one that does not exist (yet): the regular parent
// handling reports it.
func skipsKonnectReconciliation(ctx context.Context, cl client.Client, obj client.Object) bool {
	for range maxAncestorDepth {
		if skipper, ok := obj.(konnectReconciliationSkipper); ok && skipper.SkipKonnectReconciliation() {
			return true
		}
		child, ok := obj.(objectWithParentGVK)
		if !ok {
			return false
		}
		ref := child.GetParentRef()
		if ref.Type != commonv1alpha1.ObjectRefTypeNamespacedRef || ref.NamespacedRef == nil {
			return false
		}
		o, err := cl.Scheme().New(child.GetParentGVK())
		if err != nil {
			return false
		}
		parent, ok := o.(client.Object)
		if !ok {
			return false
		}
		nn := types.NamespacedName{
			Namespace: obj.GetNamespace(),
			Name:      ref.NamespacedRef.Name,
		}
		if ns := ref.NamespacedRef.Namespace; ns != nil && *ns != "" {
			nn.Namespace = *ns
		}
		if err := cl.Get(ctx, nn, parent); err != nil {
			return false
		}
		obj = parent
	}
	return false
}

// konnectProgrammedReasons are the reasons of the Programmed condition that
// only the Konnect reconciler sets. Programmed with another reason, e.g. one
// set by an on-prem controller, is left alone.
var konnectProgrammedReasons = map[string]struct{}{
	konnectv1alpha1.KonnectEntityProgrammedReasonConditionWithStatusFalseExists:             {},
	konnectv1alpha1.KonnectEntityProgrammedReasonKonnectAPIOpFailed:                         {},
	konnectv1alpha1.KonnectEntityProgrammedReasonFailedToResolveConsumerGroupRefs:           {},
	konnectv1alpha1.KonnectEntityProgrammedReasonFailedToReconcileConsumerGroupsWithKonnect: {},
	konnectv1alpha1.KonnectEntityProgrammedReasonDeletionBlocked:                            {},
}

// removeKonnectConditions removes the conditions the Konnect reconciler may
// have set on an entity that turns out not to be Konnect-managed, e.g. while
// its on-prem ancestor did not exist yet.
func removeKonnectConditions(ctx context.Context, cl client.Client, obj objectWithParentRef) (ctrl.Result, error) {
	old, ok := obj.DeepCopyObject().(client.Object)
	if !ok {
		return ctrl.Result{}, fmt.Errorf("failed to copy %T", obj)
	}
	removed := false
	for _, conditionType := range []string{
		obj.GetStatusConditionTypeParentRefValid(),
		configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs,
		commonv1alpha1.KonnectReferencesResolvedConditionType,
	} {
		if k8sutils.RemoveCondition(consts.ConditionType(conditionType), obj) {
			removed = true
		}
	}
	programmed := consts.ConditionType(konnectv1alpha1.KonnectEntityProgrammedConditionType)
	if cond, ok := k8sutils.GetCondition(programmed, obj); ok {
		if _, isKonnect := konnectProgrammedReasons[cond.Reason]; isKonnect && k8sutils.RemoveCondition(programmed, obj) {
			removed = true
		}
	}
	if !removed {
		return ctrl.Result{}, nil
	}

	// Optimistic lock: a JSON merge patch replaces the whole conditions array,
	// so patching from a stale cached object would drop conditions persisted by
	// more recent writes.
	if err := cl.Status().Patch(ctx, obj, client.MergeFromWithOptions(old, client.MergeFromWithOptimisticLock{})); err != nil {
		if apierrors.IsConflict(err) {
			return ctrl.Result{RequeueAfter: ctrlconsts.RequeueWithoutBackoff}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to remove Konnect conditions: %w", err)
	}
	return ctrl.Result{}, nil
}
