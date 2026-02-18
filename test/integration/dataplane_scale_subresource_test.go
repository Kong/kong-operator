package integration

import (
	"fmt"
	"os/exec"
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1beta1"

	"github.com/kong/kong-operator/v2/pkg/consts"
	testutils "github.com/kong/kong-operator/v2/pkg/utils/test"
	"github.com/kong/kong-operator/v2/test/helpers"
)

// runKubectlScaleDataPlane runs the kubectl scale command to test the scale subresource for the DataPlane CRD.
// It scales the DataPlane resource to the specified number of replicas and returns the output and error.
func runKubectlScaleDataPlane(t *testing.T, namespacedName types.NamespacedName, replicas int) (string, error) {
	t.Helper()

	// Execute kubectl scale command
	cmd := "kubectl scale --namespace=" + namespacedName.Namespace + " dataplane/" + namespacedName.Name + " --replicas=" + fmt.Sprint(replicas)

	out, err := exec.CommandContext(t.Context(), "sh", "-c", cmd).CombinedOutput()
	return string(out), err
}

// TestDataPlaneScaleSubresource tests the scale subresource of the DataPlane CRD.
// It verifies that when a deployment is restarted using kubectl rollout restart,
// the new ReplicaSet maintains the correct replica count.
func TestDataPlaneScaleSubresource(t *testing.T) {
	t.Parallel()

	namespace, cleaner := helpers.SetupTestEnv(t, GetCtx(), GetEnv())
	clients := GetClients()

	t.Log("deploying dataplane resource with 2 replicas")
	dataplane := &operatorv1beta1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    namespace.Name,
			GenerateName: "dataplane-scale-test-",
		},
		Spec: operatorv1beta1.DataPlaneSpec{
			DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
				Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
					DeploymentOptions: operatorv1beta1.DeploymentOptions{
						Replicas: lo.ToPtr(int32(2)), // Set initial replica count to 2
						PodTemplateSpec: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  consts.DataPlaneProxyContainerName,
										Image: helpers.GetDefaultDataPlaneImage(),
									},
								},
							},
						},
					},
				},
			},
		},
	}

	dataplaneClient := clients.OperatorClient.GatewayOperatorV1beta1().DataPlanes(namespace.Name)
	dataplane, err := dataplaneClient.Create(GetCtx(), dataplane, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(dataplane)

	dataplaneName := client.ObjectKeyFromObject(dataplane)

	t.Log("verifying dataplane gets marked ready")
	require.Eventually(t, testutils.DataPlaneIsReady(t, GetCtx(), dataplaneName, clients.OperatorClient), testutils.GatewayReadyTimeLimit, testutils.ObjectUpdateTick)

	t.Log("verifying dataplane has 2 ready replicas")
	require.Eventually(t, testutils.DataPlaneHasNReadyPods(t, GetCtx(), dataplaneName, clients, 2), testutils.GatewayReadyTimeLimit, testutils.ObjectUpdateTick)

	// Get the deployment created by the dataplane controller
	var deployment appsv1.Deployment
	require.Eventually(t, testutils.DataPlaneHasActiveDeployment(t, GetCtx(), dataplaneName, &deployment, client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
	}, clients), testutils.GatewayReadyTimeLimit, testutils.ObjectUpdateTick)

	// Test scaling through the scale subresource - use kubectl scale
	t.Log("testing scaling through scale subresource with kubectl scale")

	// Using kubectl scale command to test the scale subresource
	output, scaleErr := runKubectlScaleDataPlane(t, types.NamespacedName{
		Namespace: namespace.Name,
		Name:      dataplane.Name,
	}, 3)
	t.Logf("Scale command output: %s", output)
	require.NoError(t, scaleErr)

	t.Log("verifying dataplane scales to 3 replicas")
	require.Eventually(t, testutils.DataPlaneHasNReadyPods(t, GetCtx(), dataplaneName, clients, 3), testutils.GatewayReadyTimeLimit, testutils.ObjectUpdateTick)

	t.Log("simulating kubectl rollout restart by adding restart annotation")
	deploymentCopy := deployment.DeepCopy()
	if deploymentCopy.Spec.Template.Annotations == nil {
		deploymentCopy.Spec.Template.Annotations = make(map[string]string)
	}

	restartTime := time.Now()
	deploymentCopy.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = restartTime.Format(time.RFC3339)

	err = clients.MgrClient.Patch(GetCtx(), deploymentCopy, client.MergeFrom(&deployment))
	require.NoError(t, err)

	t.Log("waiting for new ReplicaSet to be created")

	var newReplicaSet *appsv1.ReplicaSet
	require.Eventually(t, func() bool {
		rs := FindDataPlaneReplicaSetNewerThan(
			t,
			GetCtx(),
			clients.MgrClient,
			restartTime,
			namespace.Name,
			dataplane,
		)
		if rs != nil {
			t.Logf("Found new ReplicaSet %s created at %v", rs.Name, rs.CreationTimestamp)
			newReplicaSet = rs
			return true
		}
		return false
	}, testutils.GatewayReadyTimeLimit, testutils.ObjectUpdateTick, "Failed to find new ReplicaSet after rollout restart")

	t.Log("verifying new ReplicaSet has correct replica count")
	require.NotNil(t, newReplicaSet, "New ReplicaSet should be created")

	// Wait for the ReplicaSet to have the correct replica count
	require.Eventually(t, func() bool {
		if err := clients.MgrClient.Get(GetCtx(), types.NamespacedName{
			Namespace: newReplicaSet.Namespace,
			Name:      newReplicaSet.Name,
		}, newReplicaSet); err != nil {
			t.Logf("Error getting ReplicaSet: %v", err)
			return false
		}
		t.Logf("Current ReplicaSet replicas: %d", *newReplicaSet.Spec.Replicas)
		return *newReplicaSet.Spec.Replicas == 3
	}, testutils.GatewayReadyTimeLimit, testutils.ObjectUpdateTick, "New ReplicaSet should have 3 replicas")

	t.Log("waiting for rollout to complete")
	require.Eventually(t, func() bool {
		if err := clients.MgrClient.Get(GetCtx(), types.NamespacedName{
			Namespace: newReplicaSet.Namespace,
			Name:      newReplicaSet.Name,
		}, newReplicaSet); err != nil {
			return false
		}
		return newReplicaSet.Status.ReadyReplicas == *newReplicaSet.Spec.Replicas
	}, testutils.GatewayReadyTimeLimit, testutils.ObjectUpdateTick, "New ReplicaSet should have all replicas ready")

	t.Log("verifying dataplane still has 3 ready replicas after rollout")
	require.Eventually(t, testutils.DataPlaneHasNReadyPods(t, GetCtx(), dataplaneName, clients, 3), testutils.GatewayReadyTimeLimit, testutils.ObjectUpdateTick)

	// Test the specific issue where scaling after restart didn't work properly
	t.Log("scaling DataPlane to 4 replicas after restart")
	// Test scaling through the scale subresource - use kubectl scale
	t.Log("testing scaling through scale subresource with kubectl scale")

	// Using kubectl scale command to test the scale subresource
	output, scaleErr = runKubectlScaleDataPlane(t, types.NamespacedName{
		Namespace: namespace.Name,
		Name:      dataplane.Name,
	}, 4)
	t.Logf("Scale command output: %s", output)
	require.NoError(t, scaleErr)

	t.Log("verifying dataplane scales to 4 replicas")
	require.Eventually(t, testutils.DataPlaneHasNReadyPods(t, GetCtx(), dataplaneName, clients, 4), testutils.GatewayReadyTimeLimit, testutils.ObjectUpdateTick)
}
