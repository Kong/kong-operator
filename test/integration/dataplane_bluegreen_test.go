//go:build integration_tests_bluegreen

package integration

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorv1beta1 "github.com/kong/gateway-operator/apis/v1beta1"
	"github.com/kong/gateway-operator/internal/consts"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
	testutils "github.com/kong/gateway-operator/internal/utils/test"
	"github.com/kong/gateway-operator/test/helpers"
)

func TestDataPlaneBlueGreenRollout(t *testing.T) {
	const (
		waitTime = time.Minute
		tickTime = 100 * time.Millisecond
	)

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
										// Make the test a bit faster.
										ReadinessProbe: &corev1.Probe{
											InitialDelaySeconds: 0,
											PeriodSeconds:       1,
											FailureThreshold:    3,
											SuccessThreshold:    1,
											TimeoutSeconds:      1,
											ProbeHandler: corev1.ProbeHandler{
												HTTPGet: &corev1.HTTPGetAction{
													Path:   "/status",
													Port:   intstr.FromInt(consts.DataPlaneMetricsPort),
													Scheme: corev1.URISchemeHTTP,
												},
											},
										},
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
	require.Eventually(t, testutils.DataPlaneIsProvisioned(t, ctx, dataplaneName, clients.OperatorClient), waitTime, tickTime)

	t.Run("before patching", func(t *testing.T) {
		t.Log("verifying preview deployment managed by the dataplane is present")
		require.Eventually(t, testutils.DataPlaneHasDeployment(t, ctx, dataplaneName, clients, dataplanePreviewDeploymentLabels()), waitTime, tickTime)

		t.Run("preview Admin API service", func(t *testing.T) {
			t.Log("verifying preview admin service managed by the dataplane is present")
			require.Eventually(t, testutils.DataPlaneHasService(t, ctx, dataplaneName, clients, dataplaneAdminPreviewServiceLabels()), waitTime, tickTime)

			t.Log("verifying that preview admin service has no active endpoints by default")
			adminServices := testutils.MustListDataPlaneServices(t, ctx, dataplane, clients.MgrClient, dataplaneAdminPreviewServiceLabels())
			require.Len(t, adminServices, 1)
			adminSvcNN := client.ObjectKeyFromObject(&adminServices[0])
			require.Eventually(t, testutils.DataPlaneServiceHasNActiveEndpoints(t, ctx, adminSvcNN, clients, 0), waitTime, tickTime,
				"with default rollout resource plan for DataPlane, the preview Admin Service shouldn't get an active endpoint")
		})

		t.Run("preview ingress service", func(t *testing.T) {
			t.Log("verifying preview ingress service managed by the dataplane is present")
			require.Eventually(t, testutils.DataPlaneHasService(t, ctx, dataplaneName, clients, dataplaneIngressPreviewServiceLabels()), waitTime, tickTime)

			t.Log("verifying that preview ingress service has no active endpoints by default")
			ingressServices := testutils.MustListDataPlaneServices(t, ctx, dataplane, clients.MgrClient, dataplaneIngressPreviewServiceLabels())
			require.Len(t, ingressServices, 1)
			ingressSvcNN := client.ObjectKeyFromObject(&ingressServices[0])
			require.Eventually(t, testutils.DataPlaneServiceHasNActiveEndpoints(t, ctx, ingressSvcNN, clients, 0), waitTime, tickTime,
				"with default rollout resource plan for DataPlane, the preview ingress Service shouldn't get an active endpoint")
		})
	})

	const dataplaneImageToPatch = "kong:3.1"

	t.Run("after patching", func(t *testing.T) {
		t.Logf("patching DataPlane with image %q", dataplaneImageToPatch)
		patchDataPlaneImage(t, dataplane, clients.MgrClient, dataplaneImageToPatch)

		t.Log("verifying preview deployment managed by the dataplane is present and has AvailableReplicas")
		require.Eventually(t, testutils.DataPlaneHasActiveDeployment(t, ctx, dataplaneName, clients, dataplanePreviewDeploymentLabels()), waitTime, tickTime)

		t.Run("preview Admin API service", func(t *testing.T) {
			t.Log("verifying preview admin service managed by the dataplane has an active endpoint")
			require.Eventually(t, testutils.DataPlaneHasService(t, ctx, dataplaneName, clients, dataplaneAdminPreviewServiceLabels()), waitTime, tickTime)

			t.Log("verifying that preview admin service has an active endpoint")
			adminServices := testutils.MustListDataPlaneServices(t, ctx, dataplane, clients.MgrClient, dataplaneAdminPreviewServiceLabels())
			require.Len(t, adminServices, 1)
			adminSvcNN := client.ObjectKeyFromObject(&adminServices[0])
			require.Eventually(t, testutils.DataPlaneServiceHasNActiveEndpoints(t, ctx, adminSvcNN, clients, 1), waitTime, tickTime,
				"with default rollout resource plan for DataPlane, the preview Admin Service should get an active endpoint")
		})

		t.Run("preview ingress service", func(t *testing.T) {
			t.Log("verifying preview ingress service managed by the dataplane has an active endpoint")
			require.Eventually(t, testutils.DataPlaneHasService(t, ctx, dataplaneName, clients, dataplaneIngressPreviewServiceLabels()), waitTime, tickTime)

			t.Log("verifying that preview ingress service has an active endpoint")
			ingressServices := testutils.MustListDataPlaneServices(t, ctx, dataplane, clients.MgrClient, dataplaneIngressPreviewServiceLabels())
			require.Len(t, ingressServices, 1)
			ingressSvcNN := client.ObjectKeyFromObject(&ingressServices[0])
			require.Eventually(t, testutils.DataPlaneServiceHasNActiveEndpoints(t, ctx, ingressSvcNN, clients, 1), waitTime, tickTime,
				"with default rollout resource plan for DataPlane, the preview ingress Service should get an active endpoint")
		})

		t.Run("live ingress service", func(t *testing.T) {
			t.Log("verifying that live ingress service managed by the dataplane is available")
			var liveIngressService corev1.Service
			require.Eventually(t, testutils.DataPlaneHasActiveService(t, ctx, dataplaneName, &liveIngressService, clients, dataplaneIngressLiveServiceLabels()), waitTime, tickTime)

			require.Eventually(t, testutils.DataPlaneServiceHasNActiveEndpoints(t, ctx, client.ObjectKeyFromObject(&liveIngressService), clients, 1), waitTime, tickTime,
				"live ingress Service should always have an active endpoint")
		})

		t.Run("live deployment", func(t *testing.T) {
			t.Log("verifying live deployment managed by the dataplane is present and has an available replica")
			require.Eventually(t, testutils.DataPlaneHasActiveDeployment(t, ctx, dataplaneName, clients, dataplaneLiveDeploymentLabels()), waitTime, tickTime)
		})
	})

	t.Run("checking that DataPlane rollout status has AwaitingPromotion Reason set for RolledOut condition", func(t *testing.T) {
		dataPlaneRolloutStatusConditionPredicate := func(c *metav1.Condition) func(dataplane *operatorv1beta1.DataPlane) bool {
			return func(dataplane *operatorv1beta1.DataPlane) bool {
				for _, condition := range dataplane.Status.RolloutStatus.Conditions {
					if condition.Type == c.Type && condition.Status == c.Status {
						return true
					}
					t.Logf("DataPlane Rollout Status condition: Type=%q;Reason:%q;Status:%q;Message:%q",
						condition.Type, condition.Reason, condition.Status, condition.Message,
					)
				}
				return false
			}
		}
		isAwaitingPromotion := dataPlaneRolloutStatusConditionPredicate(&metav1.Condition{
			Type:   string(consts.DataPlaneConditionTypeRolledOut),
			Reason: string(consts.DataPlaneConditionReasonRolloutAwaitingPromotion),
			Status: metav1.ConditionFalse,
		})
		require.Eventually(t,
			testutils.DataPlanePredicate(t, ctx, dataplaneName, isAwaitingPromotion, clients.OperatorClient),
			waitTime, tickTime,
		)
	})

	t.Run("after promotion", func(t *testing.T) {
		t.Logf("patching DataPlane with promotion triggering annotation %s=%s", operatorv1beta1.DataPlanePromoteWhenReadyAnnotationKey, operatorv1beta1.DataPlanePromoteWhenReadyAnnotationTrue)
		patchDataPlaneAnnotations(t, dataplane, clients.MgrClient, map[string]string{
			operatorv1beta1.DataPlanePromoteWhenReadyAnnotationKey: operatorv1beta1.DataPlanePromoteWhenReadyAnnotationTrue,
		})

		t.Run("live deployment", func(t *testing.T) {
			t.Log("verifying live deployment managed by the dataplane is present and has an available replica using the patched proxy image")
			require.Eventually(t, testutils.DataPlaneHasActiveDeployment(t, ctx, dataplaneName, clients, dataplaneLiveDeploymentLabels()), waitTime, tickTime)

			deployments := testutils.MustListDataPlaneDeployments(t, ctx, dataplane, clients, dataplaneLiveDeploymentLabels())
			require.Len(t, deployments, 1)
			deployment := deployments[0]
			proxyContainer := k8sutils.GetPodContainerByName(&deployment.Spec.Template.Spec, consts.DataPlaneProxyContainerName)
			require.NotNil(t, proxyContainer)
			require.NotNil(t, dataplaneImageToPatch, proxyContainer.Image)
		})

		t.Run("live ingress service", func(t *testing.T) {
			t.Log("verifying that live ingress service managed by the dataplane still has active endpoints")
			var liveIngressService corev1.Service
			require.Eventually(t, testutils.DataPlaneHasActiveService(t, ctx, dataplaneName, &liveIngressService, clients, dataplaneIngressLiveServiceLabels()), waitTime, tickTime)
			require.NotNil(t, liveIngressService)

			require.Eventually(t, testutils.DataPlaneServiceHasNActiveEndpoints(t, ctx, client.ObjectKeyFromObject(&liveIngressService), clients, 1), waitTime, tickTime,
				"live ingress Service should always have an active endpoint")
		})

		t.Run(fmt.Sprintf("%s annotation is cleared from DataPlane", operatorv1beta1.DataPlanePromoteWhenReadyAnnotationKey), func(t *testing.T) {
			require.Eventually(t,
				testutils.DataPlanePredicate(t, ctx, dataplaneName,
					func(dataplane *operatorv1beta1.DataPlane) bool {
						_, ok := dataplane.Annotations[operatorv1beta1.DataPlanePromoteWhenReadyAnnotationKey]
						return !ok
					},
					clients.OperatorClient,
				),
				waitTime, tickTime,
			)
		})
	})
}

func patchDataPlaneImage(t *testing.T, dataplane *operatorv1beta1.DataPlane, cl client.Client, image string) {
	oldDataplane := dataplane.DeepCopy()
	require.Len(t, dataplane.Spec.Deployment.PodTemplateSpec.Spec.Containers, 1)
	dataplane.Spec.Deployment.PodTemplateSpec.Spec.Containers[0].Image = image
	require.NoError(t, cl.Patch(ctx, dataplane, client.MergeFrom(oldDataplane)))
}

func patchDataPlaneAnnotations(t *testing.T, dataplane *operatorv1beta1.DataPlane, cl client.Client, annotations map[string]string) {
	oldDataplane := dataplane.DeepCopy()
	require.Len(t, dataplane.Spec.Deployment.PodTemplateSpec.Spec.Containers, 1)
	if dataplane.Annotations == nil {
		dataplane.Annotations = annotations
	} else {
		for k, v := range annotations {
			dataplane.Annotations[k] = v
		}
	}
	require.NoError(t, cl.Patch(ctx, dataplane, client.MergeFrom(oldDataplane)))
}

func dataplaneAdminPreviewServiceLabels() client.MatchingLabels {
	return client.MatchingLabels{
		consts.GatewayOperatorControlledLabel: consts.DataPlaneManagedLabelValue,
		consts.DataPlaneServiceTypeLabel:      string(consts.DataPlaneAdminServiceLabelValue),
		consts.DataPlaneServiceStateLabel:     consts.DataPlaneStateLabelValuePreview,
	}
}

func dataplaneIngressPreviewServiceLabels() client.MatchingLabels {
	return client.MatchingLabels{
		consts.GatewayOperatorControlledLabel: consts.DataPlaneManagedLabelValue,
		consts.DataPlaneServiceTypeLabel:      string(consts.DataPlaneIngressServiceLabelValue),
		consts.DataPlaneServiceStateLabel:     consts.DataPlaneStateLabelValuePreview,
	}
}

func dataplaneIngressLiveServiceLabels() client.MatchingLabels {
	return client.MatchingLabels{
		consts.GatewayOperatorControlledLabel: consts.DataPlaneManagedLabelValue,
		consts.DataPlaneServiceTypeLabel:      string(consts.DataPlaneIngressServiceLabelValue),
		consts.DataPlaneServiceStateLabel:     consts.DataPlaneStateLabelValueLive,
	}
}

func dataplanePreviewDeploymentLabels() client.MatchingLabels {
	return client.MatchingLabels{
		consts.GatewayOperatorControlledLabel: consts.DataPlaneManagedLabelValue,
		consts.DataPlaneDeploymentStateLabel:  consts.DataPlaneStateLabelValuePreview,
	}
}

func dataplaneLiveDeploymentLabels() client.MatchingLabels {
	return client.MatchingLabels{
		consts.GatewayOperatorControlledLabel: consts.DataPlaneManagedLabelValue,
		consts.DataPlaneDeploymentStateLabel:  consts.DataPlaneStateLabelValueLive,
	}
}
