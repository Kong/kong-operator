package resources

import (
	"fmt"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	pkgapisautoscalingv2 "k8s.io/kubernetes/pkg/apis/autoscaling/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// HPAScalingSpec holds the autoscaling parameters for a HorizontalPodAutoscaler.
// It mirrors the fields shared across all operator HorizontalScaling API types.
type HPAScalingSpec struct {
	MinReplicas *int32
	MaxReplicas int32
	Metrics     []autoscalingv2.MetricSpec
	Behavior    *autoscalingv2.HorizontalPodAutoscalerBehavior
}

// GenerateHPA generates a HorizontalPodAutoscaler owned by owner that targets
// the Deployment named deploymentName. scaling must not be nil.
func GenerateHPA(owner client.Object, scaling *HPAScalingSpec, deploymentName string) (
	*autoscalingv2.HorizontalPodAutoscaler, error,
) {
	if scaling == nil {
		return nil, fmt.Errorf("cannot generate HPA for %s/%s: scaling spec is nil", owner.GetNamespace(), owner.GetName())
	}

	labels := GetManagedLabelForOwner(owner)
	labels["app"] = owner.GetName()

	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		Name:      owner.GetName(),
		Namespace: owner.GetNamespace(),
		Labels:    labels,
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       deploymentName,
			},
			MinReplicas: scaling.MinReplicas,
			MaxReplicas: scaling.MaxReplicas,
			Behavior:    scaling.Behavior,
			Metrics:     scaling.Metrics,
		},
	}

	k8sutils.SetOwnerForObject(hpa, owner)

	// Set defaults so we don't get a diff when comparing against the cluster object.
	pkgapisautoscalingv2.SetDefaults_HorizontalPodAutoscaler(hpa)

	return hpa, nil
}
