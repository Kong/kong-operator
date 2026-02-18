package integration

import (
	"context"
	"fmt"
	"maps"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kcfgdataplane "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/dataplane"
	operatorv1beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1beta1"

	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	testutils "github.com/kong/kong-operator/v2/pkg/utils/test"
	"github.com/kong/kong-operator/v2/test/helpers"
	"github.com/kong/kong-operator/v2/test/helpers/eventually"
)

func TestDataPlaneBlueGreenRollout(t *testing.T) {
	if !blueGreenController {
		t.Skipf("KONG_OPERATOR_BLUEGREEN_CONTROLLER not set, skipping")
	}
	const (
		waitTime = time.Minute
		tickTime = 100 * time.Millisecond
	)

	namespace, cleaner := helpers.SetupTestEnv(t, GetCtx(), GetEnv())

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
		Spec: testBlueGreenDataPlaneSpec(),
	}

	dataplaneClient := GetClients().OperatorClient.GatewayOperatorV1beta1().DataPlanes(namespace.Name)

	dataplane, err := dataplaneClient.Create(GetCtx(), dataplane, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(dataplane)

	t.Log("verifying dataplane gets marked ready")
	require.Eventually(t, testutils.DataPlaneIsReady(t, GetCtx(), dataplaneName, GetClients().OperatorClient), waitTime, tickTime)

	t.Run("before patching", func(t *testing.T) {
		t.Log("verifying preview deployment managed by the dataplane is present")
		require.Eventually(t, testutils.DataPlaneHasDeployment(t, GetCtx(), dataplaneName, nil, clients, dataplanePreviewDeploymentLabels()), waitTime, tickTime)

		t.Run("preview Admin API service", func(t *testing.T) {
			t.Log("verifying preview admin service managed by the dataplane is present")
			require.Eventually(t, testutils.DataPlaneHasService(t, GetCtx(), dataplaneName, clients, dataplaneAdminPreviewServiceLabels()), waitTime, tickTime)

			t.Log("verifying that preview admin service has no active endpoints by default")
			adminServices := testutils.MustListDataPlaneServices(t, GetCtx(), dataplane, GetClients().MgrClient, dataplaneAdminPreviewServiceLabels())
			require.Len(t, adminServices, 1)
			adminSvcNN := client.ObjectKeyFromObject(&adminServices[0])
			require.Eventually(t, testutils.DataPlaneServiceHasNActiveEndpoints(t, GetCtx(), adminSvcNN, clients, 0), waitTime, tickTime,
				"with default rollout resource plan for DataPlane, the preview Admin Service shouldn't get an active endpoint")
		})

		t.Run("preview ingress service", func(t *testing.T) {
			t.Log("verifying preview ingress service managed by the dataplane is present")
			require.Eventually(t, testutils.DataPlaneHasService(t, GetCtx(), dataplaneName, clients, dataplaneIngressPreviewServiceLabels()), waitTime, tickTime)

			t.Log("verifying that preview ingress service has no active endpoints by default")
			ingressServices := testutils.MustListDataPlaneServices(t, GetCtx(), dataplane, GetClients().MgrClient, dataplaneIngressPreviewServiceLabels())
			require.Len(t, ingressServices, 1)
			ingressSvcNN := client.ObjectKeyFromObject(&ingressServices[0])
			require.Eventually(t, testutils.DataPlaneServiceHasNActiveEndpoints(t, GetCtx(), ingressSvcNN, clients, 0), waitTime, tickTime,
				"with default rollout resource plan for DataPlane, the preview ingress Service shouldn't get an active endpoint")
		})
	})

	dataplaneImageToPatch := helpers.GetDefaultDataPlaneBaseImage() + ":3.4"

	t.Run("after patching", func(t *testing.T) {
		patchDataPlaneImage(GetCtx(), t, dataplane, GetClients().MgrClient, dataplaneImageToPatch)

		t.Log("verifying preview deployment managed by the dataplane is present and has AvailableReplicas")
		require.Eventually(t, testutils.DataPlaneHasActiveDeployment(t, GetCtx(), dataplaneName, &appsv1.Deployment{}, dataplanePreviewDeploymentLabels(), clients), waitTime, tickTime)

		t.Run("preview Admin API service", func(t *testing.T) {
			t.Log("verifying preview admin service managed by the dataplane has an active endpoint")
			require.Eventually(t, testutils.DataPlaneHasService(t, GetCtx(), dataplaneName, clients, dataplaneAdminPreviewServiceLabels()), waitTime, tickTime)

			t.Log("verifying that preview admin service has an active endpoint")
			adminServices := testutils.MustListDataPlaneServices(t, GetCtx(), dataplane, GetClients().MgrClient, dataplaneAdminPreviewServiceLabels())
			require.Len(t, adminServices, 1)
			adminSvcNN := client.ObjectKeyFromObject(&adminServices[0])
			require.Eventually(t, testutils.DataPlaneServiceHasNActiveEndpoints(t, GetCtx(), adminSvcNN, clients, 1), waitTime, tickTime,
				"with default rollout resource plan for DataPlane, the preview Admin Service should get an active endpoint")
		})

		t.Run("preview ingress service", func(t *testing.T) {
			t.Log("verifying preview ingress service managed by the dataplane has an active endpoint")
			require.Eventually(t, testutils.DataPlaneHasService(t, GetCtx(), dataplaneName, clients, dataplaneIngressPreviewServiceLabels()), waitTime, tickTime)

			t.Log("verifying that preview ingress service has an active endpoint")
			ingressServices := testutils.MustListDataPlaneServices(t, GetCtx(), dataplane, GetClients().MgrClient, dataplaneIngressPreviewServiceLabels())
			require.Len(t, ingressServices, 1)
			ingressSvcNN := client.ObjectKeyFromObject(&ingressServices[0])
			require.Eventually(t, testutils.DataPlaneServiceHasNActiveEndpoints(t, GetCtx(), ingressSvcNN, clients, 1), waitTime, tickTime,
				"with default rollout resource plan for DataPlane, the preview ingress Service should get an active endpoint")
		})

		t.Run("live ingress service", func(t *testing.T) {
			t.Log("verifying that live ingress service managed by the dataplane is available")
			var liveIngressService corev1.Service
			require.Eventually(t, testutils.DataPlaneHasActiveService(t, GetCtx(), dataplaneName, &liveIngressService, clients, dataplaneIngressLiveServiceLabels()), waitTime, tickTime)

			require.Eventually(t, testutils.DataPlaneServiceHasNActiveEndpoints(t, GetCtx(), client.ObjectKeyFromObject(&liveIngressService), clients, 1), waitTime, tickTime,
				"live ingress Service should always have an active endpoint")
		})

		t.Run("live deployment", func(t *testing.T) {
			t.Log("verifying live deployment managed by the dataplane is present and has an available replica")
			require.Eventually(t, testutils.DataPlaneHasActiveDeployment(t, GetCtx(), dataplaneName, &appsv1.Deployment{}, dataplaneLiveDeploymentLabels(), clients), waitTime, tickTime)
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
			Type:   string(kcfgdataplane.DataPlaneConditionTypeRolledOut),
			Reason: string(kcfgdataplane.DataPlaneConditionReasonRolloutAwaitingPromotion),
			Status: metav1.ConditionFalse,
		})
		require.Eventually(t,
			testutils.DataPlanePredicate(t, GetCtx(), dataplaneName, isAwaitingPromotion, GetClients().OperatorClient),
			waitTime, tickTime,
		)
	})

	t.Run("after promotion", func(t *testing.T) {
		t.Logf("patching DataPlane with promotion triggering annotation %s=%s", operatorv1beta1.DataPlanePromoteWhenReadyAnnotationKey, operatorv1beta1.DataPlanePromoteWhenReadyAnnotationTrue)
		patchDataPlaneAnnotations(t, dataplane, GetClients().MgrClient, map[string]string{
			operatorv1beta1.DataPlanePromoteWhenReadyAnnotationKey: operatorv1beta1.DataPlanePromoteWhenReadyAnnotationTrue,
		})

		t.Run("live deployment", func(t *testing.T) {
			t.Log("verifying live deployment managed by the dataplane is present and has an available replica using the patched proxy image")

			require.Eventually(t,
				testutils.DataPlaneHasDeployment(t, GetCtx(), dataplaneName, nil, clients, dataplaneLiveDeploymentLabels(),
					func(d appsv1.Deployment) bool {
						proxyContainer := k8sutils.GetPodContainerByName(&d.Spec.Template.Spec, consts.DataPlaneProxyContainerName)
						return proxyContainer != nil && dataplaneImageToPatch == proxyContainer.Image
					},
				),
				waitTime, tickTime)
		})

		t.Run("live ingress service", func(t *testing.T) {
			t.Log("verifying that live ingress service managed by the dataplane still has active endpoints")
			var liveIngressService corev1.Service
			require.Eventually(t, testutils.DataPlaneHasActiveService(t, GetCtx(), dataplaneName, &liveIngressService, clients, dataplaneIngressLiveServiceLabels()), waitTime, tickTime)
			require.NotNil(t, liveIngressService)

			require.Eventually(t, testutils.DataPlaneServiceHasNActiveEndpoints(t, GetCtx(), client.ObjectKeyFromObject(&liveIngressService), clients, 1), waitTime, tickTime,
				"live ingress Service should always have an active endpoint")
		})

		t.Run(fmt.Sprintf("%s annotation is cleared from DataPlane", operatorv1beta1.DataPlanePromoteWhenReadyAnnotationKey), func(t *testing.T) {
			require.Eventually(t,
				testutils.DataPlanePredicate(t, GetCtx(), dataplaneName,
					func(dataplane *operatorv1beta1.DataPlane) bool {
						_, ok := dataplane.Annotations[operatorv1beta1.DataPlanePromoteWhenReadyAnnotationKey]
						return !ok
					},
					GetClients().OperatorClient,
				),
				waitTime, tickTime,
			)
		})
	})

	t.Run("removing rollout strategy removes preview resources", func(t *testing.T) {
		t.Logf("patching DataPlane by removing the rollout strategy")
		old := dataplane.DeepCopy()
		dataplane.Spec.Deployment.Rollout = nil
		require.NoError(t, GetClients().MgrClient.Patch(GetCtx(), dataplane, client.MergeFrom(old)))

		t.Run("preview deployment", func(t *testing.T) {
			t.Log("verifying that preview deployment managed by the dataplane is removed")

			require.Eventually(t,
				testutils.Not(
					testutils.DataPlaneHasDeployment(t, GetCtx(), dataplaneName, nil, clients, dataplanePreviewDeploymentLabels())),
				waitTime, tickTime)
		})

		t.Run("preview ingress service", func(t *testing.T) {
			t.Log("verifying that preview ingress service managed by the dataplane is removed")
			var previewIngressService corev1.Service
			require.Eventually(t,
				testutils.Not(
					testutils.DataPlaneHasActiveService(t, GetCtx(), dataplaneName, &previewIngressService, clients, dataplaneIngressPreviewServiceLabels())),
				waitTime, tickTime)
		})

		t.Run("dataplane status rollout should be cleared", func(t *testing.T) {
			require.Eventually(t,
				testutils.DataPlanePredicate(t, GetCtx(), dataplaneName, func(dataplane *operatorv1beta1.DataPlane) bool {
					return dataplane.Status.RolloutStatus == nil
				}, GetClients().OperatorClient),
				waitTime, tickTime)
		})
	})
}

func TestDataPlaneBlueGreenHorizontalScaling(t *testing.T) {
	if !blueGreenController {
		t.Skipf("KONG_OPERATOR_BLUEGREEN_CONTROLLER not set, skipping")
	}
	const (
		waitTime = time.Minute
		tickTime = 100 * time.Millisecond
	)

	namespace, cleaner := helpers.SetupTestEnv(t, GetCtx(), GetEnv())

	t.Log("deploying a dataplane")
	dataplaneName := types.NamespacedName{
		Namespace: namespace.Name,
		Name:      uuid.NewString(),
	}
	dataplane := &operatorv1beta1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: dataplaneName.Namespace,
			Name:      dataplaneName.Name,
		},
		Spec: testBlueGreenDataPlaneSpec(),
	}

	dataplane.Spec.Deployment.Scaling = &operatorv1beta1.Scaling{
		HorizontalScaling: &operatorv1beta1.HorizontalScaling{
			MinReplicas: lo.ToPtr(int32(2)),
			MaxReplicas: 5,
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: "cpu",
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: lo.ToPtr(int32(20)),
						},
					},
				},
			},
		},
	}

	dataplaneClient := GetClients().OperatorClient.GatewayOperatorV1beta1().DataPlanes(namespace.Name)

	dataplane, err := dataplaneClient.Create(GetCtx(), dataplane, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(dataplane)

	t.Logf("verifying DataPlane %s gets marked ready", dataplane.Name)
	require.Eventually(t, testutils.DataPlaneIsReady(t, GetCtx(), dataplaneName, GetClients().OperatorClient), waitTime, tickTime)

	dataplaneImageToPatch := helpers.GetDefaultDataPlaneBaseImage() + ":3.4"
	patchDataPlaneImage(GetCtx(), t, dataplane, GetClients().MgrClient, dataplaneImageToPatch)
	var previewDeployment appsv1.Deployment
	require.Eventually(t, testutils.DataPlaneHasActiveDeployment(t, GetCtx(), dataplaneName, &previewDeployment, dataplanePreviewDeploymentLabels(), clients), waitTime, tickTime)

	t.Logf("checking if DataPlane %s has an HPA", dataplane.Name)
	var hpa autoscalingv2.HorizontalPodAutoscaler
	require.Eventually(t, testutils.DataPlaneHasHPA(t, GetCtx(), dataplane, &hpa, clients),
		waitTime, tickTime, "HPA should be created for DataPlane %s", dataplane.Name)
	require.NotNil(t, hpa)
	require.NotNil(t, hpa.Spec.MinReplicas)
	assert.EqualValues(t, 2, *hpa.Spec.MinReplicas)
	assert.Equal(t, int32(5), hpa.Spec.MaxReplicas)
	require.Len(t, hpa.Spec.Metrics, 1)
	require.NotNil(t, hpa.Spec.Metrics[0].Resource)
	require.NotNil(t, hpa.Spec.Metrics[0].Resource.Target.AverageUtilization)
	assert.Equal(t, int32(20), *hpa.Spec.Metrics[0].Resource.Target.AverageUtilization)

	t.Logf("checking if the HPA %s is pointing to the correct Deployment", hpa.Name)
	require.Eventually(t, testutils.HPAPredicate(t, GetCtx(), client.ObjectKeyFromObject(&hpa),
		func(hpa *autoscalingv2.HorizontalPodAutoscaler) bool {
			return hpa.Spec.ScaleTargetRef.Name != previewDeployment.Name
		}, GetClients().MgrClient),
		waitTime, tickTime, "HPA should not target the preview deployment")

	var deployment2 appsv1.Deployment
	require.Eventually(t, testutils.DataPlaneHasDeployment(t, GetCtx(), dataplaneName, &deployment2, clients,
		client.MatchingLabels{
			consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
			consts.DataPlaneDeploymentStateLabel: consts.DataPlaneStateLabelValueLive,
		}, func(d appsv1.Deployment) bool {
			return hpa.Spec.ScaleTargetRef.Name == d.Name
		}),
		waitTime, tickTime, "HPA should target the live deployment")

	t.Logf("patching DataPlane with promotion triggering annotation %s=%s", operatorv1beta1.DataPlanePromoteWhenReadyAnnotationKey, operatorv1beta1.DataPlanePromoteWhenReadyAnnotationTrue)
	patchDataPlaneAnnotations(t, dataplane, GetClients().MgrClient, map[string]string{
		operatorv1beta1.DataPlanePromoteWhenReadyAnnotationKey: operatorv1beta1.DataPlanePromoteWhenReadyAnnotationTrue,
	})
	t.Logf("HPA %s should now point to just promoted %s Deployment", hpa.Name, previewDeployment.Name)
	require.Eventually(t, testutils.HPAPredicate(t, GetCtx(), client.ObjectKeyFromObject(&hpa),
		func(hpa *autoscalingv2.HorizontalPodAutoscaler) bool {
			return hpa.Spec.ScaleTargetRef.Name == previewDeployment.Name
		}, GetClients().MgrClient),
		waitTime, tickTime)
}

func TestDataPlaneBlueGreenResourcesNotDeletedUntilOwnerIsRemoved(t *testing.T) {
	if !blueGreenController {
		t.Skipf("KONG_OPERATOR_BLUEGREEN_CONTROLLER not set, skipping")
	}
	const (
		waitTime = time.Minute
		tickTime = 100 * time.Millisecond
	)

	namespace, cleaner := helpers.SetupTestEnv(t, GetCtx(), GetEnv())

	t.Log("deploying dataplane")
	dataplane := &operatorv1beta1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace.Name,
			Name:      uuid.NewString(),
		},
		Spec: testBlueGreenDataPlaneSpec(),
	}
	dataplaneName := client.ObjectKeyFromObject(dataplane)

	dataplaneClient := GetClients().OperatorClient.GatewayOperatorV1beta1().DataPlanes(namespace.Name)
	dataplane, err := dataplaneClient.Create(GetCtx(), dataplane, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(dataplane)

	t.Log("ensuring all live dependent resources are created")
	var (
		liveIngressService = &corev1.Service{}
		liveAdminService   = &corev1.Service{}
		liveDeployment     = &appsv1.Deployment{}
		liveTLSSecret      = &corev1.Secret{}
	)
	require.Eventually(t, testutils.DataPlaneHasActiveService(t, GetCtx(), dataplaneName, liveIngressService, clients, dataplaneIngressLiveServiceLabels()), waitTime, tickTime)
	require.NotNil(t, liveIngressService)

	require.Eventually(t, testutils.DataPlaneHasActiveService(t, GetCtx(), dataplaneName, liveAdminService, clients, dataplaneAdminLiveServiceLabels()), waitTime, tickTime)
	require.NotNil(t, liveAdminService)

	require.Eventually(t, testutils.DataPlaneHasActiveDeployment(t, GetCtx(), dataplaneName, liveDeployment, dataplaneLiveDeploymentLabels(), clients), waitTime, tickTime)
	require.NotNil(t, liveDeployment)

	require.Eventually(t, testutils.DataPlaneHasServiceSecret(t, GetCtx(), dataplaneName, client.ObjectKeyFromObject(liveAdminService), liveTLSSecret, clients), waitTime, tickTime)
	require.NotNil(t, liveTLSSecret)

	t.Log("patching dataplane with another dataplane image to trigger rollout")
	dataplaneImageToPatch := helpers.GetDefaultDataPlaneBaseImage() + ":3.4"
	patchDataPlaneImage(GetCtx(), t, dataplane, GetClients().MgrClient, dataplaneImageToPatch)

	t.Log("ensuring all preview dependent resources are created")
	var (
		previewIngressService = &corev1.Service{}
		previewAdminService   = &corev1.Service{}
		previewDeployment     = &appsv1.Deployment{}
		previewTLSSecret      = &corev1.Secret{}
	)

	require.Eventually(t, testutils.DataPlaneHasActiveService(t, GetCtx(), dataplaneName, previewIngressService, clients, dataplaneIngressPreviewServiceLabels()), waitTime, tickTime)
	require.NotNil(t, previewIngressService)

	require.Eventually(t, testutils.DataPlaneHasActiveService(t, GetCtx(), dataplaneName, previewAdminService, clients, dataplaneAdminPreviewServiceLabels()), waitTime, tickTime)
	require.NotNil(t, previewAdminService)

	require.Eventually(t, testutils.DataPlaneHasActiveDeployment(t, GetCtx(), dataplaneName, previewDeployment, dataplanePreviewDeploymentLabels(), clients), waitTime, tickTime)
	require.NotNil(t, previewDeployment)

	require.Eventually(t, testutils.DataPlaneHasServiceSecret(t, GetCtx(), dataplaneName, client.ObjectKeyFromObject(previewAdminService), previewTLSSecret, clients), waitTime, tickTime)
	require.NotNil(t, previewTLSSecret)

	dependentResources := []client.Object{
		liveIngressService,
		liveAdminService,
		liveDeployment,
		liveTLSSecret,
		previewIngressService,
		previewAdminService,
		previewDeployment,
		previewTLSSecret,
	}

	t.Log("ensuring dataplane owned resources after deletion are not immediately deleted")
	for _, resource := range dependentResources {
		require.NoError(t, GetClients().MgrClient.Delete(GetCtx(), resource))

		require.Eventually(t, func() bool {
			err := GetClients().MgrClient.Get(GetCtx(), client.ObjectKeyFromObject(resource), resource)
			if err != nil {
				t.Logf("error getting %T: %v", resource, err)
				return false
			}

			if resource.GetDeletionTimestamp().IsZero() {
				t.Logf("%T %q has no deletion timestamp", resource, resource.GetName())
				return false
			}

			return true
		}, waitTime, tickTime, "resource %T %q should not be deleted immediately after dataplane deletion", resource, resource.GetName())
	}

	t.Log("deleting dataplane and ensuring its owned resources are deleted after that")
	require.NoError(t, GetClients().MgrClient.Delete(GetCtx(), dataplane))
	for _, resource := range dependentResources {
		eventually.WaitForObjectToNotExist(t, ctx, GetClients().MgrClient, resource, waitTime, tickTime,
			"should be deleted after dataplane deletion",
		)
	}
}

func testBlueGreenDataPlaneSpec() operatorv1beta1.DataPlaneSpec {
	return operatorv1beta1.DataPlaneSpec{
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
									Image: helpers.GetDefaultDataPlaneImage(),
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
												Port:   intstr.FromInt32(consts.DataPlaneMetricsPort),
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
	}
}

func patchDataPlaneImage(ctx context.Context, t *testing.T, dataplane *operatorv1beta1.DataPlane, cl client.Client, image string) {
	t.Helper()
	t.Logf("patching DataPlane %s with image %q", dataplane.Name, image)

	oldDataPlane := dataplane.DeepCopy()
	require.Len(t, dataplane.Spec.Deployment.PodTemplateSpec.Spec.Containers, 1)
	dataplane.Spec.Deployment.PodTemplateSpec.Spec.Containers[0].Image = image
	require.NoError(t, cl.Patch(ctx, dataplane, client.MergeFrom(oldDataPlane)))
}

func patchDataPlaneAnnotations(t *testing.T, dataplane *operatorv1beta1.DataPlane, cl client.Client, annotations map[string]string) {
	oldDataPlane := dataplane.DeepCopy()
	require.Len(t, dataplane.Spec.Deployment.PodTemplateSpec.Spec.Containers, 1)
	if dataplane.Annotations == nil {
		dataplane.Annotations = annotations
	} else {
		maps.Copy(dataplane.Annotations, annotations)
	}
	require.NoError(t, cl.Patch(GetCtx(), dataplane, client.MergeFrom(oldDataPlane)))
}

func dataplaneAdminPreviewServiceLabels() client.MatchingLabels {
	return client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
		consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneAdminServiceLabelValue),
		consts.DataPlaneServiceStateLabel:    consts.DataPlaneStateLabelValuePreview,
	}
}

func dataplaneAdminLiveServiceLabels() client.MatchingLabels {
	return client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
		consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneAdminServiceLabelValue),
		consts.DataPlaneServiceStateLabel:    consts.DataPlaneStateLabelValueLive,
	}
}

func dataplaneIngressPreviewServiceLabels() client.MatchingLabels {
	return client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
		consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneIngressServiceLabelValue),
		consts.DataPlaneServiceStateLabel:    consts.DataPlaneStateLabelValuePreview,
	}
}

func dataplaneIngressLiveServiceLabels() client.MatchingLabels {
	return client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
		consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneIngressServiceLabelValue),
		consts.DataPlaneServiceStateLabel:    consts.DataPlaneStateLabelValueLive,
	}
}

func dataplanePreviewDeploymentLabels() client.MatchingLabels {
	return client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
		consts.DataPlaneDeploymentStateLabel: consts.DataPlaneStateLabelValuePreview,
	}
}

func dataplaneLiveDeploymentLabels() client.MatchingLabels {
	return client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
		consts.DataPlaneDeploymentStateLabel: consts.DataPlaneStateLabelValueLive,
	}
}
