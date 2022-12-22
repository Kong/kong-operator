//go:build integration_tests
// +build integration_tests

package integration

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	"github.com/kong/gateway-operator/internal/consts"
	gwtypes "github.com/kong/gateway-operator/internal/types"
	gatewayutils "github.com/kong/gateway-operator/internal/utils/gateway"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
	testutils "github.com/kong/gateway-operator/internal/utils/test"
	"github.com/kong/gateway-operator/pkg/vars"
	"github.com/kong/gateway-operator/test/helpers"
)

func TestManualGatewayUpgradesAndDowngrades(t *testing.T) {
	t.Parallel()
	namespace, cleaner := helpers.SetupTestEnv(t, ctx, env)

	originalControlPlaneImageName := "kong/kubernetes-ingress-controller"
	originalControlPlaneImageVersion := "2.5.0"
	originalControlPlaneImage := fmt.Sprintf("%s:%s", originalControlPlaneImageName, originalControlPlaneImageVersion)

	originalDataPlaneImageName := "kong/kong"
	originalDataPlaneImageVersion := "2.7.0"
	originalDataPlaneImage := fmt.Sprintf("%s:%s", originalDataPlaneImageName, originalDataPlaneImageVersion)

	newControlPlaneImageVersion := "2.6.0"
	newControlPlaneImage := fmt.Sprintf("%s:%s", originalControlPlaneImageName, newControlPlaneImageVersion)

	newDataPlaneImageVersion := "2.8.0"
	newDataPlaneImage := fmt.Sprintf("%s:%s", originalDataPlaneImageName, newDataPlaneImageVersion)

	t.Log("deploying a GatewayConfiguration resource")
	gatewayConfig := &operatorv1alpha1.GatewayConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace.Name,
			Name:      uuid.NewString(),
		},
		Spec: operatorv1alpha1.GatewayConfigurationSpec{
			ControlPlaneDeploymentOptions: &operatorv1alpha1.ControlPlaneDeploymentOptions{
				DeploymentOptions: operatorv1alpha1.DeploymentOptions{
					ContainerImage: pointer.String(originalControlPlaneImageName),
					Version:        pointer.String(originalControlPlaneImageVersion),
				},
			},
			DataPlaneDeploymentOptions: &operatorv1alpha1.DataPlaneDeploymentOptions{
				DeploymentOptions: operatorv1alpha1.DeploymentOptions{
					ContainerImage: pointer.String(originalDataPlaneImageName),
					Version:        pointer.String(originalDataPlaneImageVersion),
				},
			},
		},
	}
	var err error
	gatewayConfig, err = clients.OperatorClient.ApisV1alpha1().GatewayConfigurations(namespace.Name).Create(ctx, gatewayConfig, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayConfig)

	t.Log("deploying a GatewayClass resource with the GatewayConfiguration attached via ParametersReference")
	gatewayClass := &gatewayv1beta1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: uuid.NewString(),
		},
		Spec: gatewayv1beta1.GatewayClassSpec{
			ParametersRef: &gatewayv1beta1.ParametersReference{
				Group:     gatewayv1beta1.Group(operatorv1alpha1.SchemeGroupVersion.Group),
				Kind:      gatewayv1beta1.Kind("GatewayConfiguration"),
				Namespace: (*gatewayv1beta1.Namespace)(&gatewayConfig.Namespace),
				Name:      gatewayConfig.Name,
			},
			ControllerName: gatewayv1beta1.GatewayController(vars.ControllerName()),
		},
	}
	gatewayClass, err = clients.GatewayClient.GatewayV1beta1().GatewayClasses().Create(ctx, gatewayClass, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayClass)

	t.Log("deploying Gateway resource")
	gateway := &gwtypes.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace.Name,
			Name:      uuid.NewString(),
		},
		Spec: gatewayv1beta1.GatewaySpec{
			GatewayClassName: gatewayv1beta1.ObjectName(gatewayClass.Name),
			Listeners: []gatewayv1beta1.Listener{{
				Name:     "http",
				Protocol: gatewayv1beta1.HTTPProtocolType,
				Port:     gatewayv1beta1.PortNumber(80),
			}},
		},
	}
	gateway, err = clients.GatewayClient.GatewayV1beta1().Gateways(namespace.Name).Create(ctx, gateway, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gateway)

	t.Log("verifying that the ControlPlane receives the configuration override")
	require.Eventually(t, func() bool {
		controlplanes, err := gatewayutils.ListControlPlanesForGateway(ctx, clients.MgrClient, gateway)
		if err != nil {
			return false
		}
		if len(controlplanes) != 1 {
			return false
		}
		return controlplanes[0].Spec.ContainerImage != nil &&
			controlplanes[0].Spec.Version != nil &&
			*controlplanes[0].Spec.ContainerImage == originalControlPlaneImageName &&
			*controlplanes[0].Spec.Version == originalControlPlaneImageVersion
	}, testutils.ControlPlaneSchedulingTimeLimit, time.Second)

	t.Log("verifying that the DataPlane receives the configuration override")
	require.Eventually(t, func() bool {
		dataplanes, err := gatewayutils.ListDataPlanesForGateway(ctx, clients.MgrClient, gateway)
		if err != nil {
			return false
		}
		if len(dataplanes) != 1 {
			return false
		}
		return dataplanes[0].Spec.ContainerImage != nil &&
			dataplanes[0].Spec.Version != nil &&
			*dataplanes[0].Spec.ContainerImage == originalDataPlaneImageName &&
			*dataplanes[0].Spec.Version == originalDataPlaneImageVersion
	}, testutils.GatewayReadyTimeLimit, time.Second)

	t.Log("verifying initial pod images for Gateway")
	require.Eventually(t, func() bool {
		upToDate, err := verifyContainerImageForGateway(gateway, originalControlPlaneImage, originalDataPlaneImage)
		return err == nil && upToDate
	}, time.Minute, time.Second)

	t.Log("upgrading the ControlPlane version for the Gateway")
	require.Eventually(t, func() bool {
		return changeControlPlaneImage(gatewayConfig, originalControlPlaneImageName, newControlPlaneImageVersion) == nil
	}, time.Second*10, time.Second)

	t.Log("verifying that the ControlPlane receives the configuration override")
	require.Eventually(t, func() bool {
		controlplanes, err := gatewayutils.ListControlPlanesForGateway(ctx, clients.MgrClient, gateway)
		if err != nil {
			return false
		}
		if len(controlplanes) != 1 {
			return false
		}
		return controlplanes[0].Spec.ContainerImage != nil &&
			controlplanes[0].Spec.Version != nil &&
			*controlplanes[0].Spec.ContainerImage == originalControlPlaneImageName &&
			*controlplanes[0].Spec.Version == newControlPlaneImageVersion
	}, testutils.ControlPlaneSchedulingTimeLimit, time.Second)

	t.Log("verifying upgraded ControlPlane Pod images for Gateway")
	require.Eventually(t, func() bool {
		upToDate, err := verifyContainerImageForGateway(gateway, newControlPlaneImage, originalDataPlaneImage)
		return err == nil && upToDate
	}, time.Minute, time.Second)

	t.Log("upgrading the DataPlane version for the Gateway")
	require.Eventually(t, func() bool {
		return changeDataPlaneImage(gatewayConfig, originalDataPlaneImageName, newDataPlaneImageVersion) == nil
	}, time.Second*10, time.Second)

	t.Log("verifying that the DataPlane receives the configuration override")
	require.Eventually(t, func() bool {
		dataplanes, err := gatewayutils.ListDataPlanesForGateway(ctx, clients.MgrClient, gateway)
		if err != nil {
			return false
		}
		if len(dataplanes) != 1 {
			return false
		}
		return dataplanes[0].Spec.ContainerImage != nil &&
			dataplanes[0].Spec.Version != nil &&
			*dataplanes[0].Spec.ContainerImage == originalDataPlaneImageName &&
			*dataplanes[0].Spec.Version == newDataPlaneImageVersion
	}, testutils.GatewayReadyTimeLimit, time.Second)

	t.Log("verifying upgraded DataPlane Pod images for Gateway")
	require.Eventually(t, func() bool {
		upToDate, err := verifyContainerImageForGateway(gateway, newControlPlaneImage, newDataPlaneImage)
		return err == nil && upToDate
	}, time.Minute, time.Second)

	t.Log("downgrading the ControlPlane version for the Gateway")
	require.Eventually(t, func() bool {
		return changeControlPlaneImage(gatewayConfig, originalControlPlaneImageName, originalControlPlaneImageVersion) == nil
	}, time.Second*10, time.Second)

	t.Log("verifying that the ControlPlane receives the configuration override")
	require.Eventually(t, func() bool {
		controlplanes, err := gatewayutils.ListControlPlanesForGateway(ctx, clients.MgrClient, gateway)
		if err != nil {
			return false
		}
		if len(controlplanes) != 1 {
			return false
		}
		return controlplanes[0].Spec.ContainerImage != nil &&
			controlplanes[0].Spec.Version != nil &&
			*controlplanes[0].Spec.ContainerImage == originalControlPlaneImageName &&
			*controlplanes[0].Spec.Version == originalControlPlaneImageVersion
	}, testutils.ControlPlaneSchedulingTimeLimit, time.Second)

	t.Log("verifying downgraded ControlPlane Pod images for Gateway")
	require.Eventually(t, func() bool {
		upToDate, err := verifyContainerImageForGateway(gateway, originalControlPlaneImage, newDataPlaneImage)
		return err == nil && upToDate
	}, time.Minute, time.Second)

	t.Log("downgrading the DataPlane version for the Gateway")
	require.Eventually(t, func() bool {
		return changeDataPlaneImage(gatewayConfig, originalDataPlaneImageName, originalDataPlaneImageVersion) == nil
	}, time.Second*10, time.Second)

	t.Log("verifying that the DataPlane receives the configuration override")
	require.Eventually(t, func() bool {
		dataplanes, err := gatewayutils.ListDataPlanesForGateway(ctx, clients.MgrClient, gateway)
		if err != nil {
			return false
		}
		if len(dataplanes) != 1 {
			return false
		}
		return dataplanes[0].Spec.ContainerImage != nil &&
			dataplanes[0].Spec.Version != nil &&
			*dataplanes[0].Spec.ContainerImage == originalDataPlaneImageName &&
			*dataplanes[0].Spec.Version == originalDataPlaneImageVersion
	}, testutils.GatewayReadyTimeLimit, time.Second)

	t.Log("verifying downgraded DataPlane Pod images for Gateway")
	require.Eventually(t, func() bool {
		upToDate, err := verifyContainerImageForGateway(gateway, originalControlPlaneImage, originalDataPlaneImage)
		return err == nil && upToDate
	}, time.Minute, time.Second)
}

// verifyContainerImageForGateway indicates whether or not the underlying
// Pods' containers are configured with the images provided.
func verifyContainerImageForGateway(gateway *gwtypes.Gateway, controlPlaneImage, dataPlaneImage string) (bool, error) {
	controlPlanes, err := gatewayutils.ListControlPlanesForGateway(ctx, clients.MgrClient, gateway)
	if err != nil {
		return false, err
	}

	dataPlanes, err := gatewayutils.ListDataPlanesForGateway(ctx, clients.MgrClient, gateway)
	if err != nil {
		return false, err
	}

	if len(controlPlanes) != 1 {
		return false, fmt.Errorf("waiting for only 1 ControlPlane")
	}

	if len(dataPlanes) != 1 {
		return false, fmt.Errorf("waiting for only 1 DataPlane")
	}

	deployments, err := k8sutils.ListDeploymentsForOwner(
		ctx,
		clients.MgrClient,
		consts.GatewayOperatorControlledLabel,
		consts.ControlPlaneManagedLabelValue,
		controlPlanes[0].Namespace,
		controlPlanes[0].UID,
	)
	if err != nil {
		return false, err
	}

	if len(deployments) != 1 {
		return false, fmt.Errorf("waiting for only 1 ControlPlane Deployment")
	}

	for _, deployment := range deployments {
		if len(deployment.Spec.Template.Spec.Containers) < 1 {
			return false, fmt.Errorf("waiting for ControlPlane Deployment to have at least 1 container")
		}
		for _, container := range deployment.Spec.Template.Spec.Containers {
			if container.Image != controlPlaneImage {
				return false, nil
			}
		}
	}

	deployments, err = k8sutils.ListDeploymentsForOwner(
		ctx,
		clients.MgrClient,
		consts.GatewayOperatorControlledLabel,
		consts.DataPlaneManagedLabelValue,
		dataPlanes[0].Namespace,
		dataPlanes[0].UID,
	)
	if err != nil {
		return false, err
	}

	if len(deployments) != 1 {
		return false, fmt.Errorf("waiting for only 1 DataPlane Deployment")
	}

	for _, deployment := range deployments {
		if len(deployment.Spec.Template.Spec.Containers) < 1 {
			return false, fmt.Errorf("waiting for DataPlane Deployment to have at least 1 container")
		}
		for _, container := range deployment.Spec.Template.Spec.Containers {
			if container.Image != dataPlaneImage {
				return false, nil
			}
		}
	}

	return true, nil
}

// changeControlPlaneImage is a helper function to update the image
// for ControlPlanes in a given GatewayConfiguration.
func changeControlPlaneImage(
	gcfg *operatorv1alpha1.GatewayConfiguration,
	controlPlaneImageName,
	controlPlaneImageVersion string,
) error {
	// refresh the object
	gcfg, err := clients.OperatorClient.ApisV1alpha1().GatewayConfigurations(gcfg.Namespace).Get(ctx, gcfg.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	gcfg.Spec.ControlPlaneDeploymentOptions.ContainerImage = &controlPlaneImageName
	gcfg.Spec.ControlPlaneDeploymentOptions.Version = &controlPlaneImageVersion

	_, err = clients.OperatorClient.ApisV1alpha1().GatewayConfigurations(gcfg.Namespace).Update(ctx, gcfg, metav1.UpdateOptions{})
	return err
}

// changeDataPlaneImage is a helper function to update the image
// for DataPlane in a given GatewayConfiguration.
func changeDataPlaneImage(
	gcfg *operatorv1alpha1.GatewayConfiguration,
	dataPlaneImageName,
	dataPlaneImageVersion string,
) error {
	// refresh the object
	gcfg, err := clients.OperatorClient.ApisV1alpha1().GatewayConfigurations(gcfg.Namespace).Get(ctx, gcfg.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	gcfg.Spec.DataPlaneDeploymentOptions.ContainerImage = &dataPlaneImageName
	gcfg.Spec.DataPlaneDeploymentOptions.Version = &dataPlaneImageVersion

	_, err = clients.OperatorClient.ApisV1alpha1().GatewayConfigurations(gcfg.Namespace).Update(ctx, gcfg, metav1.UpdateOptions{})
	return err
}
