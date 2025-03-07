package integration

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kong/kubernetes-testing-framework/pkg/utils/kubernetes/generators"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/pkg/consts"
	testutils "github.com/kong/gateway-operator/pkg/utils/test"
	"github.com/kong/gateway-operator/test"
	"github.com/kong/gateway-operator/test/helpers"
	"github.com/kong/gateway-operator/test/helpers/certificate"
	"github.com/kong/gateway-operator/test/helpers/deploy"

	kcfgconsts "github.com/kong/kubernetes-configuration/api/common/consts"
	commonv1alpha1 "github.com/kong/kubernetes-configuration/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

func TestKonnectExtension(t *testing.T) {
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

	// Create a Konnect control plane for the KonnectExtension to attach to.
	cp := deploy.KonnectGatewayControlPlane(t, GetCtx(), clientNamespaced, authCfg,
		deploy.WithTestIDLabel(testID),
	)

	t.Logf("Waiting for Konnect ID to be assigned to ControlPlane %s/%s", cp.Namespace, cp.Name)
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), k8stypes.NamespacedName{Name: cp.Name, Namespace: cp.Namespace}, cp)
		require.NoError(t, err)
		assertKonnectEntityProgrammed(t, cp)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

	// Order of deleting objects with finalizers:
	// KongRoute & KongService -> DataPlane -> KonnectExtension -> Secret -> KonnectGatewayControlPlane.
	// The first object deleted by calling `deleteObjectAndWaitForDeletionFn` will be deleted last when added by `CleanUp`,
	// so the order of calling the deleting function should be a reverse of the order above.
	// After they are all deleted, the namespace can be deleted in the final clean up.
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, cp.DeepCopy()))

	// Create a secret used as dataplane certificate for the KonnectExtension.

	cert, key := certificate.MustGenerateSelfSignedCertPEMFormat()

	dpCert1 := deploy.Secret(
		t, ctx, clientNamespaced,
		map[string][]byte{
			consts.TLSCRT: cert,
			consts.TLSKey: key,
		},
		deploy.WithLabel("konghq.com/konnect-dp-cert", "true"),
	)
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, dpCert1.DeepCopy()))

	t.Log("deploying backend deployment (httpbin) of HTTPRoute")
	container := generators.NewContainer("httpbin", testutils.HTTPBinImage, 80)
	deployment := generators.NewDeploymentForContainer(container)
	require.NoError(t, clientNamespaced.Create(ctx, deployment))

	t.Logf("exposing deployment %s via service", deployment.Name)
	service := generators.NewServiceForDeployment(deployment, corev1.ServiceTypeClusterIP)
	require.NoError(t, clientNamespaced.Create(ctx, service))

	t.Log("Creating a KongService and a KongRoute to the service")
	ks := deploy.KongService(t, ctx, clientNamespaced,
		deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
		func(obj client.Object) {
			ks, ok := obj.(*configurationv1alpha1.KongService)
			require.True(t, ok)
			ks.Spec.KongServiceAPISpec = configurationv1alpha1.KongServiceAPISpec{
				Name: lo.ToPtr("httpbin"),
				URL:  lo.ToPtr(fmt.Sprintf("http://%s.%s.svc.cluster.local/", service.Name, ns.Name)),
				Host: fmt.Sprintf("%s.%s.svc.cluster.local", service.Name, ns.Name),
			}
		},
	)
	t.Logf("Waiting for KongService to be updated with Konnect ID")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), k8stypes.NamespacedName{Name: ks.Name, Namespace: ks.Namespace}, ks)
		require.NoError(t, err)
		assertKonnectEntityProgrammed(t, ks)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, ks))

	// Tests on KonnectExtension with KonnectID control plane ref.
	t.Logf("Creating a KonnectExtension with KonnectID typed control plane ref")
	keWithKonnectIDCPRef := deploy.KonnectExtension(
		t, ctx, clientNamespaced,
		deploy.WithKonnectConfiguration[*konnectv1alpha1.KonnectExtension](konnectv1alpha1.KonnectConfiguration{
			APIAuthConfigurationRef: konnectv1alpha1.KonnectAPIAuthConfigurationRef{
				Name: authCfg.Name,
			},
		}),
		deploy.WithKonnectIDControlPlaneRef(cp),
		setKonnectExtensionDPCertSecretRef(t, dpCert1),
	)
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, keWithKonnectIDCPRef.DeepCopy()))

	t.Logf("Waiting for KonnectExtension %s/%s to have expected conditions set to True", keWithKonnectIDCPRef.Namespace, keWithKonnectIDCPRef.Name)
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		ok, msg := checkKonnectExtensionConditions(t, keWithKonnectIDCPRef)
		assert.Truef(t, ok, "condition check failed: %s, conditions: %+v", msg, keWithKonnectIDCPRef.Status.Conditions)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

	t.Logf("waiting for status.konnect and status.dataPlaneClientAuth to be set for KonnectExtension %s/%s", keWithKonnectIDCPRef.Namespace, keWithKonnectIDCPRef.Name)
	require.EventuallyWithT(t,
		checkKonnectExtensionStatus(keWithKonnectIDCPRef, cp.GetKonnectID(), dpCert1.Name),
		testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

	// Create a DataPlane using the KonnectExension.
	t.Logf("Creating a DataPlane using the KonnectExtension %s/%s", keWithKonnectIDCPRef.Namespace, keWithKonnectIDCPRef.Name)
	dp := &operatorv1beta1.DataPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns.Name,
			Name:      "test-konnect-extension-dp-1",
		},
		Spec: operatorv1beta1.DataPlaneSpec{
			DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
				Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
					DeploymentOptions: operatorv1beta1.DeploymentOptions{
						Replicas: lo.ToPtr(int32(1)),
						PodTemplateSpec: &corev1.PodTemplateSpec{
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
										},
									},
								},
							},
						},
					},
				},
				Extensions: []commonv1alpha1.ExtensionRef{
					{
						Group: konnectv1alpha1.GroupVersion.Group,
						Kind:  "KonnectExtension",
						NamespacedRef: commonv1alpha1.NamespacedRef{
							Name: keWithKonnectIDCPRef.Name,
						},
					},
				},
			},
		},
	}
	require.NoError(t, clientNamespaced.Create(ctx, dp))
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, dp))

	dpName := k8stypes.NamespacedName{
		Namespace: dp.Namespace,
		Name:      dp.Name,
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
		err := clientNamespaced.Get(ctx, k8stypes.NamespacedName{
			Namespace: dpIngressService.Namespace,
			Name:      dpIngressService.Name,
		}, &dpIngressService)
		require.NoError(t, err)
		return len(dpIngressService.Status.LoadBalancer.Ingress) > 0
	}, waitTime, tickTime)
	dpIngressIP := dpIngressService.Status.LoadBalancer.Ingress[0].IP
	require.Eventuallyf(t, Expect404WithNoRouteFunc(t, GetCtx(), "http://"+dpIngressIP), waitTime, tickTime,
		"Should receive 'No Route' response from dataplane's ingress service IP %s", dpIngressIP)

	kr := deploy.KongRouteAttachedToService(t, ctx, clientNamespaced, ks)
	t.Logf("Waiting for KongRoute to be updated with Konnect ID")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), k8stypes.NamespacedName{Name: kr.Name, Namespace: kr.Namespace}, kr)
		require.NoError(t, err)

		assertKonnectEntityProgrammed(t, kr)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, kr))

	t.Log("route to / path of service httpbin should receive a 200 OK response")
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

	// Tests on KonnectExtension with KonnectNamespacedRef control plane ref.
	dpCert2 := deploy.Secret(
		t, ctx, clientNamespaced,
		map[string][]byte{
			consts.TLSCRT: cert,
			consts.TLSKey: key,
		},
		deploy.WithLabel("konghq.com/konnect-dp-cert", "true"),
	)
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, dpCert2.DeepCopy()))
	t.Logf("Creating a KonnectExtension with KonnectNamespacedRef typed control plane ref")
	keWithNamespacedCPRef := deploy.KonnectExtension(
		t, ctx,
		clientNamespaced,
		deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
		setKonnectExtensionDPCertSecretRef(t, dpCert2),
	)
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, keWithNamespacedCPRef.DeepCopy()))

	t.Logf("Waiting for KonnectExtension %s/%s to have expected conditions set to True", keWithNamespacedCPRef.Namespace, keWithNamespacedCPRef.Name)
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		ok, msg := checkKonnectExtensionConditions(t, keWithNamespacedCPRef)
		assert.Truef(t, ok, "condition check failed: %s, conditions: %+v", msg, keWithNamespacedCPRef.Status.Conditions)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

	t.Logf("waiting for status.konnect and status.dataPlaneClientAuth to be set for KonnectExtension %s/%s", keWithNamespacedCPRef.Namespace, keWithNamespacedCPRef.Name)
	require.EventuallyWithT(t,
		checkKonnectExtensionStatus(keWithNamespacedCPRef, cp.GetKonnectID(), dpCert2.Name),
		testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

}

func setKonnectExtensionDPCertSecretRef(t *testing.T, s *corev1.Secret) deploy.ObjOption {
	return func(obj client.Object) {
		ke, ok := obj.(*konnectv1alpha1.KonnectExtension)
		require.True(t, ok)
		ke.Spec.ClientAuth = &konnectv1alpha1.KonnectExtensionClientAuth{
			CertificateSecret: konnectv1alpha1.CertificateSecret{
				Provisioning: lo.ToPtr(konnectv1alpha1.ManualSecretProvisioning),
				CertificateSecretRef: &konnectv1alpha1.SecretRef{
					Name: s.Name,
				},
			},
		}
	}
}

func checkKonnectExtensionConditions(t *assert.CollectT, ke *konnectv1alpha1.KonnectExtension) (bool, string) {
	err := GetClients().MgrClient.Get(GetCtx(), k8stypes.NamespacedName{Name: ke.Name, Namespace: ke.Namespace}, ke)
	require.NoError(t, err)

	checkConditionTypes := []kcfgconsts.ConditionType{
		konnectv1alpha1.ControlPlaneRefValidConditionType,
		konnectv1alpha1.DataPlaneCertificateProvisionedConditionType,
		konnectv1alpha1.KonnectExtensionReadyConditionType,
	}
	return helpers.CheckAllConditionsTrue(ke, checkConditionTypes)
}

func checkKonnectExtensionStatus(
	ke *konnectv1alpha1.KonnectExtension,
	expectedKonnectCPID string,
	expectedDPCertificateSecretName string,
) func(t *assert.CollectT) {
	return func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), k8stypes.NamespacedName{Name: ke.Name, Namespace: ke.Namespace}, ke)
		require.NoError(t, err)
		// Check Konnect control plane ID
		require.NotNil(t, ke.Status.Konnect, "status.konnect should be present")
		assert.Equal(t, expectedKonnectCPID, ke.Status.Konnect.ControlPlaneID, "Konnect control plane ID should be set in status")
		// Check dataplane client auth
		require.NotNil(t, ke.Status.DataPlaneClientAuth, "status.dataPlaneClientAuth should be present")
		require.NotNil(t, ke.Status.DataPlaneClientAuth.CertificateSecretRef, "status.dataPlaneClientAuth.certiifcateSecretRef should be present")
		assert.Equal(t, expectedDPCertificateSecretName, ke.Status.DataPlaneClientAuth.CertificateSecretRef.Name,
			"status.dataPlaneClientAuth.certiifcateSecretRef should have the expected secret name")
	}
}
