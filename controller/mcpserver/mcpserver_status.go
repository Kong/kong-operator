package mcpserver

import (
	"context"
	"fmt"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	sdkops "github.com/kong/kong-operator/v2/controller/konnect/ops/sdk"
)

// buildVersionStatuses constructs one MCPServerVersionStatus per version found
// across the Deployment's ReplicaSets. During a rolling update there may be
// pods from multiple versions running simultaneously.
func buildVersionStatuses(
	ctx context.Context,
	cl client.Client,
	deployment *appsv1.Deployment,
) ([]sdkkonnectcomp.MCPServerVersionStatus, error) {
	// List ReplicaSets owned by this Deployment.
	var rsList appsv1.ReplicaSetList
	if err := cl.List(ctx, &rsList,
		client.InNamespace(deployment.Namespace),
		client.MatchingLabels(deployment.Spec.Selector.MatchLabels),
	); err != nil {
		return nil, fmt.Errorf("failed to list ReplicaSets for Deployment %s/%s: %w",
			deployment.Namespace, deployment.Name, err)
	}

	deploymentUID := deployment.UID
	var statuses []sdkkonnectcomp.MCPServerVersionStatus
	for i := range rsList.Items {
		rs := &rsList.Items[i]

		// Only consider ReplicaSets owned by this Deployment.
		if !isOwnedBy(rs.OwnerReferences, deploymentUID) {
			continue
		}

		// List pods belonging to this ReplicaSet.
		selector, err := metav1.LabelSelectorAsSelector(rs.Spec.Selector)
		if err != nil {
			return nil, fmt.Errorf("failed to parse ReplicaSet %s/%s selector: %w",
				rs.Namespace, rs.Name, err)
		}
		var podList corev1.PodList
		if err := cl.List(ctx, &podList,
			client.InNamespace(rs.Namespace),
			client.MatchingLabelsSelector{Selector: selector},
		); err != nil {
			return nil, fmt.Errorf("failed to list pods for ReplicaSet %s/%s: %w",
				rs.Namespace, rs.Name, err)
		}

		// Skip ReplicaSets that are scaled to zero and have no pods left
		// (including terminating ones). This ensures an old version stays
		// in the status array until all its pods are fully gone.
		if replicasOrDefault(rs.Spec.Replicas) == 0 && len(podList.Items) == 0 {
			continue
		}

		newStatus := sdkkonnectcomp.MCPServerVersionStatus{
			Version:         rs.Spec.Template.Annotations[mcpServerVersionAnnotationKey],
			DesiredReplicas: int64(replicasOrDefault(rs.Spec.Replicas)),
			CreatedReplicas: int64(rs.Status.Replicas),
			PodsStatus:      classifyPods(podList.Items),
		}
		if newStatus.CreatedReplicas > 0 || newStatus.DesiredReplicas > 0 {
			statuses = append(statuses, newStatus)
		}
	}

	return statuses, nil
}

// isOwnedBy returns true if the owner references contain the given UID.
func isOwnedBy(refs []metav1.OwnerReference, uid types.UID) bool {
	for i := range refs {
		if refs[i].UID == uid {
			return true
		}
	}
	return false
}

// replicasOrDefault returns *replicas or 1 (the Kubernetes default) if nil.
func replicasOrDefault(replicas *int32) int32 {
	if replicas != nil {
		return *replicas
	}
	return 1
}

// classifyPods classifies pods into ready, starting, and failing buckets.
func classifyPods(pods []corev1.Pod) sdkkonnectcomp.MCPServerPodsStatus {
	var ready, starting, failing int64
	for i := range pods {
		switch classifyPod(&pods[i]) {
		case podReady:
			ready++
		case podFailing:
			failing++
		case podTerminating:
			// Excluded from counts — pod is being deleted.
		default:
			starting++
		}
	}
	return sdkkonnectcomp.MCPServerPodsStatus{
		Ready:    ready,
		Starting: starting,
		Failing:  failing,
	}
}

type podClass int

const (
	podStarting podClass = iota
	podReady
	podFailing
	podTerminating
)

// classifyPod determines whether a single pod is ready, starting, or failing.
// Pods with a deletion timestamp are classified as terminating and excluded
// from the counts.
func classifyPod(pod *corev1.Pod) podClass {
	if pod.DeletionTimestamp != nil {
		return podTerminating
	}

	switch pod.Status.Phase {
	case corev1.PodFailed:
		return podFailing
	case corev1.PodSucceeded:
		// Completed pods are not useful for a long-running server; treat as
		// failing so the count signals something unexpected.
		return podFailing
	case corev1.PodPending, corev1.PodRunning, corev1.PodUnknown:
		// Fall through to container-level inspection below.
	}

	// Check for containers stuck in a failing waiting state.
	for i := range pod.Status.ContainerStatuses {
		cs := &pod.Status.ContainerStatuses[i]
		if cs.State.Waiting != nil && isFailingWaitReason(cs.State.Waiting.Reason) {
			return podFailing
		}
	}
	for i := range pod.Status.InitContainerStatuses {
		cs := &pod.Status.InitContainerStatuses[i]
		if cs.State.Waiting != nil && isFailingWaitReason(cs.State.Waiting.Reason) {
			return podFailing
		}
	}

	// A pod is ready if all its containers are ready.
	if isPodReady(pod) {
		return podReady
	}

	return podStarting
}

// isFailingWaitReason returns true for container waiting reasons that indicate
// a persistent failure rather than normal startup.
func isFailingWaitReason(reason string) bool {
	switch reason {
	case "CrashLoopBackOff",
		"ImagePullBackOff",
		"ErrImagePull",
		"CreateContainerConfigError",
		"InvalidImageName",
		"CreateContainerError":
		return true
	}
	return false
}

// isPodReady returns true when the pod has the Ready condition set to True.
func isPodReady(pod *corev1.Pod) bool {
	for i := range pod.Status.Conditions {
		if pod.Status.Conditions[i].Type == corev1.PodReady {
			return pod.Status.Conditions[i].Status == corev1.ConditionTrue
		}
	}
	return false
}

// postStatusToKonnect pushes the MCPServer version statuses to Konnect.
func postStatusToKonnect(
	ctx context.Context,
	sdk sdkops.SDKWrapper,
	cpID, mcpServerID string,
	versionStatuses []sdkkonnectcomp.MCPServerVersionStatus,
) error {
	resp, err := sdk.GetMCPServersSDK().PostMcpServerStatus(ctx, sdkkonnectops.PostMcpServerStatusRequest{
		ControlPlaneID: cpID,
		McpServerID:    mcpServerID,
		RequestBody:    versionStatuses,
	})
	if err != nil {
		return fmt.Errorf("failed to post MCPServer status to Konnect: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected status code %d when posting MCPServer status to Konnect", resp.StatusCode)
	}
	return nil
}
