package dataplane

import (
	"maps"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	operatorv1beta1 "github.com/kong/kong-operator/api/gateway-operator/v1beta1"
	"github.com/kong/kong-operator/pkg/consts"
	k8sresources "github.com/kong/kong-operator/pkg/utils/kubernetes/resources"
)

func TestNewDeploymentBuilder(t *testing.T) {
	logger := logr.Discard()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, operatorv1beta1.AddToScheme(scheme))

	fakeClient := fakectrlruntimeclient.
		NewClientBuilder().
		WithScheme(scheme).
		Build()

	builder := NewDeploymentBuilder(logger, fakeClient)
	assert.NotNil(t, builder)
	assert.Equal(t, logger, builder.logger)
	assert.Equal(t, fakeClient, builder.client)
}

func TestDeploymentBuilderWithOptions(t *testing.T) {
	logger := logr.Discard()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, operatorv1beta1.AddToScheme(scheme))

	dataplane := &operatorv1beta1.DataPlane{}
	dataplane.Status.Selector = "test-selector"

	fakeClient := fakectrlruntimeclient.
		NewClientBuilder().
		WithScheme(scheme).
		Build()

	builder := NewDeploymentBuilder(logger, fakeClient)

	// Test WithClusterCertificate
	certName := "test-cert"
	builder = builder.WithClusterCertificate(certName)
	assert.Equal(t, certName, builder.clusterCertificateName)

	// Test WithAdditionalLabels
	labels := client.MatchingLabels{"test": "label"}
	builder = builder.WithAdditionalLabels(labels)
	assert.Equal(t, labels, builder.additionalLabels)

	// Test WithDefaultImage
	image := "test-image:latest"
	builder = builder.WithDefaultImage(image)
	assert.Equal(t, image, builder.defaultImage)

	// Test WithSecretLabelSelector
	selector := "test-selector"
	builder = builder.WithSecretLabelSelector(selector)
	assert.Equal(t, selector, builder.secretLabelSelector)

	// Test WithOpts
	opts := []k8sresources.DeploymentOpt{
		labelSelectorFromDataPlaneStatusSelectorDeploymentOpt(dataplane),
	}
	builder = builder.WithOpts(opts...)
	assert.Equal(t, opts, builder.opts)
}

func TestDeploymentBuilder_BuildAndDeploy(t *testing.T) {
	type dataplaneGenParams struct {
		image   string
		volumes []corev1.Volume
	}
	// Helper to generate a DataPlane with specified image and volumes
	dataplaneGen := func(params dataplaneGenParams) *operatorv1beta1.DataPlane {
		return &operatorv1beta1.DataPlane{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-dataplane",
				Namespace: "default",
				UID:       "test-uid",
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
											Image: params.image,
										},
									},
									Volumes: params.volumes,
								},
							},
						},
					},
				},
			},
		}
	}
	testCases := []struct {
		name                   string
		dataplane              *operatorv1beta1.DataPlane
		enforceConfig          bool
		validateDataPlaneImage bool
		expectError            bool
	}{
		{
			name: "custom image fails validation",
			dataplane: dataplaneGen(
				dataplaneGenParams{
					image: "custom-kong:2.8",
				},
			),
			enforceConfig:          true,
			validateDataPlaneImage: true,
			expectError:            true,
		},
		{
			name: "custom image passes validation",
			dataplane: dataplaneGen(
				dataplaneGenParams{
					image: "custom-kong:2.8",
				},
			),
			enforceConfig:          true,
			validateDataPlaneImage: false,
			expectError:            false,
		},
		{
			name: "kong image succeeds validation",
			dataplane: dataplaneGen(
				dataplaneGenParams{
					image: "kong/kong-gateway:3.11",
				},
			),
			enforceConfig:          true,
			validateDataPlaneImage: true,
			expectError:            false,
		},
		{
			name: "custom volume succeeds",
			dataplane: dataplaneGen(
				dataplaneGenParams{
					image: "kong/kong-gateway:3.11",
					volumes: []corev1.Volume{
						{
							Name: "custom-volume",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{
									Medium: corev1.StorageMediumMemory,
								},
							},
						},
					},
				},
			),
			enforceConfig:          true,
			validateDataPlaneImage: true,
			expectError:            false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			logger := logr.Discard()
			scheme := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(scheme))
			require.NoError(t, appsv1.AddToScheme(scheme))
			require.NoError(t, operatorv1beta1.AddToScheme(scheme))

			fakeClient := fakectrlruntimeclient.
				NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tc.dataplane).
				Build()

			builder := NewDeploymentBuilder(logger, fakeClient).
				WithDefaultImage("kong:3.0").
				WithClusterCertificate("test-cert").
				WithAdditionalLabels(map[string]string{"app": "test"}).
				WithSecretLabelSelector("test-selector").
				WithOpts(
					labelSelectorFromDataPlaneStatusSelectorDeploymentOpt(tc.dataplane),
				)

			deployment, res, err := builder.BuildAndDeploy(t.Context(), tc.dataplane, tc.enforceConfig, tc.validateDataPlaneImage)

			if tc.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.NotNil(t, deployment)
			assert.NotNil(t, res)

			// Verify deployment exists in fake client
			var fetchedDeployment appsv1.Deployment
			err = fakeClient.Get(t.Context(), client.ObjectKey{
				Name:      deployment.Name,
				Namespace: deployment.Namespace,
			}, &fetchedDeployment)
			require.NoError(t, err)
			assert.Equal(t, deployment.Name, fetchedDeployment.Name)
			assert.Equal(t, deployment.Namespace, fetchedDeployment.Namespace)
		})
	}
}

func TestGenerateDataPlaneDeployment(t *testing.T) {
	testCases := []struct {
		name                   string
		dataplane              *operatorv1beta1.DataPlane
		defaultImage           string
		validateDataPlaneImage bool
		additionalLabels       map[string]string
		expectError            bool
		expectedImage          string
	}{
		{
			name: "default image is used when not specified in dataplane",
			dataplane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dataplane",
					Namespace: "default",
					UID:       "test-uid",
				},
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
							DeploymentOptions: operatorv1beta1.DeploymentOptions{
								PodTemplateSpec: &corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{
											{
												Name: consts.DataPlaneProxyContainerName,
											},
										},
									},
								},
							},
						},
					},
				},
			},
			defaultImage:           "kong:3.0",
			validateDataPlaneImage: false,
			additionalLabels:       map[string]string{"app": "test"},
			expectError:            false,
			expectedImage:          "kong:3.0",
		},
		{
			name: "dataplane image is used when specified",
			dataplane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dataplane",
					Namespace: "default",
					UID:       "test-uid",
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
												Image: "custom-kong:2.8",
											},
										},
									},
								},
							},
						},
					},
				},
			},
			defaultImage:           "kong:3.0",
			validateDataPlaneImage: false,
			additionalLabels:       map[string]string{"app": "test"},
			expectError:            false,
			expectedImage:          "custom-kong:2.8",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			additionalLabels := map[string]string{}
			maps.Copy(additionalLabels, tc.additionalLabels)

			deployment, err := generateDataPlaneDeployment(tc.validateDataPlaneImage, tc.dataplane, tc.defaultImage, additionalLabels)

			if tc.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.NotNil(t, deployment)

			// Check if labels were applied correctly
			for k, v := range tc.additionalLabels {
				assert.Equal(t, v, deployment.Labels[k])
			}

			// Find the proxy container and check its image
			var proxyContainer *corev1.Container
			for i, container := range deployment.Spec.Template.Spec.Containers {
				if container.Name == consts.DataPlaneProxyContainerName {
					proxyContainer = &deployment.Spec.Template.Spec.Containers[i]
					break
				}
			}

			require.NotNil(t, proxyContainer, "Proxy container not found")
			assert.Equal(t, tc.expectedImage, proxyContainer.Image)
		})
	}
}

func TestApplyDeploymentUserPatchesForDataPlane(t *testing.T) {
	testCases := []struct {
		name           string
		dataplane      *operatorv1beta1.DataPlane
		deployment     *k8sresources.Deployment
		expectError    bool
		expectedEnvVar string
		expectedValue  string
	}{
		{
			name: "user patch is applied correctly",
			dataplane: &operatorv1beta1.DataPlane{
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
							DeploymentOptions: operatorv1beta1.DeploymentOptions{
								PodTemplateSpec: &corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{
											{
												Name: consts.DataPlaneProxyContainerName,
												Env: []corev1.EnvVar{
													{
														Name:  "TEST_VAR",
														Value: "test-value",
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
			deployment: &k8sresources.Deployment{
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: consts.DataPlaneProxyContainerName,
								},
							},
						},
					},
				},
			},
			expectError:    false,
			expectedEnvVar: "TEST_VAR",
			expectedValue:  "test-value",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := applyDeploymentUserPatchesForDataPlane(tc.dataplane, tc.deployment)

			if tc.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.NotNil(t, result)

			// Find the proxy container and check if env var was applied
			var proxyContainer *corev1.Container
			for i, container := range result.Spec.Template.Spec.Containers {
				if container.Name == consts.DataPlaneProxyContainerName {
					proxyContainer = &result.Spec.Template.Spec.Containers[i]
					break
				}
			}

			require.NotNil(t, proxyContainer, "Proxy container not found")

			var foundEnvVar *corev1.EnvVar
			for i, env := range proxyContainer.Env {
				if env.Name == tc.expectedEnvVar {
					foundEnvVar = &proxyContainer.Env[i]
					break
				}
			}

			require.NotNil(t, foundEnvVar, "Expected env var not found")
			assert.Equal(t, tc.expectedValue, foundEnvVar.Value)
		})
	}
}

func TestSetClusterCertVars(t *testing.T) {
	deployment := &k8sresources.Deployment{
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: consts.DataPlaneProxyContainerName,
						},
					},
				},
			},
		},
	}

	secretName := "test-cert-secret"
	result := setClusterCertVars(deployment, secretName)

	// Check if volume was added
	volumeFound := false
	for _, vol := range result.Spec.Template.Spec.Volumes {
		if vol.Name == consts.ClusterCertificateVolume {
			volumeFound = true
			assert.Equal(t, secretName, vol.Secret.SecretName)
			break
		}
	}
	assert.True(t, volumeFound, "Volume not found")

	// Find proxy container
	var proxyContainer *corev1.Container
	for i, container := range result.Spec.Template.Spec.Containers {
		if container.Name == consts.DataPlaneProxyContainerName {
			proxyContainer = &result.Spec.Template.Spec.Containers[i]
			break
		}
	}
	require.NotNil(t, proxyContainer, "Proxy container not found")

	// Check if volume mount was added
	volumeMountFound := false
	for _, mount := range proxyContainer.VolumeMounts {
		if mount.Name == consts.ClusterCertificateVolume {
			volumeMountFound = true
			assert.Equal(t, consts.ClusterCertificateVolumeMountPath, mount.MountPath)
			break
		}
	}
	assert.True(t, volumeMountFound, "Volume mount not found")

	// Check if env vars were set
	certEnvFound := false
	keyEnvFound := false
	for _, env := range proxyContainer.Env {
		if env.Name == "KONG_CLUSTER_CERT" {
			certEnvFound = true
			assert.Contains(t, env.Value, "tls.crt")
		}
		if env.Name == "KONG_CLUSTER_CERT_KEY" {
			keyEnvFound = true
			assert.Contains(t, env.Value, "tls.key")
		}
	}
	assert.True(t, certEnvFound, "KONG_CLUSTER_CERT env var not found")
	assert.True(t, keyEnvFound, "KONG_CLUSTER_CERT_KEY env var not found")
}

func TestPodTemplateSpecHasRestartAnnotation(t *testing.T) {
	testCases := []struct {
		name           string
		podTemplate    *corev1.PodTemplateSpec
		expectedValue  string
		expectedResult bool
	}{
		{
			name:           "nil pod template",
			podTemplate:    nil,
			expectedValue:  "",
			expectedResult: false,
		},
		{
			name: "pod template without annotations",
			podTemplate: &corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{},
			},
			expectedValue:  "",
			expectedResult: false,
		},
		{
			name: "pod template with empty restart annotation",
			podTemplate: &corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						restartAnnotationKey: "",
					},
				},
			},
			expectedValue:  "",
			expectedResult: false,
		},
		{
			name: "pod template with valid restart annotation",
			podTemplate: &corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						restartAnnotationKey: "2023-10-01T10:00:00Z",
					},
				},
			},
			expectedValue:  "2023-10-01T10:00:00Z",
			expectedResult: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			value, result := podTemplateSpecHasRestartAnnotation(tc.podTemplate)
			assert.Equal(t, tc.expectedValue, value)
			assert.Equal(t, tc.expectedResult, result)
		})
	}
}

func TestListOrReduceDataPlaneDeployments(t *testing.T) {
	testCases := []struct {
		name                string
		existingDeployments []appsv1.Deployment
		expectedReduced     bool
		expectError         bool
		expectedDeployment  string
	}{
		{
			name:                "no deployments",
			existingDeployments: []appsv1.Deployment{},
			expectedReduced:     false,
			expectError:         false,
			expectedDeployment:  "",
		},
		{
			name: "one deployment",
			existingDeployments: []appsv1.Deployment{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "deployment-1",
						Namespace: "default",
						Labels: map[string]string{
							"app":                                    "test",
							"gateway-operator.konghq.com/managed-by": "dataplane",
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: "gateway-operator.konghq.com/v1beta1",
								Kind:       "DataPlane",
								UID:        "test-uid",
							},
						},
					},
				},
			},
			expectedReduced:    false,
			expectError:        false,
			expectedDeployment: "deployment-1",
		},
		{
			name: "multiple deployments",
			existingDeployments: []appsv1.Deployment{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "deployment-1",
						Namespace: "default",
						Labels: map[string]string{
							"app":                                    "test",
							"gateway-operator.konghq.com/managed-by": "dataplane",
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: "gateway-operator.konghq.com/v1beta1",
								Kind:       "DataPlane",
								UID:        "test-uid",
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "deployment-2",
						Namespace: "default",
						Labels: map[string]string{
							"app":                                    "test",
							"gateway-operator.konghq.com/managed-by": "dataplane",
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: "gateway-operator.konghq.com/v1beta1",
								Kind:       "DataPlane",
								UID:        "test-uid",
							},
						},
					},
				},
			},
			expectedReduced:    true,
			expectError:        true,
			expectedDeployment: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(scheme))
			require.NoError(t, appsv1.AddToScheme(scheme))
			require.NoError(t, operatorv1beta1.AddToScheme(scheme))
			require.NoError(t, gatewayv1.Install(scheme))

			dataplane := &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
					UID:       "test-uid",
				},
			}

			clientBuilder := fakectrlruntimeclient.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(dataplane)

			for i := range tc.existingDeployments {
				clientBuilder = clientBuilder.WithObjects(&tc.existingDeployments[i])
			}

			client := clientBuilder.Build()

			reduced, deployment, err := listOrReduceDataPlaneDeployments(t.Context(), client, dataplane, map[string]string{})

			assert.Equal(t, tc.expectedReduced, reduced)

			if tc.expectError {
				assert.Error(t, err)
				return
			}

			if tc.expectedDeployment == "" {
				assert.Nil(t, deployment)
				return
			}

			require.NotNil(t, deployment)
			assert.Equal(t, tc.expectedDeployment, deployment.Name)
		})
	}
}

func TestIsRecentDeploymentRestart(t *testing.T) {
	currentTime := metav1.Now()
	oldTime := metav1.NewTime(currentTime.Add(-10 * 60 * time.Minute)) // 10 minutes old

	testCases := []struct {
		name           string
		podTemplate    *corev1.PodTemplateSpec
		expectedResult bool
	}{
		{
			name:           "nil pod template",
			podTemplate:    nil,
			expectedResult: false,
		},
		{
			name: "pod template with recent restart annotation",
			podTemplate: &corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						restartAnnotationKey: currentTime.Format(time.RFC3339),
					},
				},
			},
			expectedResult: true,
		},
		{
			name: "pod template with old restart annotation",
			podTemplate: &corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						restartAnnotationKey: oldTime.Format(time.RFC3339),
					},
				},
			},
			expectedResult: false,
		},
		{
			name: "pod template with invalid restart annotation",
			podTemplate: &corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						restartAnnotationKey: "invalid-time",
					},
				},
			},
			expectedResult: true, // Unparseable times are treated as restart for safety
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, result := isRecentDeploymentRestart(tc.podTemplate, logr.Discard())
			assert.Equal(t, tc.expectedResult, result)
		})
	}
}
