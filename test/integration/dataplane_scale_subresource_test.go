package integration

import (
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/pkg/consts"
	testutils "github.com/kong/gateway-operator/pkg/utils/test"
	"github.com/kong/gateway-operator/test/helpers"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
)

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
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{
									"label-a": "value-a",
									"label-x": "value-x",
								},
							},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Env: []corev1.EnvVar{
											{
												Name:  "TEST_ENV",
												Value: "test",
											},
										},
										Name:  consts.DataPlaneProxyContainerName,
										Image: helpers.GetDefaultDataPlaneImage(),
									},
								},
							},
						},
					},
				},
				Network: operatorv1beta1.DataPlaneNetworkOptions{
					Services: &operatorv1beta1.DataPlaneServices{
						Ingress: &operatorv1beta1.DataPlaneServiceOptions{
							ServiceOptions: operatorv1beta1.ServiceOptions{
								Annotations: map[string]string{
									"purpose": "scale",
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

	// Test scaling through the scale subresource
	t.Log("testing scaling through scale subresource")
	require.Eventually(t,
		testutils.DataPlaneUpdateEventually(t, GetCtx(), dataplaneName, clients, func(dp *operatorv1beta1.DataPlane) {
			dp.Spec.Deployment.Replicas = lo.ToPtr(int32(3)) // Scale up to 3 replicas
		}),
		testutils.GatewayReadyTimeLimit, testutils.ObjectUpdateTick)

	t.Log("verifying dataplane scales to 3 replicas")
	require.Eventually(t, testutils.DataPlaneHasNReadyPods(t, GetCtx(), dataplaneName, clients, 3), testutils.GatewayReadyTimeLimit, testutils.ObjectUpdateTick)
}
