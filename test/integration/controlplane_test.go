package integration

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kcfgcontrolplane "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/controlplane"
	kcfgdataplane "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/dataplane"
	operatorv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1alpha1"
	operatorv1beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1beta1"

	"github.com/kong/kong-operator/controller/pkg/builder"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/pkg/consts"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
	k8sresources "github.com/kong/kong-operator/pkg/utils/kubernetes/resources"
	testutils "github.com/kong/kong-operator/pkg/utils/test"
	"github.com/kong/kong-operator/test/helpers"
)

var dataplaneSpec = operatorv1beta1.DataPlaneSpec{
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
}

func TestControlPlaneWhenNoDataPlane(t *testing.T) {
	t.Skip("Skipping, not specifying a DataPlane is not supported now")

	t.Parallel()
	namespace, cleaner := helpers.SetupTestEnv(t, GetCtx(), GetEnv())

	cl := GetClients().MgrClient

	dataplaneNN := types.NamespacedName{
		Namespace: namespace.Name,
		Name:      uuid.NewString(),
	}
	dataplane := &operatorv1beta1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: dataplaneNN.Namespace,
			Name:      dataplaneNN.Name,
		},
		Spec: dataplaneSpec,
	}
	t.Log("deploying dataplane resource")
	require.NoError(t, cl.Create(GetCtx(), dataplane))
	cleaner.Add(dataplane)

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
			ControlPlaneOptions: gwtypes.ControlPlaneOptions{},
		},
	}

	t.Log("deploying controlplane resource")
	require.NoError(t, cl.Create(GetCtx(), controlplane))
	cleaner.Add(controlplane)

	t.Log("verifying deployments managed by the dataplane are ready")
	require.Eventually(t, testutils.DataPlaneHasActiveDeployment(t, GetCtx(), dataplaneNN, &appsv1.Deployment{}, client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
	}, clients), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)

	t.Log("verifying services managed by the dataplane")
	require.Eventually(t, testutils.DataPlaneHasService(t, GetCtx(), dataplaneNN, clients, client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
		consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneIngressServiceLabelValue),
	}), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)

	t.Log("verifying controlplane is now provisioned")
	require.Eventually(t, testutils.ControlPlaneIsProvisioned(t, GetCtx(), controlplaneName, clients), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)

	t.Log("verifying controlplane is now ready")
	require.Eventually(t, testutils.ControlPlaneIsReady(t, GetCtx(), controlplaneName, clients), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)

	t.Log("removing the dataplane resource")
	require.NoError(t, cl.Delete(GetCtx(), dataplane))

	t.Log("verifying controlplane is not ready anymore")
	require.Eventually(t, testutils.Not(testutils.ControlPlaneIsReady(t, GetCtx(), controlplaneName, clients)), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)
}

func TestControlPlaneEssentials(t *testing.T) {
	t.Parallel()
	namespace, cleaner := helpers.SetupTestEnv(t, GetCtx(), GetEnv())
	cl := GetClients().MgrClient

	dataplane := &operatorv1beta1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    namespace.Name,
			GenerateName: "dp-essentials-",
		},
		Spec: dataplaneSpec,
	}
	t.Log("deploying dataplane resource")
	require.NoError(t, cl.Create(GetCtx(), dataplane))
	cleaner.Add(dataplane)
	dataplaneName := client.ObjectKeyFromObject(dataplane)

	t.Log("verifying deployments managed by the dataplane are ready")
	require.Eventually(t, testutils.DataPlaneHasActiveDeployment(t, GetCtx(), dataplaneName, &appsv1.Deployment{}, client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
	}, clients), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)

	t.Log("verifying services managed by the dataplane")
	require.Eventually(t, testutils.DataPlaneHasActiveService(t, GetCtx(), dataplaneName, nil, clients, client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
		consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneIngressServiceLabelValue),
	}), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)

	controlplane := &gwtypes.ControlPlane{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gwtypes.ControlPlaneGVR().GroupVersion().String(),
			Kind:       "ControlPlane",
		},
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "controlplane-",
			Namespace:    namespace.Name,
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

	t.Log("deploying controlplane resource")
	require.NoError(t, cl.Create(GetCtx(), controlplane))
	addToCleanup(t, cl, controlplane)
	controlplaneName := client.ObjectKeyFromObject(controlplane)

	t.Log("verifying controlplane gets marked scheduled")
	require.Eventually(t, testutils.ControlPlaneIsScheduled(t, GetCtx(), controlplaneName, GetClients().OperatorClient), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)

	t.Log("verifying that the controlplane gets marked as provisioned")
	require.Eventually(t, testutils.ControlPlaneIsProvisioned(t, GetCtx(), controlplaneName, clients), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)

	t.Run("webhook", func(t *testing.T) {
		t.Skip("Skipping webhook tests for now, as they are not implemented yet for ControlPlane v2alpha1, TODO: https://github.com/Kong/kong-operator/issues/1367")

		t.Log("verifying controlplane has a validating webhook service created")
		require.Eventually(t, testutils.ControlPlaneHasAdmissionWebhookService(t, GetCtx(), controlplane, clients), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)

		t.Log("verifying controlplane has a validating webhook certificate secret created")
		require.Eventually(t, testutils.ControlPlaneHasAdmissionWebhookCertificateSecret(t, GetCtx(), controlplane, clients), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)

		t.Log("verifying controlplane has a validating webhook configuration created")
		require.Eventually(t, testutils.ControlPlaneHasAdmissionWebhookConfiguration(t, GetCtx(), controlplane, clients), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)

		t.Log("verifying controlplane's webhook is functional")
		eventuallyVerifyControlPlaneWebhookIsFunctional(t, GetCtx(), client.NewNamespacedClient(clients.MgrClient, namespace.Name))
	})
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

// TODO: https://github.com/Kong/kong-operator/issues/1367
func verifyControlPlaneDeploymentAdmissionWebhookMount(t *testing.T, deployment *appsv1.Deployment) { //nolint:unused
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
