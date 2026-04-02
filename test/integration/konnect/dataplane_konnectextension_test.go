package konnect

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	testutils "github.com/kong/kong-operator/v2/pkg/utils/test"
	"github.com/kong/kong-operator/v2/test"
	"github.com/kong/kong-operator/v2/test/helpers"
	"github.com/kong/kong-operator/v2/test/helpers/asserts"
	"github.com/kong/kong-operator/v2/test/helpers/conditions"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
	"github.com/kong/kong-operator/v2/test/helpers/envs"
	"github.com/kong/kong-operator/v2/test/helpers/object"
	"github.com/kong/kong-operator/v2/test/helpers/volumes"
	"github.com/kong/kong-operator/v2/test/integration"
)

func TestDataPlaneWithKonnectExtension(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	namespace, _ := helpers.SetupTestEnv(t, ctx, integration.GetEnv())
	clients := integration.GetClients()
	cl := clients.MgrClient
	operatorClient := clients.OperatorClient

	// Generate a test ID for labeling resources in order to easily identify them in Konnect.
	testID := uuid.NewString()[:8]
	t.Logf("Test ID: %s", testID)

	// Build a namespaced client for convenience
	clientNamespaced := client.NewNamespacedClient(cl, namespace.Name)

	// Create a KonnectAPIAuthConfiguration
	// using the token from the test environment
	// and the Konnect server URL from the test environment
	authCfg := deploy.KonnectAPIAuthConfiguration(t, ctx, clientNamespaced,
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
	cp := deploy.KonnectGatewayControlPlane(t, ctx, clientNamespaced, authCfg,
		deploy.WithTestIDLabel(testID),
		deploy.KonnectGatewayControlPlaneLabel(deploy.KonnectTestIDLabel, testID),
	)
	t.Cleanup(object.DeleteAndWaitForDeletionFn(context.Background(), t, cl, cp.DeepCopy()))

	t.Logf("Waiting for a Konnect ID for KonnectGatewayControlPlane %s/%s", cp.Namespace, cp.Name)
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := cl.Get(ctx, types.NamespacedName{Name: cp.Name, Namespace: cp.Namespace}, cp)
		require.NoError(t, err)
		conditions.KonnectEntityIsProgrammed(t, cp)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

	// Creating a KonnectExtension that references the ControlPlane created above
	konnectExtension := deploy.KonnectExtension(
		t, ctx, clientNamespaced,
		deploy.WithKonnectExtensionKonnectNamespacedRefControlPlaneRef(cp),
	)
	t.Cleanup(object.DeleteAndWaitForDeletionFn(context.Background(), t, cl, konnectExtension.DeepCopy()))

	t.Logf("Waiting for KonnectExtension %s/%s to have all conditions set to True", konnectExtension.Namespace, konnectExtension.Name)
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		ok, msg := conditions.CheckKonnectExtensionConditions(ctx, t, cl,
			konnectExtension,
			helpers.CheckAllConditionsTrue,
			konnectv1alpha1.ControlPlaneRefValidConditionType,
			konnectv1alpha1.DataPlaneCertificateProvisionedConditionType,
			konnectv1alpha2.KonnectExtensionReadyConditionType)
		assert.Truef(t, ok, "condition check failed: %s, conditions: %+v", msg, konnectExtension.Status.Conditions)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

	t.Logf("Waiting for status.konnect and status.dataPlaneClientAuth to be set for KonnectExtension %s/%s", konnectExtension.Namespace, konnectExtension.Name)
	require.EventuallyWithT(t,
		conditions.CheckKonnectExtensionStatus(ctx, cl, konnectExtension, cp.GetKonnectID(), ""),
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

	dataplaneClient := operatorClient.GatewayOperatorV1beta1().DataPlanes(namespace.Name)
	dataplane, err := dataplaneClient.Create(ctx, dataplane, metav1.CreateOptions{})
	require.NoError(t, err)
	// NOTE: We use delete.ObjectAndWaitForDeletionFn cl, to ensure the dataplane is fully cleaned up
	// because it uses the KonnectExtension and currently we have a limitation that a KonnectExtension
	// cannot be deleted if there is a DataPlane using it.
	t.Cleanup(object.DeleteAndWaitForDeletionFn(context.Background(), t, cl, dataplane.DeepCopy()))

	dataplaneName := client.ObjectKeyFromObject(dataplane)

	t.Log("Verifying that the dataplane is marked as ready")
	require.Eventually(t, testutils.DataPlaneIsReady(t, ctx, dataplaneName, operatorClient), waitTime, tickTime)

	t.Log("Verifying deployments managed by the dataplane")
	require.Eventually(t, testutils.DataPlaneHasActiveDeployment(t, ctx, dataplaneName, &appsv1.Deployment{}, client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
	}, clients), waitTime, tickTime)

	t.Log("Verifying that the dataplane service receives IP addresses")
	var dataplaneIngressService corev1.Service
	require.Eventually(t, testutils.DataPlaneHasActiveService(t, ctx, dataplaneName, &dataplaneIngressService, clients, client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
		consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneIngressServiceLabelValue),
	}), waitTime, tickTime)

	var dataplaneIP string
	require.Eventually(t, func() bool {
		dataplaneService, err := clients.K8sClient.CoreV1().Services(dataplane.Namespace).Get(ctx, dataplaneIngressService.Name, metav1.GetOptions{})
		require.NoError(t, err)
		if len(dataplaneService.Status.LoadBalancer.Ingress) > 0 {
			dataplaneIP = dataplaneService.Status.LoadBalancer.Ingress[0].IP
			return true
		}
		return false
	}, waitTime, tickTime)

	require.Eventually(t, asserts.Expect404WithNoRouteFunc(t, ctx, "http://"+dataplaneIP), waitTime, tickTime)

	// Verify that the custom volume is configured correctly
	t.Log("Verifying that the custom volume is configured correctly")
	deployments := testutils.MustListDataPlaneDeployments(t, ctx, dataplane, clients, client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
	})
	require.Len(t, deployments, 1, "There must be only one DataPlane deployment")
	deployment := &deployments[0]

	// Verify the custom volume
	customVol := volumes.GetByName(deployment.Spec.Template.Spec.Volumes, "custom-vol")
	require.NotNil(t, customVol, "The dataplane pod should have the custom-vol volume")
	require.NotNil(t, customVol.EmptyDir, "custom-vol should be an emptyDir volume")

	// Verify the custom volume mount and env var in the proxy container
	proxyContainer := k8sutils.GetPodContainerByName(
		&deployment.Spec.Template.Spec, consts.DataPlaneProxyContainerName)
	require.NotNil(t, proxyContainer)
	// Check that the TEST_ENV env var is set
	testEnv := envs.GetValueByName(proxyContainer.Env, "TEST_ENV")
	require.Equal(t, "test", testEnv, "The TEST_ENV environment variable should be set to 'test' in the proxy container")
	// Check for the custom volume mount
	customVolMount := volumes.GetMountsByVolumeName(proxyContainer.VolumeMounts, "custom-vol")
	require.Len(t, customVolMount, 1, "The proxy container should mount the custom-vol volume")
	require.Equal(t, "/usr/local/lib/custom", customVolMount[0].MountPath, "The proxy container should mount custom-vol at path /usr/local/lib/custom")

	// Verify dataplane status
	t.Log("Verifying that the dataplane status is correctly populated with the backup service name and its addresses")
	require.Eventually(t, testutils.DataPlaneHasServiceAndAddressesInStatus(t, ctx, dataplaneName, clients), waitTime, tickTime)
}
