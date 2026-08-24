package kubernetes

import (
	appsv1 "k8s.io/api/apps/v1"
)

// DeploymentRolloutComplete reports whether deployment's current generation
// has been fully rolled out: the controller has observed the latest spec,
// and all desired replicas have been updated and are available. It mirrors
// the check `kubectl rollout status` performs.
func DeploymentRolloutComplete(deployment *appsv1.Deployment) bool {
	replicas := int32(1)
	if deployment.Spec.Replicas != nil {
		replicas = *deployment.Spec.Replicas
	}

	status := deployment.Status
	return status.ObservedGeneration >= deployment.Generation &&
		status.UpdatedReplicas == replicas &&
		status.Replicas == replicas &&
		status.AvailableReplicas == replicas
}
