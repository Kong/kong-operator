//go:build integration_tests

package integration

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	"github.com/kong/gateway-operator/internal/consts"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
	testutils "github.com/kong/gateway-operator/internal/utils/test"
	"github.com/kong/gateway-operator/test/helpers"
)

func TestControlPlaneWhenNoDataPlane(t *testing.T) {
	t.Parallel()
	namespace, cleaner := helpers.SetupTestEnv(t, ctx, env)

	dataplaneClient := clients.OperatorClient.ApisV1alpha1().DataPlanes(namespace.Name)
	controlplaneClient := clients.OperatorClient.ApisV1alpha1().ControlPlanes(namespace.Name)

	controlplaneName := types.NamespacedName{
		Namespace: namespace.Name,
		Name:      uuid.NewString(),
	}
	controlplane := &operatorv1alpha1.ControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: controlplaneName.Namespace,
			Name:      controlplaneName.Name,
		},
		Spec: operatorv1alpha1.ControlPlaneSpec{
			ControlPlaneOptions: operatorv1alpha1.ControlPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
					Pods: operatorv1alpha1.PodsOptions{
						ContainerImage: lo.ToPtr(consts.DefaultControlPlaneBaseImage),
						Version:        lo.ToPtr(consts.DefaultControlPlaneTag),
					},
				},
				DataPlane: nil,
			},
		},
	}

	// Control plane needs a dataplane to exist to properly function.
	dataplaneName := types.NamespacedName{
		Namespace: namespace.Name,
		Name:      uuid.NewString(),
	}
	dataplane := &operatorv1alpha1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: dataplaneName.Namespace,
			Name:      dataplaneName.Name,
		},
		Spec: operatorv1alpha1.DataPlaneSpec{
			DataPlaneOptions: operatorv1alpha1.DataPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
					Pods: operatorv1alpha1.PodsOptions{
						ContainerImage: lo.ToPtr(consts.DefaultDataPlaneBaseImage),
						Version:        lo.ToPtr(consts.DefaultDataPlaneTag),
					},
				},
			},
		},
	}

	t.Log("deploying controlplane resource without dataplane attached")
	controlplane, err := controlplaneClient.Create(ctx, controlplane, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(controlplane)

	t.Log("verifying controlplane state reflects lack of dataplane")
	require.Eventually(t, testutils.ControlPlaneDetectedNoDataplane(t, ctx, controlplaneName, clients), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)

	t.Log("verifying controlplane deployment has no active replicas")
	require.Eventually(t, testutils.Not(testutils.ControlPlaneHasActiveDeployment(t, ctx, controlplaneName, clients)), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)

	t.Log("deploying dataplane resource")
	dataplane, err = dataplaneClient.Create(ctx, dataplane, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(dataplane)

	t.Log("verifying deployments managed by the dataplane are ready")
	require.Eventually(t, testutils.DataPlaneHasActiveDeployment(t, ctx, dataplaneName, clients), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)

	t.Log("verifying services managed by the dataplane")
	require.Eventually(t, testutils.DataPlaneHasService(t, ctx, dataplaneName, clients), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)

	t.Log("attaching dataplane to controlplane")
	controlplane, err = controlplaneClient.Get(ctx, controlplane.Name, metav1.GetOptions{})
	require.NoError(t, err)
	controlplane.Spec.DataPlane = &dataplane.Name
	controlplane, err = controlplaneClient.Update(ctx, controlplane, metav1.UpdateOptions{})
	require.NoError(t, err)

	t.Log("verifying controlplane is now provisioned")
	require.Eventually(t, testutils.ControlPlaneIsProvisioned(t, ctx, controlplaneName, clients), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)

	t.Log("verifying controlplane deployment has active replicas")
	require.Eventually(t, testutils.ControlPlaneHasActiveDeployment(t, ctx, controlplaneName, clients), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)

	t.Log("removing dataplane from controlplane")
	controlplane, err = controlplaneClient.Get(ctx, controlplane.Name, metav1.GetOptions{})
	require.NoError(t, err)
	controlplane.Spec.DataPlane = nil
	_, err = controlplaneClient.Update(ctx, controlplane, metav1.UpdateOptions{})
	require.NoError(t, err)

	t.Log("verifying controlplane deployment has no active replicas")
	require.Eventually(t, testutils.Not(testutils.ControlPlaneHasActiveDeployment(t, ctx, controlplaneName, clients)), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)
}

func TestControlPlaneEssentials(t *testing.T) {
	t.Parallel()
	namespace, cleaner := helpers.SetupTestEnv(t, ctx, env)

	dataplaneClient := clients.OperatorClient.ApisV1alpha1().DataPlanes(namespace.Name)
	controlplaneClient := clients.OperatorClient.ApisV1alpha1().ControlPlanes(namespace.Name)

	// Control plane needs a dataplane to exist to properly function.
	dataplaneName := types.NamespacedName{
		Namespace: namespace.Name,
		Name:      uuid.NewString(),
	}
	dataplane := &operatorv1alpha1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: dataplaneName.Namespace,
			Name:      dataplaneName.Name,
		},
		Spec: operatorv1alpha1.DataPlaneSpec{
			DataPlaneOptions: operatorv1alpha1.DataPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
					Pods: operatorv1alpha1.PodsOptions{
						ContainerImage: lo.ToPtr(consts.DefaultDataPlaneBaseImage),
						Version:        lo.ToPtr(consts.DefaultDataPlaneTag),
					},
				},
			},
		},
	}

	controlplaneName := types.NamespacedName{
		Namespace: namespace.Name,
		Name:      uuid.NewString(),
	}
	controlplane := &operatorv1alpha1.ControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: controlplaneName.Namespace,
			Name:      controlplaneName.Name,
		},
		Spec: operatorv1alpha1.ControlPlaneSpec{
			ControlPlaneOptions: operatorv1alpha1.ControlPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
					Pods: operatorv1alpha1.PodsOptions{
						Labels: map[string]string{
							"label-a": "value-a",
							"label-x": "value-x",
						},
						Env: []corev1.EnvVar{
							{Name: "TEST_ENV", Value: "test"},
						},
						ContainerImage: lo.ToPtr(consts.DefaultControlPlaneBaseImage),
						Version:        lo.ToPtr(consts.DefaultControlPlaneTag),
					},
				},
				DataPlane: &dataplane.Name,
			},
		},
	}

	t.Log("deploying dataplane resource")
	dataplane, err := dataplaneClient.Create(ctx, dataplane, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(dataplane)

	t.Log("verifying deployments managed by the dataplane are ready")
	require.Eventually(t, testutils.DataPlaneHasActiveDeployment(t, ctx, dataplaneName, clients), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)

	t.Log("verifying services managed by the dataplane")
	require.Eventually(t, testutils.DataPlaneHasActiveService(t, ctx, dataplaneName, nil, clients), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)

	t.Log("deploying controlplane resource")
	controlplane, err = controlplaneClient.Create(ctx, controlplane, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(controlplane)

	t.Log("verifying controlplane gets marked scheduled")
	require.Eventually(t, testutils.ControlPlaneIsScheduled(t, ctx, controlplaneName, clients.OperatorClient), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)

	t.Log("verifying controlplane owns clusterrole and clusterrolebinding")
	require.Eventually(t, testutils.ControlPlaneHasClusterRole(t, ctx, controlplane, clients), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)
	require.Eventually(t, testutils.ControlPlaneHasClusterRoleBinding(t, ctx, controlplane, clients), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)

	t.Log("verifying that the controlplane gets marked as provisioned")
	require.Eventually(t, testutils.ControlPlaneIsProvisioned(t, ctx, controlplaneName, clients), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)

	t.Log("verifying controlplane deployment has active replicas")
	require.Eventually(t, testutils.ControlPlaneHasActiveDeployment(t, ctx, controlplaneName, clients), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)

	t.Logf("verifying that pod labels were set per the provided spec")
	require.Eventually(t, func() bool {
		deployments := testutils.MustListControlPlaneDeployments(t, ctx, controlplane, clients)
		require.Len(t, deployments, 1, "There must be only one ControlPlane deployment")
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
	}, testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)

	// check environment variables of deployments and pods.
	deployments := testutils.MustListControlPlaneDeployments(t, ctx, controlplane, clients)
	require.Len(t, deployments, 1, "There must be only one ControlPlane deployment")
	deployment := &deployments[0]

	t.Log("verifying controlplane Deployment.Pods.Env vars")
	checkControlPlaneDeploymentEnvVars(t, deployment)

	t.Log("deleting the  controlplane ClusterRole and ClusterRoleBinding")
	clusterRoles := testutils.MustListControlPlaneClusterRoles(t, ctx, controlplane, clients)
	require.Len(t, clusterRoles, 1, "There must be only one ControlPlane ClusterRole")
	require.NoError(t, clients.MgrClient.Delete(ctx, &clusterRoles[0]))
	clusterRoleBindings := testutils.MustListControlPlaneClusterRoleBindings(t, ctx, controlplane, clients)
	require.Len(t, clusterRoleBindings, 1, "There must be only one ControlPlane ClusterRoleBinding")
	require.NoError(t, clients.MgrClient.Delete(ctx, &clusterRoleBindings[0]))

	t.Log("verifying controlplane ClusterRole and ClusterRoleBinding have been re-created")
	require.Eventually(t, testutils.ControlPlaneHasClusterRole(t, ctx, controlplane, clients), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)
	require.Eventually(t, testutils.ControlPlaneHasClusterRoleBinding(t, ctx, controlplane, clients), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)

	t.Log("deleting the controlplane Deployment")
	require.NoError(t, clients.MgrClient.Delete(ctx, deployment))

	t.Log("verifying deployments managed by the dataplane after deletion")
	require.Eventually(t, testutils.ControlPlaneHasActiveDeployment(t, ctx, controlplaneName, clients), time.Minute, time.Second)

	t.Log("verifying controlplane Deployment.Pods.Env vars")
	checkControlPlaneDeploymentEnvVars(t, deployment)

	// delete controlplane and verify that cluster wide resources removed.
	t.Log("verifying cluster wide resources removed after controlplane deleted")
	err = controlplaneClient.Delete(ctx, controlplane.Name, metav1.DeleteOptions{})
	require.NoError(t, err)
	require.Eventually(t, testutils.Not(testutils.ControlPlaneHasClusterRole(t, ctx, controlplane, clients)), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)
	require.Eventually(t, testutils.Not(testutils.ControlPlaneHasClusterRoleBinding(t, ctx, controlplane, clients)), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)
	t.Log("verifying controlplane disappears after cluster resources are deleted")
	require.Eventually(t, func() bool {
		_, err := clients.OperatorClient.ApisV1alpha1().ControlPlanes(controlplaneName.Namespace).Get(ctx, controlplaneName.Name, metav1.GetOptions{})
		return k8serrors.IsNotFound(err)
	}, testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick,
		func() string {
			controlplane, err := clients.OperatorClient.ApisV1alpha1().ControlPlanes(controlplaneName.Namespace).Get(ctx, controlplaneName.Name, metav1.GetOptions{})
			if err != nil {
				return fmt.Sprintf("failed to get controlplane %s, error %v", controlplaneName.Name, err)
			}
			return fmt.Sprintf("last state of control plane: %#v", controlplane)
		},
	)
}

func checkControlPlaneDeploymentEnvVars(t *testing.T, deployment *appsv1.Deployment) {
	controllerContainer := k8sutils.GetPodContainerByName(&deployment.Spec.Template.Spec, consts.ControlPlaneControllerContainerName)
	require.NotNil(t, controllerContainer)

	envs := controllerContainer.Env
	t.Log("verifying env POD_NAME comes from metadata.name")
	podNameValueFrom := getEnvValueFromByName(envs, "POD_NAME")
	fieldRefMetadataName := &corev1.EnvVarSource{
		FieldRef: &corev1.ObjectFieldSelector{
			APIVersion: "v1",
			FieldPath:  "metadata.name",
		},
	}
	require.Truef(t, reflect.DeepEqual(fieldRefMetadataName, podNameValueFrom),
		"ValueFrom of POD_NAME should be the same as expected: expected %#v,actual %#v",
		fieldRefMetadataName, podNameValueFrom,
	)
	t.Log("verifying custom env TEST_ENV has value configured in controlplane")
	testEnvValue := getEnvValueByName(envs, "TEST_ENV")
	require.Equal(t, "test", testEnvValue)
}

func TestControlPlaneUpdate(t *testing.T) {
	t.Parallel()
	namespace, cleaner := helpers.SetupTestEnv(t, ctx, env)

	dataplaneClient := clients.OperatorClient.ApisV1alpha1().DataPlanes(namespace.Name)
	controlplaneClient := clients.OperatorClient.ApisV1alpha1().ControlPlanes(namespace.Name)

	dataplaneName := types.NamespacedName{
		Namespace: namespace.Name,
		Name:      uuid.NewString(),
	}
	dataplane := &operatorv1alpha1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: dataplaneName.Namespace,
			Name:      dataplaneName.Name,
		},
		Spec: operatorv1alpha1.DataPlaneSpec{
			DataPlaneOptions: operatorv1alpha1.DataPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
					Pods: operatorv1alpha1.PodsOptions{
						ContainerImage: lo.ToPtr(consts.DefaultDataPlaneBaseImage),
						Version:        lo.ToPtr(consts.DefaultDataPlaneTag),
					},
				},
			},
		},
	}

	controlplaneName := types.NamespacedName{
		Namespace: namespace.Name,
		Name:      uuid.NewString(),
	}
	controlplane := &operatorv1alpha1.ControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: controlplaneName.Namespace,
			Name:      controlplaneName.Name,
		},
		Spec: operatorv1alpha1.ControlPlaneSpec{
			ControlPlaneOptions: operatorv1alpha1.ControlPlaneOptions{
				Deployment: operatorv1alpha1.DeploymentOptions{
					Pods: operatorv1alpha1.PodsOptions{
						Env: []corev1.EnvVar{
							{
								Name: "TEST_ENV", Value: "before_update",
							},
						},
						ContainerImage: lo.ToPtr(consts.DefaultControlPlaneBaseImage),
						Version:        lo.ToPtr(consts.DefaultControlPlaneTag),
					},
				},
				DataPlane: &dataplane.Name,
			},
		},
	}

	t.Log("deploying dataplane resource")
	dataplane, err := dataplaneClient.Create(ctx, dataplane, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(dataplane)

	t.Log("verifying deployments managed by the dataplane are ready")
	require.Eventually(t,
		testutils.DataPlaneHasActiveDeployment(t, ctx, dataplaneName, clients),
		testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick,
	)

	t.Log("deploying controlplane resource")
	controlplane, err = controlplaneClient.Create(ctx, controlplane, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(controlplane)

	t.Log("verifying that the controlplane gets marked as provisioned")
	require.Eventually(t, testutils.ControlPlaneIsProvisioned(t, ctx, controlplaneName, clients),
		testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick,
	)

	t.Log("verifying controlplane deployment has active replicas")
	require.Eventually(t, testutils.ControlPlaneHasActiveDeployment(t, ctx, controlplaneName, clients),
		testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick,
	)

	// check environment variables of deployments and pods.
	deployments := testutils.MustListControlPlaneDeployments(t, ctx, controlplane, clients)
	require.Len(t, deployments, 1, "There must be only one ControlPlane deployment")
	deployment := &deployments[0]

	t.Logf("verifying environment variable TEST_ENV in deployment before update")
	container := k8sutils.GetPodContainerByName(&deployment.Spec.Template.Spec, consts.ControlPlaneControllerContainerName)
	require.NotNil(t, container)
	testEnv := getEnvValueByName(container.Env, "TEST_ENV")
	require.Equal(t, "before_update", testEnv)

	t.Logf("updating controlplane resource")
	controlplane, err = controlplaneClient.Get(ctx, controlplaneName.Name, metav1.GetOptions{})
	require.NoError(t, err)
	controlplane.Spec.Deployment.Pods.Env = []corev1.EnvVar{
		{
			Name: "TEST_ENV", Value: "after_update",
		},
	}
	_, err = controlplaneClient.Update(ctx, controlplane, metav1.UpdateOptions{})
	require.NoError(t, err)

	t.Logf("verifying environment variable TEST_ENV in deployment after update")
	require.Eventually(t, func() bool {
		deployments := testutils.MustListControlPlaneDeployments(t, ctx, controlplane, clients)
		require.Len(t, deployments, 1, "There must be only one ControlPlane deployment")
		deployment := &deployments[0]

		container := k8sutils.GetPodContainerByName(&deployment.Spec.Template.Spec, consts.ControlPlaneControllerContainerName)
		require.NotNil(t, container)
		testEnv := getEnvValueByName(container.Env, "TEST_ENV")
		t.Logf("Tenvironment variable TEST_ENV is now %s in deployment", testEnv)
		return testEnv == "after_update"
	},
		testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick,
	)

	var correctReadinessProbePath string
	t.Run("controlplane is not Ready when the underlying deployment changes state to not Ready", func(t *testing.T) {
		deployments := testutils.MustListControlPlaneDeployments(t, ctx, controlplane, clients)
		require.Len(t, deployments, 1, "There must be only one ControlPlane deployment")
		deployment := &deployments[0]
		require.Len(t, deployment.Spec.Template.Spec.Containers, 1)
		container := &deployment.Spec.Template.Spec.Containers[0]
		correctReadinessProbePath = container.ReadinessProbe.HTTPGet.Path
		container.ReadinessProbe.HTTPGet.Path = "/status_which_will_always_return_404"
		_, err = env.Cluster().Client().AppsV1().Deployments(namespace.Name).Update(ctx, deployment, metav1.UpdateOptions{})
		require.NoError(t, err)

		t.Logf("verifying that controlplane is indeed not Ready when the underlying deployment is not Ready")
		require.Eventually(t,
			testutils.ControlPlaneIsNotReady(t, ctx, controlplaneName, clients),
			testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick,
		)
	})

	t.Run("controlplane gets Ready when the underlying deployment changes state to Ready", func(t *testing.T) {
		deployments := testutils.MustListControlPlaneDeployments(t, ctx, controlplane, clients)
		require.Len(t, deployments, 1, "There must be only one ControlPlane deployment")
		deployment := &deployments[0]
		container := k8sutils.GetPodContainerByName(&deployment.Spec.Template.Spec, consts.ControlPlaneControllerContainerName)
		container.ReadinessProbe.HTTPGet.Path = correctReadinessProbePath
		_, err = env.Cluster().Client().AppsV1().Deployments(namespace.Name).Update(ctx, deployment, metav1.UpdateOptions{})
		require.NoError(t, err)

		require.Eventually(t,
			testutils.ControlPlaneIsReady(t, ctx, controlplaneName, clients),
			testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick,
		)
	})
}
