package integration

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/controller/pkg/builder"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/pkg/consts"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
	k8sresources "github.com/kong/kong-operator/pkg/utils/kubernetes/resources"
	testutils "github.com/kong/kong-operator/pkg/utils/test"
	"github.com/kong/kong-operator/test/helpers"
	"github.com/kong/kong-operator/test/helpers/eventually"

	kcfgcontrolplane "github.com/kong/kubernetes-configuration/api/gateway-operator/controlplane"
	kcfgdataplane "github.com/kong/kubernetes-configuration/api/gateway-operator/dataplane"
	operatorv1alpha1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1alpha1"
	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
)

func TestControlPlaneWhenNoDataPlane(t *testing.T) {
	t.Skip("Using KIC as a library in ControlPlane controller broke this test (https://github.com/kong/kong-operator/issues/1197)")

	t.Parallel()
	namespace, cleaner := helpers.SetupTestEnv(t, GetCtx(), GetEnv())

	dataplaneClient := GetClients().OperatorClient.GatewayOperatorV1beta1().DataPlanes(namespace.Name)
	controlplaneClient := GetClients().OperatorClient.GatewayOperatorV2alpha1().ControlPlanes(namespace.Name)

	controlplaneName := types.NamespacedName{
		Namespace: namespace.Name,
		Name:      uuid.NewString(),
	}
	controlplane := &gwtypes.ControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: controlplaneName.Namespace,
			Name:      controlplaneName.Name,
		},
		Spec: gwtypes.ControlPlaneSpec{
			ControlPlaneOptions: gwtypes.ControlPlaneOptions{},
		},
	}

	// Control plane needs a dataplane to exist to properly function.
	dataplaneNN := types.NamespacedName{
		Namespace: namespace.Name,
		Name:      uuid.NewString(),
	}
	dataplane := &operatorv1beta1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: dataplaneNN.Namespace,
			Name:      dataplaneNN.Name,
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
										Image: helpers.GetDefaultDataPlaneImage(),
									},
								},
							},
						},
					},
				},
			},
		},
	}

	t.Log("deploying controlplane resource without dataplane attached")
	controlplane, err := controlplaneClient.Create(GetCtx(), controlplane, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(controlplane)

	t.Log("verifying controlplane state reflects lack of dataplane")
	require.Eventually(t, testutils.ControlPlaneDetectedNoDataPlane(t, GetCtx(), controlplaneName, clients), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)

	t.Log("verifying controlplane deployment has no active replicas")
	require.Eventually(t, testutils.Not(testutils.ControlPlaneHasActiveDeployment(t, GetCtx(), controlplaneName, clients)), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)

	t.Log("deploying dataplane resource")
	dataplane, err = dataplaneClient.Create(GetCtx(), dataplane, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(dataplane)

	t.Log("verifying deployments managed by the dataplane are ready")
	require.Eventually(t, testutils.DataPlaneHasActiveDeployment(t, GetCtx(), dataplaneNN, &appsv1.Deployment{}, client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
	}, clients), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)

	t.Log("verifying services managed by the dataplane")
	require.Eventually(t, testutils.DataPlaneHasService(t, GetCtx(), dataplaneNN, clients, client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
		consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneIngressServiceLabelValue),
	}), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)

	t.Log("attaching dataplane to controlplane")
	require.Eventually(t,
		testutils.ControlPlaneUpdateEventually(t, GetCtx(), controlplaneName, clients, func(cp *gwtypes.ControlPlane) {
			cp.Spec.DataPlane = gwtypes.ControlPlaneDataPlaneTarget{
				Type: gwtypes.ControlPlaneDataPlaneTargetRefType,
				Ref: &gwtypes.ControlPlaneDataPlaneTargetRef{
					Name: dataplane.Name,
				},
			}
		}),
		testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick,
	)

	t.Log("verifying controlplane is now provisioned")
	require.Eventually(t, testutils.ControlPlaneIsProvisioned(t, GetCtx(), controlplaneName, clients), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)

	t.Log("verifying controlplane deployment has active replicas")
	require.Eventually(t, testutils.ControlPlaneHasActiveDeployment(t, GetCtx(), controlplaneName, clients), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)
}

func TestControlPlaneEssentials(t *testing.T) {
	t.Skip("Using KIC as a library in ControlPlane controller broke this test (https://github.com/kong/kong-operator/issues/1190)")

	t.Parallel()
	namespace, cleaner := helpers.SetupTestEnv(t, GetCtx(), GetEnv())

	dataplaneClient := GetClients().OperatorClient.GatewayOperatorV1beta1().DataPlanes(namespace.Name)
	controlplaneClient := GetClients().OperatorClient.GatewayOperatorV2alpha1().ControlPlanes(namespace.Name)

	// Control plane needs a dataplane to exist to properly function.
	dataplaneName := types.NamespacedName{
		Namespace: namespace.Name,
		Name:      uuid.NewString(),
	}
	dataplane := &operatorv1beta1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: dataplaneName.Namespace,
			Name:      dataplaneName.Name,
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
										Image: helpers.GetDefaultDataPlaneImage(),
										// Speed up the test.
										ReadinessProbe: func() *corev1.Probe {
											p := k8sresources.GenerateDataPlaneReadinessProbe(consts.DataPlaneStatusEndpoint)
											p.InitialDelaySeconds = 1
											p.PeriodSeconds = 1
											return p
										}(),
									},
								},
							},
						},
					},
				},
			},
		},
	}

	controlplaneName := types.NamespacedName{
		Namespace: namespace.Name,
		Name:      uuid.NewString(),
	}
	controlplane := &gwtypes.ControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: controlplaneName.Namespace,
			Name:      controlplaneName.Name,
		},
		Spec: gwtypes.ControlPlaneSpec{
			DataPlane: gwtypes.ControlPlaneDataPlaneTarget{
				Type: gwtypes.ControlPlaneDataPlaneTargetRefType,
				Ref: &gwtypes.ControlPlaneDataPlaneTargetRef{
					Name: dataplane.Name,
				},
			},
		},
	}

	t.Log("deploying dataplane resource")
	dataplane, err := dataplaneClient.Create(GetCtx(), dataplane, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(dataplane)

	t.Log("verifying deployments managed by the dataplane are ready")
	require.Eventually(t, testutils.DataPlaneHasActiveDeployment(t, GetCtx(), dataplaneName, &appsv1.Deployment{}, client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
	}, clients), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)

	t.Log("verifying services managed by the dataplane")
	require.Eventually(t, testutils.DataPlaneHasActiveService(t, GetCtx(), dataplaneName, nil, clients, client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
		consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneIngressServiceLabelValue),
	}), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)

	t.Log("deploying controlplane resource")
	controlplane, err = controlplaneClient.Create(GetCtx(), controlplane, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(controlplane)
	controlplane.TypeMeta = metav1.TypeMeta{
		APIVersion: operatorv1beta1.SchemeGroupVersion.String(),
		Kind:       "ControlPlane",
	}

	t.Log("verifying controlplane gets marked scheduled")
	require.Eventually(t, testutils.ControlPlaneIsScheduled(t, GetCtx(), controlplaneName, GetClients().OperatorClient), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)

	t.Log("verifying controlplane owns clusterrole and clusterrolebinding")
	require.Eventually(t, testutils.ControlPlaneHasClusterRole(t, GetCtx(), controlplane, clients), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)
	require.Eventually(t, testutils.ControlPlaneHasClusterRoleBinding(t, GetCtx(), controlplane, clients), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)

	t.Log("verifying that the controlplane gets marked as provisioned")
	require.Eventually(t, testutils.ControlPlaneIsProvisioned(t, GetCtx(), controlplaneName, clients), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)

	t.Log("verifying controlplane deployment has active replicas")
	require.Eventually(t, testutils.ControlPlaneHasActiveDeployment(t, GetCtx(), controlplaneName, clients), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)

	t.Logf("verifying that pod labels were set per the provided spec")
	require.Eventually(t, func() bool {
		deployments := testutils.MustListControlPlaneDeployments(t, GetCtx(), controlplane, clients)
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
	deployments := testutils.MustListControlPlaneDeployments(t, GetCtx(), controlplane, clients)
	require.Len(t, deployments, 1, "There must be only one ControlPlane deployment")
	deployment := &deployments[0]

	t.Log("verifying controlplane Deployment.Pods.Env vars")
	checkControlPlaneDeploymentEnvVars(t, deployment, controlplane.Name)

	t.Log("deleting the controlplane ClusterRole")
	clusterRoles := testutils.MustListControlPlaneClusterRoles(t, GetCtx(), controlplane, clients)
	require.Len(t, clusterRoles, 1, "There must be only one ControlPlane ClusterRole")
	require.NoError(t, GetClients().MgrClient.Delete(GetCtx(), &clusterRoles[0]))

	t.Log("verifying controlplane ClusterRole and ClusterRoleBinding have been re-created")
	require.Eventually(t, testutils.ControlPlaneHasClusterRole(t, GetCtx(), controlplane, clients), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)
	require.Eventually(t, testutils.ControlPlaneHasClusterRoleBinding(t, GetCtx(), controlplane, clients), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)
	require.Eventually(t, testutils.ControlPlaneCRBContainsCRAndSA(t, ctx, controlplane, clients), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)

	t.Log("deleting the controlplane ClusterRoleBinding")
	clusterRoleBindings := testutils.MustListControlPlaneClusterRoleBindings(t, GetCtx(), controlplane, clients)
	require.Len(t, clusterRoleBindings, 1, "There must be only one ControlPlane ClusterRoleBinding")
	require.NoError(t, GetClients().MgrClient.Delete(GetCtx(), &clusterRoleBindings[0]))

	t.Log("verifying controlplane ClusterRole and ClusterRoleBinding have been re-created")
	require.Eventually(t, testutils.ControlPlaneHasClusterRole(t, GetCtx(), controlplane, clients), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)
	require.Eventually(t, testutils.ControlPlaneHasClusterRoleBinding(t, GetCtx(), controlplane, clients), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)
	require.Eventually(t, testutils.ControlPlaneCRBContainsCRAndSA(t, ctx, controlplane, clients), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)

	t.Log("deleting the controlplane Deployment")
	require.NoError(t, GetClients().MgrClient.Delete(GetCtx(), deployment))

	t.Log("verifying deployments managed by the dataplane after deletion")
	require.Eventually(t, testutils.ControlPlaneHasActiveDeployment(t, GetCtx(), controlplaneName, clients), time.Minute, time.Second)

	t.Log("verifying controlplane Deployment.Pods.Env vars")
	checkControlPlaneDeploymentEnvVars(t, deployment, controlplane.Name)

	t.Log("verifying controlplane has a validating webhook service created")
	require.Eventually(t, testutils.ControlPlaneHasAdmissionWebhookService(t, GetCtx(), controlplane, clients), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)

	t.Log("verifying controlplane has a validating webhook certificate secret created")
	require.Eventually(t, testutils.ControlPlaneHasAdmissionWebhookCertificateSecret(t, GetCtx(), controlplane, clients), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)

	t.Log("verifying controlplane Deployment has validating webhook certificates mounted")
	verifyControlPlaneDeploymentAdmissionWebhookMount(t, deployment)

	t.Log("verifying controlplane has a validating webhook configuration created")
	require.Eventually(t, testutils.ControlPlaneHasAdmissionWebhookConfiguration(t, GetCtx(), controlplane, clients), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)

	t.Log("verifying controlplane's webhook is functional")
	eventuallyVerifyControlPlaneWebhookIsFunctional(t, GetCtx(), client.NewNamespacedClient(clients.MgrClient, namespace.Name))

	t.Log("verifying that controlplane's ClusterRole is patched if it goes out of sync")
	clusterRoles = testutils.MustListControlPlaneClusterRoles(t, GetCtx(), controlplane, clients)
	require.Len(t, clusterRoles, 1, "There must be only one ControlPlane ClusterRole")
	clusterRole := clusterRoles[0]
	idx := slices.IndexFunc(clusterRole.Rules, func(pr rbacv1.PolicyRule) bool {
		return pr.Resources != nil && slices.Contains(pr.Resources, "endpointslices")
	})
	require.GreaterOrEqual(t, idx, 0)
	endpointSlicesRule := clusterRole.Rules[idx]
	oldClusterRole := clusterRole.DeepCopy()
	require.NotEmpty(t, clusterRole.Rules)
	clusterRole.Rules = slices.Delete(clusterRole.Rules, idx, idx+1)
	t.Logf("deleting endpointslices policyrule form %s clusterrole", clusterRole.Name)
	require.NoError(t, clients.MgrClient.Patch(GetCtx(), &clusterRole, client.MergeFrom(oldClusterRole)))
	t.Log("verifying that controlplane's ClusterRole is patched with the policy rule that was removed")
	require.Eventually(t, testutils.ControlPlanesClusterRoleHasPolicyRule(t, GetCtx(), controlplane, clients, endpointSlicesRule), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)

	t.Log("verifying that controlplane's ClusterRoleBinding is patched if it goes out of sync")
	clusterRoleBindings = testutils.MustListControlPlaneClusterRoleBindings(t, GetCtx(), controlplane, clients)
	require.Len(t, clusterRoleBindings, 1, "There must be only one ControlPlane ClusterRoleBinding")
	clusterRoleBinding := clusterRoleBindings[0]
	require.NotEmpty(t, clusterRoleBinding.Subjects)
	subject := clusterRoleBinding.Subjects[0]
	oldClusterRoleBinding := clusterRoleBinding.DeepCopy()
	clusterRoleBinding.Subjects = slices.Delete(clusterRoleBinding.Subjects, 0, 1)
	t.Logf("deleting %s/%s subject form %s clusterrolebinding", subject.Namespace, subject.Name, clusterRoleBinding.Name)
	require.NoError(t, clients.MgrClient.Patch(GetCtx(), &clusterRoleBinding, client.MergeFrom(oldClusterRoleBinding)))
	t.Log("verifying that controlplane's ClusterRoleBinding is patched with the subject that was removed")
	require.Eventually(t, testutils.ControlPlanesClusterRoleBindingHasSubject(t, GetCtx(), controlplane, clients, subject), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)

	// delete controlplane and verify that cluster wide resources removed.
	t.Log("verifying cluster wide resources removed after controlplane deleted")
	require.NoError(t, controlplaneClient.Delete(GetCtx(), controlplane.Name, metav1.DeleteOptions{}))
	require.Eventually(t, testutils.Not(testutils.ControlPlaneHasClusterRole(t, GetCtx(), controlplane, clients)), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)
	require.Eventually(t, testutils.Not(testutils.ControlPlaneHasClusterRoleBinding(t, GetCtx(), controlplane, clients)), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)
	require.Eventually(t, testutils.Not(testutils.ControlPlaneHasAdmissionWebhookConfiguration(t, GetCtx(), controlplane, clients)), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)
	t.Logf("verifying controlplane %s disappears after cluster resources are deleted", controlplane.Name)
	eventually.WaitForObjectToNotExist(t, ctx, GetClients().MgrClient, controlplane,
		testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick,
	)
}

func TestControlPlaneWatchNamespaces(t *testing.T) {
	t.Skip("Using KIC as a library in ControlPlane controller broke this test  (https://github.com/kong/kong-operator/issues/1659)")

	t.Parallel()
	namespace, cleaner := helpers.SetupTestEnv(t, GetCtx(), GetEnv())
	cl := GetClients().MgrClient

	dp := builder.NewDataPlaneBuilder().
		WithObjectMeta(metav1.ObjectMeta{
			Namespace:    namespace.Name,
			GenerateName: "dp-watchnamespaces-",
		}).
		WithPodTemplateSpec(&corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  consts.DataPlaneProxyContainerName,
						Image: helpers.GetDefaultDataPlaneImage(),
					},
				},
			},
		}).
		Build()

	t.Log("deploying dataplane resource")
	require.NoError(t, cl.Create(GetCtx(), dp))
	cleaner.Add(dp)

	createNamespace := func(t *testing.T, cl client.Client, cleaner *clusters.Cleaner, generateName string) *corev1.Namespace {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: generateName,
			},
		}
		require.NoError(t, cl.Create(GetCtx(), ns))
		cleaner.AddNamespace(ns)
		return ns
	}
	nsA := createNamespace(t, cl, cleaner, "test-namespace-a")
	nsB := createNamespace(t, cl, cleaner, "test-namespace-b")

	cp := &gwtypes.ControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    namespace.Name,
			GenerateName: "cp-watchnamespaces-",
		},
		Spec: gwtypes.ControlPlaneSpec{
			DataPlane: gwtypes.ControlPlaneDataPlaneTarget{
				Type: gwtypes.ControlPlaneDataPlaneTargetRefType,
				Ref: &gwtypes.ControlPlaneDataPlaneTargetRef{
					Name: dp.Name,
				},
			},
			ControlPlaneOptions: gwtypes.ControlPlaneOptions{
				WatchNamespaces: &operatorv1beta1.WatchNamespaces{
					Type: operatorv1beta1.WatchNamespacesTypeList,
					List: []string{
						nsA.Name,
						nsB.Name,
					},
				},
			},
		},
	}

	t.Log("deploying controlplane resource")
	require.NoError(t, cl.Create(GetCtx(), cp))
	cleaner.Add(cp)

	t.Log("verifying controlplane has a status condition indicating missing WatchNamespaceGrants")
	require.Eventually(t,
		testutils.ObjectPredicates(t, clients.MgrClient,
			testutils.MatchCondition[*gwtypes.ControlPlane](t).
				Type(string(kcfgcontrolplane.ConditionTypeWatchNamespaceGrantValid)).
				Status(metav1.ConditionFalse).
				Reason(string(kcfgcontrolplane.ConditionReasonWatchNamespaceGrantInvalid)).
				Predicate(),
			testutils.MatchCondition[*gwtypes.ControlPlane](t).
				Type(string(kcfgdataplane.ReadyType)).
				Status(metav1.ConditionFalse).
				Predicate(),
		).Match(cp),
		testutils.ControlPlaneCondDeadline, 2*testutils.ControlPlaneCondTick,
	)

	t.Log("add missing WatchNamespaceGrants")
	wA := watchNamespaceGrantForNamespace(t, cl, cp, nsA.Name)
	cleaner.Add(wA)
	wB := watchNamespaceGrantForNamespace(t, cl, cp, nsB.Name)
	cleaner.Add(wB)

	t.Log("verifying controlplane has a status condition indicating no missing WatchNamespaceGrants and it's Ready")
	require.Eventually(t,
		testutils.ObjectPredicates(t, clients.MgrClient,
			testutils.MatchCondition[*gwtypes.ControlPlane](t).
				Type(string(kcfgcontrolplane.ConditionTypeWatchNamespaceGrantValid)).
				Status(metav1.ConditionTrue).
				Reason(string(kcfgcontrolplane.ConditionReasonWatchNamespaceGrantValid)).
				Predicate(),
			testutils.MatchCondition[*gwtypes.ControlPlane](t).
				Type(string(kcfgdataplane.ReadyType)).
				Status(metav1.ConditionTrue).
				Predicate(),
		).Match(cp),
		testutils.ControlPlaneCondDeadline, 2*testutils.ControlPlaneCondTick,
	)

	t.Log("verifying that operator creates Roles and RoleBindings in the watched namespaces")
	require.EventuallyWithT(t,
		func(t *assert.CollectT) {
			check := func(t require.TestingT, namespace string) {
				nsOpt := client.InNamespace(namespace)

				roles := testutils.MustListControlPlaneRoles(t, GetCtx(), cp, clients.MgrClient, nsOpt)
				require.Lenf(t, roles, 1, "There must be only one Role in the watched namespace %s", namespace)
				roleBindings := testutils.MustListControlPlaneRoleBindings(t, GetCtx(), cp, clients.MgrClient, nsOpt)
				require.Lenf(t, roleBindings, 1, "There must be only one RoleBinding in the watched namespace %s", namespace)
			}

			check(t, nsA.Name)
			check(t, nsB.Name)
		},
		testutils.ControlPlaneCondDeadline, 2*testutils.ControlPlaneCondTick,
	)

	require.NoError(t, cl.Delete(GetCtx(), wA))
	t.Log("verifying that after removing a WatchNamespaceGrant for a watched namesace controlplane has a status condition indicating invalid/missing WatchNamespaceGrants")
	require.Eventually(t,
		testutils.ObjectPredicates(t, clients.MgrClient,
			testutils.MatchCondition[*gwtypes.ControlPlane](t).
				Type(string(kcfgcontrolplane.ConditionTypeWatchNamespaceGrantValid)).
				Status(metav1.ConditionFalse).
				Reason(string(kcfgcontrolplane.ConditionReasonWatchNamespaceGrantInvalid)).
				Predicate(),
			testutils.MatchCondition[*gwtypes.ControlPlane](t).
				Type(string(kcfgdataplane.ReadyType)).
				Status(metav1.ConditionFalse).
				Predicate(),
		).Match(cp),
		testutils.ControlPlaneCondDeadline, 2*testutils.ControlPlaneCondTick,
	)
}

func watchNamespaceGrantForNamespace(t *testing.T, cl client.Client, cp *gwtypes.ControlPlane, ns string) *operatorv1alpha1.WatchNamespaceGrant {
	wng := &operatorv1alpha1.WatchNamespaceGrant{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: ns + "-refgrant-",
			Namespace:    ns,
		},
		Spec: operatorv1alpha1.WatchNamespaceGrantSpec{
			From: []operatorv1alpha1.WatchNamespaceGrantFrom{
				{
					Group:     operatorv1beta1.SchemeGroupVersion.Group,
					Kind:      "ControlPlane",
					Namespace: cp.Namespace,
				},
			},
		},
	}

	require.NoError(t, cl.Create(GetCtx(), wng))
	return wng
}

func checkControlPlaneDeploymentEnvVars(t *testing.T, deployment *appsv1.Deployment, controlplaneName string) {
	controllerContainer := k8sutils.GetPodContainerByName(&deployment.Spec.Template.Spec, consts.ControlPlaneControllerContainerName)
	require.NotNil(t, controllerContainer)

	envs := controllerContainer.Env
	t.Log("verifying env POD_NAME comes from metadata.name")
	podNameValueFrom := GetEnvValueFromByName(envs, "POD_NAME")
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

	t.Log("verifying CONTROLLER_ELECTION_ID env has value configured in controlplane")
	electionIDEnvValue := GetEnvValueByName(envs, "CONTROLLER_ELECTION_ID")
	require.Equal(t, fmt.Sprintf("%s.konghq.com", controlplaneName), electionIDEnvValue)

	t.Log("verifying custom env TEST_ENV has value configured in controlplane")
	testEnvValue := GetEnvValueByName(envs, "TEST_ENV")
	require.Equal(t, "test", testEnvValue)

	t.Log("verifying that control plane has a validating webhook env var set")
	admissionWebhookListen := GetEnvValueByName(envs, "CONTROLLER_ADMISSION_WEBHOOK_LISTEN")
	require.Equal(t, consts.ControlPlaneAdmissionWebhookEnvVarValue, admissionWebhookListen)
}

func verifyControlPlaneDeploymentAdmissionWebhookMount(t *testing.T, deployment *appsv1.Deployment) {
	volumes := deployment.Spec.Template.Spec.Volumes
	volumeFound := lo.ContainsBy(volumes, func(v corev1.Volume) bool {
		return v.Name == consts.ControlPlaneAdmissionWebhookVolumeName
	})
	require.Truef(t, volumeFound, "volume %s not found in deployment, actual: %s", consts.ControlPlaneAdmissionWebhookVolumeName, deployment.Spec.Template.Spec.Volumes)

	controllerContainer := k8sutils.GetPodContainerByName(&deployment.Spec.Template.Spec, consts.ControlPlaneControllerContainerName)
	require.NotNil(t, controllerContainer, "container %s not found in deployment", consts.ControlPlaneControllerContainerName)

	volumeMount, ok := lo.Find(controllerContainer.VolumeMounts, func(vm corev1.VolumeMount) bool {
		return vm.Name == consts.ControlPlaneAdmissionWebhookVolumeName
	})
	require.Truef(t, ok,
		"volume mount %s not found in container %s, actual: %v",
		consts.ControlPlaneAdmissionWebhookVolumeName,
		consts.ControlPlaneControllerContainerName,
		controllerContainer.VolumeMounts,
	)
	require.Equal(t, consts.ControlPlaneAdmissionWebhookVolumeMountPath, volumeMount.MountPath)
}

// eventuallyVerifyControlPlaneWebhookIsFunctional verifies that the controlplane validating webhook
// is functional by creating a resource that should be rejected by the webhook and verifying that
// it is rejected.
func eventuallyVerifyControlPlaneWebhookIsFunctional(t *testing.T, ctx context.Context, cl client.Client) {
	require.Eventually(t, func() bool {
		ing := netv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test-ingress-",
				Annotations: map[string]string{
					"konghq.com/protocols": "invalid",
				},
			},
			Spec: netv1.IngressSpec{
				IngressClassName: lo.ToPtr(ingressClass),
				DefaultBackend: &netv1.IngressBackend{
					Service: &netv1.IngressServiceBackend{
						Name: "test",
						Port: netv1.ServiceBackendPort{
							Number: 8080,
						},
					},
				},
			},
		}

		err := cl.Create(ctx, &ing)
		if err == nil {
			t.Logf("ControlPlane webhook accepted an invalid Ingress %s, retrying and waiting for webhook to become functional", client.ObjectKeyFromObject(&ing))
			return false
		}
		if !strings.Contains(err.Error(), "admission webhook \"ingresses.validation.ingress-controller.konghq.com\" denied the request") {
			t.Logf("unexpected error: %v", err)
			return false
		}
		return true
	}, testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)
}

func TestControlPlaneUpdate(t *testing.T) {
	t.Skip("Using KIC as a library in ControlPlane controller broke this test (https://github.com/kong/kong-operator/issues/1196)")

	t.Parallel()
	namespace, cleaner := helpers.SetupTestEnv(t, GetCtx(), GetEnv())

	dataplaneClient := GetClients().OperatorClient.GatewayOperatorV1beta1().DataPlanes(namespace.Name)
	controlplaneClient := GetClients().OperatorClient.GatewayOperatorV2alpha1().ControlPlanes(namespace.Name)

	dataplaneName := types.NamespacedName{
		Namespace: namespace.Name,
		Name:      uuid.NewString(),
	}
	dataplane := &operatorv1beta1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: dataplaneName.Namespace,
			Name:      dataplaneName.Name,
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
										Image: helpers.GetDefaultDataPlaneImage(),
										ReadinessProbe: &corev1.Probe{
											InitialDelaySeconds: 1,
											PeriodSeconds:       1,
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	controlplaneName := types.NamespacedName{
		Namespace: namespace.Name,
		Name:      uuid.NewString(),
	}
	controlplane := &gwtypes.ControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: controlplaneName.Namespace,
			Name:      controlplaneName.Name,
		},
		Spec: gwtypes.ControlPlaneSpec{
			DataPlane: gwtypes.ControlPlaneDataPlaneTarget{
				Type: gwtypes.ControlPlaneDataPlaneTargetRefType,
				Ref: &gwtypes.ControlPlaneDataPlaneTargetRef{
					Name: dataplane.Name,
				},
			},
		},
	}

	t.Log("deploying dataplane resource")
	dataplane, err := dataplaneClient.Create(GetCtx(), dataplane, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(dataplane)

	t.Log("verifying deployments managed by the dataplane are ready")
	require.Eventually(t,
		testutils.DataPlaneHasActiveDeployment(t, GetCtx(), dataplaneName, &appsv1.Deployment{}, client.MatchingLabels{
			consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
		}, clients),
		testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick,
	)

	t.Log("deploying controlplane resource")
	controlplane, err = controlplaneClient.Create(GetCtx(), controlplane, metav1.CreateOptions{})
	require.NoError(t, err)
	cleaner.Add(controlplane)

	t.Log("verifying that the controlplane gets marked as provisioned")
	require.Eventually(t, testutils.ControlPlaneIsProvisioned(t, GetCtx(), controlplaneName, clients),
		testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick,
	)
}
