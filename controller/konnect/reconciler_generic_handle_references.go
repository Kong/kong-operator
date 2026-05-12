package konnect

// TODO: this file contains manually maintained reference handling for generated Konnect types.
// This is a temporary solution until we have a more generic way of handling
// references for generated types, e.g. by generating reference handling code in the future with crd-from-oas.

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kong/kong-operator/v2/api/common/consts"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	ctrlconsts "github.com/kong/kong-operator/v2/controller/consts"
	"github.com/kong/kong-operator/v2/controller/konnect/constraints"
	"github.com/kong/kong-operator/v2/controller/pkg/patch"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

var (
	_crossRefHandlers map[string]crossRefHandlerI
)

func init() {
	// _crossRefHandlers contains the list of cross-reference handlers to be used in
	// handleGeneratedTypeReferences for handling inter-CR references configured via
	// the crd-from-oas references config, for example:
	// - crossRefHandlerImpl[konnectv1alpha1.EventGatewayBackendCluster, *konnectv1alpha1.EventGatewayBackendCluster]
	//   for handling references to EventGatewayBackendCluster.
	// This list is manually maintained for now, but in the future we may want to
	// generate this list based on the generated types and their reference configurations.
	_crossRefHandlers = map[string]crossRefHandlerI{
		"EventGatewayBackendCluster": newCrossRefHandlerForKind[konnectv1alpha1.EventGatewayBackendCluster](),
	}
}

// objectWithCrossReferences is implemented by entities that have inter-CR
// references configured via the crd-from-oas references config, for example:
// - EventGatewayListener with references to EventGatewayBackendCluster.
type objectWithCrossReferences interface {
	k8sutils.ConditionsAwareObject
	GetCrossReferences() []konnectv1alpha1.CrossReference
	SetCrossReferenceID(kind, id string)
}

// crossRefHandlerI handles fetching and validating a specific referenced kind.
type crossRefHandlerI interface {
	handleCrossRef(
		ctx context.Context,
		cl client.Client,
		obj objectWithCrossReferences,
		ref *commonv1alpha1.ObjectRef,
	) (ctrl.Result, error)
	crossRefKind() string
}

// crossRefHandlerImpl is a generic cross-reference handler for a specific type.
type crossRefHandlerImpl[T parentT, TPtr parentTPtr[T]] struct{}

func newCrossRefHandlerForKind[T parentT, TPtr parentTPtr[T]]() crossRefHandlerImpl[T, TPtr] {
	return crossRefHandlerImpl[T, TPtr]{}
}

func (h crossRefHandlerImpl[T, TPtr]) crossRefKind() string {
	return constraints.EntityTypeName[T]()
}

func (h crossRefHandlerImpl[T, TPtr]) handleCrossRef(
	ctx context.Context,
	cl client.Client,
	obj objectWithCrossReferences,
	ref *commonv1alpha1.ObjectRef,
) (ctrl.Result, error) {
	kind := h.crossRefKind()
	condType := consts.ConditionType(kind + "RefValid")

	referenced, nn, err := getParentForRef[T, TPtr](ctx, cl, *ref, obj.GetNamespace())
	if err != nil {
		if res, errStatus := patch.StatusWithCondition(
			ctx, cl, obj,
			condType,
			metav1.ConditionFalse,
			consts.ConditionReason("Invalid"),
			err.Error(),
		); errStatus != nil || !res.IsZero() {
			return res, errStatus
		}
		return ctrl.Result{}, err
	}

	if delTimestamp := referenced.GetDeletionTimestamp(); !delTimestamp.IsZero() {
		msg := fmt.Sprintf("Referenced %s %s is being deleted", kind, nn)
		if res, errStatus := patch.StatusWithCondition(
			ctx, cl, obj,
			condType,
			metav1.ConditionFalse,
			consts.ConditionReason("Invalid"),
			msg,
		); errStatus != nil || !res.IsZero() {
			return res, errStatus
		}
		return ctrl.Result{}, ReferencedObjectIsBeingDeletedError{
			Reference:         nn,
			DeletionTimestamp: delTimestamp.Time,
		}
	}

	cond, ok := k8sutils.GetCondition(konnectv1alpha1.KonnectEntityProgrammedConditionType, referenced)
	if !ok || cond.Status != metav1.ConditionTrue {
		msg := fmt.Sprintf("Referenced %s %s is not programmed yet", kind, nn)
		if res, errStatus := patch.StatusWithCondition(
			ctx, cl, obj,
			condType,
			metav1.ConditionFalse,
			consts.ConditionReason("NotProgrammed"),
			msg,
		); errStatus != nil || !res.IsZero() {
			return res, errStatus
		}
		return ctrl.Result{RequeueAfter: ctrlconsts.RequeueWithoutBackoff}, nil
	}

	if referenced.GetKonnectID() == "" {
		refErr := ReferencedObjectIsInvalidError{
			Reference: nn.String(),
			Msg:       fmt.Sprintf("Referenced %s does not have a Konnect ID yet", kind),
		}
		if res, errStatus := patch.StatusWithCondition(
			ctx, cl, obj,
			condType,
			metav1.ConditionFalse,
			consts.ConditionReason("Invalid"),
			refErr.Error(),
		); errStatus != nil || !res.IsZero() {
			return res, errStatus
		}
		return ctrl.Result{}, refErr
	}

	old := obj.DeepCopyObject().(objectWithCrossReferences)
	obj.SetCrossReferenceID(kind, referenced.GetKonnectID())
	if _, err := patch.ApplyStatusPatchIfNotEmpty(ctx, cl, ctrllog.FromContext(ctx), obj, old); err != nil {
		if apierrors.IsConflict(err) {
			return ctrl.Result{RequeueAfter: ctrlconsts.RequeueWithoutBackoff}, nil
		}
		return ctrl.Result{}, err
	}

	if res, errStatus := patch.StatusWithCondition(
		ctx, cl, obj,
		condType,
		metav1.ConditionTrue,
		consts.ConditionReason("Valid"),
		fmt.Sprintf("Referenced %s %s is programmed", kind, nn),
	); errStatus != nil || !res.IsZero() {
		return res, errStatus
	}

	return ctrl.Result{}, nil
}

// handleCrossReferences processes all cross-CR references on the entity.
func (r *KonnectEntityReconciler[T, TEnt]) handleCrossReferences(
	ctx context.Context,
	ent TEnt,
) (bool, ctrl.Result, error) {
	obj, ok := any(ent).(objectWithCrossReferences)
	if !ok {
		return false, ctrl.Result{}, nil
	}

	refs := obj.GetCrossReferences()
	if len(refs) == 0 {
		return false, ctrl.Result{}, nil
	}

	for _, ref := range refs {
		if ref.Ref == nil {
			continue
		}
		handler, ok := _crossRefHandlers[ref.Kind]
		if !ok {
			return true, ctrl.Result{}, fmt.Errorf("no cross-reference handler for kind %q", ref.Kind)
		}
		res, err := handler.handleCrossRef(ctx, r.Client, obj, ref.Ref)
		stop, res, err := handleRefResult(ctx, r.Client, ent, res, err)
		if stop || err != nil {
			return true, res, err
		}
	}

	return false, ctrl.Result{}, nil
}
