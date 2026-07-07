package konnect

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kong/kubernetes-testing-framework/pkg/utils/kubernetes/generators"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/controller/pkg/builder"
	"github.com/kong/kong-operator/v2/pkg/consts"
	testutils "github.com/kong/kong-operator/v2/pkg/utils/test"
	"github.com/kong/kong-operator/v2/test"
	"github.com/kong/kong-operator/v2/test/helpers"
	"github.com/kong/kong-operator/v2/test/helpers/asserts"
	"github.com/kong/kong-operator/v2/test/helpers/certificate"
	"github.com/kong/kong-operator/v2/test/helpers/conditions"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
	"github.com/kong/kong-operator/v2/test/helpers/object"
	"github.com/kong/kong-operator/v2/test/integration"
)

func TestKonnectExtensionKonnectControlPlaneNotFound(t *testing.T) {
	ctx := t.Context()
	ns, _ := helpers.SetupTestEnv(t, ctx, integration.GetEnv())
	cl := integration.GetClients().MgrClient

	// Let's generate a unique test ID that we can refer to in Konnect entities.
	// Using only the first 8 characters of the UUID to keep the ID short enough for Konnect to accept it as a part
	// of an entity name.
	testID := uuid.NewString()[:8]
	t.Logf("Running Konnect extensions test with ID: %s", testID)

	// Create an APIAuth for test.
	clientNamespaced := client.NewNamespacedClient(integration.GetClients().MgrClient, ns.Name)

	konnectExtension := deploy.KonnectExtension(
		t, ctx, clientNamespaced,
		deploy.WithKonnectExtensionKonnectNamespacedRefControlPlaneRef(&konnectv1alpha2.KonnectGatewayControlPlane{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "controlplane-not-found",
				Namespace: ns.Name,
			},
		}),
	)

	t.Logf("Waiting for KonnectExtension %s/%s to have expected conditions set to False", konnectExtension.Namespace, konnectExtension.Name)
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		ok, msg := conditions.CheckKonnectExtensionConditions(ctx, t, cl,
			konnectExtension,
			helpers.CheckAllConditionsFalse,
			konnectv1alpha1.ControlPlaneRefValidConditionType,
			konnectv1alpha2.KonnectExtensionReadyConditionType)
		assert.Truef(t, ok, "condition check failed: %s, conditions: %+v", msg, konnectExtension.Status.Conditions)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)
}

func TestKonnectExtensionControlPlaneRotation(t *testing.T) {
	ctx := t.Context()
	ns, _ := helpers.SetupTestEnv(t, ctx, integration.GetEnv())
	cl := integration.GetClients().MgrClient

	// Let's generate a unique test ID that we can refer to in Konnect entities.
	// Using only the first 8 characters of the UUID to keep the ID short enough for Konnect to accept it as a part
	// of an entity name.
	testID := uuid.NewString()[:8]
	t.Logf("Running Konnect extensions test with ID: %s", testID)

	// Create an APIAuth for test.
	clientNamespaced := client.NewNamespacedClient(cl, ns.Name)

	authCfg := deploy.KonnectAPIAuthConfiguration(t, ctx, clientNamespaced,
		deploy.WithTestIDLabel(testID),
		deploy.KonnectAPIAuthConfigurationWithTestToken(test.KonnectAccessToken(), test.KonnectServerURL()),
	)

	// Create a Konnect control plane for the KonnectExtension to attach to.
	cp := deploy.KonnectGatewayControlPlane(t, ctx, clientNamespaced, authCfg,
		deploy.WithTestIDLabel(testID),
		deploy.KonnectGatewayControlPlaneLabel(deploy.KonnectTestIDLabel, testID),
	)

	t.Logf("Waiting for Konnect ID to be assigned to ControlPlane %s/%s", cp.Namespace, cp.Name)
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := cl.Get(ctx, k8stypes.NamespacedName{Name: cp.Name, Namespace: cp.Namespace}, cp)
		require.NoError(t, err)
		conditions.KonnectEntityIsProgrammed(t, cp)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

	konnectExtension := deploy.KonnectExtension(
		t, ctx, clientNamespaced,
		deploy.WithKonnectExtensionKonnectNamespacedRefControlPlaneRef(cp),
	)

	t.Logf("Waiting for KonnectExtension %s/%s to have expected conditions set to True", konnectExtension.Namespace, konnectExtension.Name)
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		ok, msg := conditions.CheckKonnectExtensionConditions(ctx, t, cl,
			konnectExtension,
			helpers.CheckAllConditionsTrue,
			konnectv1alpha1.ControlPlaneRefValidConditionType,
			konnectv1alpha1.DataPlaneCertificateProvisionedConditionType,
			konnectv1alpha2.KonnectExtensionReadyConditionType)
		assert.Truef(t, ok, "condition check failed: %s, conditions: %+v", msg, konnectExtension.Status.Conditions)
	}, testutils.ObjectUpdateTimeout*30, testutils.ObjectUpdateTick)

	t.Logf("waiting for status.konnect and status.dataPlaneClientAuth to be set for KonnectExtension %s/%s", konnectExtension.Namespace, konnectExtension.Name)
	require.EventuallyWithT(t,
		conditions.CheckKonnectExtensionStatus(ctx, cl, konnectExtension, cp.GetKonnectID(), ""),
		testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

	t.Logf("deleting Konnect control plane %s/%s", cp.Namespace, cp.Name)
	object.DeleteAndWaitForDeletionFn(context.Background(), t, cl, cp.DeepCopy())()

	// Create a Konnect control plane for the KonnectExtension to attach to.
	cp = deploy.KonnectGatewayControlPlane(t, ctx, clientNamespaced, authCfg,
		deploy.WithTestIDLabel(testID),
		deploy.WithName(cp.Name), // Reuse the same name to ensure the KonnectExtension is recreated with the same name.
		deploy.KonnectGatewayControlPlaneLabel(deploy.KonnectTestIDLabel, testID),
	)
	t.Cleanup(object.DeleteAndWaitForDeletionFn(context.Background(), t, cl, cp.DeepCopy()))

	t.Logf("Waiting for Konnect ID to be assigned to ControlPlane %s/%s", cp.Namespace, cp.Name)
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := cl.Get(ctx, k8stypes.NamespacedName{Name: cp.Name, Namespace: cp.Namespace}, cp)
		require.NoError(t, err)
		conditions.KonnectEntityIsProgrammed(t, cp)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

	t.Logf("Waiting for KonnectExtension %s/%s to have expected conditions set to True", konnectExtension.Namespace, konnectExtension.Name)
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		ok, msg := conditions.CheckKonnectExtensionConditions(ctx, t, cl,
			konnectExtension,
			helpers.CheckAllConditionsTrue,
			konnectv1alpha1.ControlPlaneRefValidConditionType,
			konnectv1alpha1.DataPlaneCertificateProvisionedConditionType,
			konnectv1alpha2.KonnectExtensionReadyConditionType)
		assert.Truef(t, ok, "condition check failed: %s, conditions: %+v", msg, konnectExtension.Status.Conditions)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

	t.Logf("waiting for status.konnect and status.dataPlaneClientAuth to be properly updated for KonnectExtension %s/%s", konnectExtension.Namespace, konnectExtension.Name)
	require.EventuallyWithT(t,
		conditions.CheckKonnectExtensionStatus(ctx, cl, konnectExtension, cp.GetKonnectID(), ""),
		testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

	// delete the KonnectExtension first to avoid the ControlPlane gets deleted first and
	// the KonnectExtension gets stuck in deletion.
	object.DeleteAndWaitForDeletionFn(context.Background(), t, cl, konnectExtension.DeepCopy())()
}

func TestKonnectExtension(t *testing.T) {
	ctx := t.Context()
	ns, _ := helpers.SetupTestEnv(t, ctx, integration.GetEnv())
	cl := integration.GetClients().MgrClient

	// Let's generate a unique test ID that we can refer to in Konnect entities.
	// Using only the first 8 characters of the UUID to keep the ID short enough for Konnect to accept it as a part
	// of an entity name.
	testID := uuid.NewString()[:8]
	t.Logf("Running Konnect extensions test with ID: %s", testID)

	// Create an APIAuth for test.
	clientNamespaced := client.NewNamespacedClient(integration.GetClients().MgrClient, ns.Name)

	authCfg := deploy.KonnectAPIAuthConfiguration(t, ctx, clientNamespaced,
		deploy.WithTestIDLabel(testID),
		deploy.KonnectAPIAuthConfigurationWithTestToken(test.KonnectAccessToken(), test.KonnectServerURL()),
	)

	t.Log("deploying backend deployment (httpbin) of HTTPRoute")
	container := generators.NewContainer("httpbin", testutils.HTTPBinImage, 80)
	deployment := generators.NewDeploymentForContainer(container)
	require.NoError(t, clientNamespaced.Create(ctx, deployment))

	t.Logf("exposing deployment %s via service", deployment.Name)
	service := generators.NewServiceForDeployment(deployment, corev1.ServiceTypeClusterIP)
	require.NoError(t, clientNamespaced.Create(ctx, service))

	t.Run("Konnect hybrid ControlPlane", func(t *testing.T) {
		// Create a Konnect control plane for the KonnectExtension to attach to.
		cp := deploy.KonnectGatewayControlPlane(t, ctx, clientNamespaced, authCfg,
			deploy.WithTestIDLabel(testID),
			deploy.KonnectGatewayControlPlaneLabel(deploy.KonnectTestIDLabel, testID),
		)
		t.Cleanup(object.DeleteAndWaitForDeletionFn(context.Background(), t, cl, cp.DeepCopy()))

		t.Logf("Waiting for Konnect ID to be assigned to ControlPlane %s/%s", cp.Namespace, cp.Name)
		require.EventuallyWithT(t, func(t *assert.CollectT) {
			err := integration.GetClients().MgrClient.Get(ctx, k8stypes.NamespacedName{Name: cp.Name, Namespace: cp.Namespace}, cp)
			require.NoError(t, err)
			conditions.KonnectEntityIsProgrammed(t, cp)
		}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

		t.Run("Origin ControlPlane", func(t *testing.T) {
			// Create entities to check proper working on Konnect.
			deployKonnectEntitiesForKonnectExtensionTest(t, cl, KonnectExtensionTestCaseParams{
				konnectControlPlane: cp,
				client:              clientNamespaced,
				namespace:           ns.Name,
				service:             service,
				authConfigName:      authCfg.Name,
			})

			// run the KonnectExtension test cases.
			konnectExtensionTestCases(t, cl, KonnectExtensionTestCaseParams{
				konnectControlPlane: cp,
				service:             service,
				client:              clientNamespaced,
				namespace:           ns.Name,
				authConfigName:      authCfg.Name,
			})
		})

		t.Run("Mirror ControlPlane", func(t *testing.T) {
			// Create a Mirror Konnect control plane for the KonnectExtension to attach to.
			mirrorCP := deploy.KonnectGatewayControlPlane(t, ctx, clientNamespaced, authCfg,
				deploy.WithTestIDLabel(testID),
				deploy.WithMirrorSource(cp.GetKonnectID()),
			)
			t.Cleanup(object.DeleteAndWaitForDeletionFn(context.Background(), t, cl, mirrorCP.DeepCopy()))

			t.Logf("Waiting for Konnect ID to be assigned to ControlPlane %s/%s", mirrorCP.Namespace, mirrorCP.Name)
			require.EventuallyWithT(t, func(t *assert.CollectT) {
				err := integration.GetClients().MgrClient.Get(ctx, k8stypes.NamespacedName{Name: mirrorCP.Name, Namespace: mirrorCP.Namespace}, mirrorCP)
				require.NoError(t, err)
				conditions.KonnectEntityIsProgrammed(t, mirrorCP)
			}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

			require.Eventually(t,
				testutils.ObjectPredicates(t, cl,
					testutils.MatchCondition[*konnectv1alpha2.KonnectGatewayControlPlane](t).
						Type(string(konnectv1alpha1.ControlPlaneMirroredConditionType)).
						Status(metav1.ConditionTrue).
						Reason(string(konnectv1alpha1.ControlPlaneMirroredReasonMirrored)).
						Predicate(),
				).Match(mirrorCP),
				testutils.ControlPlaneCondDeadline, 2*testutils.ControlPlaneCondTick,
			)

			// Create entities to check proper working on Konnect.
			deployKonnectEntitiesForKonnectExtensionTest(t, cl, KonnectExtensionTestCaseParams{
				konnectControlPlane: mirrorCP,
				client:              clientNamespaced,
				namespace:           ns.Name,
				service:             service,
				authConfigName:      authCfg.Name,
			})

			konnectExtensionTestCases(t, cl, KonnectExtensionTestCaseParams{
				konnectControlPlane: mirrorCP,
				service:             service,
				client:              clientNamespaced,
				namespace:           ns.Name,
				authConfigName:      authCfg.Name,
			})
		})
	})

	t.Run("Konnect hybrid ControlPlane different order of deletion (KonnectGatewayControlPlane before KonnectExtension)", func(t *testing.T) {
		// Create a Konnect control plane for the KonnectExtension to attach to.
		cp := deploy.KonnectGatewayControlPlane(t, ctx, clientNamespaced, authCfg,
			deploy.WithTestIDLabel(testID),
			deploy.KonnectGatewayControlPlaneLabel(deploy.KonnectTestIDLabel, testID),
		)

		t.Logf("Waiting for Konnect ID to be assigned to ControlPlane %s/%s", cp.Namespace, cp.Name)
		require.EventuallyWithT(t, func(t *assert.CollectT) {
			err := integration.GetClients().MgrClient.Get(ctx, k8stypes.NamespacedName{Name: cp.Name, Namespace: cp.Namespace}, cp)
			require.NoError(t, err)
			conditions.KonnectEntityIsProgrammed(t, cp)
		}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

		konnectExtension := deploy.KonnectExtension(
			t,
			ctx,
			clientNamespaced,
			deploy.WithKonnectExtensionKonnectNamespacedRefControlPlaneRef(cp),
		)

		t.Logf("Waiting for KonnectExtension %s/%s to have expected conditions set to True", konnectExtension.Namespace, konnectExtension.Name)
		require.EventuallyWithT(t, func(t *assert.CollectT) {
			ok, msg := conditions.CheckKonnectExtensionConditions(ctx, t, cl,
				konnectExtension,
				helpers.CheckAllConditionsTrue,
				konnectv1alpha1.ControlPlaneRefValidConditionType,
				konnectv1alpha1.DataPlaneCertificateProvisionedConditionType,
				konnectv1alpha2.KonnectExtensionReadyConditionType)
			assert.Truef(t, ok, "condition check failed: %s, conditions: %+v", msg, konnectExtension.Status.Conditions)
		}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

		t.Logf("waiting for status.konnect and status.dataPlaneClientAuth to be set for KonnectExtension %s/%s", konnectExtension.Namespace, konnectExtension.Name)
		require.EventuallyWithT(t,
			conditions.CheckKonnectExtensionStatus(ctx, cl, konnectExtension, cp.GetKonnectID(), ""),
			testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

		t.Log("Test deletion")

		// Enforce order explicitly to check the behavior.
		t.Log("Deleting KonnectControlPlane")
		object.DeleteAndWaitForDeletionFn(context.Background(), t, cl, cp.DeepCopy())()
		t.Log("Deleting KonnectExtension")
		object.DeleteAndWaitForDeletionFn(context.Background(), t, cl, konnectExtension.DeepCopy())()
	})
}

type KonnectExtensionTestCaseParams struct {
	konnectControlPlane *konnectv1alpha2.KonnectGatewayControlPlane
	service             *corev1.Service
	namespace           string
	client              client.Client
	authConfigName      string
}

func konnectExtensionTestCases(t *testing.T, cl client.Client, params KonnectExtensionTestCaseParams) {
	ctx := t.Context()
	cert, key := certificate.MustGenerateCertPEMFormat()

	t.Run("KonnectExtension with KonnectNamespacedRef control plane ref", func(t *testing.T) {
		t.Run("manual secret provisioning", func(t *testing.T) {
			t.Logf("Creating a Secret Certificate for the KonnectExtension")
			secretCert := deploy.Secret(
				t, ctx, params.client,
				map[string][]byte{
					consts.TLSCRT: cert,
					consts.TLSKey: key,
				},
				deploy.WithLabel(
					"konghq.com/konnect-dp-cert", "true",
				),
				deploy.WithLabel(
					"konghq.com/secret", "true",
				),
			)
			t.Cleanup(object.DeleteAndWaitForDeletionFn(context.Background(), t, cl, secretCert.DeepCopy()))

			konnectExtension := deploy.KonnectExtension(
				t, ctx, params.client,
				deploy.WithKonnectExtensionKonnectNamespacedRefControlPlaneRef(params.konnectControlPlane),
				setKonnectExtensionDPCertSecretRef(t, secretCert),
			)
			t.Cleanup(object.DeleteAndWaitForDeletionFn(context.Background(), t, cl, konnectExtension.DeepCopy()))

			params := KonnectExtensionTestBodyParams{
				konnectControlPlane: params.konnectControlPlane,
				konnectExtension:    konnectExtension,
				secret:              secretCert,
				client:              params.client,
				authConfigName:      params.authConfigName,
				namespace:           params.namespace,
			}
			konnectExtensionTestBody(t, cl, params)
		})

		t.Run("automatic secret provisioning", func(t *testing.T) {
			konnectExtension := deploy.KonnectExtension(
				t, ctx, params.client,
				deploy.WithKonnectExtensionKonnectNamespacedRefControlPlaneRef(params.konnectControlPlane),
			)
			t.Cleanup(object.DeleteAndWaitForDeletionFn(context.Background(), t, cl, konnectExtension.DeepCopy()))
			params := KonnectExtensionTestBodyParams{
				konnectControlPlane: params.konnectControlPlane,
				konnectExtension:    konnectExtension,
				secret:              nil, // automatic provisioning
				client:              params.client,
				authConfigName:      params.authConfigName,
				namespace:           params.namespace,
			}
			konnectExtensionTestBody(t, cl, params)
		})
	})
}

// KonnectExtensionTestBodyParams is a struct that holds the parameters for the test body function.
type KonnectExtensionTestBodyParams struct {
	KonnectExtensionTestCaseParams

	konnectExtension    *konnectv1alpha2.KonnectExtension
	secret              *corev1.Secret
	authConfigName      string
	konnectControlPlane *konnectv1alpha2.KonnectGatewayControlPlane
	namespace           string
	client              client.Client
}

func deployKonnectEntitiesForKonnectExtensionTest(
	t *testing.T,
	cl client.Client,
	params KonnectExtensionTestCaseParams,
) {
	t.Helper()

	ctx := t.Context()
	ks := deploy.KongService(t, ctx, params.client,
		deploy.WithKonnectNamespacedRefControlPlaneRef(params.konnectControlPlane),
		func(obj client.Object) {
			ks, ok := obj.(*configurationv1alpha1.KongService)
			require.True(t, ok)
			ks.Spec.KongServiceAPISpec = configurationv1alpha1.KongServiceAPISpec{
				Name: new("httpbin"),
				URL:  new(fmt.Sprintf("http://%s.%s.svc.cluster.local/", params.service.Name, params.namespace)),
				Host: fmt.Sprintf("%s.%s.svc.cluster.local", params.service.Name, params.namespace),
			}
		},
	)
	t.Logf("Waiting for KongService to be updated with Konnect ID")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := integration.GetClients().MgrClient.Get(ctx, k8stypes.NamespacedName{Name: ks.Name, Namespace: ks.Namespace}, ks)
		require.NoError(t, err)
		conditions.KonnectEntityIsProgrammed(t, ks)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)
	t.Cleanup(object.DeleteAndWaitForDeletionFn(context.Background(), t, cl, ks))

	kr := deploy.KongRoute(
		t, ctx, params.client,
		deploy.WithNamespacedKongServiceRef(ks),
		func(obj client.Object) {
			s := obj.(*configurationv1alpha1.KongRoute)
			s.Spec.Paths = []string{"/test"}
		},
	)
	t.Logf("Waiting for KongRoute to be updated with Konnect ID")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := integration.GetClients().MgrClient.Get(ctx, k8stypes.NamespacedName{Name: kr.Name, Namespace: kr.Namespace}, kr)
		require.NoError(t, err)

		conditions.KonnectEntityIsProgrammed(t, kr)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)
	t.Cleanup(object.DeleteAndWaitForDeletionFn(context.Background(), t, cl, kr))
}

// konnectExtensionTestBody is a function that runs the test body for KonnectExtension.
// The logic herein defined is shared between all the dataplane KonnectExtension tests.
func konnectExtensionTestBody(t *testing.T, cl client.Client, p KonnectExtensionTestBodyParams) {
	t.Helper()
	ctx := t.Context()

	t.Logf("Waiting for KonnectExtension %s/%s to have expected conditions set to True", p.konnectExtension.Namespace, p.konnectExtension.Name)
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		ok, msg := conditions.CheckKonnectExtensionConditions(ctx, t, cl,
			p.konnectExtension,
			helpers.CheckAllConditionsTrue,
			konnectv1alpha1.ControlPlaneRefValidConditionType,
			konnectv1alpha1.DataPlaneCertificateProvisionedConditionType,
			konnectv1alpha2.KonnectExtensionReadyConditionType)
		assert.Truef(t, ok, "condition check failed: %s, conditions: %+v", msg, p.konnectExtension.Status.Conditions)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

	t.Logf("waiting for status.konnect and status.dataPlaneClientAuth to be set for KonnectExtension %s/%s", p.konnectExtension.Namespace, p.konnectExtension.Name)
	require.EventuallyWithT(t,
		conditions.CheckKonnectExtensionStatus(ctx, cl, p.konnectExtension, p.konnectControlPlane.GetKonnectID(), ""),
		testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

	t.Logf("Creating a DataPlane using the KonnectExtension %s/%s", p.konnectExtension.Namespace, p.konnectExtension.Name)
	dataPlane := builder.NewDataPlaneBuilder().
		WithObjectMeta(metav1.ObjectMeta{
			Namespace: p.namespace,
			Name:      "test-konnect-extension",
		}).
		WithPodTemplateSpec(&corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  consts.DataPlaneProxyContainerName,
						Image: helpers.GetDefaultDataPlaneImage(),
						Env: []corev1.EnvVar{
							{
								Name:  "KONG_LOG_LEVEL",
								Value: "debug",
							},
							{
								Name:  "KONG_INCREMENTAL_SYNC",
								Value: "off",
							},
						},
						ReadinessProbe: &corev1.Probe{
							InitialDelaySeconds: 3,
							PeriodSeconds:       3,
						},
					},
				},
			},
		}).
		WithExtensions(
			[]commonv1alpha1.ExtensionRef{
				{
					Group: konnectv1alpha1.GroupVersion.Group,
					Kind:  "KonnectExtension",
					NamespacedRef: commonv1alpha1.NamespacedRef{
						Name: p.konnectExtension.Name,
					},
				},
			},
		).Build()
	require.NoError(t, p.client.Create(ctx, dataPlane))
	t.Cleanup(object.DeleteAndWaitForDeletionFn(context.Background(), t, cl, dataPlane))

	dpName := k8stypes.NamespacedName{
		Namespace: dataPlane.Namespace,
		Name:      dataPlane.Name,
	}

	t.Log("verifying dataplane gets marked provisioned")
	require.Eventually(t, testutils.DataPlaneIsReady(t, ctx, dpName, integration.GetClients().OperatorClient), waitTime, tickTime)

	t.Logf("verifying dataplane %s has ingress service", dpName)
	var dpIngressService corev1.Service
	require.Eventually(t, testutils.DataPlaneHasActiveService(t, ctx, dpName, &dpIngressService, integration.GetClients(), client.MatchingLabels{
		consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
		consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneIngressServiceLabelValue),
	}), waitTime, tickTime)

	t.Log("verifying dataplane services receive IP addresses")
	require.Eventually(t, func() bool {
		err := p.client.Get(ctx, k8stypes.NamespacedName{
			Namespace: dpIngressService.Namespace,
			Name:      dpIngressService.Name,
		}, &dpIngressService)
		require.NoError(t, err)
		return len(dpIngressService.Status.LoadBalancer.Ingress) > 0
	}, waitTime, tickTime)
	dpIngressIP := dpIngressService.Status.LoadBalancer.Ingress[0].IP
	require.Eventuallyf(t, asserts.Expect404WithNoRouteFunc(t, ctx, "http://"+dpIngressIP), waitTime, tickTime,
		"Should receive 'No Route' response from dataplane's ingress service IP %s", dpIngressIP)

	t.Log("route to /test path of service httpbin should receive a 200 OK response")
	httpClient, err := helpers.CreateHTTPClient(nil, "")
	require.NoError(t, err)
	const routeAccessTimeout = 3 * time.Minute
	request := helpers.MustBuildRequest(t, ctx, http.MethodGet, "http://"+dpIngressIP+"/test", "")
	require.Eventually(
		t,
		testutils.GetResponseBodyContains(t, httpClient, request, "<title>httpbin.org</title>"),
		routeAccessTimeout,
		time.Second,
	)
}

func setKonnectExtensionDPCertSecretRef(t *testing.T, s *corev1.Secret) deploy.ObjOption {
	return func(obj client.Object) {
		ke, ok := obj.(*konnectv1alpha2.KonnectExtension)
		require.True(t, ok)
		ke.Spec.ClientAuth = &konnectv1alpha2.KonnectExtensionClientAuth{
			CertificateSecret: konnectv1alpha2.CertificateSecret{
				Provisioning: new(konnectv1alpha2.ManualSecretProvisioning),
				CertificateSecretRef: &konnectv1alpha2.SecretRef{
					Name: s.Name,
				},
			},
		}
	}
}
