//go:build integration_tests
// +build integration_tests

package integration

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	gatewayutils "github.com/kong/gateway-operator/internal/utils/gateway"
	"github.com/kong/gateway-operator/pkg/vars"
)

const (
	testEnvVar         = "KONG_INTEGRATION_TESTS"
	testEnvVal         = "TEST_VALUE"
	testEnvVarFromName = "KONG_INTEGRATION_TESTS_FROM"
	testEnvVarFromKV   = "dzhambul"

	// gatewaySchedulingTimeLimit is the maximum amount of time to wait for
	// a supported ControlPlane to be created after a Gateway resource is
	// created for it.
	controlPlaneSchedulingTimeLimit = time.Minute * 3
)

func TestGatewayConfigurationEssentials(t *testing.T) {
	namespace, cleaner := setup(t)
	defer func() { assert.NoError(t, cleaner.Cleanup(ctx)) }()

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace.Name,
			Name:      uuid.NewString(),
		},
		Data: map[string]string{
			testEnvVarFromKV: testEnvVarFromKV,
		},
	}
	configMap, err := env.Cluster().Client().CoreV1().ConfigMaps(namespace.Name).Create(ctx, configMap, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(configMap)

	t.Log("deploying a GatewayConfiguration resource")
	gatewayConfig := &operatorv1alpha1.GatewayConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace.Name,
			Name:      uuid.NewString(),
		},
		Spec: operatorv1alpha1.GatewayConfigurationSpec{
			DataPlaneDeploymentOptions: &operatorv1alpha1.DataPlaneDeploymentOptions{
				DeploymentOptions: operatorv1alpha1.DeploymentOptions{
					Env: []corev1.EnvVar{
						{
							Name:  testEnvVar,
							Value: testEnvVal,
						},
						{
							Name: testEnvVarFromName,
							ValueFrom: &corev1.EnvVarSource{
								ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: configMap.Name,
									},
									Key: testEnvVarFromKV,
								},
							},
						},
					},
				},
			},
			ControlPlaneDeploymentOptions: &operatorv1alpha1.ControlPlaneDeploymentOptions{
				DeploymentOptions: operatorv1alpha1.DeploymentOptions{
					Env: []corev1.EnvVar{
						{
							Name:  testEnvVar,
							Value: testEnvVal,
						},
						{
							Name: testEnvVarFromName,
							ValueFrom: &corev1.EnvVarSource{
								ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: configMap.Name,
									},
									Key: testEnvVarFromKV,
								},
							},
						},
					},
				},
			},
		},
	}
	gatewayConfig, err = operatorClient.ApisV1alpha1().GatewayConfigurations(namespace.Name).Create(ctx, gatewayConfig, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayConfig)

	t.Log("deploying a GatewayClass resource with the GatewayConfiguration attached via ParametersReference")
	gatewayClass := &gatewayv1alpha2.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: uuid.NewString(),
		},
		Spec: gatewayv1alpha2.GatewayClassSpec{
			ParametersRef: &gatewayv1alpha2.ParametersReference{
				Group:     gatewayv1alpha2.Group(operatorv1alpha1.SchemeGroupVersion.Group),
				Kind:      gatewayv1alpha2.Kind("GatewayConfiguration"),
				Namespace: (*gatewayv1alpha2.Namespace)(&gatewayConfig.Namespace),
				Name:      gatewayConfig.Name,
			},
			ControllerName: gatewayv1alpha2.GatewayController(vars.ControllerName),
		},
	}
	gatewayClass, err = gatewayClient.GatewayV1alpha2().GatewayClasses().Create(ctx, gatewayClass, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayClass)

	t.Log("deploying Gateway resource")
	gateway := &gatewayv1alpha2.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace.Name,
			Name:      uuid.NewString(),
		},
		Spec: gatewayv1alpha2.GatewaySpec{
			GatewayClassName: gatewayv1alpha2.ObjectName(gatewayClass.Name),
			Listeners: []gatewayv1alpha2.Listener{{
				Name:     "http",
				Protocol: gatewayv1alpha2.HTTPProtocolType,
				Port:     gatewayv1alpha2.PortNumber(80),
			}},
		},
	}
	gateway, err = gatewayClient.GatewayV1alpha2().Gateways(namespace.Name).Create(ctx, gateway, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gateway)

	t.Log("verifying that the DataPlane receives the configuration override")
	require.Eventually(t, func() bool {
		dataplanes, err := gatewayutils.ListDataPlanesForGateway(ctx, mgrClient, gateway)
		if err != nil {
			return false
		}
		if len(dataplanes) != 1 {
			return false
		}
		for _, envVar := range dataplanes[0].Spec.Env {
			if envVar.Name == testEnvVar && envVar.Value == testEnvVal {
				return true
			}
		}
		return false
	}, gatewayReadyTimeLimit, time.Second)

	t.Log("verifying that the ControlPlane receives the configuration override")
	require.Eventually(t, func() bool {
		controlplanes, err := gatewayutils.ListControlPlanesForGateway(ctx, mgrClient, gateway)
		if err != nil {
			return false
		}
		if len(controlplanes) != 1 {
			return false
		}
		for _, envVar := range controlplanes[0].Spec.Env {
			if envVar.Name == testEnvVar && envVar.Value == testEnvVal {
				return true
			}
		}
		return false
	}, controlPlaneSchedulingTimeLimit, time.Second)

	t.Log("verifying that the DataPlane receives the configuration override")
	require.Eventually(t, func() bool {
		dataplanes, err := gatewayutils.ListDataPlanesForGateway(ctx, mgrClient, gateway)
		if err != nil {
			return false
		}
		if len(dataplanes) != 1 {
			return false
		}
		for _, envVar := range dataplanes[0].Spec.Env {
			if envVar.Name == testEnvVarFromName && envVar.ValueFrom.ConfigMapKeyRef.Key == testEnvVarFromKV {
				return true
			}
		}
		return false
	}, gatewayReadyTimeLimit, time.Second)

	t.Log("verifying that the ControlPlane receives the configuration override")
	require.Eventually(t, func() bool {
		controlplanes, err := gatewayutils.ListControlPlanesForGateway(ctx, mgrClient, gateway)
		if err != nil {
			return false
		}
		if len(controlplanes) != 1 {
			return false
		}
		for _, envVar := range controlplanes[0].Spec.Env {
			if envVar.Name == testEnvVarFromName && envVar.ValueFrom.ConfigMapKeyRef.Key == testEnvVarFromKV {
				return true
			}
		}
		return false
	}, controlPlaneSchedulingTimeLimit, time.Second)

	t.Log("removing the GatewayConfiguration attachment")
	require.Eventually(t, func() bool {
		gatewayClass, err = gatewayClient.GatewayV1alpha2().GatewayClasses().Get(ctx, gatewayClass.Name, metav1.GetOptions{})
		if err != nil {
			return false
		}

		gatewayClass.Spec.ParametersRef = nil
		gatewayClass, err = gatewayClient.GatewayV1alpha2().GatewayClasses().Update(ctx, gatewayClass, metav1.UpdateOptions{})
		return err == nil
	}, gatewaySchedulingTimeLimit, time.Second)

	t.Log("verifying that the DataPlane loses the configuration override")
	require.Eventually(t, func() bool {
		dataplanes, err := gatewayutils.ListDataPlanesForGateway(ctx, mgrClient, gateway)
		if err != nil {
			return false
		}
		if len(dataplanes) != 1 {
			return false
		}
		for _, envVar := range dataplanes[0].Spec.Env {
			if envVar.Name == testEnvVarFromName && envVar.ValueFrom.ConfigMapKeyRef.Key == testEnvVarFromKV {
				return false
			}
			if envVar.Name == testEnvVar && envVar.Value == testEnvVal {
				return false
			}
		}
		return true
	}, gatewayReadyTimeLimit, time.Second)

	t.Log("verifying that the ControlPlane receives the configuration override")
	require.Eventually(t, func() bool {
		controlplanes, err := gatewayutils.ListControlPlanesForGateway(ctx, mgrClient, gateway)
		if err != nil {
			return false
		}
		if len(controlplanes) != 1 {
			return false
		}
		for _, envVar := range controlplanes[0].Spec.Env {
			if envVar.Name == testEnvVarFromName && envVar.ValueFrom.ConfigMapKeyRef.Key == testEnvVarFromKV {
				return false
			}
			if envVar.Name == testEnvVar && envVar.Value == testEnvVal {
				return false
			}
		}
		return true
	}, controlPlaneSchedulingTimeLimit, time.Second)
}
