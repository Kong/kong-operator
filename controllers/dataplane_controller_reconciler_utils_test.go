package controllers

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/scheme"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
	k8sresources "github.com/kong/gateway-operator/internal/utils/kubernetes/resources"
	"github.com/kong/gateway-operator/internal/versions"
)

func init() {
	if err := gatewayv1beta1.AddToScheme(scheme.Scheme); err != nil {
		fmt.Println("error while adding gatewayv1beta1 scheme")
		os.Exit(1)
	}
	if err := operatorv1alpha1.AddToScheme(scheme.Scheme); err != nil {
		fmt.Println("error while adding operatorv1alpha1 scheme")
		os.Exit(1)
	}
}

func TestEnsureDeploymentForDataPlane(t *testing.T) {
	expectedDeploymentStrategy := appsv1.DeploymentStrategy{
		Type: appsv1.RollingUpdateDeploymentStrategyType,
		RollingUpdate: &appsv1.RollingUpdateDeployment{
			MaxUnavailable: &intstr.IntOrString{
				Type:   intstr.Int,
				IntVal: 0,
			},
			MaxSurge: &intstr.IntOrString{
				Type:   intstr.Int,
				IntVal: 1,
			},
		},
	}

	testCases := []struct {
		name           string
		dataPlane      *operatorv1alpha1.DataPlane
		certSecretName string
		testBody       func(t *testing.T, reconciler DataPlaneReconciler, dataPlane *operatorv1alpha1.DataPlane, certSecretName string)
	}{
		{
			name: "no existing DataPlane deployment",
			dataPlane: &operatorv1alpha1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
			},
			certSecretName: "certificate",
			testBody: func(t *testing.T, reconciler DataPlaneReconciler, dataPlane *operatorv1alpha1.DataPlane, certSecretName string) {
				ctx := context.Background()
				createdOrUpdated, deployment, err := reconciler.ensureDeploymentForDataPlane(ctx, dataPlane, certSecretName)
				require.True(t, createdOrUpdated)
				require.NoError(t, err)
				require.Equal(t, expectedDeploymentStrategy, deployment.Spec.Strategy)
			},
		},
		{
			name: "existing DataPlane deployment gets updated with expected spec.Strategy",
			dataPlane: &operatorv1alpha1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
			},
			certSecretName: "certificate",
			testBody: func(t *testing.T, reconciler DataPlaneReconciler, dataPlane *operatorv1alpha1.DataPlane, certSecretName string) {
				ctx := context.Background()
				dataplaneImage, err := generateDataPlaneImage(dataPlane, versions.IsDataPlaneSupported)
				require.NoError(t, err)
				// generate the DataPlane as it is supposed to be, change the .spec.strategy field, and create it.
				existingDeployment := k8sresources.GenerateNewDeploymentForDataPlane(dataPlane, dataplaneImage, certSecretName)
				existingDeployment.Spec.Strategy.RollingUpdate.MaxUnavailable = &intstr.IntOrString{
					Type:   intstr.Int,
					IntVal: 5,
				}
				k8sutils.SetOwnerForObject(existingDeployment, dataPlane)
				addLabelForDataplane(existingDeployment)
				require.NoError(t, reconciler.Client.Create(ctx, existingDeployment))

				createdOrUpdated, deployment, err := reconciler.ensureDeploymentForDataPlane(ctx, dataPlane, certSecretName)
				require.True(t, createdOrUpdated, "the DataPlane deployment should be updated with the original strategy")
				require.NoError(t, err)
				require.Equal(t, expectedDeploymentStrategy, deployment.Spec.Strategy)
			},
		},
		{
			name: "existing DataPlane deployment does not get updated when already has expected spec.Strategy",
			dataPlane: &operatorv1alpha1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
			},
			certSecretName: "certificate",
			testBody: func(t *testing.T, reconciler DataPlaneReconciler, dataPlane *operatorv1alpha1.DataPlane, certSecretName string) {
				ctx := context.Background()
				dataplaneImage, err := generateDataPlaneImage(dataPlane, versions.IsDataPlaneSupported)
				require.NoError(t, err)
				// generate the DataPlane as it is expected to be and create it.
				existingDeployment := k8sresources.GenerateNewDeploymentForDataPlane(dataPlane, dataplaneImage, certSecretName)
				k8sutils.SetOwnerForObject(existingDeployment, dataPlane)
				addLabelForDataplane(existingDeployment)
				require.NoError(t, reconciler.Client.Create(ctx, existingDeployment))

				createdOrUpdated, deployment, err := reconciler.ensureDeploymentForDataPlane(ctx, dataPlane, certSecretName)
				require.False(t, createdOrUpdated, "the DataPlane deployment should not be updated")
				require.NoError(t, err)
				require.Equal(t, expectedDeploymentStrategy, deployment.Spec.Strategy)
			},
		},
	}

	for _, tc := range testCases {
		tc := tc

		fakeClient := fakectrlruntimeclient.
			NewClientBuilder().
			WithScheme(scheme.Scheme).
			Build()

		reconciler := DataPlaneReconciler{
			Client: fakeClient,
		}

		t.Run(tc.name, func(t *testing.T) {
			tc.testBody(t, reconciler, tc.dataPlane, tc.certSecretName)
		})
	}
}
