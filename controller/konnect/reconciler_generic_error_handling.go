package konnect

import (
	"context"
	"net/url"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/consts"
	"github.com/kong/kong-operator/v2/controller/pkg/patch"
)

// handleOpsErr handles network errors.
// If the error is a network error, it patches the status condition
// of the Konnect entity to reflect the failure to communicate with the Konnect API.
func (r *KonnectEntityReconciler[T, TEnt]) handleOpsErr(
	ctx context.Context, ent TEnt, errURL *url.Error,
) (ctrl.Result, error) {
	if errURL == nil {
		return ctrl.Result{}, nil
	}

	if res, err := patch.StatusWithCondition(ctx, r.Client, ent,
		konnectv1alpha1.KonnectEntityProgrammedConditionType,
		metav1.ConditionFalse,
		konnectv1alpha1.KonnectEntityProgrammedReasonKonnectAPIOpFailed,
		errURL.Error(),
	); err != nil || !res.IsZero() {
		return res, err
	}

	// After patching the condition, requeue without backoff to retry the network operation.
	return ctrl.Result{RequeueAfter: consts.RequeueWithoutBackoff}, nil
}
