package konnect

import (
	"context"
	"errors"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/controller/konnect/constraints"
	"github.com/kong/gateway-operator/controller/pkg/patch"

	kcfgconsts "github.com/kong/kubernetes-configuration/api/common/consts"
	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
)

// handleTypeSpecific handles type-specific logic for Konnect entities.
// These include e.g.:
// - checking KongConsumer's credential secret refs
func handleTypeSpecific[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](
	ctx context.Context,
	cl client.Client,
	ent TEnt,
) (bool, ctrl.Result, error) {
	var (
		updated   bool
		isProblem bool
		res       ctrl.Result
		err       error
	)
	switch e := any(ent).(type) {
	case *configurationv1.KongConsumer:
		updated, isProblem = handleKongConsumerSpecific(ctx, cl, e)
	default:
	}

	if updated {
		res, err = patchWithProgrammedStatusConditionBasedOnOtherConditions(ctx, cl, ent)
	}

	return isProblem, res, err
}

func handleKongConsumerSpecific(
	ctx context.Context,
	cl client.Client,
	c *configurationv1.KongConsumer,
) (stop bool, isProblem bool) {
	// Check if the credential secret refs are valid.

	var errs []error
	for _, secretName := range c.Credentials {
		var (
			nn = types.NamespacedName{
				Namespace: c.Namespace,
				Name:      secretName,
			}
			secret corev1.Secret
		)
		if err := cl.Get(ctx, nn, &secret); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) == 0 {
		updated := patch.SetStatusWithConditionIfDifferent(
			c,
			configurationv1.ConditionKongConsumerCredentialSecretRefsValid,
			metav1.ConditionTrue,
			kcfgconsts.ConditionReason(configurationv1.ReasonKongConsumerCredentialSecretRefsValid),
			"",
		)

		return updated, false
	}

	updated := patch.SetStatusWithConditionIfDifferent(
		c,
		configurationv1.ConditionKongConsumerCredentialSecretRefsValid,
		metav1.ConditionFalse,
		kcfgconsts.ConditionReason(configurationv1.ReasonKongConsumerCredentialSecretRefInvalid),
		errors.Join(errs...).Error(),
	)

	return updated, true
}
