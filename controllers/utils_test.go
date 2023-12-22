package controllers

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math"
	"math/big"
	"testing"
	"time"

	"github.com/kong/kubernetes-testing-framework/pkg/utils/kubernetes/generators"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	ctrlruntimelog "sigs.k8s.io/controller-runtime/pkg/log/zap"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	operatorv1beta1 "github.com/kong/gateway-operator/apis/v1beta1"
	"github.com/kong/gateway-operator/controllers/pkg/log"
	"github.com/kong/gateway-operator/controllers/pkg/op"
	gwtypes "github.com/kong/gateway-operator/internal/types"
	k8sresources "github.com/kong/gateway-operator/internal/utils/kubernetes/resources"
)

func Test_ensureContainerImageUpdated(t *testing.T) {
	for _, tt := range []struct {
		name          string
		originalImage string
		newImage      string
		expectedImage string
		updated       bool
		wantErr       string
	}{
		{
			name:          "invalid images produce an error",
			originalImage: "fake:invalid:image:2.7.0",
			newImage:      "kong/kong:2.7.0",
			wantErr:       "invalid container image found: fake:invalid:image:2.7.0",
		},
		{
			name:          "setting new image when existing is local with port is allowed",
			originalImage: "localhost:5000/kic:2.7.0",
			newImage:      "kong/kong:2.7.0",
			expectedImage: "kong/kong:2.7.0",
			updated:       true,
		},
		{
			name:          "setting new local image is allowed",
			originalImage: "kong/kong:2.7.0",
			newImage:      "localhost:5000/kong:2.7.0",
			expectedImage: "localhost:5000/kong:2.7.0",
			updated:       true,
		},
		{
			name:          "same image and version makes no changes",
			originalImage: "kong/kong:2.7.0",
			newImage:      "kong/kong:2.7.0",
			expectedImage: "kong/kong:2.7.0",
			updated:       false,
		},
		{
			name:          "version added when not originally present",
			originalImage: "kong/kong",
			newImage:      "kong/kong:2.7.0",
			expectedImage: "kong/kong:2.7.0",
			updated:       true,
		},
		{
			name:          "version is changed when a new one is provided",
			originalImage: "kong/kong:2.7.0",
			newImage:      "kong/kong:3.0.0",
			expectedImage: "kong/kong:3.0.0",
			updated:       true,
		},
		{
			name:          "image is added when not originally present",
			originalImage: "",
			newImage:      "kong/kong",
			expectedImage: "kong/kong",
			updated:       true,
		},
		{
			name:          "image is changed when a new one is provided",
			originalImage: "kong/kong",
			newImage:      "kong/kong-gateway",
			expectedImage: "kong/kong-gateway",
			updated:       true,
		},
		{
			name:          "image and version are added when not originally present",
			originalImage: "",
			newImage:      "kong/kong-gateway:3.0.0",
			expectedImage: "kong/kong-gateway:3.0.0",
			updated:       true,
		},
		{
			name:          "image and version are changed when new ones are provided",
			originalImage: "kong/kong:2.7.0",
			newImage:      "kong/kong-gateway:3.0.0",
			expectedImage: "kong/kong-gateway:3.0.0",
			updated:       true,
		},
		{
			name:          "image and version are changed when new ones are provided with local registry",
			originalImage: "kong/kong:2.7.0",
			newImage:      "localhost:5000/kong-gateway:3.0.0",
			expectedImage: "localhost:5000/kong-gateway:3.0.0",
			updated:       true,
		},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			container := generators.NewContainer("test", tt.originalImage, 80)
			updated, err := ensureContainerImageUpdated(&container, tt.newImage)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Equal(t, tt.wantErr, err.Error())
			} else {
				assert.NoError(t, err)
			}

			assert.Equal(t, tt.updated, updated)
			if updated {
				assert.NotEqual(t, tt.originalImage, container.Image)
			} else {
				assert.Equal(t, tt.originalImage, container.Image)
			}

			if tt.expectedImage != "" {
				assert.Equal(t, tt.expectedImage, container.Image)
			}
		})
	}
}

func TestLog(t *testing.T) {
	var buf bytes.Buffer
	logger := ctrlruntimelog.New(func(o *ctrlruntimelog.Options) {
		o.DestWriter = &buf
	})

	gw := gwtypes.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gw",
			Namespace: "ns",
		},
	}
	t.Run("info logging works both for values and pointers to objects", func(t *testing.T) {
		t.Cleanup(func() { buf.Reset() })
		log.Info(logger, "message about gw", gw)
		require.NotContains(t, buf.String(), "unexpected type processed for")
		buf.Reset()
		log.Info(logger, "message about gw", &gw)
		require.NotContains(t, buf.String(), "unexpected type processed for")
	})

	t.Run("debug logging works both for values and pointers to objects", func(t *testing.T) {
		t.Cleanup(func() { buf.Reset() })
		log.Debug(logger, "message about gw", gw)
		require.NotContains(t, buf.String(), "unexpected type processed for")
		log.Debug(logger, "message about gw", &gw)
		require.NotContains(t, buf.String(), "unexpected type processed for")
	})

	t.Run("trace logging works both for values and pointers to objects", func(t *testing.T) {
		t.Cleanup(func() { buf.Reset() })
		log.Trace(logger, "message about gw", gw)
		require.NotContains(t, buf.String(), "unexpected type processed for")
		buf.Reset()
		log.Trace(logger, "message about gw", &gw)
		require.NotContains(t, buf.String(), "unexpected type processed for")
		buf.Reset()
	})

	t.Run("logging works and prints correct fields", func(t *testing.T) {
		t.Cleanup(func() { buf.Reset() })
		log.Info(logger, "message about gw", gw)
		entry := struct {
			Level     string `json:"level,omitempty"`
			Msg       string `json:"msg,omitempty"`
			Name      string `json:"name,omitempty"`
			Namespace string `json:"namespace,omitempty"`
		}{}
		require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
		assert.Equal(t, entry.Msg, "message about gw")
		assert.Equal(t, entry.Level, "info")
		assert.Equal(t, entry.Name, "gw")
		assert.Equal(t, entry.Namespace, "ns")
	})
}

func TestDeploymentOptionsDeepEqual(t *testing.T) {
	const (
		containerName = "controller"
	)

	testcases := []struct {
		name         string
		o1, o2       *operatorv1alpha1.DeploymentOptions
		envsToIgnore []string
		expect       bool
	}{
		{
			name:   "nils are equal",
			expect: true,
		},
		{
			name:   "empty values are equal",
			o1:     &operatorv1alpha1.DeploymentOptions{},
			o2:     &operatorv1alpha1.DeploymentOptions{},
			expect: true,
		},
		{
			name: "different resource requirements implies different deployment options",
			o1: &operatorv1alpha1.DeploymentOptions{
				PodTemplateSpec: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: containerName,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1000m"),
										corev1.ResourceMemory: resource.MustParse("128Mi"),
									},
									Limits: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1000m"),
										corev1.ResourceMemory: resource.MustParse("128Mi"),
									},
								},
							},
						},
					},
				},
			},
			o2: &operatorv1alpha1.DeploymentOptions{
				PodTemplateSpec: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: containerName,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1000m"),
										corev1.ResourceMemory: resource.MustParse("128Mi"),
									},
									Limits: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1000m"),
										corev1.ResourceMemory: resource.MustParse("256Mi"),
									},
								},
							},
						},
					},
				},
			},
			expect: false,
		},
		{
			name: "different pod labels implies different deployment options",
			o1: &operatorv1alpha1.DeploymentOptions{
				PodTemplateSpec: &corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"a": "v",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: containerName,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1000m"),
										corev1.ResourceMemory: resource.MustParse("128Mi"),
									},
									Limits: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1000m"),
										corev1.ResourceMemory: resource.MustParse("256Mi"),
									},
								},
							},
						},
					},
				},
			},
			o2: &operatorv1alpha1.DeploymentOptions{
				PodTemplateSpec: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: containerName,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1000m"),
										corev1.ResourceMemory: resource.MustParse("128Mi"),
									},
									Limits: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1000m"),
										corev1.ResourceMemory: resource.MustParse("256Mi"),
									},
								},
							},
						},
					},
				},
			},
			expect: false,
		},
		{
			name: "different image implies different deployment options",
			o1: &operatorv1alpha1.DeploymentOptions{
				PodTemplateSpec: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  containerName,
								Image: "image:v1.0",
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1000m"),
										corev1.ResourceMemory: resource.MustParse("128Mi"),
									},
									Limits: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1000m"),
										corev1.ResourceMemory: resource.MustParse("256Mi"),
									},
								},
							},
						},
					},
				},
			},
			o2: &operatorv1alpha1.DeploymentOptions{
				PodTemplateSpec: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: containerName,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1000m"),
										corev1.ResourceMemory: resource.MustParse("128Mi"),
									},
									Limits: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1000m"),
										corev1.ResourceMemory: resource.MustParse("256Mi"),
									},
								},
							},
						},
					},
				},
			},
			expect: false,
		},
		{
			name: "different env var implies different deployment options",
			o1: &operatorv1alpha1.DeploymentOptions{
				PodTemplateSpec: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  containerName,
								Image: "image:v1.0",
								Env: []corev1.EnvVar{
									{
										Name:  "KONG_TEST_VAR",
										Value: "VALUE1",
									},
								},
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1000m"),
										corev1.ResourceMemory: resource.MustParse("128Mi"),
									},
									Limits: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1000m"),
										corev1.ResourceMemory: resource.MustParse("256Mi"),
									},
								},
							},
						},
					},
				},
			},
			o2: &operatorv1alpha1.DeploymentOptions{
				PodTemplateSpec: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: containerName,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1000m"),
										corev1.ResourceMemory: resource.MustParse("128Mi"),
									},
									Limits: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1000m"),
										corev1.ResourceMemory: resource.MustParse("256Mi"),
									},
								},
							},
						},
					},
				},
			},
			expect: false,
		},
		{
			name: "the same",
			o1: &operatorv1alpha1.DeploymentOptions{
				PodTemplateSpec: &corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"a": "1",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  containerName,
								Image: "image:v1.0",
								Env: []corev1.EnvVar{
									{
										Name:  "KONG_TEST_VAR",
										Value: "VALUE1",
									},
								},
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1000m"),
										corev1.ResourceMemory: resource.MustParse("128Mi"),
									},
									Limits: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1000m"),
										corev1.ResourceMemory: resource.MustParse("256Mi"),
									},
								},
							},
						},
					},
				},
			},
			o2: &operatorv1alpha1.DeploymentOptions{
				PodTemplateSpec: &corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"a": "1",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  containerName,
								Image: "image:v1.0",
								Env: []corev1.EnvVar{
									{
										Name:  "KONG_TEST_VAR",
										Value: "VALUE1",
									},
								},
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1000m"),
										corev1.ResourceMemory: resource.MustParse("128Mi"),
									},
									Limits: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1000m"),
										corev1.ResourceMemory: resource.MustParse("256Mi"),
									},
								},
							},
						},
					},
				},
			},
			expect: true,
		},
		{
			name: "different replicas implies different deployment options",
			o1: &operatorv1alpha1.DeploymentOptions{
				Replicas: lo.ToPtr(int32(1)),
				PodTemplateSpec: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: containerName,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1000m"),
										corev1.ResourceMemory: resource.MustParse("128Mi"),
									},
									Limits: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1000m"),
										corev1.ResourceMemory: resource.MustParse("256Mi"),
									},
								},
							},
						},
					},
				},
			},
			o2: &operatorv1alpha1.DeploymentOptions{
				Replicas: lo.ToPtr(int32(3)),
				PodTemplateSpec: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: containerName,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1000m"),
										corev1.ResourceMemory: resource.MustParse("128Mi"),
									},
									Limits: corev1.ResourceList{
										corev1.ResourceCPU:    resource.MustParse("1000m"),
										corev1.ResourceMemory: resource.MustParse("256Mi"),
									},
								},
							},
						},
					},
				},
			},
			expect: false,
		},
		{
			name: "different env vars but included in the vars to ignore implies equal opts",
			o1: &operatorv1alpha1.DeploymentOptions{
				PodTemplateSpec: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: containerName,
								Env: []corev1.EnvVar{
									{
										Name:  "KONG_TEST_VAR",
										Value: "VALUE1",
									},
								},
							},
						},
					},
				},
			},
			o2: &operatorv1alpha1.DeploymentOptions{
				PodTemplateSpec: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: containerName,
							},
						},
					},
				},
			},
			envsToIgnore: []string{"KONG_TEST_VAR"},
			expect:       true,
		},
		{
			name: "different env vars with 1 one them included in the vars to ignore implies unequal opts",
			o1: &operatorv1alpha1.DeploymentOptions{
				PodTemplateSpec: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: containerName,
								Env: []corev1.EnvVar{
									{
										Name:  "KONG_TEST_VAR",
										Value: "VALUE1",
									},
									{
										Name:  "KONG_TEST_VAR_2",
										Value: "VALUE2",
									},
								},
							},
						},
					},
				},
			},
			o2: &operatorv1alpha1.DeploymentOptions{
				PodTemplateSpec: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: containerName,
							},
						},
					},
				},
			},
			envsToIgnore: []string{"KONG_TEST_VAR"},
			expect:       false,
		},
		{
			name: "different labels unequal opts",
			o1: &operatorv1alpha1.DeploymentOptions{
				PodTemplateSpec: &corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"a": "a",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: containerName,
								Env: []corev1.EnvVar{
									{
										Name:  "KONG_TEST_VAR",
										Value: "VALUE1",
									},
								},
							},
						},
					},
				},
			},
			o2: &operatorv1alpha1.DeploymentOptions{
				PodTemplateSpec: &corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"a": "a",
							"b": "b",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: containerName,
								Env: []corev1.EnvVar{
									{
										Name:  "KONG_TEST_VAR",
										Value: "VALUE1",
									},
								},
							},
						},
					},
				},
			},
			envsToIgnore: []string{"KONG_TEST_VAR"},
			expect:       false,
		},
	}

	for _, tc := range testcases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			ret := alphaDeploymentOptionsDeepEqual(tc.o1, tc.o2, tc.envsToIgnore...)
			if tc.expect {
				require.True(t, ret)
			} else {
				require.False(t, ret)
			}
		})
	}
}

func TestMaybeCreateCertificateSecret(t *testing.T) {
	createDataPlane := func(nn types.NamespacedName, opt ...func(dp *operatorv1beta1.DataPlane)) *operatorv1beta1.DataPlane {
		dp := &operatorv1beta1.DataPlane{
			ObjectMeta: metav1.ObjectMeta{
				Name:      nn.Name,
				Namespace: nn.Namespace,
			},
		}
		for _, o := range opt {
			o(dp)
		}
		return dp
	}

	WithUUID := func(u types.UID) func(dp *operatorv1beta1.DataPlane) {
		return func(dp *operatorv1beta1.DataPlane) {
			dp.UID = u
		}
	}

	type NN = types.NamespacedName

	testCases := []struct {
		name                     string
		dataPlane                *operatorv1beta1.DataPlane
		subject                  string
		mtlsCASecretNN           NN
		additionalMatchingLabels client.MatchingLabels
		expectedResult           op.CreatedUpdatedOrNoop
		expectedError            error
		objectList               client.ObjectList
	}{
		{
			name:      "no certificate secret exists and gets created as expected",
			dataPlane: createDataPlane(NN{Name: "dp-1", Namespace: "ns"}),
			subject:   "test-subject",
			mtlsCASecretNN: NN{
				Name:      "test-mtls-secret",
				Namespace: "ns",
			},
			additionalMatchingLabels: nil,
			expectedResult:           op.Created,
			expectedError:            nil,
		},
		{
			name:      "existing secret certificate gets deleted and re-created with it doesn't have the expected contents",
			dataPlane: createDataPlane(NN{Name: "dp-1", Namespace: "ns"}, WithUUID(types.UID("1234"))),
			subject:   "test-subject",
			mtlsCASecretNN: NN{
				Name:      "test-mtls-secret",
				Namespace: "ns",
			},
			additionalMatchingLabels: nil,
			objectList: &corev1.SecretList{
				Items: []corev1.Secret{
					func() corev1.Secret {
						dp := createDataPlane(NN{Name: "dp-1", Namespace: "ns"}, WithUUID(types.UID("1234")))

						labels := k8sresources.GetManagedLabelForOwner(dp)
						return corev1.Secret{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "secret-1",
								Namespace: "ns",
								Labels:    labels,
								OwnerReferences: []metav1.OwnerReference{
									{
										Kind:       "DataPlane",
										APIVersion: operatorv1beta1.SchemeGroupVersion.Group + "/" + operatorv1beta1.SchemeGroupVersion.Version,
										UID:        types.UID("1234"),
									},
								},
							},
						}
					}(),
				},
			},
			expectedResult: op.Created,
			expectedError:  nil,
		},
		{
			name:      "when more than 1 secret exists, secrets are reduced",
			dataPlane: createDataPlane(NN{Name: "dp-1", Namespace: "ns"}, WithUUID(types.UID("1234"))),
			subject:   "test-subject",
			mtlsCASecretNN: NN{
				Name:      "test-mtls-secret",
				Namespace: "ns",
			},
			additionalMatchingLabels: nil,
			objectList: &corev1.SecretList{
				Items: []corev1.Secret{
					func() corev1.Secret {
						dp := createDataPlane(NN{Name: "dp-1", Namespace: "ns"}, WithUUID(types.UID("1234")))

						labels := k8sresources.GetManagedLabelForOwner(dp)
						return corev1.Secret{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "secret-1",
								Namespace: "ns",
								Labels:    labels,
								OwnerReferences: []metav1.OwnerReference{
									{
										Kind:       "DataPlane",
										APIVersion: operatorv1beta1.SchemeGroupVersion.Group + "/" + operatorv1beta1.SchemeGroupVersion.Version,
										UID:        types.UID("1234"),
									},
								},
							},
						}
					}(),
					func() corev1.Secret {
						dp := createDataPlane(NN{Name: "dp-1", Namespace: "ns"}, WithUUID(types.UID("1234")))

						labels := k8sresources.GetManagedLabelForOwner(dp)
						return corev1.Secret{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "secret-2",
								Namespace: "ns",
								Labels:    labels,
								OwnerReferences: []metav1.OwnerReference{
									{
										Kind:       "DataPlane",
										APIVersion: operatorv1beta1.SchemeGroupVersion.Group + "/" + operatorv1beta1.SchemeGroupVersion.Version,
										UID:        types.UID("1234"),
									},
								},
							},
						}
					}(),
				},
			},
			expectedResult: op.Noop,
			expectedError:  errors.New("number of secrets reduced"),
		},
	}

	for _, tc := range testCases {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()

			scheme := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(scheme))
			require.NoError(t, certificatesv1.AddToScheme(scheme))
			require.NoError(t, operatorv1beta1.AddToScheme(scheme))

			builder := fakectrlruntimeclient.
				NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tc.dataPlane)
			if tc.objectList != nil {
				builder.WithLists(tc.objectList)
			}
			fakeClient := builder.Build()

			caSecret, err := generateCACert(tc.mtlsCASecretNN)
			require.NoError(t, err)
			require.NoError(t, fakeClient.Create(ctx, caSecret))

			res, secret, err := maybeCreateCertificateSecret(
				ctx,
				tc.dataPlane,
				tc.subject,
				tc.mtlsCASecretNN,
				[]certificatesv1.KeyUsage{
					certificatesv1.UsageServerAuth,
				},
				fakeClient,
				tc.additionalMatchingLabels,
			)

			if tc.expectedError != nil {
				require.Error(t, err)
				require.EqualError(t, tc.expectedError, "number of secrets reduced")
				return
			}

			require.Equal(t, tc.expectedResult, res)
			require.Equal(t, caSecret.Data["tls.crt"], secret.Data["ca.crt"], "created secret 'ca.crt' should be equal to CA cert's 'tls.crt'")

			_, ok := secret.Data["tls.crt"]
			require.True(t, ok, "generated secret does not contain 'tls.crt'")

			key, ok := secret.Data["tls.key"]
			require.True(t, ok, "generated secret does not contain 'tls.key'")
			tlsKeyPemBlock, _ := pem.Decode(key)
			require.NotNil(t, tlsKeyPemBlock)
			_, err = x509.ParseECPrivateKey(tlsKeyPemBlock.Bytes)
			require.NoError(t, err)
		})

	}
}

func generateCACert(nn types.NamespacedName) (*corev1.Secret, error) {
	serial, err := rand.Int(rand.Reader, big.NewInt(math.MaxInt64))
	if err != nil {
		return nil, err
	}

	template := x509.Certificate{
		Subject: pkix.Name{
			CommonName:   "Kong Gateway Operator CA",
			Organization: []string{"Kong, Inc."},
			Country:      []string{"US"},
		},
		SerialNumber:          serial,
		SignatureAlgorithm:    x509.ECDSAWithSHA256,
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Second * 315400000),
		KeyUsage:              x509.KeyUsageCertSign + x509.KeyUsageKeyEncipherment + x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	privDer, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return nil, err
	}

	der, err := x509.CreateCertificate(rand.Reader, &template, &template, priv.Public(), priv)
	if err != nil {
		return nil, err
	}

	signedSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: nn.Namespace,
			Name:      nn.Name,
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			"tls.crt": pem.EncodeToMemory(&pem.Block{
				Type:  "CERTIFICATE",
				Bytes: der,
			}),

			"tls.key": pem.EncodeToMemory(&pem.Block{
				Type:  "EC PRIVATE KEY",
				Bytes: privDer,
			}),
		},
	}

	return signedSecret, nil
}
