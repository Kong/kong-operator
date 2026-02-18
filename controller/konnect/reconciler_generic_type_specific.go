package konnect

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kcfgconsts "github.com/kong/kubernetes-configuration/v2/api/common/consts"
	configurationv1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1"
	konnectv1alpha2 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha2"

	"github.com/kong/kong-operator/v2/controller/konnect/constraints"
	"github.com/kong/kong-operator/v2/controller/konnect/ops"
	sdkops "github.com/kong/kong-operator/v2/controller/konnect/ops/sdk"
	"github.com/kong/kong-operator/v2/controller/pkg/patch"
)

// handleTypeSpecific handles type-specific logic for Konnect entities.
// These include e.g.:
// - checking KongConsumer's credential secret refs
func handleTypeSpecific[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](
	ctx context.Context,
	sdk sdkops.SDKWrapper,
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
	case *konnectv1alpha2.KonnectGatewayControlPlane:
		updated, err = handleKonnectGatewayControlPlaneSpecific(ctx, sdk, e)
		if err != nil {
			return false, ctrl.Result{}, err
		}
	default:
	}

	if updated {
		res, err = patchWithProgrammedStatusConditionBasedOnOtherConditions(ctx, cl, ent)
	}

	return isProblem, res, err
}

func handleKonnectGatewayControlPlaneSpecific(
	ctx context.Context,
	sdk sdkops.SDKWrapper,
	kgcp *konnectv1alpha2.KonnectGatewayControlPlane,
) (updated bool, err error) {
	// If it's not set it means that first time we are reconciling the KonnectGatewayControlPlane,
	// in subsequent reconciliations it should be set. Otherwise it will be reported and everything
	// in this helper is irrelevant.
	kgcpID := kgcp.GetKonnectID()
	if kgcpID == "" {
		return false, nil
	}
	konnectCP, err := ops.GetControlPlaneByID(ctx, sdk.GetControlPlaneSDK(), kgcpID)
	if err != nil {
		return false, fmt.Errorf("can't read KonnectGatewayControlPlane with ID: %s from Konnect API: %w", kgcpID, err)
	}
	kgcp.Status.Endpoints = &konnectv1alpha2.KonnectEndpoints{
		TelemetryEndpoint:    konnectCP.Config.TelemetryEndpoint,
		ControlPlaneEndpoint: konnectCP.Config.ControlPlaneEndpoint,
	}
	return true, nil
}

func handleKongConsumerSpecific(
	ctx context.Context,
	cl client.Client,
	c *configurationv1.KongConsumer,
) (updated bool, isProblem bool) {
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
		updated = patch.SetStatusWithConditionIfDifferent(
			c,
			configurationv1.ConditionKongConsumerCredentialSecretRefsValid,
			metav1.ConditionTrue,
			kcfgconsts.ConditionReason(configurationv1.ReasonKongConsumerCredentialSecretRefsValid),
			"",
		)

		return updated, false
	}

	updated = patch.SetStatusWithConditionIfDifferent(
		c,
		configurationv1.ConditionKongConsumerCredentialSecretRefsValid,
		metav1.ConditionFalse,
		kcfgconsts.ConditionReason(configurationv1.ReasonKongConsumerCredentialSecretRefInvalid),
		errors.Join(errs...).Error(),
	)

	return updated, true
}
