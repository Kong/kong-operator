package dataplane

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	"github.com/kong/gateway-operator/internal/consts"
)

func TestValidateDeployOptions(t *testing.T) {
	b := fakeclient.NewClientBuilder()
	b.WithObjects(
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "test-cm"},
			Data: map[string]string{
				"off": "off",
			},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "test-secret"},
			// fake client does not encode fields in StringData to Data,
			// so here we should usebase64 encoded value in Data.
			Data: map[string][]byte{
				"postgres": []byte(base64.StdEncoding.EncodeToString([]byte("postgres"))),
			},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "test-cm-2"},
			// fake client does not encode fields in StringData to Data,
			// so here we should usebase64 encoded value in Data.
			Data: map[string]string{
				"KONG_DATABASE": "xxx",
			},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "test-secret-2"},
			// fake client does not encode fields in StringData to Data,
			// so here we should usebase64 encoded value in Data.
			Data: map[string][]byte{
				"DATABASE": []byte(base64.StdEncoding.EncodeToString([]byte("xxx"))),
			},
		},
	)

	testCases := []struct {
		msg       string
		dataplane *operatorv1alpha1.DataPlane
		hasError  bool
		errMsg    string
	}{
		{
			msg: "dataplane with dbmode=off should be valid",
			dataplane: &operatorv1alpha1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-db-off",
					Namespace: "default",
				},
				Spec: operatorv1alpha1.DataPlaneSpec{
					DataPlaneDeploymentOptions: operatorv1alpha1.DataPlaneDeploymentOptions{
						DeploymentOptions: operatorv1alpha1.DeploymentOptions{
							Env: []corev1.EnvVar{
								{
									Name:  consts.EnvVarKongDatabase,
									Value: "off",
								},
							},
						},
					},
				},
			},
			hasError: false,
		},
		{
			msg: "dataplane with empty dbmode should be valid",
			dataplane: &operatorv1alpha1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-db-off",
					Namespace: "default",
				},
				Spec: operatorv1alpha1.DataPlaneSpec{
					DataPlaneDeploymentOptions: operatorv1alpha1.DataPlaneDeploymentOptions{
						DeploymentOptions: operatorv1alpha1.DeploymentOptions{
							Env: []corev1.EnvVar{
								{
									Name:  consts.EnvVarKongDatabase,
									Value: "",
								},
							},
						},
					},
				},
			},
			hasError: false,
		},
		{
			msg: "dataplane with dbmode=postgres should be invalid",
			dataplane: &operatorv1alpha1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-db-postgres",
					Namespace: "default",
				},
				Spec: operatorv1alpha1.DataPlaneSpec{
					DataPlaneDeploymentOptions: operatorv1alpha1.DataPlaneDeploymentOptions{
						DeploymentOptions: operatorv1alpha1.DeploymentOptions{
							Env: []corev1.EnvVar{
								{
									Name:  consts.EnvVarKongDatabase,
									Value: "postgres",
								},
							},
						},
					},
				},
			},
			hasError: true,
			errMsg:   "database backend postgres of dataplane not supported currently",
		},
		{
			msg: "dataplane with arbitrary dbmode should be invalid",
			dataplane: &operatorv1alpha1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-db-postgres",
					Namespace: "default",
				},
				Spec: operatorv1alpha1.DataPlaneSpec{
					DataPlaneDeploymentOptions: operatorv1alpha1.DataPlaneDeploymentOptions{
						DeploymentOptions: operatorv1alpha1.DeploymentOptions{
							Env: []corev1.EnvVar{
								{
									Name:  consts.EnvVarKongDatabase,
									Value: "xxx",
								},
							},
						},
					},
				},
			},
			hasError: true,
			errMsg:   "database backend xxx of dataplane not supported currently",
		},
		{
			msg: "dataplane with dbmode=off (from configmap) should be valid",
			dataplane: &operatorv1alpha1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-db-off-in-cm",
					Namespace: "default",
				},
				Spec: operatorv1alpha1.DataPlaneSpec{
					DataPlaneDeploymentOptions: operatorv1alpha1.DataPlaneDeploymentOptions{
						DeploymentOptions: operatorv1alpha1.DeploymentOptions{
							Env: []corev1.EnvVar{
								{
									Name: consts.EnvVarKongDatabase,
									ValueFrom: &corev1.EnvVarSource{
										ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{Name: "test-cm"},
											Key:                  "off",
										},
									},
								},
							},
						},
					},
				},
			},
			hasError: false,
		},
		{
			msg: "dataplane with dbmode=postgres (from secret) should be invalid",
			dataplane: &operatorv1alpha1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-db-postgres-in-secret",
					Namespace: "default",
				},
				Spec: operatorv1alpha1.DataPlaneSpec{
					DataPlaneDeploymentOptions: operatorv1alpha1.DataPlaneDeploymentOptions{
						DeploymentOptions: operatorv1alpha1.DeploymentOptions{
							Env: []corev1.EnvVar{
								{
									Name: consts.EnvVarKongDatabase,
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
											Key:                  "postgres",
										},
									},
								},
							},
						},
					},
				},
			},
			hasError: true,
			errMsg:   "database backend postgres of dataplane not supported currently",
		},
		{
			msg: "dataplane with dbmode=xxx (from configmap in envFrom) should be invalid",
			dataplane: &operatorv1alpha1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-db-off-in-cm",
					Namespace: "default",
				},
				Spec: operatorv1alpha1.DataPlaneSpec{
					DataPlaneDeploymentOptions: operatorv1alpha1.DataPlaneDeploymentOptions{
						DeploymentOptions: operatorv1alpha1.DeploymentOptions{
							EnvFrom: []corev1.EnvFromSource{
								{
									Prefix: "",
									ConfigMapRef: &corev1.ConfigMapEnvSource{
										LocalObjectReference: corev1.LocalObjectReference{Name: "test-cm-2"},
									},
								},
							},
						},
					},
				},
			},
			hasError: true,
			errMsg:   "database backend xxx of dataplane not supported currently",
		},
		{
			msg: "dataplane with dbmode=xxx (from secret in envFrom) should be invalid",
			dataplane: &operatorv1alpha1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-db-off-in-secret",
					Namespace: "default",
				},
				Spec: operatorv1alpha1.DataPlaneSpec{
					DataPlaneDeploymentOptions: operatorv1alpha1.DataPlaneDeploymentOptions{
						DeploymentOptions: operatorv1alpha1.DeploymentOptions{
							EnvFrom: []corev1.EnvFromSource{
								{
									Prefix: "KONG_",
									SecretRef: &corev1.SecretEnvSource{
										LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret-2"},
									},
								},
							},
						},
					},
				},
			},
			hasError: true,
			errMsg:   "database backend xxx of dataplane not supported currently",
		},
	}

	for _, tc := range testCases {
		v := &Validator{
			c: b.Build(),
		}
		err := v.Validate(tc.dataplane)
		if !tc.hasError {
			require.NoErrorf(t, err, tc.msg)
		} else {
			require.ErrorContainsf(t, err, tc.errMsg, tc.msg)
		}
	}
}
