package hpa

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"github.com/samber/lo"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/structured-merge-diff/v6/fieldpath"

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
	// GetReplicas returns the static replica count, or nil when it is not set.
	// It is ignored when horizontal scaling is configured.
	GetReplicas() *int32
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

// DeploymentReplicas returns the spec.replicas to apply to a Deployment owned
// by owner. It can be used to determine initial replica count for the Deployment,
// when existing is nil.
func DeploymentReplicas(owner Owner, existing *appsv1.Deployment) *int32 {
	scaling := owner.GetHorizontalScaling()
	if scaling == nil {
		return new(lo.FromPtrOr(owner.GetReplicas(), 1))
	}
	minReplicas := lo.FromPtrOr(scaling.MinReplicas, 1)
	if existing == nil {
		return new(minReplicas)
	}
	return new(max(lo.FromPtr(existing.Spec.Replicas), minReplicas))
}

// DesiredDeploymentReplicas returns the spec.replicas to apply to the
// Deployment owned by owner and identified by key. When owner is horizontally
// scaled, the Deployment is fetched from cluster and actual value is used.
// Once spec.replicas has been set through the scale subresource (i.e. by the
// HPA), nil is returned: spec.replicas must then be left out of the applied
// configuration, so that the HPA scale is never reverted. The scale took
// ownership of the field away from other field managers, so leaving it out
// does not remove it. The exception is a Deployment scaled to 0 (e.g. with
// kubectl scale): the HPA does not scale a target at 0 replicas, so it is
// scaled back up, see DeploymentReplicas.
func DesiredDeploymentReplicas(
	ctx context.Context,
	cl client.Reader,
	owner Owner,
	key client.ObjectKey,
) (*int32, error) {
	if owner.GetHorizontalScaling() == nil {
		return DeploymentReplicas(owner, nil), nil
	}

	var deployment appsv1.Deployment
	if err := cl.Get(ctx, key, &deployment); err != nil {
		if apierrors.IsNotFound(err) {
			return DeploymentReplicas(owner, nil), nil
		}
		return nil, fmt.Errorf("failed getting Deployment %s: %w", key, err)
	}

	scaled, err := replicasSetThroughScaleSubresource(&deployment)
	if err != nil {
		return nil, fmt.Errorf("failed checking managed fields of Deployment %s: %w", key, err)
	}
	if scaled && lo.FromPtr(deployment.Spec.Replicas) != 0 {
		return nil, nil
	}
	return DeploymentReplicas(owner, &deployment), nil
}

// replicasSetThroughScaleSubresource reports whether spec.replicas of
// deployment is owned by a field manager which set it through the scale
// subresource, as the HPA controller does. The field manager is not matched
// by name, as it differs between the HPA controller (kube-controller-manager)
// and other scalers.
func replicasSetThroughScaleSubresource(deployment *appsv1.Deployment) (bool, error) {
	for _, entry := range deployment.GetManagedFields() {
		if entry.Operation != metav1.ManagedFieldsOperationUpdate || entry.Subresource != "scale" || entry.FieldsV1 == nil {
			continue
		}
		var owned fieldpath.Set
		if err := owned.FromJSON(entry.FieldsV1.GetRawReader()); err != nil {
			return false, fmt.Errorf("failed decoding managed fields of field manager %q: %w", entry.Manager, err)
		}
		if owned.Has(replicasPath) {
			return true, nil
		}
	}
	return false, nil
}

var replicasPath = fieldpath.MakePathOrDie("spec", "replicas")
