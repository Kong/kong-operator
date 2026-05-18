package konnect

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/v2/api/common/consts"
	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/pkg/patch"
	"github.com/kong/kong-operator/v2/internal/utils/crossnamespace"
)

// handlePluginRef validates the pluginRef of a KongPluginBinding before Konnect SDK operations.
// It checks that the referenced KongPlugin exists, and for cross-namespace refs it also verifies
// that a KongReferenceGrant permits the reference. If ent is not a KongPluginBinding, it returns
// immediately with no action.
//
// Running this check early in the reconcile loop (before Konnect SDK operations) ensures that
// plugin and grant changes are reflected immediately via the watch-triggered enqueue rather than
// waiting for the sync period.
//
// Returns:
//   - ctrl.Result: non-zero if a requeue is needed
//   - bool: true if reconciliation should stop (ref is invalid)
//   - error: any error encountered
func handlePluginRef(
	ctx context.Context,
	cl client.Client,
	ent client.Object,
) (ctrl.Result, bool, error) {
	pluginBinding, ok := ent.(*configurationv1alpha1.KongPluginBinding)
	if !ok {
		return ctrl.Result{}, false, nil
	}

	pluginRef := pluginBinding.Spec.PluginReference
	pluginNS := pluginRef.Namespace
	if pluginNS == "" {
		pluginNS = pluginBinding.GetNamespace()
	}

	deleting := !pluginBinding.GetDeletionTimestamp().IsZero()

	// For cross-namespace references, verify a KongReferenceGrant permits access.
	if pluginNS != pluginBinding.GetNamespace() {
		err := crossnamespace.CheckKongReferenceGrantForResource(
			ctx,
			cl,
			pluginBinding.GetNamespace(),
			pluginNS,
			pluginRef.Name,
			metav1.GroupVersionKind(pluginBinding.GetObjectKind().GroupVersionKind()),
			metav1.GroupVersionKind(configurationv1.SchemeGroupVersion.WithKind("KongPlugin")),
		)
		if err != nil {
			if deleting {
				return ctrl.Result{}, false, nil
			}
			if crossnamespace.IsReferenceNotGranted(err) {
				if res, errStatus := patch.StatusWithCondition(
					ctx, cl, pluginBinding,
					consts.ConditionType(konnectv1alpha1.KongPluginRefValidConditionType),
					metav1.ConditionFalse,
					consts.ConditionReason(konnectv1alpha1.KongPluginRefReasonRefNotPermitted),
					fmt.Sprintf("KongReferenceGrants do not allow access to KongPlugin %s/%s", pluginNS, pluginRef.Name),
				); errStatus != nil || !res.IsZero() {
					return res, true, errStatus
				}
				return ctrl.Result{}, true, nil
			}
			return ctrl.Result{}, true, err
		}
	}

	// Verify the referenced KongPlugin actually exists.
	var plugin configurationv1.KongPlugin
	if err := cl.Get(ctx, client.ObjectKey{Name: pluginRef.Name, Namespace: pluginNS}, &plugin); err != nil {
		if deleting {
			return ctrl.Result{}, false, nil
		}
		msg := fmt.Sprintf("KongPlugin %s/%s not found", pluginNS, pluginRef.Name)
		if !apierrors.IsNotFound(err) {
			msg = fmt.Sprintf("failed to get KongPlugin %s/%s: %s", pluginNS, pluginRef.Name, err)
		}
		if res, errStatus := patch.StatusWithCondition(
			ctx, cl, pluginBinding,
			consts.ConditionType(konnectv1alpha1.KongPluginRefValidConditionType),
			metav1.ConditionFalse,
			consts.ConditionReason(konnectv1alpha1.KongPluginRefReasonInvalid),
			msg,
		); errStatus != nil || !res.IsZero() {
			return res, true, errStatus
		}
		return ctrl.Result{}, true, nil
	}

	// Plugin exists (and grant is valid for cross-namespace refs).
	if res, errStatus := patch.StatusWithCondition(
		ctx, cl, pluginBinding,
		consts.ConditionType(konnectv1alpha1.KongPluginRefValidConditionType),
		metav1.ConditionTrue,
		consts.ConditionReason(konnectv1alpha1.KongPluginRefReasonValid),
		fmt.Sprintf("KongPlugin %s/%s exists and is accessible", pluginNS, pluginRef.Name),
	); errStatus != nil || !res.IsZero() {
		return res, true, errStatus
	}

	return ctrl.Result{}, false, nil
}
