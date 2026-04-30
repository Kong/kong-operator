package konnect

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/v2/api/common/consts"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	ctrlconsts "github.com/kong/kong-operator/v2/controller/consts"
	"github.com/kong/kong-operator/v2/controller/pkg/patch"
	"github.com/kong/kong-operator/v2/internal/utils/crossnamespace"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

func ensureKongReferenceGrantForParentRef[
	T interface {
		client.Object
		k8sutils.ConditionsAware
	},
](
	ctx context.Context,
	cl client.Client,
	ent T,
	ref commonv1alpha1.ObjectRef,
	parentTypeName string,
) (ctrl.Result, error) {
	if ref.Type != commonv1alpha1.ObjectRefTypeNamespacedRef ||
		ref.NamespacedRef == nil ||
		ref.NamespacedRef.Namespace == nil ||
		*ref.NamespacedRef.Namespace == ent.GetNamespace() {
		if res, errStatus := patch.StatusWithoutCondition(
			ctx, cl, ent,
			configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs,
		); errStatus != nil || !res.IsZero() {
			return res, errStatus
		}
		return ctrl.Result{}, nil
	}

	targetNamespace := *ref.NamespacedRef.Namespace
	err := crossnamespace.CheckKongReferenceGrantForResource(
		ctx,
		cl,
		ent.GetNamespace(),
		targetNamespace,
		ref.NamespacedRef.Name,
		metav1.GroupVersionKind(ent.GetObjectKind().GroupVersionKind()),
		metav1.GroupVersionKind(konnectv1alpha1.GroupVersion.WithKind(parentTypeName)),
	)

	if crossnamespace.IsReferenceNotGranted(err) {
		if res, errStatus := patch.StatusWithCondition(
			ctx, cl, ent,
			consts.ConditionType(configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs),
			metav1.ConditionFalse,
			configurationv1alpha1.KongReferenceGrantReasonRefNotPermitted,
			fmt.Sprintf(
				"KongReferenceGrants do not allow access to %s %s/%s",
				parentTypeName,
				targetNamespace,
				ref.NamespacedRef.Name,
			),
		); errStatus != nil || !res.IsZero() {
			return res, errStatus
		}
		return ctrl.Result{RequeueAfter: ctrlconsts.RequeueWithoutBackoff}, nil
	}
	if err != nil {
		return ctrl.Result{}, err
	}

	if res, errStatus := patch.StatusWithCondition(
		ctx, cl, ent,
		consts.ConditionType(configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs),
		metav1.ConditionTrue,
		configurationv1alpha1.KongReferenceGrantReasonResolvedRefs,
		fmt.Sprintf(
			"KongReferenceGrants allow access to %s %s/%s",
			parentTypeName,
			targetNamespace,
			ref.NamespacedRef.Name,
		),
	); errStatus != nil || !res.IsZero() {
		return res, errStatus
	}

	return ctrl.Result{}, nil
}
