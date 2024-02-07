package controlplane

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	controllerruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	"github.com/kong/gateway-operator/internal/consts"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
	k8sresources "github.com/kong/gateway-operator/internal/utils/kubernetes/resources"
)

func TestEnsureClusterRole(t *testing.T) {
	clusterRole, err := k8sresources.GenerateNewClusterRoleForControlPlane("test-controlplane", consts.DefaultControlPlaneImage)
	assert.NoError(t, err)
	clusterRole.Name = "test-clusterrole"
	wrongClusterRole := clusterRole.DeepCopy()
	wrongClusterRole.Rules = append(wrongClusterRole.Rules,
		rbacv1.PolicyRule{
			APIGroups: []string{
				"fake.group",
			},
			Resources: []string{
				"fakeResource",
			},
			Verbs: []string{
				"create", "patch",
			},
		},
	)
	wrongClusterRole2 := clusterRole.DeepCopy()
	wrongClusterRole2.ObjectMeta.Labels["aaa"] = "bbb"

	testCases := []struct {
		Name                string
		controlplane        operatorv1alpha1.ControlPlane
		existingClusterRole *rbacv1.ClusterRole
		createdorUpdated    bool
		expectedClusterRole rbacv1.ClusterRole
		err                 error
	}{
		{
			Name: "no existing clusterrole",
			controlplane: operatorv1alpha1.ControlPlane{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "gateway-operator.konghq.com/v1alpha1",
					Kind:       "ControlPlane",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-controlplane",
					Namespace: "test-namespace",
					UID:       types.UID(uuid.NewString()),
				},
				Spec: operatorv1alpha1.ControlPlaneSpec{
					ControlPlaneOptions: operatorv1alpha1.ControlPlaneOptions{
						Deployment: operatorv1alpha1.DeploymentOptions{
							PodTemplateSpec: &corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{
										{
											Name:  consts.ControlPlaneControllerContainerName,
											Image: consts.DefaultControlPlaneImage,
										},
									},
								},
							},
						},
					},
				},
			},
			createdorUpdated:    true,
			expectedClusterRole: *clusterRole,
		},
		{
			Name: "up to date clusterrole",
			controlplane: operatorv1alpha1.ControlPlane{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "gateway-operator.konghq.com/v1alpha1",
					Kind:       "ControlPlane",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-controlplane",
					Namespace: "test-namespace",
					UID:       types.UID(uuid.NewString()),
				},
				Spec: operatorv1alpha1.ControlPlaneSpec{
					ControlPlaneOptions: operatorv1alpha1.ControlPlaneOptions{
						Deployment: operatorv1alpha1.DeploymentOptions{
							PodTemplateSpec: &corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{
										{
											Name:  consts.ControlPlaneControllerContainerName,
											Image: consts.DefaultControlPlaneImage,
										},
									},
								},
							},
						},
					},
				},
			},
			existingClusterRole: clusterRole,
			expectedClusterRole: *clusterRole,
		},
		{
			Name: "out of date clusterrole, object meta",
			controlplane: operatorv1alpha1.ControlPlane{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "gateway-operator.konghq.com/v1alpha1",
					Kind:       "ControlPlane",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-controlplane",
					Namespace: "test-namespace",
					UID:       types.UID(uuid.NewString()),
				},
				Spec: operatorv1alpha1.ControlPlaneSpec{
					ControlPlaneOptions: operatorv1alpha1.ControlPlaneOptions{
						Deployment: operatorv1alpha1.DeploymentOptions{
							PodTemplateSpec: &corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{
										{
											Name:  consts.ControlPlaneControllerContainerName,
											Image: consts.DefaultControlPlaneImage,
										},
									},
								},
							},
						},
					},
				},
			},
			existingClusterRole: wrongClusterRole2,
			createdorUpdated:    true,
			expectedClusterRole: *clusterRole,
		},
	}

	for _, tc := range testCases {
		tc := tc

		ObjectsToAdd := []controllerruntimeclient.Object{
			&tc.controlplane,
		}

		if tc.existingClusterRole != nil {
			k8sutils.SetOwnerForObject(tc.existingClusterRole, &tc.controlplane)
			ObjectsToAdd = append(ObjectsToAdd, tc.existingClusterRole)
		}

		fakeClient := fakectrlruntimeclient.
			NewClientBuilder().
			WithScheme(scheme.Scheme).
			WithObjects(ObjectsToAdd...).
			Build()

		r := Reconciler{
			Client: fakeClient,
			Scheme: scheme.Scheme,
		}

		t.Run(tc.Name, func(t *testing.T) {
			createdOrUpdated, generatedClusterRole, err := r.ensureClusterRole(context.Background(), &tc.controlplane)
			require.Equal(t, tc.err, err)
			require.Equal(t, tc.createdorUpdated, createdOrUpdated)
			require.Equal(t, tc.expectedClusterRole.Rules, generatedClusterRole.Rules)
			require.Equal(t, tc.expectedClusterRole.AggregationRule, generatedClusterRole.AggregationRule)
			require.Equal(t, tc.expectedClusterRole.Labels, generatedClusterRole.Labels)
		})
	}
}
