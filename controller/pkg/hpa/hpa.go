package hpa

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	"github.com/kong/kong-operator/v2/controller/pkg/patch"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	k8sreduce "github.com/kong/kong-operator/v2/pkg/utils/kubernetes/reduce"
	k8sresources "github.com/kong/kong-operator/v2/pkg/utils/kubernetes/resources"
)

// Owner is an object that owns a Deployment which can be horizontally scaled
// by a HorizontalPodAutoscaler.
type Owner interface {
	client.Object
	// GetHorizontalScaling returns the horizontal scaling options, or nil when
	// horizontal scaling is not configured.
	GetHorizontalScaling() *commonv1alpha1.HorizontalScaling
}

// Ensure reconciles the HorizontalPodAutoscaler owned by owner that targets
// the Deployment named deploymentName. If owner has no horizontal scaling
// configured, any existing HPAs owned by owner are deleted.
func Ensure(
	ctx context.Context,
	cl client.Client,
	logger logr.Logger,
	owner Owner,
	deploymentName string,
) (op.Result, error) {
	scaling := owner.GetHorizontalScaling()
	hpas, err := k8sutils.ListHPAsForOwner(
		ctx, cl, owner.GetNamespace(), owner.GetUID(), k8sresources.GetManagedLabelForOwner(owner),
	)
	if err != nil {
		return op.Noop, fmt.Errorf("failed listing HPAs for %s %s/%s: %w", ownerKind(cl, owner), owner.GetNamespace(), owner.GetName(), err)
	}

	if scaling == nil {
		if err := k8sreduce.ReduceHPAs(ctx, cl, hpas, k8sreduce.FilterNone); err != nil {
			return op.Noop, fmt.Errorf("failed reducing HPAs for %s %s/%s: %w", ownerKind(cl, owner), owner.GetNamespace(), owner.GetName(), err)
		}
		return op.Noop, nil
	}

	if len(hpas) > 1 {
		if err := k8sreduce.ReduceHPAs(ctx, cl, hpas, k8sreduce.FilterHPAs); err != nil {
			return op.Noop, fmt.Errorf("failed reducing HPAs for %s %s/%s: %w", ownerKind(cl, owner), owner.GetNamespace(), owner.GetName(), err)
		}
		return op.Noop, nil
	}

	generatedHPA, err := k8sresources.GenerateHPA(owner, scaling, deploymentName)
	if err != nil {
		return op.Noop, err
	}

	if len(hpas) == 1 {
		existingHPA := &hpas[0]
		oldExistingHPA := existingHPA.DeepCopy()
		var updated bool
		updated, existingHPA.ObjectMeta = k8sutils.EnsureObjectMetaIsUpdated(existingHPA.ObjectMeta, generatedHPA.ObjectMeta)
		if !cmp.Equal(existingHPA.Spec, generatedHPA.Spec) {
			existingHPA.Spec = generatedHPA.Spec
			updated = true
		}
		res, _, err := patch.ApplyPatchIfNotEmpty(ctx, cl, logger, existingHPA, oldExistingHPA, updated)
		return res, err
	}

	if err := cl.Create(ctx, generatedHPA); err != nil {
		return op.Noop, fmt.Errorf("failed creating HPA for %s %s/%s: %w", ownerKind(cl, owner), owner.GetNamespace(), owner.GetName(), err)
	}
	return op.Created, nil
}

// ownerKind returns the kind of owner, used in error messages. It is resolved
// through the client's scheme because TypeMeta of typed objects is not
// guaranteed to be populated.
func ownerKind(cl client.Client, owner client.Object) string {
	gvk, err := cl.GroupVersionKindFor(owner)
	if err != nil {
		return fmt.Sprintf("%T", owner)
	}
	return gvk.Kind
}
