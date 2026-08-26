package resources

import (
	"fmt"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	pkgapisautoscalingv2 "k8s.io/kubernetes/pkg/apis/autoscaling/v2"

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// GenerateHPAForDataPlane generate an HPA for the given DataPlane.
// The provided deploymentName is the name of the Deployment that the HPA
// will target using its ScaleTargetRef.
func GenerateHPAForDataPlane(dataplane *operatorv1beta1.DataPlane, deploymentName string) (
	*autoscalingv2.HorizontalPodAutoscaler, error,
) {
	if scaling := dataplane.Spec.Deployment.Scaling; scaling == nil || scaling.HorizontalScaling == nil {
		return nil, fmt.Errorf("cannot generate HPA for DataPlane %s which doesn't have horizontal autoscaling turned on", dataplane.Name)
	}

	labels := GetManagedLabelForOwner(dataplane)
	labels["app"] = dataplane.Name

	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dataplane.Name,
			Namespace: dataplane.Namespace,
			Labels:    labels,
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       deploymentName,
			},
		},
	}
	scaling := dataplane.Spec.Deployment.Scaling
	hpa.Spec.MinReplicas = scaling.HorizontalScaling.MinReplicas
	hpa.Spec.MaxReplicas = scaling.HorizontalScaling.MaxReplicas
	hpa.Spec.Behavior = scaling.HorizontalScaling.Behavior
	hpa.Spec.Metrics = scaling.HorizontalScaling.Metrics

	k8sutils.SetOwnerForObject(hpa, dataplane)

	// Set defaults for the HPA so that we don't get a diff when we compare
	// it with what's in the cluster.
	pkgapisautoscalingv2.SetDefaults_HorizontalPodAutoscaler(hpa)

	return hpa, nil
}

// GenerateHPAForAIGatewayDataPlane generates an HPA for the given AIGatewayDataPlane.
// The provided deploymentName is the name of the Deployment that the HPA
// will target using its ScaleTargetRef.
func GenerateHPAForAIGatewayDataPlane(aigwdp *aigatewayv1alpha1.AIGatewayDataPlane, deploymentName string) (
	*autoscalingv2.HorizontalPodAutoscaler, error,
) {
	if aigwdp.Spec.Deployment == nil ||
		aigwdp.Spec.Deployment.Scaling == nil ||
		aigwdp.Spec.Deployment.Scaling.HorizontalScaling == nil {
		return nil, fmt.Errorf("cannot generate HPA for AIGatewayDataPlane %s which doesn't have horizontal autoscaling turned on", aigwdp.Name)
	}

	labels := GetManagedLabelForOwner(aigwdp)
	labels["app"] = aigwdp.Name

	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      aigwdp.Name,
			Namespace: aigwdp.Namespace,
			Labels:    labels,
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       deploymentName,
			},
		},
	}
	scaling := aigwdp.Spec.Deployment.Scaling.HorizontalScaling
	hpa.Spec.MinReplicas = scaling.MinReplicas
	hpa.Spec.MaxReplicas = scaling.MaxReplicas
	hpa.Spec.Behavior = scaling.Behavior
	hpa.Spec.Metrics = scaling.Metrics

	k8sutils.SetOwnerForObject(hpa, aigwdp)
	pkgapisautoscalingv2.SetDefaults_HorizontalPodAutoscaler(hpa)

	return hpa, nil
}
