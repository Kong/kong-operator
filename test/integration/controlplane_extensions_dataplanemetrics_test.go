package integration

import (
	"testing"
	"time"

	"github.com/kong/kubernetes-testing-framework/pkg/utils/kubernetes/generators"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	operatorv2beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v2beta1"
	"github.com/kong/kong-operator/v2/pkg/consts"
	osstestutils "github.com/kong/kong-operator/v2/pkg/utils/test"
	"github.com/kong/kong-operator/v2/pkg/vars"
	osshelpers "github.com/kong/kong-operator/v2/test/helpers"
)

func TestControlPlaneExtensionsDataPlaneMetrics(t *testing.T) {
	t.Parallel()

	createExtensionRefWithoutNamespace := func(extRefName string) commonv1alpha1.ExtensionRef {
		return commonv1alpha1.ExtensionRef{
			Group: operatorv1alpha1.SchemeGroupVersion.Group,
			Kind:  operatorv1alpha1.DataPlaneMetricsExtensionKind,
			NamespacedRef: commonv1alpha1.NamespacedRef{
				Name: extRefName,
			},
		}
	}

	const (
		waitTimeout = 3 * time.Minute
		interval    = time.Second
	)

	ctx := GetCtx()
	namespace, cleaner := osshelpers.SetupTestEnv(t, ctx, GetEnv())

	clients := GetClients()
	operatorClient := clients.OperatorClient
	gwClient := clients.GatewayClient
	mgrClient := clients.MgrClient
	k8sClient := clients.K8sClient

	t.Log("deploying a minimal HTTP container deployment to test Ingress routes")
	container := generators.NewContainer("httpbin", osstestutils.HTTPBinImage, 80)
	deployment := generators.NewDeploymentForContainer(container)
	deployment, err := k8sClient.AppsV1().Deployments(namespace.Name).Create(ctx, deployment, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(deployment)

	t.Logf("exposing deployment %s via service", deployment.Name)
	service := generators.NewServiceForDeployment(deployment, corev1.ServiceTypeLoadBalancer)
	service, err = k8sClient.CoreV1().Services(namespace.Name).Create(ctx, service, metav1.CreateOptions{})
	require.NoError(t, err)
	t.Logf("service %s created", service.Name)
	cleaner.Add(service)

	dpMetricExt1 := &operatorv1alpha1.DataPlaneMetricsExtension{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "dataplane-metrics-ext-",
		},
		Spec: operatorv1alpha1.DataPlaneMetricsExtensionSpec{
			ServiceSelector: operatorv1alpha1.ServiceSelector{
				MatchNames: []operatorv1alpha1.ServiceSelectorEntry{
					{
						Name: service.Name,
					},
				},
			},
			Config: operatorv1alpha1.MetricsConfig{
				Latency: true,
			},
		},
	}
	dbMetricExt1, err := operatorClient.GatewayOperatorV1alpha1().DataPlaneMetricsExtensions(namespace.Name).Create(ctx, dpMetricExt1, metav1.CreateOptions{})
	require.NoError(t, err)
	t.Logf("DataPlaneMetricsExtension %s created", dbMetricExt1.Name)
	cleaner.Add(dbMetricExt1)

	t.Log("deploying a GatewayConfiguration resource")
	gatewayConfig := &operatorv2beta1.GatewayConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "gwconfig-",
			Namespace:    namespace.Name,
		},
		Spec: operatorv2beta1.GatewayConfigurationSpec{
			DataPlaneOptions: &operatorv2beta1.GatewayConfigDataPlaneOptions{
				Deployment: operatorv2beta1.DataPlaneDeploymentOptions{
					DeploymentOptions: operatorv2beta1.DeploymentOptions{
						PodTemplateSpec: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  consts.DataPlaneProxyContainerName,
										Image: consts.DefaultDataPlaneImage,
										// Speed up the test.
										ReadinessProbe: &corev1.Probe{
											InitialDelaySeconds: 1,
											PeriodSeconds:       1,
											SuccessThreshold:    1,
										},
									},
								},
							},
						},
					},
				},
			},
			Extensions: []commonv1alpha1.ExtensionRef{
				{
					Kind:  "DataPlaneMetricsExtension",
					Group: operatorv1alpha1.SchemeGroupVersion.Group,
					NamespacedRef: commonv1alpha1.NamespacedRef{
						Name: dbMetricExt1.Name,
					},
				},
			},
		},
	}
	gatewayConfig, err = operatorClient.GatewayOperatorV2beta1().GatewayConfigurations(namespace.Name).Create(ctx, gatewayConfig, metav1.CreateOptions{})
	require.NoError(t, err)
	t.Logf("deployed GatewayConfiguration %s", gatewayConfig.Name)
	cleaner.Add(gatewayConfig)

	t.Log("deploying a GatewayClass resource with the GatewayConfiguration attached via ParametersReference")
	gatewayClass := &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "gwclass-",
		},
		Spec: gatewayv1.GatewayClassSpec{
			ParametersRef: &gatewayv1.ParametersReference{
				Group:     gatewayv1.Group(operatorv1alpha1.SchemeGroupVersion.Group),
				Kind:      gatewayv1.Kind("GatewayConfiguration"),
				Namespace: (*gatewayv1.Namespace)(&gatewayConfig.Namespace),
				Name:      gatewayConfig.Name,
			},
			ControllerName: gatewayv1.GatewayController(vars.ControllerName()),
		},
	}
	gatewayClass, err = gwClient.GatewayV1().GatewayClasses().Create(ctx, gatewayClass, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayClass)
	t.Logf("deployed GatewayClass %s", gatewayClass.Name)

	t.Log("deploying Gateway resource")
	gateway := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "gw-",
		},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: gatewayv1.ObjectName(gatewayClass.Name),
			Listeners: []gatewayv1.Listener{{
				Name:     "http",
				Protocol: gatewayv1.HTTPProtocolType,
				Port:     gatewayv1.PortNumber(80),
			}},
		},
	}
	gateway, err = gwClient.GatewayV1().Gateways(namespace.Name).Create(ctx, gateway, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gateway)
	t.Logf("deployed Gateway %s", gateway.Name)

	t.Log("verifying that the ControlPlane becomes provisioned")
	require.Eventually(t, osstestutils.GatewayControlPlaneIsProvisioned(t, ctx, gateway, clients), waitTimeout, interval)
	controlplanes := osstestutils.MustListControlPlanesForGateway(t, ctx, gateway, clients)
	require.Len(t, controlplanes, 1)
	cp := controlplanes[0]

	pluginMatchingLabels := client.MatchingLabels{
		consts.GatewayOperatorKongPluginTypeLabel:     consts.KongPluginNamePrometheus,
		consts.GatewayOperatorManagedByNameLabel:      cp.GetName(),
		consts.GatewayOperatorManagedByNamespaceLabel: cp.GetNamespace(),
		consts.GatewayOperatorManagedByLabel:          "controlplane",
	}

	t.Run("verify Prometheus plugin is created and Service gets konghq.com/plugins annotation equal to its name", func(t *testing.T) {
		var kongPlugin configurationv1.KongPlugin
		require.Eventually(t, func() bool {
			var kongPlugins configurationv1.KongPluginList
			err := mgrClient.List(ctx, &kongPlugins,
				client.InNamespace(namespace.Name),
				pluginMatchingLabels,
			)
			if err != nil {
				t.Logf("error listing KongPlugins: %v", err)
				return false
			}
			if len(kongPlugins.Items) == 0 {
				return false
			}
			kongPlugin = kongPlugins.Items[0]
			return true
		}, waitTimeout, interval)

		require.Eventually(t, func() bool {
			if err := mgrClient.Get(ctx, client.ObjectKeyFromObject(service), service); err != nil {
				t.Logf("error getting Service %s: %v", service, err)
				return false
			}

			if service.Annotations == nil {
				return false
			}

			a, ok := service.Annotations[consts.KongIngressControllerPluginsAnnotation]
			if !ok {
				return false
			}
			return a == kongPlugin.Name
		}, waitTimeout, interval)
	})

	t.Run("verify Prometheus plugin is deleted when ControlPlane extension ref is removed and Service gets konghq.com/plugins annotation cleared", func(t *testing.T) {
		t.Logf("updating GatewayConfiguration %s to remove the DataPlaneMetricsExtension ref", gatewayConfig.Name)
		gatewayConfig.Spec.Extensions = nil
		gatewayConfig, err = operatorClient.GatewayOperatorV2beta1().GatewayConfigurations(namespace.Name).Update(ctx, gatewayConfig, metav1.UpdateOptions{})
		require.NoError(t, err)

		t.Logf("checking if KongPlugin is deleted")
		require.Eventually(t, func() bool {
			var kongPlugins configurationv1.KongPluginList
			err := mgrClient.List(ctx, &kongPlugins,
				client.InNamespace(namespace.Name),
				pluginMatchingLabels,
			)
			if err != nil {
				t.Logf("error listing KongPlugins: %v", err)
				return false
			}

			if len(kongPlugins.Items) > 0 {
				t.Log("kongPlugin is still in place")
				return false
			}
			return true
		}, waitTimeout, interval)

		t.Logf("check that the Service %s has no %s annotation", service.Name, consts.KongIngressControllerPluginsAnnotation)
		require.Eventually(t, func() bool {
			if err := mgrClient.Get(ctx, client.ObjectKeyFromObject(service), service); err != nil {
				t.Logf("error getting Service %s: %v", service, err)
				return false
			}

			if service.Annotations == nil {
				return true
			}

			_, ok := service.Annotations[consts.KongIngressControllerPluginsAnnotation]
			return !ok
		}, waitTimeout, interval)
	})

	t.Run("verify Prometheus plugin is re-created and Service gets konghq.com/plugins annotation set again equal to created Plugins's name", func(t *testing.T) {
		t.Logf("updating GatewayConfiguration %s to re-add the DataPlaneMetricsExtension ref", gatewayConfig.Name)
		gatewayConfig.Spec.Extensions = []commonv1alpha1.ExtensionRef{
			createExtensionRefWithoutNamespace(dbMetricExt1.Name),
		}
		gatewayConfig, err = operatorClient.GatewayOperatorV2beta1().GatewayConfigurations(namespace.Name).Update(ctx, gatewayConfig, metav1.UpdateOptions{})
		require.NoError(t, err)

		var kongPlugin configurationv1.KongPlugin
		require.Eventually(t, func() bool {
			var kongPlugins configurationv1.KongPluginList
			err := mgrClient.List(ctx, &kongPlugins,
				client.InNamespace(namespace.Name),
				pluginMatchingLabels,
			)
			if err != nil {
				t.Logf("error listing KongPlugins: %v", err)
				return false
			}
			if len(kongPlugins.Items) == 0 {
				return false
			}
			kongPlugin = kongPlugins.Items[0]
			return true
		}, waitTimeout, interval)

		require.Eventually(t, func() bool {
			if err := mgrClient.Get(ctx, client.ObjectKeyFromObject(service), service); err != nil {
				t.Logf("error getting Service %s: %v", service, err)
				return false
			}

			if service.Annotations == nil {
				return false
			}

			a, ok := service.Annotations[consts.KongIngressControllerPluginsAnnotation]
			if !ok {
				return false
			}
			return a == kongPlugin.Name
		}, waitTimeout, interval)
	})
}
