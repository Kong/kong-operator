package dataplane

import (
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"

	operatorv1beta1 "github.com/kong/gateway-operator/api/v1beta1"
)

// DataPlaneWatchBuilder creates a controller builder pre-configured with
// the necessary watches for DataPlane resources that are managed by
// the operator.
func DataPlaneWatchBuilder(mgr ctrl.Manager) *builder.Builder {
	return ctrl.NewControllerManagedBy(mgr).
		// Watch DataPlane objects.
		For(&operatorv1beta1.DataPlane{}).
		// Watch for changes in Secrets created by the dataplane controller.
		Owns(&corev1.Secret{}).
		// Watch for changes in Services created by the dataplane controller.
		Owns(&corev1.Service{}).
		// Watch for changes in Deployments created by the dataplane controller.
		Owns(&appsv1.Deployment{}).
		// Watch for changes in HPA created by the dataplane controller.
		Owns(&autoscalingv2.HorizontalPodAutoscaler{}).
		// Watch for changes in PodDisruptionBudgets created by the dataplane controller.
		Owns(&policyv1.PodDisruptionBudget{})
}
