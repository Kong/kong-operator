package konnect

import (
	"context"
	"fmt"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/controller/pkg/patch"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

// handleAPIAuthStatusCondition handles the status conditions for the APIAuthConfiguration reference.
func handleAPIAuthStatusCondition[T interface {
	client.Object
	k8sutils.ConditionsAware
}](
	ctx context.Context,
	cl client.Client,
	ent T,
	apiAuth konnectv1alpha1.KonnectAPIAuthConfiguration,
	err error,
) (requeue bool, res ctrl.Result, retErr error) {
	if err != nil {
		if k8serrors.IsNotFound(err) {
			if res, err := patch.StatusWithCondition(
				ctx, cl, ent,
				konnectv1alpha1.KonnectEntityAPIAuthConfigurationResolvedRefConditionType,
				metav1.ConditionFalse,
				konnectv1alpha1.KonnectEntityAPIAuthConfigurationResolvedRefReasonRefNotFound,
				fmt.Sprintf("Referenced KonnectAPIAuthConfiguration %s not found", client.ObjectKeyFromObject(&apiAuth)),
			); err != nil || !res.IsZero() {
				return true, ctrl.Result{}, err
			}

			return true, ctrl.Result{}, nil
		}

		if res, err := patch.StatusWithCondition(
			ctx, cl, ent,
			konnectv1alpha1.KonnectEntityAPIAuthConfigurationResolvedRefConditionType,
			metav1.ConditionFalse,
			konnectv1alpha1.KonnectEntityAPIAuthConfigurationResolvedRefReasonRefInvalid,
			fmt.Sprintf("KonnectAPIAuthConfiguration reference %s is invalid: %v", client.ObjectKeyFromObject(&apiAuth), err),
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
		if res, err := patch.StatusWithCondition(
			ctx, cl, ent,
			konnectv1alpha1.KonnectEntityAPIAuthConfigurationResolvedRefConditionType,
			metav1.ConditionTrue,
			konnectv1alpha1.KonnectEntityAPIAuthConfigurationResolvedRefReasonResolvedRef,
			fmt.Sprintf("KonnectAPIAuthConfiguration reference %s is resolved", client.ObjectKeyFromObject(&apiAuth)),
		); err != nil || !res.IsZero() {
			return true, res, err
		}
		return true, ctrl.Result{}, nil
	}

	// Check if the referenced APIAuthConfiguration is valid.
	if cond, present := k8sutils.GetCondition(konnectv1alpha1.KonnectEntityAPIAuthConfigurationValidConditionType, &apiAuth); !present ||
		cond.Status != metav1.ConditionTrue ||
		cond.Reason != konnectv1alpha1.KonnectEntityAPIAuthConfigurationReasonValid {

		// If it's invalid then set the "APIAuthValid" status condition on
		// the entity to False with "Invalid" reason.
		if res, err := patch.StatusWithCondition(
			ctx, cl, ent,
			konnectv1alpha1.KonnectEntityAPIAuthConfigurationValidConditionType,
			metav1.ConditionFalse,
			konnectv1alpha1.KonnectEntityAPIAuthConfigurationReasonInvalid,
			conditionMessageReferenceKonnectAPIAuthConfigurationInvalid(client.ObjectKeyFromObject(&apiAuth)),
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
		cond.Message != conditionMessageReferenceKonnectAPIAuthConfigurationValid(client.ObjectKeyFromObject(&apiAuth)) {

		if res, err := patch.StatusWithCondition(
			ctx, cl, ent,
			konnectv1alpha1.KonnectEntityAPIAuthConfigurationValidConditionType,
			metav1.ConditionTrue,
			konnectv1alpha1.KonnectEntityAPIAuthConfigurationReasonValid,
			conditionMessageReferenceKonnectAPIAuthConfigurationValid(client.ObjectKeyFromObject(&apiAuth)),
		); err != nil || !res.IsZero() {
			return true, res, err
		}
		return true, ctrl.Result{}, nil
	}

	return false, ctrl.Result{}, nil
}
