package konnect

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kong/kong-operator/v2/api/common/consts"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/konnect/constraints"
	"github.com/kong/kong-operator/v2/controller/pkg/patch"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

type createInputsAware interface {
	GetCreateInputsHash() string
	SetCreateInputsHash(string)
	CalculateCreateInputsHash(context.Context, client.Client) (string, error)
}

func handleCreateInputs[T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T]](
	ctx context.Context,
	cl client.Client,
	ent TEnt,
) (ctrl.Result, error) {
	inputs, ok := any(ent).(createInputsAware)
	if !ok || !ent.GetDeletionTimestamp().IsZero() || ent.GetKonnectStatus().GetKonnectID() == "" {
		return ctrl.Result{}, nil
	}
	setCondition := func(status metav1.ConditionStatus, reason consts.ConditionReason, message string) (ctrl.Result, error) {
		return patch.StatusWithCondition(ctx, cl, ent,
			konnectv1alpha1.CreateInputsAppliedConditionType, status, reason, message)
	}
	if len(getSecretRefs(ent)) > 0 {
		if c, ok := k8sutils.GetCondition(konnectv1alpha1.SecretRefValidConditionType, ent); ok && c.Status != metav1.ConditionTrue {
			return setCondition(metav1.ConditionUnknown, konnectv1alpha1.CreateInputsReasonUnavailable,
				"Cannot compare create inputs while referenced Secrets are unavailable; the recorded fingerprint is unchanged")
		}
	}
	hash, err := inputs.CalculateCreateInputsHash(ctx, cl)
	if err != nil {
		if res, statusErr := setCondition(metav1.ConditionUnknown, konnectv1alpha1.CreateInputsReasonUnavailable,
			"Cannot resolve current create inputs; the recorded fingerprint is unchanged"); statusErr != nil || !res.IsZero() {
			return res, statusErr
		}
		return ctrl.Result{}, fmt.Errorf("failed to fingerprint %s create inputs: %w", ent.GetTypeName(), err)
	}
	if inputs.GetCreateInputsHash() == "" {
		old := ent.DeepCopyObject().(TEnt)
		inputs.SetCreateInputsHash(hash)
		k8sutils.SetCondition(k8sutils.NewConditionWithGeneration(
			konnectv1alpha1.CreateInputsAppliedConditionType,
			metav1.ConditionTrue,
			konnectv1alpha1.CreateInputsReasonBaselineEstablished,
			"Current inputs are the baseline for future comparisons; the values already in Konnect cannot be verified",
			ent.GetGeneration(),
		), ent)
		if _, err := patch.ApplyStatusPatchIfNotEmpty(ctx, cl, ctrllog.FromContext(ctx), ent, old); err != nil {
			if apierrors.IsConflict(err) {
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, fmt.Errorf("failed to persist create-input baseline: %w", err)
		}
		return ctrl.Result{}, nil
	}
	if hash != inputs.GetCreateInputsHash() {
		action := "Recreate the Kubernetes object to apply the changes"
		if _, ok := any(ent).(*configurationv1alpha1.EventGatewayStaticKey); ok {
			action = "Create a new EventGatewayStaticKey and update the policies to reference it"
		}
		return setCondition(metav1.ConditionFalse, konnectv1alpha1.CreateInputsReasonValueChanged,
			"Create inputs have changed, but this entity cannot be updated in Konnect. "+action)
	}
	return setCondition(metav1.ConditionTrue, konnectv1alpha1.CreateInputsReasonUnchanged,
		"Current inputs match the recorded create-input fingerprint")
}
