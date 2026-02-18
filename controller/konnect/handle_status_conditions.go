package konnect

import (
	"context"
	"fmt"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/pkg/patch"
	"github.com/kong/kong-operator/v2/internal/utils/crossnamespace"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// handleAPIAuthStatusCondition handles the status conditions for the APIAuthConfiguration reference.
// The last variadic parameter is related to the optional depending conditions. This parameter is set only when
// a check fails and the related condition gets marked as False.
// Example: The KonnectExtension resource has a ready condition that must be set to False if any of the
// conditions set here are false. Passing it as a depending condition will ensure that the it
// is set to False if any of the conditions set here are false.
func handleAPIAuthStatusCondition[T interface {
	client.Object
	k8sutils.ConditionsAware
}](
	ctx context.Context,
	cl client.Client,
	ent T,
	apiAuth konnectv1alpha1.KonnectAPIAuthConfiguration,
	apiAuthNN client.ObjectKey,
	err error,
	dependingConditions ...metav1.Condition,
) (requeue bool, res ctrl.Result, retErr error) {
	resolvedRefCondition := metav1.Condition{
		Type:    konnectv1alpha1.KonnectEntityAPIAuthConfigurationResolvedRefConditionType,
		Status:  metav1.ConditionTrue,
		Reason:  konnectv1alpha1.KonnectEntityAPIAuthConfigurationResolvedRefReasonResolvedRef,
		Message: fmt.Sprintf("KonnectAPIAuthConfiguration reference %s is resolved", apiAuthNN),
	}

	if err != nil {
		resolvedRefCondition.Status = metav1.ConditionFalse
		if k8serrors.IsNotFound(err) {
			resolvedRefCondition.Reason = konnectv1alpha1.KonnectEntityAPIAuthConfigurationResolvedRefReasonRefNotFound
			resolvedRefCondition.Message = fmt.Sprintf("Referenced KonnectAPIAuthConfiguration %s not found", apiAuthNN)
			if res, _, err := patch.StatusWithConditions(
				ctx,
				cl,
				ent,
				append(dependingConditions, resolvedRefCondition)...,
			); err != nil || !res.IsZero() {
				return true, ctrl.Result{}, err
			}
			return true, ctrl.Result{}, nil
		}

		if crossnamespace.IsReferenceNotGranted(err) {
			resolvedRefCondition.Reason = konnectv1alpha1.KonnectEntityAPIAuthConfigurationResolvedRefReasonRefNotPermitted
			resolvedRefCondition.Message = err.Error()
			if res, _, err := patch.StatusWithConditions(
				ctx,
				cl,
				ent,
				append(dependingConditions, resolvedRefCondition)...,
			); err != nil || !res.IsZero() {
				return true, ctrl.Result{}, err
			}
			return true, ctrl.Result{}, nil
		}

		resolvedRefCondition.Reason = konnectv1alpha1.KonnectEntityAPIAuthConfigurationResolvedRefReasonRefInvalid
		resolvedRefCondition.Message = fmt.Sprintf("KonnectAPIAuthConfiguration reference %s is invalid: %v", apiAuthNN, err)
		if res, _, err := patch.StatusWithConditions(
			ctx,
			cl,
			ent,
			append(dependingConditions, resolvedRefCondition)...,
		); err != nil || !res.IsZero() {
			return true, ctrl.Result{}, err
		}
		return true, ctrl.Result{}, fmt.Errorf("failed to get KonnectAPIAuthConfiguration: %w", err)
	}

	// Update the status if the reference is resolved and it's not as expected.
	if cond, present := k8sutils.GetCondition(konnectv1alpha1.KonnectEntityAPIAuthConfigurationResolvedRefConditionType, ent); !present ||
		cond.Status != metav1.ConditionTrue ||
		cond.ObservedGeneration != ent.GetGeneration() ||
		cond.Reason != konnectv1alpha1.KonnectEntityAPIAuthConfigurationResolvedRefReasonResolvedRef {
		if res, _, err := patch.StatusWithConditions(
			ctx,
			cl,
			ent,
			resolvedRefCondition,
		); err != nil || !res.IsZero() {
			return true, res, err
		}
		return true, ctrl.Result{}, nil
	}

	apiAuthValidCondition := metav1.Condition{
		Type:    konnectv1alpha1.KonnectEntityAPIAuthConfigurationValidConditionType,
		Status:  metav1.ConditionTrue,
		Reason:  konnectv1alpha1.KonnectEntityAPIAuthConfigurationReasonValid,
		Message: conditionMessageReferenceKonnectAPIAuthConfigurationValid(apiAuthNN),
	}

	if cond, present := k8sutils.GetCondition(konnectv1alpha1.KonnectEntityAPIAuthConfigurationValidConditionType, &apiAuth); !present ||
		cond.Status != metav1.ConditionTrue ||
		cond.Reason != konnectv1alpha1.KonnectEntityAPIAuthConfigurationReasonValid {

		apiAuthValidCondition.Status = metav1.ConditionFalse
		apiAuthValidCondition.Reason = konnectv1alpha1.KonnectEntityAPIAuthConfigurationReasonInvalid
		apiAuthValidCondition.Message = conditionMessageReferenceKonnectAPIAuthConfigurationInvalid(apiAuthNN)
		// If it's invalid then set the "APIAuthValid" status condition on
		// the entity to False with "Invalid" reason.
		if res, _, err := patch.StatusWithConditions(
			ctx,
			cl,
			ent,
			append(dependingConditions, apiAuthValidCondition)...,
		); err != nil || !res.IsZero() {
			return true, res, err
		}

		return true, ctrl.Result{}, nil
	}

	// If the referenced APIAuthConfiguration is valid, set the "APIAuthValid"
	// condition to True with "Valid" reason.
	// Only perform the update if the condition is not as expected.
	if cond, present := k8sutils.GetCondition(konnectv1alpha1.KonnectEntityAPIAuthConfigurationValidConditionType, ent); !present ||
		cond.Status != metav1.ConditionTrue ||
		cond.Reason != konnectv1alpha1.KonnectEntityAPIAuthConfigurationReasonValid ||
		cond.ObservedGeneration != ent.GetGeneration() ||
		cond.Message != conditionMessageReferenceKonnectAPIAuthConfigurationValid(apiAuthNN) {

		if res, _, err := patch.StatusWithConditions(
			ctx,
			cl,
			ent,
			apiAuthValidCondition,
		); err != nil || !res.IsZero() {
			return true, res, err
		}
		return true, ctrl.Result{}, nil
	}

	return false, ctrl.Result{}, nil
}
