package integration

import (
	"testing"

	"github.com/google/uuid"
	"github.com/kong/kubernetes-testing-framework/pkg/clusters"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kcfgcontrolplane "github.com/kong/kong-operator/v2/api/gateway-operator/controlplane"
	kcfgdataplane "github.com/kong/kong-operator/v2/api/gateway-operator/dataplane"
	gov1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	gov1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	gov2beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v2beta1"
	"github.com/kong/kong-operator/v2/controller/controlplane"
	"github.com/kong/kong-operator/v2/controller/pkg/builder"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sresources "github.com/kong/kong-operator/v2/pkg/utils/kubernetes/resources"
	testutils "github.com/kong/kong-operator/v2/pkg/utils/test"
	"github.com/kong/kong-operator/v2/test/helpers"
)

var dataplaneSpec = gov1beta1.DataPlaneSpec{
	DataPlaneOptions: gov1beta1.DataPlaneOptions{
		Deployment: gov1beta1.DataPlaneDeploymentOptions{
			DeploymentOptions: gov1beta1.DeploymentOptions{
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

func TestControlPlaneEssentials(t *testing.T) {
	t.Parallel()
	namespace, cleaner := helpers.SetupTestEnv(t, GetCtx(), GetEnv())
	cl := GetClients().MgrClient

	dataplane := &gov1beta1.DataPlane{
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
			ControlPlaneOptions: gwtypes.ControlPlaneOptions{
				IngressClass: lo.ToPtr(ingressClass),
			},
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

	t.Log("verifying that the controlplane gets marked as optionsValid")
	require.Eventually(t, testutils.ControlPlaneIsOptionsValid(t, GetCtx(), controlplaneName, clients), testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick)
}

func TestControlPlaneWatchNamespaces(t *testing.T) {
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
				IngressClass: lo.ToPtr(ingressClass),
				WatchNamespaces: &gov2beta1.WatchNamespaces{
					Type: gov2beta1.WatchNamespacesTypeList,
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
	_ = watchNamespaceGrantForNamespace(t, cl, cp, nsB.Name)

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

	require.NoError(t, cl.Delete(GetCtx(), wA))
	t.Log("verifying that after removing a WatchNamespaceGrant for a watched namespace controlplane has a status condition indicating invalid/missing WatchNamespaceGrants")
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

func watchNamespaceGrantForNamespace(t *testing.T, cl client.Client, cp *gwtypes.ControlPlane, ns string) *gov1alpha1.WatchNamespaceGrant {
	wng := &gov1alpha1.WatchNamespaceGrant{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: ns + "-refgrant-",
			Namespace:    ns,
		},
		Spec: gov1alpha1.WatchNamespaceGrantSpec{
			From: []gov1alpha1.WatchNamespaceGrantFrom{
				{
					Group:     gov1beta1.SchemeGroupVersion.Group,
					Kind:      "ControlPlane",
					Namespace: cp.Namespace,
				},
			},
		},
	}

	require.NoError(t, cl.Create(GetCtx(), wng))
	return wng
}

func TestControlPlaneUpdate(t *testing.T) {
	t.Parallel()
	namespace, cleaner := helpers.SetupTestEnv(t, GetCtx(), GetEnv())

	dataplaneClient := GetClients().OperatorClient.GatewayOperatorV1beta1().DataPlanes(namespace.Name)

	dataplaneName := types.NamespacedName{
		Namespace: namespace.Name,
		Name:      uuid.NewString(),
	}
	dataplane := &gov1beta1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: dataplaneName.Namespace,
			Name:      dataplaneName.Name,
		},
		Spec: gov1beta1.DataPlaneSpec{
			DataPlaneOptions: gov1beta1.DataPlaneOptions{
				Deployment: gov1beta1.DataPlaneDeploymentOptions{
					DeploymentOptions: gov1beta1.DeploymentOptions{
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
	cp := &gwtypes.ControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: controlplaneName.Namespace,
			Name:      controlplaneName.Name,
		},
		Spec: gwtypes.ControlPlaneSpec{
			ControlPlaneOptions: gwtypes.ControlPlaneOptions{
				IngressClass: lo.ToPtr(ingressClass),
			},
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
	require.NoError(t, GetClients().MgrClient.Create(GetCtx(), cp))
	cleaner.Add(cp)

	t.Log("verifying that the controlplane gets marked as provisioned")
	require.Eventually(t, testutils.ControlPlaneIsProvisioned(t, GetCtx(), controlplaneName, clients),
		testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick,
	)

	t.Log("verifying that the controlplane gets marked as ready")
	require.Eventually(t, testutils.ControlPlaneIsReady(t, GetCtx(), controlplaneName, clients),
		testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick,
	)

	t.Log("updating controlplane to disable the Kong Consumer controller")
	require.Eventually(t,
		testutils.ControlPlaneUpdateEventually(t, GetCtx(), controlplaneName, clients,
			func(cp *gwtypes.ControlPlane) {
				cp.Spec.Controllers = append(cp.Spec.Controllers, gwtypes.ControlPlaneController{
					Name:  controlplane.ControllerNameKongConsumer,
					State: gwtypes.ControlPlaneControllerStateDisabled,
				})
			},
		),
		testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick,
	)

	t.Log("verifying that the controlplane has the Kong Consumer controller disabled (set in status field)")
	require.EventuallyWithT(t,
		func(t *assert.CollectT) {
			var cp gwtypes.ControlPlane
			require.NoError(t, GetClients().MgrClient.Get(GetCtx(), controlplaneName, &cp))

			assert.Contains(t, cp.Status.Controllers, gwtypes.ControlPlaneController{
				Name:  controlplane.ControllerNameKongConsumer,
				State: gwtypes.ControlPlaneControllerStateDisabled,
			})
		},
		testutils.ControlPlaneCondDeadline, testutils.ControlPlaneCondTick,
	)
}
