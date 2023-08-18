//go:build integration_tests_bluegreen

package integration

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1beta1 "github.com/kong/gateway-operator/apis/v1beta1"
	"github.com/kong/gateway-operator/internal/consts"
	testutils "github.com/kong/gateway-operator/internal/utils/test"
	"github.com/kong/gateway-operator/test/helpers"
)

func TestDataPlaneBlueGreenRollout(t *testing.T) {
	namespace, cleaner := helpers.SetupTestEnv(t, ctx, env)

	t.Log("deploying dataplane resource with 1 replica")
	dataplaneName := types.NamespacedName{
		Namespace: namespace.Name,
		Name:      uuid.NewString(),
	}
	dataplane := &operatorv1beta1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: dataplaneName.Namespace,
			Name:      dataplaneName.Name,
		},
		Spec: operatorv1beta1.DataPlaneSpec{
			DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
				Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
					Rollout: &operatorv1beta1.Rollout{
						Strategy: operatorv1beta1.RolloutStrategy{
							BlueGreen: &operatorv1beta1.BlueGreenStrategy{
								Promotion: operatorv1beta1.Promotion{
									Strategy: operatorv1beta1.BreakBeforePromotion,
								},
							},
						},
					},
					DeploymentOptions: operatorv1beta1.DeploymentOptions{
						PodTemplateSpec: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  consts.DataPlaneProxyContainerName,
										Image: consts.DefaultDataPlaneImage,
									},
								},
							},
						},
					},
				},
			},
		},
	}

	dataplaneClient := clients.OperatorClient.ApisV1beta1().DataPlanes(namespace.Name)

	dataplane, err := dataplaneClient.Create(ctx, dataplane, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(dataplane)

	t.Log("verifying dataplane gets marked provisioned")
	require.Eventually(t, testutils.DataPlaneIsProvisioned(t, ctx, dataplaneName, clients.OperatorClient), time.Minute, time.Second)

	t.Log("verifying preview deployment managed by the dataplane")
	require.Eventually(t, testutils.DataPlaneHasActiveDeployment(t, ctx, dataplaneName, clients, client.MatchingLabels{
		consts.GatewayOperatorControlledLabel: consts.DataPlaneManagedLabelValue,
		consts.DataPlaneDeploymentStateLabel:  consts.DataPlaneStateLabelValuePreview,
	}), time.Minute, time.Second)

	t.Log("verifying preview admin service managed by the dataplane")
	var dataplaneAdminPreviewService corev1.Service
	require.Eventually(t, testutils.DataPlaneHasActiveService(t, ctx, dataplaneName, &dataplaneAdminPreviewService, clients, client.MatchingLabels{
		consts.GatewayOperatorControlledLabel: consts.DataPlaneManagedLabelValue,
		consts.DataPlaneServiceTypeLabel:      string(consts.DataPlaneAdminServiceLabelValue),
		consts.DataPlaneServiceStateLabel:     consts.DataPlaneStateLabelValuePreview,
	}), time.Minute, time.Second)

	t.Log("verifying preview admin service's endpoints")
	require.Eventually(t, testutils.DataPlaneServiceHasActiveEndpoints(t, ctx, types.NamespacedName{
		Name:      dataplaneAdminPreviewService.Name,
		Namespace: dataplaneAdminPreviewService.Namespace,
	}, clients), time.Minute, time.Second)
}
