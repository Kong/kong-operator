package kubernetes

import (
	appsv1 "k8s.io/api/apps/v1"
)

// DeploymentRolloutComplete reports whether deployment's current generation
// has been fully rolled out: the controller has observed the latest spec,
// and all desired replicas have been updated and are available. It mirrors
// the check `kubectl rollout status` performs, except a deployment scaled to
// zero replicas is never considered complete here (unlike kubectl, which
// treats that as a trivially successful rollout). Callers use this to mean
// "healthy and serving", which zero replicas never is.
func DeploymentRolloutComplete(deployment *appsv1.Deployment) bool {
	replicas := int32(1)
	if deployment.Spec.Replicas != nil {
		replicas = *deployment.Spec.Replicas
	}
	if replicas == 0 {
		return false
	}

	status := deployment.Status
	return status.ObservedGeneration >= deployment.Generation &&
		status.UpdatedReplicas == replicas &&
		status.Replicas == replicas &&
		status.AvailableReplicas == replicas
}
