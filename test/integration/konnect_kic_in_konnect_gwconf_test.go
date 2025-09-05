package integration

import (
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

	commonv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha2"

	"github.com/kong/kong-operator/controller/pkg/builder"
	"github.com/kong/kong-operator/pkg/consts"
	testutils "github.com/kong/kong-operator/pkg/utils/test"
	"github.com/kong/kong-operator/test"
	"github.com/kong/kong-operator/test/helpers"
	"github.com/kong/kong-operator/test/helpers/deploy"
)

func TestKicInKonnectGatewayConfigurationTest(t *testing.T) {
	t.Skip("to implement KIC in Konnect test")
	ns, _ := helpers.SetupTestEnv(t, GetCtx(), GetEnv())

	// Let's generate a unique test ID that we can refer to in Konnect entities.
	// Using only the first 8 characters of the UUID to keep the ID short enough for Konnect to accept it as a part
	// of an entity name.
	testID := uuid.NewString()[:8]
	t.Logf("Running Konnect extensions test with ID: %s", testID)

	// Create an APIAuth for test.
	clientNamespaced := client.NewNamespacedClient(GetClients().MgrClient, ns.Name)

	authCfg := deploy.KonnectAPIAuthConfiguration(t, GetCtx(), clientNamespaced,
		deploy.WithTestIDLabel(testID),
		func(obj client.Object) {
			authCfg := obj.(*konnectv1alpha1.KonnectAPIAuthConfiguration)
			authCfg.Spec.Type = konnectv1alpha1.KonnectAPIAuthTypeToken
			authCfg.Spec.Token = test.KonnectAccessToken()
			authCfg.Spec.ServerURL = test.KonnectServerURL()
		},
	)

	t.Log("deploying backend deployment (httpbin) of HTTPRoute")
	container := generators.NewContainer("httpbin", testutils.HTTPBinImage, 80)
	deployment := generators.NewDeploymentForContainer(container)
	require.NoError(t, clientNamespaced.Create(ctx, deployment))

	t.Logf("exposing deployment %s via service", deployment.Name)
	service := generators.NewServiceForDeployment(deployment, corev1.ServiceTypeClusterIP)
	require.NoError(t, clientNamespaced.Create(ctx, service))

	// Create a Konnect control plane for the KonnectExtension to attach to.
	cp := deploy.KonnectGatewayControlPlane(t, GetCtx(), clientNamespaced, authCfg,
		deploy.WithTestIDLabel(testID),
	)
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, cp.DeepCopy()))

	t.Logf("Waiting for Konnect ID to be assigned to ControlPlane %s/%s", cp.Namespace, cp.Name)
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), k8stypes.NamespacedName{Name: cp.Name, Namespace: cp.Namespace}, cp)
		require.NoError(t, err)
		assertKonnectEntityProgrammed(t, cp)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

	t.Run("Origin ControlPlane", func(t *testing.T) {
		// Create entities to check proper working on Konnect.
		deployKonnectEntitiesForKonnectExtension(t, KonnectTestCaseParams{
			konnectControlPlane: cp,
			client:              clientNamespaced,
			namespace:           ns.Name,
			service:             service,
			authConfigName:      authCfg.Name,
		})

		// run the KonnectExtension test cases.
		KonnectExtensionTestCases(t, KonnectTestCaseParams{
			konnectControlPlane: cp,
			service:             service,
			client:              clientNamespaced,
			namespace:           ns.Name,
			authConfigName:      authCfg.Name,
		}, KonnectKICInKonnectTestBody)
	})

	t.Run("Mirror ControlPlane", func(t *testing.T) {
		// Create a Mirror Konnect control plane for the KonnectExtension to attach to.
		mirrorCP := deploy.KonnectGatewayControlPlane(t, GetCtx(), clientNamespaced, authCfg,
			deploy.WithTestIDLabel(testID),
			deploy.WithMirrorSource(cp.GetKonnectID()),
		)
		t.Cleanup(deleteObjectAndWaitForDeletionFn(t, mirrorCP.DeepCopy()))

		t.Logf("Waiting for Konnect ID to be assigned to ControlPlane %s/%s", mirrorCP.Namespace, mirrorCP.Name)
		require.EventuallyWithT(t, func(t *assert.CollectT) {
			err := GetClients().MgrClient.Get(GetCtx(), k8stypes.NamespacedName{Name: mirrorCP.Name, Namespace: mirrorCP.Namespace}, mirrorCP)
			require.NoError(t, err)
			assertKonnectEntityProgrammed(t, mirrorCP)
		}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

		require.Eventually(t,
			testutils.ObjectPredicates(t, clients.MgrClient,
				testutils.MatchCondition[*konnectv1alpha2.KonnectGatewayControlPlane](t).
					Type(string(konnectv1alpha1.ControlPlaneMirroredConditionType)).
					Status(metav1.ConditionTrue).
					Reason(string(konnectv1alpha1.ControlPlaneMirroredReasonMirrored)).
					Predicate(),
			).Match(mirrorCP),
			testutils.ControlPlaneCondDeadline, 2*testutils.ControlPlaneCondTick,
		)

		// Create entities to check proper working on Konnect.
		deployKonnectEntitiesForKonnectExtension(t, KonnectTestCaseParams{
			konnectControlPlane: mirrorCP,
			client:              clientNamespaced,
			namespace:           ns.Name,
			service:             service,
			authConfigName:      authCfg.Name,
		})

		KonnectExtensionTestCases(t, KonnectTestCaseParams{
			konnectControlPlane: mirrorCP,
			service:             service,
			client:              clientNamespaced,
			namespace:           ns.Name,
			authConfigName:      authCfg.Name,
		}, KonnectKICInKonnectTestBody)
	})
}

// KonnectKICInKonnectTestBody is a function that runs the test body for KonnectExtension.
// The logic herein defined is shared between all the dataplane KonnectExtension tests.
func KonnectKICInKonnectTestBody(t *testing.T, p KonnectTestBodyParams) {
	t.Logf("Waiting for KonnectExtension %s/%s to have expected conditions set to True", p.konnectExtension.Namespace, p.konnectExtension.Name)
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		ok, msg := checkKonnectExtensionConditions(t,
			p.konnectExtension,
			helpers.CheckAllConditionsTrue,
			konnectv1alpha1.ControlPlaneRefValidConditionType,
			konnectv1alpha1.DataPlaneCertificateProvisionedConditionType,
			konnectv1alpha2.KonnectExtensionReadyConditionType)
		assert.Truef(t, ok, "condition check failed: %s, conditions: %+v", msg, p.konnectExtension.Status.Conditions)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

	t.Logf("waiting for status.konnect and status.dataPlaneClientAuth to be set for KonnectExtension %s/%s", p.konnectExtension.Namespace, p.konnectExtension.Name)
	require.EventuallyWithT(t,
		checkKonnectExtensionStatus(p.konnectExtension, p.konnectControlPlane.GetKonnectID(), ""),
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
						Image: helpers.GetDefaultDataPlaneEnterpriseImage(),
						Env: []corev1.EnvVar{
							{
								Name:  "KONG_LOG_LEVEL",
								Value: "debug",
							},
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
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, dataPlane))

	dpName := k8stypes.NamespacedName{
		Namespace: dataPlane.Namespace,
		Name:      dataPlane.Name,
	}

	t.Log("verifying dataplane gets marked provisioned")
	require.Eventually(t, testutils.DataPlaneIsReady(t, GetCtx(), dpName, GetClients().OperatorClient), waitTime, tickTime)

	t.Logf("verifying dataplane %s has ingress service", dpName)
	var dpIngressService corev1.Service
	require.Eventually(t, testutils.DataPlaneHasActiveService(t, GetCtx(), dpName, &dpIngressService, clients, client.MatchingLabels{
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
	require.Eventuallyf(t, Expect404WithNoRouteFunc(t, GetCtx(), "http://"+dpIngressIP), waitTime, tickTime,
		"Should receive 'No Route' response from dataplane's ingress service IP %s", dpIngressIP)

	t.Log("route to /test path of service httpbin should receive a 200 OK response")
	httpClient, err := helpers.CreateHTTPClient(nil, "")
	require.NoError(t, err)
	const routeAccessTimeout = 3 * time.Minute
	request := helpers.MustBuildRequest(t, GetCtx(), http.MethodGet, "http://"+dpIngressIP+"/test", "")
	require.Eventually(
		t,
		testutils.GetResponseBodyContains(t, clients, httpClient, request, "<title>httpbin.org</title>"),
		routeAccessTimeout,
		time.Second,
	)
}
