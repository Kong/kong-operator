package patch

import (
	"context"
	"fmt"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"

	kcfgconsts "github.com/kong/kubernetes-configuration/v2/api/common/consts"
)

// SetStatusWithConditionIfDifferent sets the status of the provided object with the
// given condition if the condition is different from the current one.
// It does not take LastTransitionTime into account.
// The return value tells whether status needs an update.
func SetStatusWithConditionIfDifferent[T interface {
	client.Object
	k8sutils.ConditionsAware
}](
	ent T,
	conditionType kcfgconsts.ConditionType,
	conditionStatus metav1.ConditionStatus,
	conditionReason kcfgconsts.ConditionReason,
	conditionMessage string,
) bool {
	cond, ok := k8sutils.GetCondition(conditionType, ent)
	if ok &&
		cond.Status == conditionStatus &&
		cond.Reason == string(conditionReason) &&
		cond.Message == conditionMessage &&
		cond.ObservedGeneration == ent.GetGeneration() {
		// If the condition is already set and it's as expected, return.
		// We don't want to bump the condition's LastTransitionTime which
		// could cause unnecessary requeues.
		return false
	}

	k8sutils.SetCondition(
		k8sutils.NewConditionWithGeneration(
			conditionType,
			conditionStatus,
			conditionReason,
			conditionMessage,
			ent.GetGeneration(),
		),
		ent,
	)
	return true
}

// StatusWithConditions patches the status of the provided object with the
// given conditions.
func StatusWithConditions[T interface {
	client.Object
	k8sutils.ConditionsAware
}](
	ctx context.Context,
	cl client.Client,
	ent T,
	conditions ...metav1.Condition,
) (res ctrl.Result, updated bool, err error) {
	old := ent.DeepCopyObject().(T)
	var needsUpdate bool
	for _, condition := range conditions {
		if SetStatusWithConditionIfDifferent(ent,
			kcfgconsts.ConditionType(condition.Type),
			condition.Status,
			kcfgconsts.ConditionReason(condition.Reason),
			condition.Message,
		) {
			needsUpdate = true
		}
	}

	if needsUpdate {
		if err := cl.Status().Patch(ctx, ent, client.MergeFrom(old)); err != nil {
			if k8serrors.IsConflict(err) {
				return ctrl.Result{Requeue: true}, false, nil
			}
			return ctrl.Result{}, false, fmt.Errorf("failed to patch status with conditions %v: %w", conditions, err)
		}
		return ctrl.Result{}, true, nil
	}

	return ctrl.Result{}, false, nil
}

// StatusWithCondition patches the status of the provided object with the
// given condition.
// If the condition is already set and it's as expected, it returns without patching.
func StatusWithCondition[T interface {
	client.Object
	k8sutils.ConditionsAware
}](
	ctx context.Context,
	cl client.Client,
	ent T,
	conditionType kcfgconsts.ConditionType,
	conditionStatus metav1.ConditionStatus,
	conditionReason kcfgconsts.ConditionReason,
	conditionMessage string,
) (ctrl.Result, error) {
	old := ent.DeepCopyObject().(T)
	if !SetStatusWithConditionIfDifferent(ent,
		conditionType,
		conditionStatus,
		conditionReason,
		conditionMessage,
	) {
		return ctrl.Result{}, nil
	}

	if err := cl.Status().Patch(ctx, ent, client.MergeFrom(old)); err != nil {
		if k8serrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to patch status with %s condition: %w", conditionType, err)
	}

	return ctrl.Result{}, nil
}
