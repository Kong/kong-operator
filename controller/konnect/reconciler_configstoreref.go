package konnect

import (
	"context"
	"errors"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kcfgconsts "github.com/kong/kong-operator/v2/api/common/consts"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/konnect/constraints"
	"github.com/kong/kong-operator/v2/controller/pkg/patch"
	"github.com/kong/kong-operator/v2/internal/utils/crossnamespace"
)

// configStoreRefResolver is implemented by entities that can reference a
// KonnectConfigStore in their spec.
type configStoreRefResolver interface {
	ResolveConfigStoreID(ctx context.Context, cl client.Client) (string, error)
}

// handleConfigStoreRef resolves the KonnectConfigStore reference of the given entity,
// if it has one, and reflects the outcome in the ConfigStoreRefValid condition.
//
// It reports stop=true when the reference could not be resolved, so that the caller
// stops reconciliation before any Konnect SDK call: pushing the entity without the
// resolved Config Store ID would configure a broken vault in Konnect.
func handleConfigStoreRef[T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T]](
	ctx context.Context,
	cl client.Client,
	ent TEnt,
) (_ ctrl.Result, stop bool, resolvedID string, _ error) {
	resolver, ok := any(ent).(configStoreRefResolver)
	if !ok {
		return ctrl.Result{}, false, "", nil
	}
	deleting := !ent.GetDeletionTimestamp().IsZero()
	nn, hasRef := getConfigStoreRef(ent)
	if !hasRef {
		if deleting {
			return ctrl.Result{}, false, "", nil
		}
		res, err := patch.StatusWithoutCondition(
			ctx, cl, ent,
			konnectv1alpha1.ConfigStoreRefValidConditionType,
		)
		return res, err != nil || !res.IsZero(), "", err
	}

	// While the entity is being deleted its Konnect counterpart has to be removed,
	// which needs no Config Store ID. Blocking on an unresolvable reference here
	// would leave the entity stuck with its finalizer.

	// The referenced namespace has to consent to the reference. This is skipped
	// while the entity is being deleted for the same reason as above: requiring
	// the KongReferenceGrant to outlive the referring entity would make the grant
	// impossible to remove without first getting the entity stuck.
	if !deleting && nn.Namespace != ent.GetNamespace() {
		res, granted, err := ensureConfigStoreRefGrant(ctx, cl, ent, nn)
		if err != nil || !res.IsZero() || !granted {
			return res, true, "", err
		}
	}

	resolvedID, err := resolver.ResolveConfigStoreID(ctx, cl)
	if err == nil {
		if !deleting {
			if res, errStatus := patch.StatusWithCondition(
				ctx, cl, ent,
				konnectv1alpha1.ConfigStoreRefValidConditionType,
				metav1.ConditionTrue,
				konnectv1alpha1.ConfigStoreRefReasonValid,
				fmt.Sprintf("Referenced KonnectConfigStore %s is programmed", nn),
			); errStatus != nil || !res.IsZero() {
				return res, true, resolvedID, errStatus
			}
		}
		return ctrl.Result{}, false, resolvedID, nil
	}
	if deleting {
		return ctrl.Result{}, false, "", nil
	}

	reason, message, expected := configStoreRefFailure(err, nn)
	if !expected {
		// Reference errors are expected resource state and should be reflected in
		// the condition. Other errors, such as Kubernetes API failures, must be
		// returned so controller-runtime retries them instead of reporting a
		// misleading invalid-reference condition.
		return ctrl.Result{}, true, "", err
	}

	if res, errStatus := patch.StatusWithCondition(
		ctx, cl, ent,
		konnectv1alpha1.ConfigStoreRefValidConditionType,
		metav1.ConditionFalse,
		reason,
		message,
	); errStatus != nil || !res.IsZero() {
		return res, true, "", errStatus
	}

	// Don't requeue: a change to the referenced KonnectConfigStore triggers the
	// reconciliation, and an invalid reference needs a spec change to be fixed.
	return ctrl.Result{}, true, "", nil
}

// ensureConfigStoreRefGrant reports whether a KongReferenceGrant in the referenced
// namespace allows ent to reference the KonnectConfigStore nn, and records the
// denial in the ConfigStoreRefValid condition when it does not.
//
// KongVault is cluster-scoped, so its namespace is empty and the matching grant
// has to list `namespace: ""` in its `from` entry.
//
// The denial is reported on ConfigStoreRefValid rather than on the shared
// ResolvedRefs condition, which the ControlPlane ref handler already owns for this
// entity: both references of a cluster-scoped entity always cross a namespace
// boundary, so the two handlers would otherwise overwrite each other's verdict on
// every reconciliation. This mirrors what the KongPlugin ref handler does.
func ensureConfigStoreRefGrant[T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T]](
	ctx context.Context,
	cl client.Client,
	ent TEnt,
	nn types.NamespacedName,
) (_ ctrl.Result, granted bool, _ error) {
	err := crossnamespace.CheckKongReferenceGrantForResource(
		ctx,
		cl,
		ent.GetNamespace(),
		nn.Namespace,
		nn.Name,
		metav1.GroupVersionKind(ent.GetObjectKind().GroupVersionKind()),
		metav1.GroupVersionKind(konnectv1alpha1.GroupVersion.WithKind(configurationv1alpha1.KonnectConfigStoreKind)),
	)
	if err == nil {
		return ctrl.Result{}, true, nil
	}
	if !crossnamespace.IsReferenceNotGranted(err) {
		return ctrl.Result{}, false, err
	}

	// Don't requeue: the KongVault controller watches KongReferenceGrants, so
	// creating the missing grant triggers a new reconciliation on its own.
	res, errStatus := patch.StatusWithCondition(
		ctx, cl, ent,
		konnectv1alpha1.ConfigStoreRefValidConditionType,
		metav1.ConditionFalse,
		konnectv1alpha1.ConfigStoreRefReasonRefNotPermitted,
		fmt.Sprintf("KongReferenceGrants do not allow access to %s %s",
			configurationv1alpha1.KonnectConfigStoreKind, nn),
	)
	return res, false, errStatus
}

// configStoreRefFailure maps a reference resolution error to the reason and message
// of the ConfigStoreRefValid condition. It reports expected=false for errors that are
// not about the reference itself (e.g. a failing API call), which must be returned to
// the caller and retried with backoff instead of being recorded as a condition.
func configStoreRefFailure(err error, nn types.NamespacedName) (_ kcfgconsts.ConditionReason, message string, expected bool) {
	if _, ok := errors.AsType[configurationv1alpha1.ConfigStoreRefInvalidError](err); ok {
		return konnectv1alpha1.ConfigStoreRefReasonInvalid, err.Error(), true
	}
	if _, ok := errors.AsType[konnectv1alpha1.ReferenceNotFoundError](err); ok {
		return konnectv1alpha1.ConfigStoreRefReasonInvalid,
			fmt.Sprintf("Referenced KonnectConfigStore %s does not exist", nn), true
	}
	if _, ok := errors.AsType[konnectv1alpha1.ReferenceNotProgrammedError](err); ok {
		return konnectv1alpha1.ConfigStoreRefReasonNotProgrammed,
			fmt.Sprintf("Referenced KonnectConfigStore %s is not programmed yet", nn), true
	}
	return "", "", false
}

// getConfigStoreRef returns the namespaced name of the KonnectConfigStore referenced
// by the given entity, and whether it references one at all.
func getConfigStoreRef[T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T]](
	e TEnt,
) (types.NamespacedName, bool) {
	switch e := any(e).(type) {
	case *configurationv1alpha1.KongVault:
		return e.GetConfigStoreRefNamespacedName()
	default:
		return types.NamespacedName{}, false
	}
}
