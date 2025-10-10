package integration

import (
	"context"
	"testing"
	"time"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmanagerv1client "github.com/cert-manager/cert-manager/pkg/client/clientset/versioned/typed/certmanager/v1"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	kcfgdataplane "github.com/kong/kong-operator/api/gateway-operator/dataplane"
	operatorv1beta1 "github.com/kong/kong-operator/api/gateway-operator/v1beta1"
	konnectv1alpha1 "github.com/kong/kong-operator/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/controller/dataplane/certificates"
	"github.com/kong/kong-operator/pkg/consts"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
	testutils "github.com/kong/kong-operator/pkg/utils/test"
	"github.com/kong/kong-operator/test"
	"github.com/kong/kong-operator/test/helpers"
	"github.com/kong/kong-operator/test/helpers/deploy"
	"github.com/kong/kong-operator/test/helpers/eventually"
)

const (
	waitTime = 3 * time.Minute
	tickTime = 250 * time.Millisecond
)

func TestDataPlaneEssentials(t *testing.T) {
	t.Parallel()
	namespace, cleaner := helpers.SetupTestEnv(t, GetCtx(), GetEnv())

	t.Log("deploying dataplane resource")
	dataplane := &operatorv1beta1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    namespace.Name,
			GenerateName: "dataplane-",
		},
		Spec: operatorv1beta1.DataPlaneSpec{
			DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
				Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
					DeploymentOptions: operatorv1beta1.DeploymentOptions{
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
									"foo": "bar",
								},
							},
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

	dataplaneName := client.ObjectKeyFromObject(dataplane)

	t.Log("verifying dataplane gets marked provisioned")
	require.Eventually(t, testutils.DataPlaneIsReady(t, GetCtx(), dataplaneName, GetClients().OperatorClient), waitTime, tickTime)

	t.Log("verifying deployments managed by the dataplane")
	require.Eventually(t, testutils.DataPlaneHasActiveDeployment(t, GetCtx(), dataplaneName, &appsv1.Deployment{}, client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
	}, clients), waitTime, tickTime)

	t.Logf("verifying that pod labels were set per the provided spec")
	require.Eventually(t, func() bool {
		deployments := testutils.MustListDataPlaneDeployments(t, GetCtx(), dataplane, clients, client.MatchingLabels{
			consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
		})
		require.Len(t, deployments, 1, "There must be only one DataPlane deployment")
		deployment := &deployments[0]

		va, oka := deployment.Spec.Template.Labels["label-a"]
		if !oka || va != "value-a" {
			t.Logf("got unexpected %q label-a value", va)
			return false
		}
		vx, okx := deployment.Spec.Template.Labels["label-x"]
		if !okx || vx != "value-x" {
			t.Logf("got unexpected %q label-x value", vx)
			return false
		}

		return true
	}, testutils.DataPlaneCondDeadline, testutils.DataPlaneCondTick)

	// check environment variables of deployments and pods.

	t.Log("verifying dataplane Deployment.Pods.Env vars")
	deployments := testutils.MustListDataPlaneDeployments(t, GetCtx(), dataplane, clients, client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
	})
	require.Len(t, deployments, 1, "There must be only one DataPlane deployment")
	deployment := &deployments[0]

	proxyContainer := k8sutils.GetPodContainerByName(
		&deployment.Spec.Template.Spec, consts.DataPlaneProxyContainerName)
	require.NotNil(t, proxyContainer)
	envs := proxyContainer.Env
	// check specified custom envs
	testEnvValue := GetEnvValueByName(envs, "TEST_ENV")
	require.Equal(t, "test", testEnvValue)
	// check default envs added by operator
	kongDatabaseEnvValue := GetEnvValueByName(envs, consts.EnvVarKongDatabase)
	require.Equal(t, "off", kongDatabaseEnvValue)

	t.Log("verifying services managed by the dataplane")
	var dataplaneIngressService corev1.Service
	require.Eventually(t, testutils.DataPlaneHasActiveService(t, GetCtx(), dataplaneName, &dataplaneIngressService, clients, client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
		consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneIngressServiceLabelValue),
	}), waitTime, tickTime)
	t.Log("verifying annotations on the proxy service managed by the dataplane")
	require.Equal(t, "bar", dataplaneIngressService.Annotations["foo"])

	t.Log("verifying dataplane services receive IP addresses")
	var dataplaneIP string
	require.Eventually(t, func() bool {
		dataplaneService, err := GetClients().K8sClient.CoreV1().Services(dataplane.Namespace).Get(GetCtx(), dataplaneIngressService.Name, metav1.GetOptions{})
		require.NoError(t, err)
		if len(dataplaneService.Status.LoadBalancer.Ingress) > 0 {
			dataplaneIP = dataplaneService.Status.LoadBalancer.Ingress[0].IP
			return true
		}
		return false
	}, waitTime, tickTime)

	require.Eventually(t, Expect404WithNoRouteFunc(t, GetCtx(), "http://"+dataplaneIP), waitTime, tickTime)

	t.Log("deleting the dataplane deployment")
	dataplaneDeployments := testutils.MustListDataPlaneDeployments(t, GetCtx(), dataplane, clients, client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
	})
	require.Len(t, dataplaneDeployments, 1, "there must be only one dataplane deployment")
	require.NoError(t, GetClients().MgrClient.Delete(GetCtx(), &dataplaneDeployments[0]))

	t.Log("verifying deployments managed by the dataplane after deletion")
	require.Eventually(t, testutils.DataPlaneHasActiveDeployment(t, GetCtx(), dataplaneName, &appsv1.Deployment{}, client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
	}, clients), waitTime, tickTime)

	t.Log("deleting the dataplane service")
	require.NoError(t, GetClients().MgrClient.Delete(GetCtx(), &dataplaneIngressService))

	t.Log("verifying services managed by the dataplane after deletion")
	require.Eventually(t, testutils.DataPlaneHasActiveService(t, GetCtx(), dataplaneName, &dataplaneIngressService, clients, client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
		consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneIngressServiceLabelValue),
	}), waitTime, tickTime)

	t.Log("verifying dataplane services receive IP addresses after deletion")
	require.Eventually(t, func() bool {
		dataplaneService, err := GetClients().K8sClient.CoreV1().Services(dataplane.Namespace).Get(GetCtx(), dataplaneIngressService.Name, metav1.GetOptions{})
		require.NoError(t, err)
		if len(dataplaneService.Status.LoadBalancer.Ingress) > 0 {
			dataplaneIP = dataplaneService.Status.LoadBalancer.Ingress[0].IP
			return true
		}
		return false
	}, waitTime, tickTime)

	require.Eventually(t, Expect404WithNoRouteFunc(t, GetCtx(), "http://"+dataplaneIP), waitTime, tickTime)

	t.Log("verifying dataplane status is properly filled with backing service name and its addresses")
	require.Eventually(t, testutils.DataPlaneHasServiceAndAddressesInStatus(t, GetCtx(), dataplaneName, clients), waitTime, tickTime)
}

func TestDataPlaneServiceTypes(t *testing.T) {
	t.Parallel()
	namespace, cleaner := helpers.SetupTestEnv(t, GetCtx(), GetEnv())

	t.Log("deploying dataplane resource")
	dataplane := &operatorv1beta1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    namespace.Name,
			GenerateName: "dataplane-",
		},
		Spec: operatorv1beta1.DataPlaneSpec{
			DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
				Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
					DeploymentOptions: operatorv1beta1.DeploymentOptions{
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
				Network: operatorv1beta1.DataPlaneNetworkOptions{
					Services: &operatorv1beta1.DataPlaneServices{
						Ingress: &operatorv1beta1.DataPlaneServiceOptions{
							ServiceOptions: operatorv1beta1.ServiceOptions{
								Type: corev1.ServiceTypeLoadBalancer,
							},
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

	dataplaneName := client.ObjectKeyFromObject(dataplane)

	t.Log("verifying dataplane gets marked provisioned")
	require.Eventually(t, testutils.DataPlaneIsReady(t, GetCtx(), dataplaneName, GetClients().OperatorClient), waitTime, tickTime)

	t.Log("verifying deployments managed by the dataplane")
	deployment := &appsv1.Deployment{}
	require.Eventually(t, testutils.DataPlaneHasActiveDeployment(t, GetCtx(), dataplaneName, deployment, client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
	}, clients), waitTime, tickTime)

	t.Log("verifying services managed by the dataplane")
	var dataplaneIngressService corev1.Service
	require.Eventually(t, testutils.DataPlaneHasActiveService(t, GetCtx(), dataplaneName, &dataplaneIngressService, clients, client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
		consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneIngressServiceLabelValue),
	}), waitTime, tickTime)

	updateAndVerifyServiceType := func(t *testing.T, ctx context.Context, dataplaneName types.NamespacedName, clients testutils.K8sClients, dataplane *operatorv1beta1.DataPlane, dataplaneIngressService corev1.Service, serviceType corev1.ServiceType) {
		t.Helper()

		t.Logf("updating dataplane spec with proxy service type of %s", serviceType)
		require.Eventually(t,
			testutils.DataPlaneUpdateEventually(t, ctx, dataplaneName, clients, func(dp *operatorv1beta1.DataPlane) {
				dp.Spec.Network.Services.Ingress.Type = serviceType
			}),
			waitTime, tickTime)

		t.Logf("checking if dataplane proxy service type changes to %s", serviceType)
		require.Eventually(t, func() bool {
			servicesClient := GetClients().K8sClient.CoreV1().Services(dataplane.Namespace)
			dataplaneIngressService, err := servicesClient.Get(GetCtx(), dataplaneIngressService.Name, metav1.GetOptions{})
			if err != nil {
				t.Logf("error getting dataplane proxy service: %v", err)
				return false
			}
			if dataplaneIngressService.Spec.Type != serviceType {
				t.Logf("dataplane proxy service should be of %s type but is %s", serviceType, dataplaneIngressService.Spec.Type)
				return false
			}

			return true
		}, waitTime, tickTime)
	}

	tests := []struct {
		name        string
		serviceType corev1.ServiceType
	}{
		{"ClusterIP", corev1.ServiceTypeClusterIP},
		{"NodePort", corev1.ServiceTypeNodePort},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updateAndVerifyServiceType(t, ctx, dataplaneName, clients, dataplane, dataplaneIngressService, tt.serviceType)
		})
	}
}

func TestDataPlaneUpdate(t *testing.T) {
	t.Parallel()
	namespace, cleaner := helpers.SetupTestEnv(t, GetCtx(), GetEnv())

	dataplaneClient := GetClients().OperatorClient.GatewayOperatorV1beta1().DataPlanes(namespace.Name)
	t.Log("deploying dataplane resource")
	dataplane := &operatorv1beta1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    namespace.Name,
			GenerateName: "dataplane-",
		},
		Spec: operatorv1beta1.DataPlaneSpec{
			DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
				Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
					DeploymentOptions: operatorv1beta1.DeploymentOptions{
						PodTemplateSpec: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Env: []corev1.EnvVar{
											{
												Name:  "TEST_ENV",
												Value: "before_update",
											},
											{
												Name:  consts.EnvVarKongDatabase,
												Value: "off",
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
			},
		},
	}
	dataplane, err := dataplaneClient.Create(GetCtx(), dataplane, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(dataplane)

	dataplaneName := client.ObjectKeyFromObject(dataplane)

	t.Log("verifying that the dataplane gets marked as provisioned")
	require.Eventually(t, testutils.DataPlaneIsReady(
		t, GetCtx(), dataplaneName, GetClients().OperatorClient),
		testutils.DataPlaneCondDeadline, testutils.DataPlaneCondTick,
	)

	t.Log("verifying deployments managed by the dataplane")
	require.Eventually(t,
		testutils.DataPlaneHasActiveDeployment(t, GetCtx(), dataplaneName, &appsv1.Deployment{}, client.MatchingLabels{
			consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
		}, clients),
		testutils.DataPlaneCondDeadline, testutils.DataPlaneCondTick,
	)

	t.Log("verifying services managed by the dataplane")
	var dataplaneService corev1.Service
	require.Eventually(t,
		testutils.DataPlaneHasActiveService(t, GetCtx(), dataplaneName, &dataplaneService, clients, client.MatchingLabels{
			consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
			consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneIngressServiceLabelValue),
		}),
		testutils.DataPlaneCondDeadline, testutils.DataPlaneCondTick,
	)

	deployments := testutils.MustListDataPlaneDeployments(t, GetCtx(), dataplane, clients, client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
	})
	require.Len(t, deployments, 1, "There must be only one DatePlane deployment")
	deployment := &deployments[0]

	t.Logf("verifying environment variable TEST_ENV in deployment before update")
	container := k8sutils.GetPodContainerByName(&deployment.Spec.Template.Spec, consts.DataPlaneProxyContainerName)
	require.NotNil(t, container)
	testEnv := GetEnvValueByName(container.Env, "TEST_ENV")
	require.Equal(t, "before_update", testEnv)

	t.Logf("updating TEST_ENV in dataplane")
	require.Eventually(t,
		testutils.DataPlaneUpdateEventually(t, GetCtx(), dataplaneName, clients, func(dp *operatorv1beta1.DataPlane) {
			container := k8sutils.GetPodContainerByName(&dp.Spec.Deployment.PodTemplateSpec.Spec, consts.DataPlaneProxyContainerName)
			require.NotNil(t, container)
			container.Env = []corev1.EnvVar{
				{
					Name: "TEST_ENV", Value: "after_update",
				},
			}
		}),
		time.Minute, time.Second,
	)

	t.Logf("verifying environment variable TEST_ENV in deployment after update")
	require.Eventually(t, func() bool {
		deployments := testutils.MustListDataPlaneDeployments(t, GetCtx(), dataplane, clients, client.MatchingLabels{
			consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
		})
		require.Len(t, deployments, 1, "There must be only one DataPlane deployment")
		deployment := &deployments[0]

		container := k8sutils.GetPodContainerByName(&deployment.Spec.Template.Spec, consts.DataPlaneProxyContainerName)
		require.NotNil(t, container)
		testEnv := GetEnvValueByName(container.Env, "TEST_ENV")
		t.Logf("Tenvironment variable TEST_ENV is now %s in deployment", testEnv)
		return testEnv == "after_update"
	}, testutils.DataPlaneCondDeadline, testutils.DataPlaneCondTick)

	dataPlaneConditionPredicate := func(t *testing.T, c *metav1.Condition) func(dataplane *operatorv1beta1.DataPlane) bool {
		t.Helper()
		return func(dataplane *operatorv1beta1.DataPlane) bool {
			t.Helper()
			for _, condition := range dataplane.Status.Conditions {
				if condition.Type == c.Type && condition.Status == c.Status && condition.Reason == c.Reason && condition.ObservedGeneration == c.ObservedGeneration {
					return true
				}
				t.Logf("DataPlane %q condition: Type=%q;ObservedGeneration:%d,Reason:%q;Status:%q;Message:%q",
					dataplane.Name, condition.Type, condition.ObservedGeneration, condition.Reason, condition.Status, condition.Message,
				)
			}
			return false
		}
	}

	t.Run("dataplane is not Ready when the underlying deployment changes state to not Ready", func(t *testing.T) {
		require.Eventually(t,
			testutils.DataPlaneUpdateEventually(t, GetCtx(), dataplaneName, clients, func(dp *operatorv1beta1.DataPlane) {
				container := k8sutils.GetPodContainerByName(&dp.Spec.Deployment.PodTemplateSpec.Spec, consts.DataPlaneProxyContainerName)
				require.NotNil(t, container)
				container.ReadinessProbe = &corev1.Probe{
					InitialDelaySeconds: 0,
					PeriodSeconds:       1,
					FailureThreshold:    3,
					SuccessThreshold:    1,
					TimeoutSeconds:      1,
					ProbeHandler: corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{
							Path:   "/status_which_will_always_return_404",
							Port:   intstr.FromInt(consts.DataPlaneMetricsPort),
							Scheme: corev1.URISchemeHTTP,
						},
					},
				}
			}),
			time.Minute, time.Second,
		)

		// Get the dataplane after it's been updated to have an up to date generation which can be used in condition predicate.
		dataplane, err := clients.OperatorClient.GatewayOperatorV1beta1().DataPlanes(dataplaneName.Namespace).Get(GetCtx(), dataplane.Name, metav1.GetOptions{})
		require.NoError(t, err)

		isNotReady := dataPlaneConditionPredicate(t, &metav1.Condition{
			Type:               string(kcfgdataplane.ReadyType),
			Status:             metav1.ConditionFalse,
			Reason:             string(kcfgdataplane.WaitingToBecomeReadyReason),
			ObservedGeneration: dataplane.Generation,
		})
		require.Eventually(t,
			testutils.DataPlanePredicate(t, GetCtx(), dataplaneName, isNotReady, GetClients().OperatorClient),
			testutils.DataPlaneCondDeadline, testutils.DataPlaneCondTick,
		)
	})
	t.Run("dataplane gets Ready when the underlying deployment changes state to Ready", func(t *testing.T) {
		require.Eventually(t,
			testutils.DataPlaneUpdateEventually(t, GetCtx(), dataplaneName, clients, func(dp *operatorv1beta1.DataPlane) {
				container := k8sutils.GetPodContainerByName(&dp.Spec.Deployment.PodTemplateSpec.Spec, consts.DataPlaneProxyContainerName)
				require.NotNil(t, container)
				container.ReadinessProbe = &corev1.Probe{
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
				}
			}),
			time.Minute, time.Second,
		)

		// Get the dataplane after it's been updated to have an up to date generation which can be used in condition predicate.
		dataplane, err := clients.OperatorClient.GatewayOperatorV1beta1().DataPlanes(dataplaneName.Namespace).Get(GetCtx(), dataplane.Name, metav1.GetOptions{})
		require.NoError(t, err)

		isReady := dataPlaneConditionPredicate(t, &metav1.Condition{
			Type:               string(kcfgdataplane.ReadyType),
			Status:             metav1.ConditionTrue,
			Reason:             string(kcfgdataplane.ResourceReadyReason),
			ObservedGeneration: dataplane.Generation,
		})
		require.Eventually(t,
			testutils.DataPlanePredicate(t, GetCtx(), dataplaneName, isReady, GetClients().OperatorClient),
			testutils.DataPlaneCondDeadline, testutils.DataPlaneCondTick,
		)
	})

	t.Run("dataplane Ready condition gets properly update with correct ObservedGeneration", func(t *testing.T) {
		require.Eventually(t,
			testutils.DataPlaneUpdateEventually(t, GetCtx(), dataplaneName, clients, func(dp *operatorv1beta1.DataPlane) {
				container := k8sutils.GetPodContainerByName(&dp.Spec.Deployment.PodTemplateSpec.Spec, consts.DataPlaneProxyContainerName)
				require.NotNil(t, container)
				container.StartupProbe = &corev1.Probe{
					InitialDelaySeconds: 0,
					PeriodSeconds:       2,
					FailureThreshold:    30,
					ProbeHandler: corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{
							Path:   "/status",
							Port:   intstr.FromInt(consts.DataPlaneMetricsPort),
							Scheme: corev1.URISchemeHTTP,
						},
					},
				}
			}),
			time.Minute, time.Second,
		)

		// Get the dataplane after it's been updated to have an up to date generation which can be used in condition predicate.
		dataplane, err := clients.OperatorClient.GatewayOperatorV1beta1().DataPlanes(dataplaneName.Namespace).Get(GetCtx(), dataplane.Name, metav1.GetOptions{})
		require.NoError(t, err)

		isReady := dataPlaneConditionPredicate(t, &metav1.Condition{
			Type:               string(kcfgdataplane.ReadyType),
			Status:             metav1.ConditionTrue,
			Reason:             string(kcfgdataplane.ResourceReadyReason),
			ObservedGeneration: dataplane.Generation,
		})
		require.Eventually(t,
			testutils.DataPlanePredicate(t, GetCtx(), dataplaneName, isReady, GetClients().OperatorClient),
			testutils.DataPlaneCondDeadline, testutils.DataPlaneCondTick,
		)
	})

	t.Run("dataplane gets properly updated with a ReadinessProbe using port names instead of numbers", func(t *testing.T) {
		require.Eventually(t,
			testutils.DataPlaneUpdateEventually(t, GetCtx(), dataplaneName, clients, func(dp *operatorv1beta1.DataPlane) {
				container := k8sutils.GetPodContainerByName(&dp.Spec.Deployment.PodTemplateSpec.Spec, consts.DataPlaneProxyContainerName)
				require.NotNil(t, container)
				container.StartupProbe = &corev1.Probe{
					InitialDelaySeconds: 0,
					PeriodSeconds:       2,
					FailureThreshold:    30,
					ProbeHandler: corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{
							Path:   "/status",
							Port:   intstr.FromString("metrics"),
							Scheme: corev1.URISchemeHTTP,
						},
					},
				}
			}),
			time.Minute, time.Second,
		)

		// Get the dataplane after it's been updated to have an up to date generation which can be used in condition predicate.
		dataplane, err := clients.OperatorClient.GatewayOperatorV1beta1().DataPlanes(dataplaneName.Namespace).Get(GetCtx(), dataplane.Name, metav1.GetOptions{})
		require.NoError(t, err)

		isReady := dataPlaneConditionPredicate(t, &metav1.Condition{
			Type:               string(kcfgdataplane.ReadyType),
			Status:             metav1.ConditionTrue,
			Reason:             string(kcfgdataplane.ResourceReadyReason),
			ObservedGeneration: dataplane.Generation,
		})
		require.Eventually(t,
			testutils.DataPlanePredicate(t, GetCtx(), dataplaneName, isReady, GetClients().OperatorClient),
			testutils.DataPlaneCondDeadline, testutils.DataPlaneCondTick,
		)
	})
}

func TestDataPlaneHorizontalScaling(t *testing.T) {
	t.Parallel()
	namespace, cleaner := helpers.SetupTestEnv(t, GetCtx(), GetEnv())

	t.Log("deploying dataplane resource with 2 replicas")
	dataplane := &operatorv1beta1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    namespace.Name,
			GenerateName: "dataplane-",
		},
		Spec: operatorv1beta1.DataPlaneSpec{
			DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
				Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
					DeploymentOptions: operatorv1beta1.DeploymentOptions{
						Replicas: lo.ToPtr(int32(2)),
						PodTemplateSpec: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  consts.DataPlaneProxyContainerName,
										Image: helpers.GetDefaultDataPlaneImage(),
										ReadinessProbe: &corev1.Probe{
											InitialDelaySeconds: 1,
											PeriodSeconds:       1,
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

	dataplaneClient := GetClients().OperatorClient.GatewayOperatorV1beta1().DataPlanes(namespace.Name)

	dataplane, err := dataplaneClient.Create(GetCtx(), dataplane, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(dataplane)

	dataplaneName := client.ObjectKeyFromObject(dataplane)

	t.Log("verifying dataplane gets marked provisioned")
	require.Eventually(t, testutils.DataPlaneIsReady(t, GetCtx(), dataplaneName, GetClients().OperatorClient), waitTime, tickTime)

	t.Log("verifying deployments managed by the dataplane")
	deployment := &appsv1.Deployment{}
	require.Eventually(t, testutils.DataPlaneHasActiveDeployment(t, GetCtx(), dataplaneName, deployment, client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
	}, clients), waitTime, tickTime)

	t.Log("verifying that dataplane has indeed 2 ready replicas")
	require.Eventually(t, testutils.DataPlaneHasNReadyPods(t, GetCtx(), dataplaneName, clients, 2), waitTime, tickTime)

	t.Log("changing replicas in dataplane spec to 1 should scale down the deployment back to 1")
	require.Eventually(t,
		testutils.DataPlaneUpdateEventually(t, GetCtx(), dataplaneName, clients, func(dp *operatorv1beta1.DataPlane) { dp.Spec.Deployment.Replicas = lo.ToPtr(int32(1)) }),
		waitTime, tickTime)

	t.Log("verifying that dataplane has indeed 1 ready replica after scaling down")
	require.Eventually(t, testutils.DataPlaneHasNReadyPods(t, GetCtx(), dataplaneName, clients, 1), waitTime, tickTime)

	t.Log("changing from replicas to using autoscaling should create an HPA targeting the dataplane deployment")
	require.Eventually(t,
		testutils.DataPlaneUpdateEventually(t, GetCtx(), dataplaneName, clients, func(dp *operatorv1beta1.DataPlane) {
			dp.Spec.Deployment.Scaling = &operatorv1beta1.Scaling{
				HorizontalScaling: &operatorv1beta1.HorizontalScaling{
					MaxReplicas: 3,
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
			dp.Spec.Deployment.Replicas = nil
		}),
		waitTime, tickTime)

	{
		var hpa autoscalingv2.HorizontalPodAutoscaler
		require.Eventually(t, testutils.DataPlaneHasHPA(t, GetCtx(), dataplane, &hpa, clients), waitTime, tickTime)
		require.NotNil(t, hpa)
		assert.Equal(t, int32(3), hpa.Spec.MaxReplicas)
		require.Len(t, hpa.Spec.Metrics, 1)
		require.NotNil(t, hpa.Spec.Metrics[0].Resource)
		require.NotNil(t, hpa.Spec.Metrics[0].Resource.Target.AverageUtilization)
		assert.Equal(t, int32(20), *hpa.Spec.Metrics[0].Resource.Target.AverageUtilization)
	}

	t.Log("updating the horizontal scaling spec should update the relevant HPA")
	require.Eventuallyf(t,
		testutils.DataPlaneUpdateEventually(t, GetCtx(), dataplaneName, clients, func(dp *operatorv1beta1.DataPlane) {
			dp.Spec.Deployment.Scaling.HorizontalScaling.MaxReplicas = 5
			dp.Spec.Deployment.Scaling.HorizontalScaling.Metrics[0].Resource.Target.AverageUtilization = lo.ToPtr(int32(50))
			dp.Spec.Deployment.Replicas = nil
		}),
		time.Minute, time.Second, "failed to update dataplane %s", dataplane.Name)
	require.Eventuallyf(t, func() bool {
		hpas := testutils.MustListDataPlaneHPAs(t, GetCtx(), dataplane, clients, client.MatchingLabels{
			consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
		})
		if len(hpas) != 1 {
			return false
		}

		hpa := hpas[0]
		if hpa.Spec.MaxReplicas != 5 {
			return false
		}
		if len(hpa.Spec.Metrics) != 1 || hpa.Spec.Metrics[0].Resource == nil ||
			hpa.Spec.Metrics[0].Resource.Target.AverageUtilization == nil ||
			*hpa.Spec.Metrics[0].Resource.Target.AverageUtilization != 50 {
			return false
		}
		return true
	}, time.Minute, time.Second, "HPA for dataplane %s not found or not as expected", dataplane.Name)

	t.Log("removing the horizontal scaling spec should delete the relevant HPA")
	require.Eventuallyf(t,
		testutils.DataPlaneUpdateEventually(t, GetCtx(), dataplaneName, clients, func(dp *operatorv1beta1.DataPlane) {
			dp.Spec.Deployment.Scaling = nil
		}),
		time.Minute, time.Second, "failed to update dataplane %s", dataplane.Name)
	require.Eventuallyf(t, func() bool {
		hpas := testutils.MustListDataPlaneHPAs(t, GetCtx(), dataplane, clients, client.MatchingLabels{
			consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
		})
		return len(hpas) == 0
	}, time.Minute, time.Second, "HPA for dataplane %s found but should be deleted", dataplane.Name)
}

func TestDataPlaneVolumeMounts(t *testing.T) {
	t.Parallel()
	namespace, cleaner := helpers.SetupTestEnv(t, GetCtx(), GetEnv())

	t.Log("creating a secret to mount to dataplane containers")
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    namespace.Name,
			GenerateName: "secret-",
		},
		StringData: map[string]string{
			"file-0": "foo",
		},
	}
	secret, err := GetClients().K8sClient.CoreV1().Secrets(namespace.Name).Create(GetCtx(), secret, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(secret)

	t.Log("deploying dataplane resource")
	dataplane := &operatorv1beta1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    namespace.Name,
			GenerateName: "dataplane-",
		},
		Spec: operatorv1beta1.DataPlaneSpec{
			DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
				Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
					DeploymentOptions: operatorv1beta1.DeploymentOptions{
						PodTemplateSpec: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Volumes: []corev1.Volume{
									{
										Name: consts.ClusterCertificateVolume,
									},
									{
										Name: "test-volume",
										VolumeSource: corev1.VolumeSource{
											Secret: &corev1.SecretVolumeSource{
												SecretName: secret.Name,
											},
										},
									},
								},
								Containers: []corev1.Container{
									{
										Name:  consts.DataPlaneProxyContainerName,
										Image: helpers.GetDefaultDataPlaneImage(),
										VolumeMounts: []corev1.VolumeMount{
											{
												Name:      consts.ClusterCertificateVolume,
												MountPath: consts.ClusterCertificateVolumeMountPath,
												ReadOnly:  true,
											},
											{
												Name:      "test-volume",
												MountPath: "/var/test",
												ReadOnly:  true,
											},
										},
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
		},
	}
	dataplane, err = GetClients().OperatorClient.GatewayOperatorV1beta1().DataPlanes(namespace.Name).Create(GetCtx(), dataplane, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(dataplane)

	dataplaneName := client.ObjectKeyFromObject(dataplane)

	t.Log("verifying that the dataplane gets marked as Ready")
	require.Eventually(t, testutils.DataPlaneIsReady(t, GetCtx(), dataplaneName, GetClients().OperatorClient), waitTime, tickTime)

	t.Log("verifying deployments managed by the dataplane")
	require.Eventually(t, testutils.DataPlaneHasActiveDeployment(t, GetCtx(), dataplaneName, &appsv1.Deployment{}, client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
	}, clients), waitTime, tickTime)

	t.Log("verifying dataplane deployment volume mounts")
	deployments := testutils.MustListDataPlaneDeployments(t, GetCtx(), dataplane, clients, client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
	})
	require.Len(t, deployments, 1, "There must be only one DataPlane deployment")
	deployment := &deployments[0]

	proxyContainer := k8sutils.GetPodContainerByName(
		&deployment.Spec.Template.Spec, consts.DataPlaneProxyContainerName)
	require.NotNil(t, proxyContainer)

	t.Log("verifying dataplane has the default cluster-certificate volume")
	defaultVol := GetVolumeByName(deployment.Spec.Template.Spec.Volumes, consts.ClusterCertificateVolume)
	require.NotNil(t, defaultVol, "dataplane pod should have the cluster-certificate volume")

	t.Log("verifying dataplane has the custom test-volume volume")
	vol := GetVolumeByName(deployment.Spec.Template.Spec.Volumes, "test-volume")
	require.NotNil(t, vol, "dataplane pod should have the test-volume volume")
	require.NotNil(t, vol.Secret, "test-volume volume should come from secret")

	t.Log("verifying Kong proxy container has mounted the default cluster-certificate volume")
	defVolumeMounts := GetVolumeMountsByVolumeName(proxyContainer.VolumeMounts, consts.ClusterCertificateVolume)
	require.Len(t, defVolumeMounts, 1, "proxy container should mount cluster-certificate volume")
	require.Equal(t, consts.ClusterCertificateVolumeMountPath, defVolumeMounts[0].MountPath, "proxy container should mount cluster-certificate volume to path /var/cluster-certificate")
	require.True(t, defVolumeMounts[0].ReadOnly, "proxy container should mount cluster-certificate volume in read only mode")

	t.Log("verifying Kong proxy container has mounted the custom test-volume volume")
	volMounts := GetVolumeMountsByVolumeName(proxyContainer.VolumeMounts, "test-volume")
	require.Len(t, volMounts, 1, "proxy container should mount test-volume volume")
	require.Equal(t, "/var/test", volMounts[0].MountPath, "proxy container should mount test-volume volume to path /var/test")
	require.True(t, volMounts[0].ReadOnly, "proxy container should mount test-volume volume in read only mode")

	t.Log("verifying dataplane pod has custom secret volume")
	vol = GetVolumeByName(deployment.Spec.Template.Spec.Volumes, "test-volume")
	require.NotNil(t, vol, "dataplane pod should have the volume test-volume")
	require.NotNil(t, vol.Secret, "test-volume should come from secret")
	require.Equalf(t, vol.Secret.SecretName, secret.Name, "test-volume should come from secret %s", secret.Name)

	t.Log("verifying Kong proxy container has mounted custom secret volume")
	volMounts = GetVolumeMountsByVolumeName(proxyContainer.VolumeMounts, "test-volume")
	require.Len(t, volMounts, 1, "proxy container should mount custom secret volume 'test-volume'")
	require.Equal(t, "/var/test", volMounts[0].MountPath, "proxy container should mount custom secret volume to path /var/test")
	require.True(t, volMounts[0].ReadOnly, "proxy container should mount 'test-volume' in read only mode")

	t.Log("updating volumes and volume mounts in dataplane spec to verify volumes could be reconciled")
	dataplane.Spec.Deployment.PodTemplateSpec.Spec.Volumes[0] = corev1.Volume{
		Name: "test-volume-1",
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: secret.Name,
			},
		},
	}
	dataplane.Spec.DataPlaneOptions.Deployment.PodTemplateSpec.Spec.Containers[0].VolumeMounts[0] = corev1.VolumeMount{
		Name:      "test-volume-1",
		MountPath: "/var/test",
		ReadOnly:  true,
	}
	_, err = GetClients().K8sClient.AppsV1().Deployments(namespace.Name).Update(GetCtx(), deployment, metav1.UpdateOptions{})
	require.NoError(t, err, "should update deployment successfully")

	require.Eventually(t, func() bool {
		deployment, err = GetClients().K8sClient.AppsV1().Deployments(namespace.Name).Get(GetCtx(), deployment.Name, metav1.GetOptions{})
		require.NoError(t, err, "should get dataplane deployment successfully")
		vol := GetVolumeByName(deployment.Spec.Template.Spec.Volumes, "test-volume")
		if vol == nil || vol.Secret == nil || vol.Secret.SecretName != secret.Name {
			return false
		}
		proxyContainer := k8sutils.GetPodContainerByName(
			&deployment.Spec.Template.Spec, consts.DataPlaneProxyContainerName)
		require.NotNilf(t, proxyContainer, "dataplane deployment should have container %s", consts.DataPlaneProxyContainerName)
		volMounts = GetVolumeMountsByVolumeName(proxyContainer.VolumeMounts, "test-volume")
		if len(volMounts) != 1 {
			return false
		}
		if volMounts[0].MountPath != "/var/test" || !volMounts[0].ReadOnly {
			return false
		}
		return true
	}, waitTime, tickTime)
}

func TestDataPlanePodDisruptionBudget(t *testing.T) {
	t.Parallel()
	namespace, cleaner := helpers.SetupTestEnv(t, GetCtx(), GetEnv())

	t.Log("deploying DataPlane resource with 2 replicas")
	dataplane := &operatorv1beta1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    namespace.Name,
			GenerateName: "dataplane-",
		},
		Spec: operatorv1beta1.DataPlaneSpec{
			DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
				Resources: operatorv1beta1.DataPlaneResources{
					PodDisruptionBudget: &operatorv1beta1.PodDisruptionBudget{
						Spec: operatorv1beta1.PodDisruptionBudgetSpec{
							MinAvailable: lo.ToPtr(intstr.FromInt32(1)),
						},
					},
				},
				Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
					DeploymentOptions: operatorv1beta1.DeploymentOptions{
						Replicas: lo.ToPtr(int32(2)),
						PodTemplateSpec: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  consts.DataPlaneProxyContainerName,
										Image: helpers.GetDefaultDataPlaneImage(),
										ReadinessProbe: &corev1.Probe{
											InitialDelaySeconds: 1,
											PeriodSeconds:       1,
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

	dataplaneClient := GetClients().OperatorClient.GatewayOperatorV1beta1().DataPlanes(namespace.Name)

	dataplane, err := dataplaneClient.Create(GetCtx(), dataplane, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(dataplane)

	dataplaneName := client.ObjectKeyFromObject(dataplane)

	t.Log("verifying DataPlane gets marked provisioned")
	require.Eventually(t, testutils.DataPlaneIsReady(t, GetCtx(), dataplaneName, GetClients().OperatorClient), waitTime, tickTime)

	t.Log("verifying deployments managed by the DataPlane")
	deployment := &appsv1.Deployment{}
	require.Eventually(t, testutils.DataPlaneHasActiveDeployment(t, GetCtx(), dataplaneName, deployment, client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
	}, clients), waitTime, tickTime)

	t.Log("verifying that DataPlane has indeed 2 ready replicas")
	require.Eventually(t, testutils.DataPlaneHasNReadyPods(t, GetCtx(), dataplaneName, clients, 2), waitTime, tickTime)

	t.Log("verifying that the PodDisruptionBudget is created")
	pdb := policyv1.PodDisruptionBudget{}
	require.Eventually(t, testutils.DataPlaneHasPodDisruptionBudget(t, GetCtx(), dataplane, &pdb, clients, testutils.AnyPodDisruptionBudget()), waitTime, tickTime)

	t.Log("verifying the PodDisruptionBudget status is as expected")
	assert.EqualValues(t, 2, pdb.Status.ExpectedPods)
	assert.EqualValues(t, 1, pdb.Status.DesiredHealthy)
	assert.EqualValues(t, 1, pdb.Status.DisruptionsAllowed)

	t.Log("changing the PodDisruptionBudget spec in DataPlane")
	require.Eventually(t, testutils.DataPlaneUpdateEventually(t, GetCtx(), dataplaneName, clients, func(dp *operatorv1beta1.DataPlane) {
		dp.Spec.Resources.PodDisruptionBudget.Spec.MinAvailable = lo.ToPtr(intstr.FromInt32(2))
	}), waitTime, tickTime)

	t.Log("verifying the PodDisruptionBudget status is updated accordingly")
	require.Eventually(t, testutils.DataPlaneHasPodDisruptionBudget(t, GetCtx(), dataplane, &pdb, clients, func(pdb policyv1.PodDisruptionBudget) bool {
		return pdb.Status.ExpectedPods == int32(2) &&
			pdb.Status.DesiredHealthy == int32(2) &&
			pdb.Status.DisruptionsAllowed == int32(0)
	}), waitTime, tickTime)

	t.Log("removing the PodDisruptionBudget spec in DataPlane")
	require.Eventually(t, testutils.DataPlaneUpdateEventually(t, GetCtx(), dataplaneName, clients, func(dp *operatorv1beta1.DataPlane) {
		dp.Spec.Resources.PodDisruptionBudget = nil
	}), waitTime, tickTime)

	t.Log("verifying the PodDisruptionBudget is deleted")
	eventually.WaitForObjectToNotExist(t, ctx, GetClients().MgrClient, &pdb, waitTime, tickTime)
}

func TestDataPlaneServiceExternalTrafficPolicy(t *testing.T) {
	t.Parallel()
	namespace, cleaner := helpers.SetupTestEnv(t, GetCtx(), GetEnv())

	t.Log("deploying DataPlane resource with 2 replicas")
	dataplane := &operatorv1beta1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    namespace.Name,
			GenerateName: "dataplane-",
		},
		Spec: operatorv1beta1.DataPlaneSpec{
			DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
				Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
					DeploymentOptions: operatorv1beta1.DeploymentOptions{
						PodTemplateSpec: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  consts.DataPlaneProxyContainerName,
										Image: helpers.GetDefaultDataPlaneImage(),
										ReadinessProbe: &corev1.Probe{
											InitialDelaySeconds: 1,
											PeriodSeconds:       1,
										},
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
								ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyLocal,
							},
						},
					},
				},
			},
		},
	}

	verifyEventuallyExternalTrafficPolicy := func(
		t *testing.T,
		dataplaneName types.NamespacedName,
		expectedPolicy corev1.ServiceExternalTrafficPolicy,
	) {
		t.Helper()

		require.Eventually(t, testutils.DataPlaneHasService(t, GetCtx(), dataplaneName, clients,
			client.MatchingLabels{
				consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
				consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneIngressServiceLabelValue),
			},
			func(svc corev1.Service) bool {
				return svc.Spec.ExternalTrafficPolicy == expectedPolicy
			},
		), waitTime, tickTime)
	}

	dataplaneClient := GetClients().OperatorClient.GatewayOperatorV1beta1().DataPlanes(namespace.Name)

	dataplane, err := dataplaneClient.Create(GetCtx(), dataplane, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(dataplane)

	dataplaneName := client.ObjectKeyFromObject(dataplane)

	t.Log("verifying DataPlane gets marked provisioned")
	require.Eventually(t, testutils.DataPlaneIsReady(t, GetCtx(), dataplaneName, GetClients().OperatorClient), waitTime, tickTime)

	t.Log("verifying deployments managed by the DataPlane")
	deployment := &appsv1.Deployment{}
	require.Eventually(t, testutils.DataPlaneHasActiveDeployment(t, GetCtx(), dataplaneName, deployment, client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
	}, clients), waitTime, tickTime)

	t.Log("verifying the DataPlane Service ExternalTrafficPolicy is updated to Local")
	verifyEventuallyExternalTrafficPolicy(t, dataplaneName, corev1.ServiceExternalTrafficPolicyLocal)

	t.Log("setting DataPlane Service ExternalTrafficPolicy to Cluster")
	require.Eventually(t, testutils.DataPlaneUpdateEventually(t, GetCtx(), dataplaneName, clients,
		func(dp *operatorv1beta1.DataPlane) {
			dp.Spec.Network.Services.Ingress.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicyCluster
		},
	), waitTime, tickTime)

	t.Log("verifying the DataPlane Service ExternalTrafficPolicy is updated to Cluster")
	verifyEventuallyExternalTrafficPolicy(t, dataplaneName, corev1.ServiceExternalTrafficPolicyCluster)

	t.Log("changing in ExternalTrafficPolicy to Local")
	require.Eventually(t, testutils.DataPlaneUpdateEventually(t, GetCtx(), dataplaneName, clients, func(dp *operatorv1beta1.DataPlane) {
		dp.Spec.Network.Services = &operatorv1beta1.DataPlaneServices{
			Ingress: &operatorv1beta1.DataPlaneServiceOptions{
				ServiceOptions: operatorv1beta1.ServiceOptions{
					ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyLocal,
				},
			},
		}
	}), waitTime, tickTime)
	verifyEventuallyExternalTrafficPolicy(t, dataplaneName, corev1.ServiceExternalTrafficPolicyLocal)
}

func TestDataPlaneSpecifyingServiceName(t *testing.T) {
	t.Parallel()
	namespace, cleaner := helpers.SetupTestEnv(t, GetCtx(), GetEnv())

	serviceName := "ingress-service-" + uuid.NewString()
	t.Logf("deploying dataplane resource with service name specified to %s", serviceName)
	dataplane := &operatorv1beta1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    namespace.Name,
			GenerateName: "dataplane-",
		},
		Spec: operatorv1beta1.DataPlaneSpec{
			DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
				Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
					DeploymentOptions: operatorv1beta1.DeploymentOptions{
						PodTemplateSpec: &corev1.PodTemplateSpec{
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
								Name: &serviceName,
							},
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

	dataplaneName := client.ObjectKeyFromObject(dataplane)

	t.Log("verifying that dataplane is provisioned")
	require.Eventually(t, testutils.DataPlaneIsReady(t, GetCtx(), dataplaneName, GetClients().OperatorClient), waitTime, tickTime)

	t.Logf("verifying that ingress service with the specified name '%s' is created", serviceName)
	require.Eventually(t, testutils.DataPlaneHasActiveServiceWithName(t, GetCtx(), dataplaneName, nil, clients, client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
		consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneIngressServiceLabelValue),
	}, serviceName,
	), waitTime, tickTime)

	t.Log("verifying dataplane services receive IP addresses")
	var dataplaneIP string
	require.Eventually(t, func() bool {
		dataplaneService, err := GetClients().K8sClient.CoreV1().Services(dataplane.Namespace).Get(GetCtx(), serviceName, metav1.GetOptions{})
		require.NoError(t, err)
		if len(dataplaneService.Status.LoadBalancer.Ingress) > 0 {
			dataplaneIP = dataplaneService.Status.LoadBalancer.Ingress[0].IP
			return true
		}
		return false
	}, waitTime, tickTime)

	require.Eventually(t, Expect404WithNoRouteFunc(t, GetCtx(), "http://"+dataplaneIP), waitTime, tickTime)

	oldServiceName := serviceName
	serviceName = "ingress-service-" + uuid.NewString()
	t.Logf("updating ingress service name from '%s' to '%s' in dataplane", oldServiceName, serviceName)
	require.Eventually(t,
		testutils.DataPlaneUpdateEventually(t, GetCtx(), dataplaneName, clients, func(dp *operatorv1beta1.DataPlane) {
			dp.Spec.Network.Services.Ingress.Name = &serviceName
		}),
		time.Minute, time.Second,
	)

	t.Logf("verifying that ingress service with the new name '%s' is created", serviceName)
	require.Eventually(t, testutils.DataPlaneHasActiveServiceWithName(t, GetCtx(), dataplaneName, nil, clients, client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
		consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneIngressServiceLabelValue),
	}, serviceName,
	), waitTime, tickTime)

	t.Logf("verifying that the old ingress service '%s' is deleted", oldServiceName)
	require.Eventually(t, func() bool {
		_, err := clients.K8sClient.CoreV1().Services(dataplane.Namespace).Get(GetCtx(), oldServiceName, metav1.GetOptions{})
		return err != nil && k8serrors.IsNotFound(err)
	}, waitTime, tickTime)

	t.Log("verifying dataplane services receive IP addresses after service name is updated")
	require.Eventually(t, func() bool {
		dataplaneService, err := GetClients().K8sClient.CoreV1().Services(dataplane.Namespace).Get(GetCtx(), serviceName, metav1.GetOptions{})
		require.NoError(t, err)
		if len(dataplaneService.Status.LoadBalancer.Ingress) > 0 {
			dataplaneIP = dataplaneService.Status.LoadBalancer.Ingress[0].IP
			return true
		}
		return false
	}, waitTime, tickTime)

	require.Eventually(t, Expect404WithNoRouteFunc(t, GetCtx(), "http://"+dataplaneIP), waitTime, tickTime)
}

func TestDataPlaneKonnectCert(t *testing.T) {
	t.Parallel()
	namespace, cleaner := helpers.SetupTestEnv(t, GetCtx(), GetEnv())

	t.Log("deploying dataplane resource")
	dataplaneName := types.NamespacedName{
		Namespace: namespace.Name,
		Name:      uuid.NewString(),
	}
	issuer := &certmanagerv1.ClusterIssuer{
		ObjectMeta: metav1.ObjectMeta{
			Name: "fake-cluster-issuer",
		},
		Spec: certmanagerv1.IssuerSpec{
			IssuerConfig: certmanagerv1.IssuerConfig{
				SelfSigned: &certmanagerv1.SelfSignedIssuer{},
			},
		},
	}
	certClient, err := certmanagerv1client.NewForConfig(GetEnv().Cluster().Config())
	require.NoError(t, err)
	_, err = certClient.ClusterIssuers().Create(GetCtx(), issuer, metav1.CreateOptions{})
	require.NoError(t, err)
	dataplane := &operatorv1beta1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: dataplaneName.Namespace,
			Name:      dataplaneName.Name,
		},
		Spec: operatorv1beta1.DataPlaneSpec{
			DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
				Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
					DeploymentOptions: operatorv1beta1.DeploymentOptions{
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
				Network: operatorv1beta1.DataPlaneNetworkOptions{
					KonnectCertificateOptions: &operatorv1beta1.KonnectCertificateOptions{
						Issuer: operatorv1beta1.NamespacedName{
							Name: "fake-cluster-issuer",
						},
					},
				},
			},
		},
	}

	dataplaneClient := GetClients().OperatorClient.GatewayOperatorV1beta1().DataPlanes(namespace.Name)
	dataplane, err = dataplaneClient.Create(GetCtx(), dataplane, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(dataplane)

	t.Log("verifying dataplane gets marked provisioned")
	require.Eventually(t, testutils.DataPlaneIsReady(t, GetCtx(), dataplaneName, GetClients().OperatorClient), time.Minute, time.Second)

	t.Log("verifying deployments managed by the dataplane")
	require.Eventually(t, testutils.DataPlaneHasActiveDeployment(t, GetCtx(), dataplaneName, &appsv1.Deployment{}, client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
	}, GetClients()), time.Minute*2, time.Second)

	t.Log("verifying dataplane Deployment.Pods.Env vars")
	deployments := testutils.MustListDataPlaneDeployments(t, GetCtx(), dataplane, GetClients(), client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
	})
	require.Len(t, deployments, 1, "There must be only one DataPlane deployment")
	deployment := &deployments[0]

	proxyContainer := k8sutils.GetPodContainerByName(
		&deployment.Spec.Template.Spec, consts.DataPlaneProxyContainerName)
	require.NotNil(t, proxyContainer)
	envs := proxyContainer.Env

	certEnv := GetEnvValueByName(envs, consts.ClusterCertEnvKey)
	keyEnv := GetEnvValueByName(envs, consts.ClusterCertKeyEnvKey)
	require.Equal(t, certificates.DataPlaneKonnectClientCertificatePath+"tls.crt", certEnv)
	require.Equal(t, certificates.DataPlaneKonnectClientCertificatePath+"tls.key", keyEnv)

	require.NotEmpty(t, GetVolumeByName(deployment.Spec.Template.Spec.Volumes, certificates.DataPlaneKonnectClientCertificateName))
	mount := GetVolumeMountsByVolumeName(deployment.Spec.Template.Spec.Containers[0].VolumeMounts, certificates.DataPlaneKonnectClientCertificateName)[0]
	require.Equal(t, certificates.DataPlaneKonnectClientCertificatePath, mount.MountPath)
}

func TestDataPlaneWithKonnectExtension(t *testing.T) {
	t.Parallel()
	namespace, cleaner := helpers.SetupTestEnv(t, GetCtx(), GetEnv())

	// Generate a test ID for labeling resources
	// in order to easily identify them in Konnect
	testID := uuid.NewString()[:8]
	t.Logf("Test ID: %s", testID)

	ctx := GetCtx()

	// Build a namespaced client for convenience
	clientNamespaced := client.NewNamespacedClient(GetClients().MgrClient, namespace.Name)

	// Create a KonnectAPIAuthConfiguration
	// using the token from the test environment
	// and the Konnect server URL from the test environment
	authCfg := deploy.KonnectAPIAuthConfiguration(t, GetCtx(), clientNamespaced,
		deploy.WithTestIDLabel(testID),
		func(obj client.Object) {
			authCfg := obj.(*konnectv1alpha1.KonnectAPIAuthConfiguration)
			authCfg.Spec.Type = konnectv1alpha1.KonnectAPIAuthTypeToken
			authCfg.Spec.Token = test.KonnectAccessToken()
			authCfg.Spec.ServerURL = test.KonnectServerURL()
		},
	)

	// Deploy a KonnectGatewayControlPlane
	// that will be referenced by the KonnectExtension
	// and that will be automatically registered in Konnect
	// thanks to the presence of the KonnectAPIAuthConfiguration
	cp := deploy.KonnectGatewayControlPlane(t, GetCtx(), clientNamespaced, authCfg,
		deploy.WithTestIDLabel(testID),
		deploy.KonnectGatewayControlPlaneLabel(deploy.KonnectTestIDLabel, testID),
	)
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, cp.DeepCopy()))

	t.Logf("Waiting for a Konnect ID for KonnectGatewayControlPlane %s/%s", cp.Namespace, cp.Name)
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: cp.Name, Namespace: cp.Namespace}, cp)
		require.NoError(t, err)
		assertKonnectEntityProgrammed(t, cp)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

	// Creating a KonnectExtension that references the ControlPlane created above
	konnectExtension := deploy.KonnectExtension(
		t, ctx, clientNamespaced,
		deploy.WithKonnectExtensionKonnectNamespacedRefControlPlaneRef(cp),
	)
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, konnectExtension.DeepCopy()))

	t.Logf("Waiting for KonnectExtension %s/%s to have all conditions set to True", konnectExtension.Namespace, konnectExtension.Name)
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		ok, msg := checkKonnectExtensionConditions(t,
			konnectExtension,
			helpers.CheckAllConditionsTrue,
			konnectv1alpha1.ControlPlaneRefValidConditionType,
			konnectv1alpha1.DataPlaneCertificateProvisionedConditionType,
			konnectv1alpha2.KonnectExtensionReadyConditionType)
		assert.Truef(t, ok, "condition check failed: %s, conditions: %+v", msg, konnectExtension.Status.Conditions)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

	t.Logf("Waiting for status.konnect and status.dataPlaneClientAuth to be set for KonnectExtension %s/%s", konnectExtension.Namespace, konnectExtension.Name)
	require.EventuallyWithT(t,
		checkKonnectExtensionStatus(konnectExtension, cp.GetKonnectID(), ""),
		testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

	// Now creating a DataPlane that uses the KonnectExtension according to the provided manifest
	t.Log("Creating a DataPlane that uses the KonnectExtension")
	dataplane := &operatorv1beta1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace.Name,
			Name:      "dataplane-prod",
		},
		Spec: operatorv1beta1.DataPlaneSpec{
			DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
				Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
					DeploymentOptions: operatorv1beta1.DeploymentOptions{
						PodTemplateSpec: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  consts.DataPlaneProxyContainerName,
										Image: helpers.GetDefaultDataPlaneImage(),
										Env: []corev1.EnvVar{
											{
												Name:  "TEST_ENV",
												Value: "test",
											},
										},
										VolumeMounts: []corev1.VolumeMount{
											{
												Name:      "custom-vol",
												MountPath: "/usr/local/lib/custom",
											},
										},
									},
								},
								Volumes: []corev1.Volume{
									{
										Name: "custom-vol",
										VolumeSource: corev1.VolumeSource{
											EmptyDir: &corev1.EmptyDirVolumeSource{},
										},
									},
								},
							},
						},
					},
				},
				Extensions: []commonv1alpha1.ExtensionRef{
					{
						Group: konnectv1alpha1.GroupVersion.Group,
						Kind:  "KonnectExtension",
						NamespacedRef: commonv1alpha1.NamespacedRef{
							Name: konnectExtension.Name,
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

	dataplaneName := client.ObjectKeyFromObject(dataplane)

	t.Log("Verifying that the dataplane is marked as ready")
	require.Eventually(t, testutils.DataPlaneIsReady(t, GetCtx(), dataplaneName, GetClients().OperatorClient), waitTime, tickTime)

	t.Log("Verifying deployments managed by the dataplane")
	require.Eventually(t, testutils.DataPlaneHasActiveDeployment(t, GetCtx(), dataplaneName, &appsv1.Deployment{}, client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
	}, clients), waitTime, tickTime)

	t.Log("Verifying that the dataplane service receives IP addresses")
	var dataplaneIngressService corev1.Service
	require.Eventually(t, testutils.DataPlaneHasActiveService(t, GetCtx(), dataplaneName, &dataplaneIngressService, clients, client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
		consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneIngressServiceLabelValue),
	}), waitTime, tickTime)

	var dataplaneIP string
	require.Eventually(t, func() bool {
		dataplaneService, err := GetClients().K8sClient.CoreV1().Services(dataplane.Namespace).Get(GetCtx(), dataplaneIngressService.Name, metav1.GetOptions{})
		require.NoError(t, err)
		if len(dataplaneService.Status.LoadBalancer.Ingress) > 0 {
			dataplaneIP = dataplaneService.Status.LoadBalancer.Ingress[0].IP
			return true
		}
		return false
	}, waitTime, tickTime)

	require.Eventually(t, Expect404WithNoRouteFunc(t, GetCtx(), "http://"+dataplaneIP), waitTime, tickTime)

	// Verify that the custom volume is configured correctly
	t.Log("Verifying that the custom volume is configured correctly")
	deployments := testutils.MustListDataPlaneDeployments(t, GetCtx(), dataplane, clients, client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
	})
	require.Len(t, deployments, 1, "There must be only one DataPlane deployment")
	deployment := &deployments[0]

	// Verify the custom volume
	customVol := GetVolumeByName(deployment.Spec.Template.Spec.Volumes, "custom-vol")
	require.NotNil(t, customVol, "The dataplane pod should have the custom-vol volume")
	require.NotNil(t, customVol.EmptyDir, "custom-vol should be an emptyDir volume")

	// Verify the custom volume mount and env var in the proxy container
	proxyContainer := k8sutils.GetPodContainerByName(
		&deployment.Spec.Template.Spec, consts.DataPlaneProxyContainerName)
	require.NotNil(t, proxyContainer)
	// Check that the TEST_ENV env var is set
	envs := proxyContainer.Env
	testEnv := GetEnvValueByName(envs, "TEST_ENV")
	require.Equal(t, "test", testEnv, "The TEST_ENV environment variable should be set to 'test' in the proxy container")
	// Check for the custom volume mount
	customVolMount := GetVolumeMountsByVolumeName(proxyContainer.VolumeMounts, "custom-vol")
	require.Len(t, customVolMount, 1, "The proxy container should mount the custom-vol volume")
	require.Equal(t, "/usr/local/lib/custom", customVolMount[0].MountPath, "The proxy container should mount custom-vol at path /usr/local/lib/custom")

	// Verify dataplane status
	t.Log("Verifying that the dataplane status is correctly populated with the backup service name and its addresses")
	require.Eventually(t, testutils.DataPlaneHasServiceAndAddressesInStatus(t, GetCtx(), dataplaneName, clients), waitTime, tickTime)
}
