package mcpserver

import (
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestClassifyPod(t *testing.T) {
	tests := []struct {
		name     string
		pod      *corev1.Pod
		expected podClass
	}{
		{
			name: "pod with Ready condition true is ready",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					Conditions: []corev1.PodCondition{
						{Type: corev1.PodReady, Status: corev1.ConditionTrue},
					},
				},
			},
			expected: podReady,
		},
		{
			name: "pod in Failed phase is failing",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodFailed,
				},
			},
			expected: podFailing,
		},
		{
			name: "pod in Succeeded phase is failing",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodSucceeded,
				},
			},
			expected: podFailing,
		},
		{
			name: "pod in Pending phase is starting",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodPending,
				},
			},
			expected: podStarting,
		},
		{
			name: "pod with CrashLoopBackOff container is failing",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					ContainerStatuses: []corev1.ContainerStatus{
						{
							State: corev1.ContainerState{
								Waiting: &corev1.ContainerStateWaiting{
									Reason: "CrashLoopBackOff",
								},
							},
						},
					},
				},
			},
			expected: podFailing,
		},
		{
			name: "pod with ImagePullBackOff container is failing",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodPending,
					ContainerStatuses: []corev1.ContainerStatus{
						{
							State: corev1.ContainerState{
								Waiting: &corev1.ContainerStateWaiting{
									Reason: "ImagePullBackOff",
								},
							},
						},
					},
				},
			},
			expected: podFailing,
		},
		{
			name: "pod with ErrImagePull container is failing",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodPending,
					ContainerStatuses: []corev1.ContainerStatus{
						{
							State: corev1.ContainerState{
								Waiting: &corev1.ContainerStateWaiting{
									Reason: "ErrImagePull",
								},
							},
						},
					},
				},
			},
			expected: podFailing,
		},
		{
			name: "pod with CreateContainerConfigError is failing",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodPending,
					ContainerStatuses: []corev1.ContainerStatus{
						{
							State: corev1.ContainerState{
								Waiting: &corev1.ContainerStateWaiting{
									Reason: "CreateContainerConfigError",
								},
							},
						},
					},
				},
			},
			expected: podFailing,
		},
		{
			name: "pod with failing init container is failing",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodPending,
					InitContainerStatuses: []corev1.ContainerStatus{
						{
							State: corev1.ContainerState{
								Waiting: &corev1.ContainerStateWaiting{
									Reason: "CrashLoopBackOff",
								},
							},
						},
					},
				},
			},
			expected: podFailing,
		},
		{
			name: "running pod with Ready=False is starting",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					Conditions: []corev1.PodCondition{
						{Type: corev1.PodReady, Status: corev1.ConditionFalse},
					},
				},
			},
			expected: podStarting,
		},
		{
			name: "running pod with no conditions is starting",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
				},
			},
			expected: podStarting,
		},
		{
			name: "pod with ContainerCreating reason is starting",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodPending,
					ContainerStatuses: []corev1.ContainerStatus{
						{
							State: corev1.ContainerState{
								Waiting: &corev1.ContainerStateWaiting{
									Reason: "ContainerCreating",
								},
							},
						},
					},
				},
			},
			expected: podStarting,
		},
		{
			name: "terminating pod is classified as terminating",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					DeletionTimestamp: &metav1.Time{},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					Conditions: []corev1.PodCondition{
						{Type: corev1.PodReady, Status: corev1.ConditionTrue},
					},
				},
			},
			expected: podTerminating,
		},
		{
			name: "terminating failed pod is classified as terminating",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					DeletionTimestamp: &metav1.Time{},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodFailed,
				},
			},
			expected: podTerminating,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := classifyPod(tt.pod)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestClassifyPods(t *testing.T) {
	tests := []struct {
		name     string
		pods     []corev1.Pod
		expected sdkkonnectcomp.MCPServerPodsStatus
	}{
		{
			name: "mixed pod states",
			pods: []corev1.Pod{
				{
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: corev1.ConditionTrue},
						},
					},
				},
				{
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: corev1.ConditionTrue},
						},
					},
				},
				{
					Status: corev1.PodStatus{
						Phase: corev1.PodPending,
					},
				},
				{
					Status: corev1.PodStatus{
						Phase: corev1.PodFailed,
					},
				},
			},
			expected: sdkkonnectcomp.MCPServerPodsStatus{
				Ready:    2,
				Starting: 1,
				Failing:  1,
			},
		},
		{
			name: "all ready",
			pods: []corev1.Pod{
				{
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: corev1.ConditionTrue},
						},
					},
				},
			},
			expected: sdkkonnectcomp.MCPServerPodsStatus{
				Ready:    1,
				Starting: 0,
				Failing:  0,
			},
		},
		{
			name:     "no pods",
			pods:     nil,
			expected: sdkkonnectcomp.MCPServerPodsStatus{},
		},
		{
			name: "terminating pods are excluded from counts",
			pods: []corev1.Pod{
				{
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: corev1.ConditionTrue},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						DeletionTimestamp: &metav1.Time{},
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: corev1.ConditionTrue},
						},
					},
				},
			},
			expected: sdkkonnectcomp.MCPServerPodsStatus{
				Ready:    1,
				Starting: 0,
				Failing:  0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := classifyPods(tt.pods)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsPodReady(t *testing.T) {
	tests := []struct {
		name     string
		pod      *corev1.Pod
		expected bool
	}{
		{
			name: "ready condition true",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{Type: corev1.PodReady, Status: corev1.ConditionTrue},
					},
				},
			},
			expected: true,
		},
		{
			name: "ready condition false",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{Type: corev1.PodReady, Status: corev1.ConditionFalse},
					},
				},
			},
			expected: false,
		},
		{
			name: "no ready condition",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{Type: corev1.PodScheduled, Status: corev1.ConditionTrue},
					},
				},
			},
			expected: false,
		},
		{
			name: "no conditions",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{},
			},
			expected: false,
		},
		{
			name: "ready among multiple conditions",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{Type: corev1.PodScheduled, Status: corev1.ConditionTrue},
						{Type: corev1.PodReady, Status: corev1.ConditionTrue},
						{Type: corev1.ContainersReady, Status: corev1.ConditionTrue},
					},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isPodReady(tt.pod)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsFailingWaitReason(t *testing.T) {
	failingReasons := []string{
		"CrashLoopBackOff",
		"ImagePullBackOff",
		"ErrImagePull",
		"CreateContainerConfigError",
		"InvalidImageName",
		"CreateContainerError",
	}
	for _, reason := range failingReasons {
		t.Run(reason+" is failing", func(t *testing.T) {
			assert.True(t, isFailingWaitReason(reason))
		})
	}

	nonFailingReasons := []string{
		"ContainerCreating",
		"PodInitializing",
		"",
		"Pending",
	}
	for _, reason := range nonFailingReasons {
		name := reason
		if name == "" {
			name = "empty"
		}
		t.Run(name+" is not failing", func(t *testing.T) {
			assert.False(t, isFailingWaitReason(reason))
		})
	}
}

func TestIsOwnedBy(t *testing.T) {
	uid := types.UID("abc-123")
	tests := []struct {
		name     string
		refs     []metav1.OwnerReference
		expected bool
	}{
		{
			name:     "matching UID",
			refs:     []metav1.OwnerReference{{UID: uid}},
			expected: true,
		},
		{
			name:     "no match",
			refs:     []metav1.OwnerReference{{UID: "other"}},
			expected: false,
		},
		{
			name:     "empty refs",
			refs:     nil,
			expected: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, isOwnedBy(tt.refs, uid))
		})
	}
}

func TestReplicasOrDefault(t *testing.T) {
	three := int32(3)
	assert.Equal(t, int32(3), replicasOrDefault(&three))
	assert.Equal(t, int32(1), replicasOrDefault(nil))
}
