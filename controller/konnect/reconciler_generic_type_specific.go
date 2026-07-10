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

	kcfgconsts "github.com/kong/kong-operator/v2/api/common/consts"
	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/controller/konnect/constraints"
	"github.com/kong/kong-operator/v2/controller/konnect/ops"
	sdkops "github.com/kong/kong-operator/v2/controller/konnect/ops/sdk"
	"github.com/kong/kong-operator/v2/controller/pkg/patch"
)

// handleTypeSpecific handles type-specific logic for Konnect entities.
// These include e.g.:
// - checking KongConsumer's credential secret refs.
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

	if resolver, ok := any(ent).(konnectReferenceResolver); ok {
		refUpdated, refProblem, err := handleKonnectReferences(ctx, cl, ent, resolver)
		if err != nil {
			return false, ctrl.Result{}, err
		}
		updated = updated || refUpdated
		isProblem = isProblem || refProblem
	}

	switch e := any(ent).(type) {
	case *configurationv1.KongConsumer:
		u, p := handleKongConsumerSpecific(ctx, cl, e)
		updated = updated || u
		isProblem = isProblem || p
	case *konnectv1alpha2.KonnectGatewayControlPlane:
		u, err := handleKonnectGatewayControlPlaneSpecific(ctx, sdk, e)
		if err != nil {
			return false, ctrl.Result{}, err
		}
		updated = updated || u
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

// konnectReferenceResolver is implemented by generated Konnect entity types
// that declare CR references on their spec (see crd-from-oas `references` config).
type konnectReferenceResolver interface {
	ResolveKonnectReferences(ctx context.Context, cl client.Client) error
}

// handleKonnectReferences resolves the CR references declared on ent's spec (if
// any) and reflects the outcome in the KonnectReferencesResolved condition.
// Reconciliation must stop (isProblem=true) before any SDK calls when
// references fail to resolve.
func handleKonnectReferences[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](
	ctx context.Context,
	cl client.Client,
	ent TEnt,
	resolver konnectReferenceResolver,
) (updated bool, isProblem bool, err error) {
	err = resolver.ResolveKonnectReferences(ctx, cl)
	if err == nil {
		updated = patch.SetStatusWithConditionIfDifferent(
			ent,
			kcfgconsts.ConditionType(konnectv1alpha1.KonnectReferencesResolvedConditionType),
			metav1.ConditionTrue,
			kcfgconsts.ConditionReason(konnectv1alpha1.KonnectReferencesResolvedReasonResolved),
			"",
		)
		return updated, false, nil
	}

	if !isExpectedKonnectReferenceResolutionError(err) {
		return false, false, err
	}

	reason := konnectReferenceResolutionReason(err)
	updated = patch.SetStatusWithConditionIfDifferent(
		ent,
		kcfgconsts.ConditionType(konnectv1alpha1.KonnectReferencesResolvedConditionType),
		metav1.ConditionFalse,
		kcfgconsts.ConditionReason(reason),
		err.Error(),
	)
	return updated, true, nil
}

func konnectReferenceResolutionReason(err error) string {
	if hasInvalidKonnectReferenceResolutionError(err) {
		return konnectv1alpha1.KonnectReferencesResolvedReasonInvalid
	}
	if _, ok := errors.AsType[konnectv1alpha1.ReferenceNotFoundError](err); ok {
		return konnectv1alpha1.KonnectReferencesResolvedReasonNotFound
	}
	return konnectv1alpha1.KonnectReferencesResolvedReasonNotProgrammed
}

func isExpectedKonnectReferenceResolutionError(err error) bool {
	if err == nil {
		return true
	}
	if joined, ok := errors.AsType[interface {
		error
		Unwrap() []error
	}](err); ok {
		for _, e := range joined.Unwrap() {
			if !isExpectedKonnectReferenceResolutionError(e) {
				return false
			}
		}
		return true
	}
	if _, ok := errors.AsType[konnectv1alpha1.ReferenceNotFoundError](err); ok {
		return true
	}
	if _, ok := errors.AsType[konnectv1alpha1.ReferenceNotProgrammedError](err); ok {
		return true
	}
	if _, ok := errors.AsType[konnectv1alpha1.ReferenceCrossNamespaceError](err); ok {
		return true
	}
	if _, ok := errors.AsType[konnectv1alpha1.ReferenceDifferentGatewayError](err); ok {
		return true
	}
	return false
}

func hasInvalidKonnectReferenceResolutionError(err error) bool {
	if _, ok := errors.AsType[konnectv1alpha1.ReferenceCrossNamespaceError](err); ok {
		return true
	}
	if _, ok := errors.AsType[konnectv1alpha1.ReferenceDifferentGatewayError](err); ok {
		return true
	}
	return false
}
