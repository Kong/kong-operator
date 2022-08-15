//go:build integration_tests
// +build integration_tests

package integration

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
)

const (
	controlPlaneCondDeadline = time.Minute
	controlPlaneCondTick     = time.Second
)

func TestControlPlaneWhenNoDataPlane(t *testing.T) {
	namespace, cleaner := setup(t)
	defer func() { assert.NoError(t, cleaner.Cleanup(ctx)) }()

	dataplaneClient := operatorClient.ApisV1alpha1().DataPlanes(namespace.Name)
	controlplaneClient := operatorClient.ApisV1alpha1().ControlPlanes(namespace.Name)

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
			ControlPlaneDeploymentOptions: operatorv1alpha1.ControlPlaneDeploymentOptions{
				DeploymentOptions: operatorv1alpha1.DeploymentOptions{},
				DataPlane:         nil,
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
	}

	t.Log("deploying controlplane resource without dataplane attached")
	controlplane, err := controlplaneClient.Create(ctx, controlplane, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(controlplane)

	t.Log("verifying controlplane state reflects lack of dataplane")
	require.Eventually(t, controlPlaneDetectedNoDataplane(t, ctx, controlplaneName), controlPlaneCondDeadline, controlPlaneCondTick)

	t.Log("verifying controlplane deployment has no active replicas")
	require.Eventually(t, Not(controlPlaneHasActiveDeployment(t, ctx, controlplaneName)), controlPlaneCondDeadline, controlPlaneCondTick)

	t.Log("deploying dataplane resource")
	dataplane, err = dataplaneClient.Create(ctx, dataplane, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(dataplane)

	t.Log("verifying deployments managed by the dataplane are ready")
	require.Eventually(t, dataPlaneHasActiveDeployment(t, ctx, dataplaneName), controlPlaneCondDeadline, controlPlaneCondTick)

	t.Log("verifying services managed by the dataplane")
	require.Eventually(t, dataPlaneHasService(t, ctx, dataplaneName), controlPlaneCondDeadline, controlPlaneCondTick)

	t.Log("attaching dataplane to controlplane")
	controlplane, err = controlplaneClient.Get(ctx, controlplane.Name, metav1.GetOptions{})
	require.NoError(t, err)
	controlplane.Spec.DataPlane = &dataplane.Name
	controlplane, err = controlplaneClient.Update(ctx, controlplane, metav1.UpdateOptions{})
	require.NoError(t, err)

	t.Log("verifying controlplane is now provisioned")
	require.Eventually(t, controlPlaneIsProvisioned(t, ctx, controlplaneName), controlPlaneCondDeadline, controlPlaneCondTick)

	t.Log("verifying controlplane deployment has active replicas")
	require.Eventually(t, controlPlaneHasActiveDeployment(t, ctx, controlplaneName), controlPlaneCondDeadline, controlPlaneCondTick)

	t.Log("removing dataplane from controlplane")
	controlplane, err = controlplaneClient.Get(ctx, controlplane.Name, metav1.GetOptions{})
	require.NoError(t, err)
	controlplane.Spec.DataPlane = nil
	_, err = controlplaneClient.Update(ctx, controlplane, metav1.UpdateOptions{})
	require.NoError(t, err)

	t.Log("verifying controlplane deployment has no active replicas")
	require.Eventually(t, Not(controlPlaneHasActiveDeployment(t, ctx, controlplaneName)), controlPlaneCondDeadline, controlPlaneCondTick)
}

func TestControlPlaneEssentials(t *testing.T) {
	namespace, cleaner := setup(t)
	defer func() { assert.NoError(t, cleaner.Cleanup(ctx)) }()

	dataplaneClient := operatorClient.ApisV1alpha1().DataPlanes(namespace.Name)
	controlplaneClient := operatorClient.ApisV1alpha1().ControlPlanes(namespace.Name)

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
			ControlPlaneDeploymentOptions: operatorv1alpha1.ControlPlaneDeploymentOptions{
				DeploymentOptions: operatorv1alpha1.DeploymentOptions{
					Env: []corev1.EnvVar{
						{Name: "TEST_ENV", Value: "test"},
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
	require.Eventually(t, dataPlaneHasActiveDeployment(t, ctx, dataplaneName), controlPlaneCondDeadline, controlPlaneCondTick)

	t.Log("verifying services managed by the dataplane")
	require.Eventually(t, dataPlaneHasActiveService(t, ctx, dataplaneName, nil), controlPlaneCondDeadline, controlPlaneCondTick)

	t.Log("deploying controlplane resource")
	controlplane, err = controlplaneClient.Create(ctx, controlplane, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(controlplane)

	t.Log("verifying controlplane gets marked scheduled")
	require.Eventually(t, controlPlaneIsScheduled(t, ctx, controlplaneName), controlPlaneCondDeadline, controlPlaneCondTick)

	t.Log("verifying controlplane owns clusterrole and clusterrolebinding")
	require.Eventually(t, controlPlaneHasClusterRole(t, ctx, controlplane), controlPlaneCondDeadline, controlPlaneCondTick)
	require.Eventually(t, controlPlaneHasClusterRoleBinding(t, ctx, controlplane), controlPlaneCondDeadline, controlPlaneCondTick)

	t.Log("verifying that the controlplane gets marked as provisioned")
	require.Eventually(t, controlPlaneIsProvisioned(t, ctx, controlplaneName), controlPlaneCondDeadline, controlPlaneCondTick)

	t.Log("verifying controlplane deployment has active replicas")
	require.Eventually(t, controlPlaneHasActiveDeployment(t, ctx, controlplaneName), controlPlaneCondDeadline, controlPlaneCondTick)

	// check environment variables of deployments and pods.
	deployments := mustListControlPlaneDeployments(t, controlplane)
	require.Len(t, deployments, 1, "There must be only one ControlPlane deployment")
	deployment := deployments[0]
	controllerContainer := getContainerWithNameInPod(&deployment.Spec.Template.Spec, "controller")
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

	// delete controlplane and verify that cluster wide resources removed.
	t.Log("verifying cluster wide resources removed after controlplane deleted")
	err = controlplaneClient.Delete(ctx, controlplane.Name, metav1.DeleteOptions{})
	require.NoError(t, err)
	require.Eventually(t, Not(controlPlaneHasClusterRole(t, ctx, controlplane)), controlPlaneCondDeadline, controlPlaneCondTick)
	require.Eventually(t, Not(controlPlaneHasClusterRoleBinding(t, ctx, controlplane)), controlPlaneCondDeadline, controlPlaneCondTick)
	t.Log("verifying controlplane disappears after cluster resources are deleted")
	require.Eventually(t, func() bool {
		_, err := operatorClient.ApisV1alpha1().ControlPlanes(controlplaneName.Namespace).Get(ctx, controlplaneName.Name, metav1.GetOptions{})
		return k8serrors.IsNotFound(err)
	}, controlPlaneCondDeadline, controlPlaneCondTick,
		func() string {
			controlplane, err := operatorClient.ApisV1alpha1().ControlPlanes(controlplaneName.Namespace).Get(ctx, controlplaneName.Name, metav1.GetOptions{})
			if err != nil {
				return fmt.Sprintf("failed to get controlplane %s, error %v", controlplaneName.Name, err)
			}
			return fmt.Sprintf("last state of control plane: %#v", controlplane)
		},
	)
}
