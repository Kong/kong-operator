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

	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sresources "github.com/kong/kong-operator/v2/pkg/utils/kubernetes/resources"
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
			Name:      "test-dataplane",
			Namespace: "default",
			UID:       "test-uid",
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
							EmptyDir: &corev1.EmptyDirVolumeSource{
								Medium: corev1.StorageMediumMemory,
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

// TestDeploymentBuilder_BuildAndDeploy_LabelsAndAnnotations verifies that
// spec.deployment.labels/annotations are propagated onto the generated
// Deployment on creation, kept in sync on update, and that operator-managed
// annotations that are removed from the DataPlane spec are removed from the
// Deployment too (without clobbering annotations set by other actors).
func TestDeploymentBuilder_BuildAndDeploy_LabelsAndAnnotations(t *testing.T) {
	logger := logr.Discard()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, operatorv1beta1.AddToScheme(scheme))

	dataplane := &operatorv1beta1.DataPlane{
		Name:      "test-dataplane",
		Namespace: "default",
		UID:       "test-uid",
		Spec: operatorv1beta1.DataPlaneSpec{
			DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
				Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
					DeploymentOptions: operatorv1beta1.DeploymentOptions{
						Annotations: map[string]string{"foo": "bar"},
						Labels:      map[string]string{"foo": "bar"},
						PodTemplateSpec: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  consts.DataPlaneProxyContainerName,
										Image: "kong/kong-gateway:3.11",
									},
								},
							},
						},
					},
				},
			},
		},
	}

	fakeClient := fakectrlruntimeclient.
		NewClientBuilder().
		WithScheme(scheme).
		WithObjects(dataplane).
		Build()

	newBuilder := func() *DeploymentBuilder {
		return NewDeploymentBuilder(logger, fakeClient).
			WithDefaultImage("kong:3.0").
			WithClusterCertificate("test-cert")
	}

	deployment, res, err := newBuilder().BuildAndDeploy(t.Context(), dataplane, true, false)
	require.NoError(t, err)
	require.Equal(t, op.Created, res)
	require.NotNil(t, deployment)
	assert.Equal(t, "bar", deployment.Labels["foo"])
	assert.Equal(t, "bar", deployment.Annotations["foo"])
	assert.JSONEq(t, `{"foo":"bar"}`, deployment.Annotations[consts.AnnotationLastAppliedAnnotations])

	// Simulate an annotation added by another actor (e.g. kubectl), which must
	// survive the next reconciliation since it was never tracked by the operator.
	var existing appsv1.Deployment
	require.NoError(t, fakeClient.Get(t.Context(), client.ObjectKeyFromObject(deployment), &existing))
	existing.Annotations["added-by-other-actor"] = "keep-me"
	require.NoError(t, fakeClient.Update(t.Context(), &existing))

	// Update the DataPlane spec: drop "foo", add "baz".
	dataplane.Spec.Deployment.Annotations = map[string]string{"baz": "qux"}
	dataplane.Spec.Deployment.Labels = map[string]string{"baz": "qux"}

	deployment, res, err = newBuilder().BuildAndDeploy(t.Context(), dataplane, true, false)
	require.NoError(t, err)
	require.Equal(t, op.Updated, res)
	require.NotNil(t, deployment)

	assert.Equal(t, "qux", deployment.Labels["baz"])
	assert.NotContains(t, deployment.Labels, "foo")
	assert.Equal(t, dataplane.Name, deployment.Labels["app"], "base app label must survive the merge")

	assert.Equal(t, "qux", deployment.Annotations["baz"])
	assert.NotContains(t, deployment.Annotations, "foo", "annotation removed from spec must be removed from the Deployment")
	assert.Equal(t, "keep-me", deployment.Annotations["added-by-other-actor"], "annotations set by other actors must be preserved")
	assert.JSONEq(t, `{"baz":"qux"}`, deployment.Annotations[consts.AnnotationLastAppliedAnnotations])
}

// TestDeploymentBuilder_BuildAndDeploy_ScalingOnlyChangeIsNoop is a regression test
// for the DataPlane's HPA not being updated when only spec.deployment.scaling
// changes: reconcileDataPlaneDeployment used to hash the whole DataPlane spec
// (including Scaling, which has no effect on the Deployment) to decide whether the
// Deployment needed a patch, so a scaling-only change would falsely report
// op.Updated and pre-empt the DataPlane controller's HPA reconciliation for a pass.
func TestDeploymentBuilder_BuildAndDeploy_ScalingOnlyChangeIsNoop(t *testing.T) {
	dataplane := &operatorv1beta1.DataPlane{
		Name:      "test-dataplane",
		Namespace: "default",
		UID:       "test-uid",
		Spec: operatorv1beta1.DataPlaneSpec{
			DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
				Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
					DeploymentOptions: operatorv1beta1.DeploymentOptions{
						PodTemplateSpec: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  consts.DataPlaneProxyContainerName,
										Image: "kong/kong-gateway:3.11",
									},
								},
							},
						},
						Scaling: &operatorv1beta1.Scaling{
							HorizontalScaling: &operatorv1beta1.HorizontalScaling{
								MinReplicas: new(int32(2)),
								MaxReplicas: 3,
							},
						},
					},
				},
			},
		},
	}

	logger := logr.Discard()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, operatorv1beta1.AddToScheme(scheme))

	fakeClient := fakectrlruntimeclient.
		NewClientBuilder().
		WithScheme(scheme).
		WithObjects(dataplane).
		Build()

	builder := NewDeploymentBuilder(logger, fakeClient).
		WithDefaultImage("kong:3.0").
		WithClusterCertificate("test-cert").
		WithOpts(
			labelSelectorFromDataPlaneStatusSelectorDeploymentOpt(dataplane),
		)

	_, res, err := builder.BuildAndDeploy(t.Context(), dataplane, true, false)
	require.NoError(t, err)
	require.Equal(t, op.Created, res)

	// A second call with no spec changes lets the Deployment's PodTemplateSpec
	// settle to its fully-defaulted form (the fake client, unlike a real API
	// server, doesn't apply defaulting on Create), so the next call's diff is
	// solely attributable to the scaling change below.
	_, _, err = builder.BuildAndDeploy(t.Context(), dataplane, true, false)
	require.NoError(t, err)

	// Only the DataPlane's scaling changes; nothing that affects the Deployment
	// template, strategy, or replica count (existing replicas already satisfy the
	// new minReplicas).
	dataplane.Spec.Deployment.Scaling.HorizontalScaling.MinReplicas = new(int32(1))
	dataplane.Spec.Deployment.Scaling.HorizontalScaling.MaxReplicas = 2

	_, res, err = builder.BuildAndDeploy(t.Context(), dataplane, true, false)
	require.NoError(t, err)
	assert.Equal(t, op.Noop, res, "a scaling-only change must not report the Deployment as updated, "+
		"or it pre-empts the DataPlane controller's HPA reconciliation for a reconcile pass")
}

// TestDeploymentBuilder_BuildAndDeploy_ProbeDeleteWithoutEnforceConfig is a regression
// test for the readinessProbe/livenessProbe/startupProbe: {} delete sentinel (see
// StrategicMergePatchPodTemplateSpec) silently no-oping under --enforce-config=false.
// StrategicMergePatchPodTemplateSpec used to mutate the caller's PodTemplateSpec in
// place, which for this call chain is a pointer into the live DataPlane's
// spec.deployment.podTemplateSpec: the `{}` sentinel would be erased to nil before the
// spec hash used by the enforceConfig=false skip-update check was computed, so the
// hash never changed and the probe was never actually removed from the Deployment.
func TestDeploymentBuilder_BuildAndDeploy_ProbeDeleteWithoutEnforceConfig(t *testing.T) {
	dataplane := &operatorv1beta1.DataPlane{
		Name:      "test-dataplane",
		Namespace: "default",
		UID:       "test-uid",
		Spec: operatorv1beta1.DataPlaneSpec{
			DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
				Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
					DeploymentOptions: operatorv1beta1.DeploymentOptions{
						PodTemplateSpec: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  consts.DataPlaneProxyContainerName,
										Image: "kong/kong-gateway:3.11",
									},
								},
							},
						},
					},
				},
			},
		},
	}

	fakeClient := fakectrlruntimeclient.
		NewClientBuilder().
		WithScheme(scheme.Get()).
		WithObjects(dataplane).
		Build()

	builder := NewDeploymentBuilder(logr.Discard(), fakeClient).
		WithDefaultImage("kong:3.0").
		WithClusterCertificate("test-cert").
		WithOpts(
			labelSelectorFromDataPlaneStatusSelectorDeploymentOpt(dataplane),
		)

	created, res, err := builder.BuildAndDeploy(t.Context(), dataplane, true, false)
	require.NoError(t, err)
	require.Equal(t, op.Created, res)
	deploymentKey := client.ObjectKeyFromObject(created)

	// A second call with no spec changes lets the Deployment's PodTemplateSpec settle
	// to its fully-defaulted form (the fake client, unlike a real API server, doesn't
	// apply defaulting on Create), so the next call's diff is solely attributable to
	// the probe deletion below.
	_, _, err = builder.BuildAndDeploy(t.Context(), dataplane, true, false)
	require.NoError(t, err)

	var deployment appsv1.Deployment
	require.NoError(t, fakeClient.Get(t.Context(), deploymentKey, &deployment))
	require.NotNil(t, deployment.Spec.Template.Spec.Containers[0].ReadinessProbe,
		"sanity check: the generated Deployment must have a default readiness probe to delete")

	// Delete the readiness probe via the empty-probe sentinel.
	dataplane.Spec.Deployment.PodTemplateSpec.Spec.Containers[0].ReadinessProbe = &corev1.Probe{}

	_, res, err = builder.BuildAndDeploy(t.Context(), dataplane, false, false)
	require.NoError(t, err)
	assert.NotEqual(t, op.Noop, res, "the probe delete must not be skipped by the enforceConfig=false spec-hash check")

	require.NoError(t, fakeClient.Get(t.Context(), deploymentKey, &deployment))
	assert.Nil(t, deployment.Spec.Template.Spec.Containers[0].ReadinessProbe,
		"the readiness probe must actually be removed from the Deployment")

	assert.Equal(t, &corev1.Probe{},
		dataplane.Spec.Deployment.PodTemplateSpec.Spec.Containers[0].ReadinessProbe,
		"the DataPlane object itself must not have been mutated by the reconcile",
	)
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
				Name:      "test-dataplane",
				Namespace: "default",
				UID:       "test-uid",
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
				Name:      "test-dataplane",
				Namespace: "default",
				UID:       "test-uid",
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

			deployment, err := generateDataPlaneDeployment(logr.Discard(), tc.validateDataPlaneImage, tc.dataplane, tc.defaultImage, additionalLabels)

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

func TestGenerateDataPlaneDeployment_LabelsAndAnnotations(t *testing.T) {
	dataplane := &operatorv1beta1.DataPlane{
		Name:      "test-dataplane",
		Namespace: "default",
		UID:       "test-uid",
		Spec: operatorv1beta1.DataPlaneSpec{
			DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
				Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
					DeploymentOptions: operatorv1beta1.DeploymentOptions{
						Annotations: map[string]string{
							"deployment-annotation": "value",
						},
						Labels: map[string]string{
							"deployment-label": "value",
						},
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
	}

	deployment, err := generateDataPlaneDeployment(logr.Discard(), false, dataplane, "kong:3.0", nil)
	require.NoError(t, err)
	require.NotNil(t, deployment)

	assert.Equal(t, "value", deployment.Labels["deployment-label"])
	assert.Equal(t, "value", deployment.Annotations["deployment-annotation"])
	// the base "app" label set by the generator must survive the merge.
	assert.Equal(t, dataplane.Name, deployment.Labels["app"])
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

// errorCountSink is a minimal logr.LogSink that counts Error() calls.
type errorCountSink struct{ count *int }

func (s errorCountSink) Init(logr.RuntimeInfo)             {}
func (s errorCountSink) Enabled(int) bool                  { return true }
func (s errorCountSink) Info(_ int, _ string, _ ...any)    {}
func (s errorCountSink) Error(_ error, _ string, _ ...any) { *s.count++ }
func (s errorCountSink) WithValues(_ ...any) logr.LogSink  { return s }
func (s errorCountSink) WithName(_ string) logr.LogSink    { return s }

// infoCountSink is a minimal logr.LogSink that counts Info() calls.
type infoCountSink struct{ count *int }

func (s infoCountSink) Init(logr.RuntimeInfo)             {}
func (s infoCountSink) Enabled(int) bool                  { return true }
func (s infoCountSink) Info(_ int, _ string, _ ...any)    { *s.count++ }
func (s infoCountSink) Error(_ error, _ string, _ ...any) {}
func (s infoCountSink) WithValues(_ ...any) logr.LogSink  { return s }
func (s infoCountSink) WithName(_ string) logr.LogSink    { return s }

func TestWarnOperatorManagedEnvVars(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, operatorv1beta1.AddToScheme(scheme))

	makeDP := func(envVars ...corev1.EnvVar) *operatorv1beta1.DataPlane {
		return &operatorv1beta1.DataPlane{
			Name: "dp", Namespace: "default",
			Spec: operatorv1beta1.DataPlaneSpec{
				DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
					Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
						DeploymentOptions: operatorv1beta1.DeploymentOptions{
							PodTemplateSpec: &corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{
										{Name: consts.DataPlaneProxyContainerName, Env: envVars},
									},
								},
							},
						},
					},
				},
			},
		}
	}

	tests := []struct {
		name         string
		dp           *operatorv1beta1.DataPlane
		expectErrors int
	}{
		{
			name:         "no managed env vars in spec — no warning",
			dp:           makeDP(),
			expectErrors: 0,
		},
		{
			name:         "user sets KONG_CLUSTER_CERT — one warning",
			dp:           makeDP(corev1.EnvVar{Name: envKongClusterCert, Value: "/some/path"}),
			expectErrors: 1,
		},
		{
			name: "user sets both managed env vars — two warnings",
			dp: makeDP(
				corev1.EnvVar{Name: envKongClusterCert, Value: "/some/path/tls.crt"},
				corev1.EnvVar{Name: envKongClusterCertKey, Value: "/some/path/tls.key"},
			),
			expectErrors: 2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			count := 0
			logger := logr.New(errorCountSink{count: &count})
			cl := fakectrlruntimeclient.NewClientBuilder().WithScheme(scheme).Build()
			warnOperatorManagedEnvVars(t.Context(), logger, tc.dp, cl)
			assert.Equal(t, tc.expectErrors, count)
		})
	}
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
				Annotations: map[string]string{
					restartAnnotationKey: "",
				},
			},
			expectedValue:  "",
			expectedResult: false,
		},
		{
			name: "pod template with valid restart annotation",
			podTemplate: &corev1.PodTemplateSpec{
				Annotations: map[string]string{
					restartAnnotationKey: "2023-10-01T10:00:00Z",
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
			expectedReduced:    false,
			expectError:        false,
			expectedDeployment: "deployment-1",
		},
		{
			name: "multiple deployments",
			existingDeployments: []appsv1.Deployment{
				{
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
				{
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
				Name:      "test",
				Namespace: "default",
				UID:       "test-uid",
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
				Annotations: map[string]string{
					restartAnnotationKey: currentTime.Format(time.RFC3339),
				},
			},
			expectedResult: true,
		},
		{
			name: "pod template with old restart annotation",
			podTemplate: &corev1.PodTemplateSpec{
				Annotations: map[string]string{
					restartAnnotationKey: oldTime.Format(time.RFC3339),
				},
			},
			expectedResult: false,
		},
		{
			name: "pod template with invalid restart annotation",
			podTemplate: &corev1.PodTemplateSpec{
				Annotations: map[string]string{
					restartAnnotationKey: "invalid-time",
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
