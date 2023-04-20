package controllers

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/scheme"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
	"github.com/kong/gateway-operator/internal/utils/kubernetes/resources"
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
	defaultDataPlaneResources := resources.DefaultDataPlaneResources()

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
				res, deployment, err := reconciler.ensureDeploymentForDataPlane(ctx, logr.Discard(), dataPlane, certSecretName)
				require.Equal(t, Created, res)
				require.NoError(t, err)
				require.Equal(t, expectedDeploymentStrategy, deployment.Spec.Strategy)
			},
		},
		{
			name: "new DataPlane with custom secret",
			dataPlane: &operatorv1alpha1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret-volume",
					Namespace: "default",
				},
				Spec: operatorv1alpha1.DataPlaneSpec{
					DataPlaneOptions: operatorv1alpha1.DataPlaneOptions{
						Deployment: operatorv1alpha1.DeploymentOptions{
							Volumes: []corev1.Volume{
								{
									Name: "test-volume",
									VolumeSource: corev1.VolumeSource{
										Secret: &corev1.SecretVolumeSource{
											SecretName: "test-secret",
										},
									},
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "test-volume",
									MountPath: "/var/test/",
									ReadOnly:  true,
								},
							},
						},
					},
				},
			},
			certSecretName: "certificate",
			testBody: func(t *testing.T, reconciler DataPlaneReconciler, dataPlane *operatorv1alpha1.DataPlane, certSecretName string) {
				ctx := context.Background()
				createdOrUpdated, deployment, err := reconciler.ensureDeploymentForDataPlane(ctx, logr.Discard(), dataPlane, certSecretName)
				require.Equal(t, createdOrUpdated, Created)
				require.NoError(t, err)
				require.Contains(t, deployment.Spec.Template.Spec.Volumes, corev1.Volume{
					Name: "test-volume",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: "test-secret",
						},
					},
				})
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

				res, deployment, err := reconciler.ensureDeploymentForDataPlane(ctx, logr.Discard(), dataPlane, certSecretName)
				require.Equal(t, Updated, res, "the DataPlane deployment should be updated with the original strategy")
				require.NoError(t, err)
				require.Equal(t, expectedDeploymentStrategy, deployment.Spec.Strategy)
			},
		},
		{
			name: "existing DataPlane deployment does get updated when it doesn't have the resources equal to defaults",
			dataPlane: &operatorv1alpha1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
				Spec: operatorv1alpha1.DataPlaneSpec{
					DataPlaneOptions: operatorv1alpha1.DataPlaneOptions{
						Deployment: operatorv1alpha1.DeploymentOptions{
							Resources: &corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("2"),
									corev1.ResourceMemory: resource.MustParse("1237Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("3"),
									corev1.ResourceMemory: resource.MustParse("1237Mi"),
								},
							},
						},
					},
				},
			},
			certSecretName: "certificate",
			testBody: func(t *testing.T, reconciler DataPlaneReconciler, dataPlane *operatorv1alpha1.DataPlane, certSecretName string) {
				ctx := context.Background()
				dataplaneImage, err := generateDataPlaneImage(dataPlane, versions.IsDataPlaneSupported)
				require.NoError(t, err)
				// generate the DataPlane as it is expected to be and create it.
				existingDeployment := k8sresources.GenerateNewDeploymentForDataPlane(dataPlane, dataplaneImage, certSecretName)

				// generateDataPlaneImage will set deployment's containers resources
				// to the ones set in dataplane spec so we set it here to get the
				// expected behavior in reconciler's ensureDeploymentForDataPlane().
				dataPlane.Spec.Deployment.Resources.Limits[corev1.ResourceCPU] = resource.MustParse("1337m")

				k8sutils.SetOwnerForObject(existingDeployment, dataPlane)
				addLabelForDataplane(existingDeployment)
				require.NoError(t, reconciler.Client.Create(ctx, existingDeployment))

				res, deployment, err := reconciler.ensureDeploymentForDataPlane(ctx, logr.Discard(), dataPlane, certSecretName)
				require.Equal(t, Updated, res, "the DataPlane deployment should be updated to get the resources set to defaults")
				require.NoError(t, err)
				require.Len(t, deployment.Spec.Template.Spec.Containers, 1)
				require.Equal(t, *dataPlane.Spec.Deployment.Resources, deployment.Spec.Template.Spec.Containers[0].Resources)
			},
		},
		{
			name: "existing DataPlane deployment does not get updated when already has expected spec.Strategy and resources equal to defaults",
			dataPlane: &operatorv1alpha1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
				Spec: operatorv1alpha1.DataPlaneSpec{
					DataPlaneOptions: operatorv1alpha1.DataPlaneOptions{
						Deployment: operatorv1alpha1.DeploymentOptions{
							Resources: defaultDataPlaneResources,
						},
					},
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

				res, deployment, err := reconciler.ensureDeploymentForDataPlane(ctx, logr.Discard(), dataPlane, certSecretName)
				require.Equal(t, Noop, res, "the DataPlane deployment should not be updated")
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
