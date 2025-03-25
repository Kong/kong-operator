package controlplane

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	controllerruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kong/gateway-operator/pkg/consts"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"
	k8sresources "github.com/kong/gateway-operator/pkg/utils/kubernetes/resources"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
)

func TestEnsureClusterRole(t *testing.T) {
	clusterRole, err := k8sresources.GenerateNewClusterRoleForControlPlane("test-controlplane", consts.DefaultControlPlaneImage, false)
	require.NoError(t, err)

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
	wrongClusterRole2.Labels["aaa"] = "bbb"

	controlplane := operatorv1beta1.ControlPlane{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "gateway-operator.konghq.com/v1beta1",
			Kind:       "ControlPlane",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-controlplane",
			Namespace: "test-namespace",
			UID:       types.UID(uuid.NewString()),
		},
		Spec: operatorv1beta1.ControlPlaneSpec{
			ControlPlaneOptions: operatorv1beta1.ControlPlaneOptions{
				Deployment: operatorv1beta1.ControlPlaneDeploymentOptions{
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
	}

	k8sutils.SetOwnerForObjectThroughLabels(clusterRole, &controlplane)

	testCases := []struct {
		Name                string
		controlplane        operatorv1beta1.ControlPlane
		existingClusterRole *rbacv1.ClusterRole
		createdorUpdated    bool
		expectedClusterRole rbacv1.ClusterRole
		err                 error
	}{
		{
			Name:                "no existing clusterrole",
			controlplane:        controlplane,
			createdorUpdated:    true,
			expectedClusterRole: *clusterRole,
		},
		{
			Name:                "up to date clusterrole",
			controlplane:        controlplane,
			existingClusterRole: clusterRole,
			expectedClusterRole: *clusterRole,
		},
		{
			Name:                "out of date clusterrole, object meta",
			controlplane:        controlplane,
			existingClusterRole: wrongClusterRole2,
			createdorUpdated:    true,
			expectedClusterRole: *clusterRole,
		},
	}

	for _, tc := range testCases {

		ObjectsToAdd := []controllerruntimeclient.Object{
			&tc.controlplane,
		}

		if tc.existingClusterRole != nil {
			k8sutils.SetOwnerForObjectThroughLabels(tc.existingClusterRole, &tc.controlplane)
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
			createdOrUpdated, generatedClusterRole, err := r.ensureClusterRole(t.Context(), &tc.controlplane)
			require.Equal(t, tc.err, err)
			require.Equal(t, tc.createdorUpdated, createdOrUpdated)
			require.Equal(t, tc.expectedClusterRole.Rules, generatedClusterRole.Rules)
			require.Equal(t, tc.expectedClusterRole.AggregationRule, generatedClusterRole.AggregationRule)
			require.Equal(t, tc.expectedClusterRole.Labels, generatedClusterRole.Labels)
		})
	}
}

func TestEnsureClusterRoleBinding(t *testing.T) {
	const (
		testNamespace          = "test-ns"
		testControlPlane       = "test-cp"
		testServiceAccount     = "test-sa"
		testClusterRole        = "test-cr"
		testClusterRoleBinding = "test-crb"
	)

	controlPlane := &operatorv1beta1.ControlPlane{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "gateway-operator.konghq.com/v1beta1",
			Kind:       "ControlPlane",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      testControlPlane,
			Namespace: testNamespace,
			UID:       types.UID(uuid.NewString()),
		},
		Spec: operatorv1beta1.ControlPlaneSpec{
			ControlPlaneOptions: operatorv1beta1.ControlPlaneOptions{
				Deployment: operatorv1beta1.ControlPlaneDeploymentOptions{
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
	}
	expectedClusterRoleBinding := k8sresources.GenerateNewClusterRoleBindingForControlPlane(testNamespace, testControlPlane, testServiceAccount, testClusterRole)
	expectedClusterRoleBinding.Name = testClusterRoleBinding

	crbWithDifferentName := expectedClusterRoleBinding.DeepCopy()
	crbWithDifferentName.Name = expectedClusterRoleBinding.Name + "-1"

	crbWithWrongClusterRole := expectedClusterRoleBinding.DeepCopy()
	crbWithWrongClusterRole.RoleRef.Name = "wrong"

	crbWithNoServiceAccount := expectedClusterRoleBinding.DeepCopy()
	crbWithNoServiceAccount.Subjects = nil

	crbWithDifferentLabel := expectedClusterRoleBinding.DeepCopy()
	crbWithDifferentLabel.Labels["foo"] = "bar"

	testCases := []struct {
		name             string
		existingCRBs     []*rbacv1.ClusterRoleBinding
		err              error
		createdOrUpdated bool
	}{
		{
			name: "reduce multiple existing ClusterRoleBindings",
			existingCRBs: []*rbacv1.ClusterRoleBinding{
				expectedClusterRoleBinding,
				crbWithDifferentName,
			},
			err:              errors.New("number of clusterRoleBindings reduced"),
			createdOrUpdated: false,
		},
		{
			name: "ClusterRoleBinding is up to date",
			existingCRBs: []*rbacv1.ClusterRoleBinding{
				expectedClusterRoleBinding,
			},
			err:              nil,
			createdOrUpdated: false,
		},
		{
			name:             "no ClusterRoleBinding, should create one",
			existingCRBs:     nil,
			err:              nil,
			createdOrUpdated: true,
		},
		{
			name: "existing ClusterRoleBinding has wrong RoleRef, should delete existing one",
			existingCRBs: []*rbacv1.ClusterRoleBinding{
				crbWithWrongClusterRole,
			},
			err:              errors.New("name of ClusterRole changed, out of date ClusterRoleBinding deleted"),
			createdOrUpdated: false,
		},
		{
			name: "existing ClusterRoleBinding has wrong labels, should update",
			existingCRBs: []*rbacv1.ClusterRoleBinding{
				crbWithDifferentLabel,
			},
			err:              nil,
			createdOrUpdated: true,
		},
		{
			name: "existing ClusterRoleBinding does not include expected ServiceAccount, should update",
			existingCRBs: []*rbacv1.ClusterRoleBinding{
				crbWithNoServiceAccount,
			},
			err:              nil,
			createdOrUpdated: true,
		},
	}

	for _, tc := range testCases {

		objectsToAdd := []controllerruntimeclient.Object{
			controlPlane,
		}
		for _, crb := range tc.existingCRBs {
			k8sutils.SetOwnerForObjectThroughLabels(crb, controlPlane)
			objectsToAdd = append(objectsToAdd, crb)
		}

		fakeClient := fakectrlruntimeclient.
			NewClientBuilder().
			WithScheme(scheme.Scheme).
			WithObjects(objectsToAdd...).
			Build()

		r := Reconciler{
			Client: fakeClient,
			Scheme: scheme.Scheme,
		}

		t.Run(tc.name, func(t *testing.T) {
			createdOrUpdated, generatedCRB, err := r.ensureClusterRoleBinding(t.Context(), controlPlane, testServiceAccount, testClusterRole)
			require.Equal(t, tc.err, err)
			require.Equal(t, tc.createdOrUpdated, createdOrUpdated)
			// when err == nil, ensureClusterRoleBinding should return a non-nil ClusterRoleBinding with the same metadata, RoleRef
			// and contains the ServiceAccounts of the expected ClusterRoleBinding.
			if tc.err == nil {
				require.Equal(t, expectedClusterRoleBinding.Labels, generatedCRB.Labels)
				require.Equal(t, testClusterRole, generatedCRB.RoleRef.Name)
				require.Truef(t, k8sresources.ClusterRoleBindingContainsServiceAccount(generatedCRB, testNamespace, testServiceAccount),
					"ClusterRoleBinding should contain expected ServiceAccount %s/%s in its subjects",
					testNamespace, testServiceAccount,
				)
			}
		})
	}
}
