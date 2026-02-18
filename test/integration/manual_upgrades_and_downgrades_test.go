package integration

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	operatorv2beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v2beta1"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/pkg/consts"
	gatewayutils "github.com/kong/kong-operator/v2/pkg/utils/gateway"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	testutils "github.com/kong/kong-operator/v2/pkg/utils/test"
	"github.com/kong/kong-operator/v2/pkg/vars"
	"github.com/kong/kong-operator/v2/test/helpers"
)

func TestManualGatewayUpgradesAndDowngrades(t *testing.T) {
	t.Parallel()
	namespace, cleaner := helpers.SetupTestEnv(t, GetCtx(), GetEnv())

	originalDataPlaneImageName := helpers.GetDefaultDataPlaneBaseImage()
	originalDataPlaneImageVersion := "3.3.0"
	originalDataPlaneImage := fmt.Sprintf("%s:%s", originalDataPlaneImageName, originalDataPlaneImageVersion)

	newDataPlaneImageVersion := "3.6.0"
	newDataPlaneImage := fmt.Sprintf("%s:%s", originalDataPlaneImageName, newDataPlaneImageVersion)

	t.Log("deploying a GatewayConfiguration resource")
	gatewayConfig := &operatorv2beta1.GatewayConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace.Name,
			Name:      uuid.NewString(),
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
										Image: fmt.Sprintf("%s:%s", originalDataPlaneImageName, originalDataPlaneImageVersion),
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
		},
	}
	var err error
	gatewayConfig, err = GetClients().OperatorClient.GatewayOperatorV2beta1().GatewayConfigurations(namespace.Name).Create(GetCtx(), gatewayConfig, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayConfig)

	t.Log("deploying a GatewayClass resource with the GatewayConfiguration attached via ParametersReference")
	gatewayClass := &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: uuid.NewString(),
		},
		Spec: gatewayv1.GatewayClassSpec{
			ParametersRef: &gatewayv1.ParametersReference{
				Group:     gatewayv1.Group(operatorv1beta1.SchemeGroupVersion.Group),
				Kind:      gatewayv1.Kind("GatewayConfiguration"),
				Namespace: (*gatewayv1.Namespace)(&gatewayConfig.Namespace),
				Name:      gatewayConfig.Name,
			},
			ControllerName: gatewayv1.GatewayController(vars.ControllerName()),
		},
	}
	gatewayClass, err = GetClients().GatewayClient.GatewayV1().GatewayClasses().Create(GetCtx(), gatewayClass, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gatewayClass)

	t.Log("deploying Gateway resource")
	gateway := &gwtypes.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace.Name,
			Name:      uuid.NewString(),
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
	gateway, err = GetClients().GatewayClient.GatewayV1().Gateways(namespace.Name).Create(GetCtx(), gateway, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(gateway)

	t.Log("verifying that the DataPlane receives the configuration override")
	require.Eventually(t, func() bool {
		dataplanes, err := gatewayutils.ListDataPlanesForGateway(GetCtx(), GetClients().MgrClient, gateway)
		if err != nil {
			return false
		}
		if len(dataplanes) != 1 {
			return false
		}
		container := k8sutils.GetPodContainerByName(&dataplanes[0].Spec.Deployment.PodTemplateSpec.Spec, consts.DataPlaneProxyContainerName)
		if container == nil {
			return false
		}
		return container.Image == fmt.Sprintf("%s:%s", originalDataPlaneImageName, originalDataPlaneImageVersion)
	}, testutils.GatewayReadyTimeLimit, time.Second)

	t.Log("verifying initial pod images for Gateway")
	require.Eventually(t, func() bool {
		upToDate, err := verifyContainerImageForGateway(gateway, originalDataPlaneImage)
		return err == nil && upToDate
	}, time.Minute, time.Second)

	t.Log("upgrading the DataPlane version for the Gateway")
	require.Eventually(t, func() bool {
		return changeDataPlaneImage(gatewayConfig, originalDataPlaneImageName, newDataPlaneImageVersion) == nil
	}, testutils.DataPlaneCondDeadline, testutils.DataPlaneCondTick)

	t.Log("verifying that the DataPlane receives the configuration override")
	require.Eventually(t, func() bool {
		dataplanes, err := gatewayutils.ListDataPlanesForGateway(GetCtx(), GetClients().MgrClient, gateway)
		if err != nil {
			return false
		}
		if len(dataplanes) != 1 {
			return false
		}
		container := k8sutils.GetPodContainerByName(&dataplanes[0].Spec.Deployment.PodTemplateSpec.Spec, consts.DataPlaneProxyContainerName)
		if container == nil {
			return false
		}
		return container.Image == fmt.Sprintf("%s:%s", originalDataPlaneImageName, newDataPlaneImageVersion)
	}, testutils.GatewayReadyTimeLimit, time.Second)

	t.Log("verifying upgraded DataPlane Pod images for Gateway")
	require.Eventually(t, func() bool {
		upToDate, err := verifyContainerImageForGateway(gateway, newDataPlaneImage)
		return err == nil && upToDate
	}, time.Minute, time.Second)

	t.Log("downgrading the DataPlane version for the Gateway")
	require.Eventually(t, func() bool {
		return changeDataPlaneImage(gatewayConfig, originalDataPlaneImageName, originalDataPlaneImageVersion) == nil
	}, testutils.DataPlaneCondDeadline, testutils.DataPlaneCondTick)

	t.Log("verifying that the DataPlane receives the configuration override")
	require.Eventually(t, func() bool {
		dataplanes, err := gatewayutils.ListDataPlanesForGateway(GetCtx(), GetClients().MgrClient, gateway)
		if err != nil {
			return false
		}
		if len(dataplanes) != 1 {
			return false
		}
		container := k8sutils.GetPodContainerByName(&dataplanes[0].Spec.Deployment.PodTemplateSpec.Spec, consts.DataPlaneProxyContainerName)
		if container == nil {
			return false
		}
		return container.Image == fmt.Sprintf("%s:%s", originalDataPlaneImageName, originalDataPlaneImageVersion)
	}, testutils.GatewayReadyTimeLimit, time.Second)

	t.Log("verifying downgraded DataPlane Pod images for Gateway")
	require.Eventually(t, func() bool {
		upToDate, err := verifyContainerImageForGateway(gateway, originalDataPlaneImage)
		return err == nil && upToDate
	}, testutils.DataPlaneCondDeadline, testutils.DataPlaneCondTick)
}

// verifyContainerImageForGateway indicates whether or not the underlying
// Pods' containers are configured with the images provided.
func verifyContainerImageForGateway(gateway *gwtypes.Gateway, dataPlaneImage string) (bool, error) {
	dataPlanes, err := gatewayutils.ListDataPlanesForGateway(GetCtx(), GetClients().MgrClient, gateway)
	if err != nil {
		return false, err
	}

	if len(dataPlanes) != 1 {
		return false, fmt.Errorf("waiting for only 1 DataPlane")
	}

	deployments, err := k8sutils.ListDeploymentsForOwner(
		GetCtx(),
		GetClients().MgrClient,
		dataPlanes[0].Namespace,
		dataPlanes[0].UID,
		client.MatchingLabels{
			consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
		},
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

// changeDataPlaneImage is a helper function to update the image
// for DataPlane in a given GatewayConfiguration.
func changeDataPlaneImage(
	gcfg *operatorv2beta1.GatewayConfiguration,
	dataPlaneImageName,
	dataPlaneImageVersion string,
) error {
	// refresh the object
	gcfg, err := GetClients().OperatorClient.GatewayOperatorV2beta1().GatewayConfigurations(gcfg.Namespace).Get(GetCtx(), gcfg.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	container := k8sutils.GetPodContainerByName(&gcfg.Spec.DataPlaneOptions.Deployment.PodTemplateSpec.Spec, consts.DataPlaneProxyContainerName)
	if container == nil {
		return errors.New("container is nil in GatewayConfiguration DataPlane options")
	}
	container.Image = fmt.Sprintf("%s:%s", dataPlaneImageName, dataPlaneImageVersion)

	_, err = GetClients().OperatorClient.GatewayOperatorV2beta1().GatewayConfigurations(gcfg.Namespace).Update(GetCtx(), gcfg, metav1.UpdateOptions{})
	return err
}
