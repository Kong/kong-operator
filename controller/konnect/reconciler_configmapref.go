package konnect

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/konnect/constraints"
	"github.com/kong/kong-operator/v2/controller/pkg/patch"
	mgrconfig "github.com/kong/kong-operator/v2/modules/manager/config"
)

// handleConfigMapRef verifies that every ConfigMap referenced by the entity's
// ConfigMap data sources exists and contains the referenced key, and reflects
// the result in the ConfigMapRefValid condition.
//
// A missing ConfigMap or key stops the reconciliation without an error: the
// ConfigMap watch (enqueueObjectsForConfigMapRef) retriggers it once the
// ConfigMap is created or fixed. An entity that no longer references any
// ConfigMap has the condition removed, so a stale False doesn't linger.
func handleConfigMapRef[T constraints.SupportedKonnectEntityType, TEnt constraints.EntityType[T]](
	ctx context.Context,
	cl client.Client,
	ent TEnt,
) (ctrl.Result, bool, error) {
	refs, ok := configMapRefsForDataSource(ent)
	if !ok || !ent.GetDeletionTimestamp().IsZero() {
		return ctrl.Result{}, false, nil
	}

	for _, ref := range refs {
		msg, err := validateConfigMapDataSourceRef(ctx, cl, ent.GetNamespace(), ref)
		if err != nil {
			return ctrl.Result{}, true, err
		}
		if msg == "" {
			continue
		}
		if res, errStatus := patch.StatusWithCondition(
			ctx, cl, ent,
			konnectv1alpha1.ConfigMapRefValidConditionType,
			metav1.ConditionFalse,
			konnectv1alpha1.ConfigMapRefReasonInvalid,
			msg,
		); errStatus != nil || !res.IsZero() {
			return res, true, errStatus
		}
		return ctrl.Result{}, true, nil
	}

	if len(refs) == 0 {
		res, err := patch.StatusWithoutCondition(ctx, cl, ent, konnectv1alpha1.ConfigMapRefValidConditionType)
		if err != nil || !res.IsZero() {
			return res, true, err
		}
		return ctrl.Result{}, false, nil
	}
	if res, errStatus := patch.StatusWithCondition(
		ctx, cl, ent,
		konnectv1alpha1.ConfigMapRefValidConditionType,
		metav1.ConditionTrue,
		konnectv1alpha1.ConfigMapRefReasonValid,
		"Referenced ConfigMap(s) exist and contain the required key(s)",
	); errStatus != nil || !res.IsZero() {
		return res, true, errStatus
	}
	return ctrl.Result{}, false, nil
}

// validateConfigMapDataSourceRef returns a non-empty message describing why
// ref is invalid, or an error when the ConfigMap can't be read for another
// reason than not existing.
func validateConfigMapDataSourceRef(
	ctx context.Context,
	cl client.Client,
	namespace string,
	ref configMapDataSourceRef,
) (string, error) {
	var configMap corev1.ConfigMap
	if err := cl.Get(ctx, client.ObjectKey{Namespace: namespace, Name: ref.Name}, &configMap); err != nil {
		if apierrors.IsNotFound(err) {
			// A ConfigMap excluded by --config-map-label-selector is absent from
			// the cache and reads as not found.
			return fmt.Sprintf(
				"ConfigMap %s/%s not found: if it exists, it is not matched by --config-map-label-selector (%s=true by default)",
				namespace, ref.Name, mgrconfig.DefaultConfigMapLabelSelector,
			), nil
		}
		return "", fmt.Errorf("failed to fetch ConfigMap %s/%s: %w", namespace, ref.Name, err)
	}
	if _, ok := configMap.Data[ref.Key]; ok {
		return "", nil
	}
	if _, ok := configMap.BinaryData[ref.Key]; ok {
		return "", nil
	}
	return fmt.Sprintf("ConfigMap %s/%s is missing key %q", namespace, ref.Name, ref.Key), nil
}
