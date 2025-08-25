package integration

import (
	"fmt"
	"testing"

	"github.com/kong/kong-operator/pkg/consts"
	testutils "github.com/kong/kong-operator/pkg/utils/test"
	"github.com/kong/kong-operator/test/helpers"
	"github.com/kong/kong-operator/test/helpers/certificate"
	"github.com/kong/kong-operator/test/helpers/deploy"
	kcfgconsts "github.com/kong/kubernetes-configuration/v2/api/common/consts"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1alpha1"
	konnectv1alpha2 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha2"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// KonnectTestCaseParams is a struct that holds the parameters for the KonnectExtension test cases.
type KonnectTestCaseParams struct {
	konnectControlPlane *konnectv1alpha2.KonnectGatewayControlPlane
	service             *corev1.Service
	namespace           string
	client              client.Client
	authConfigName      string
}

// KonnectTestBodyParams is a struct that holds the parameters for the test body function.
type KonnectTestBodyParams struct {
	KonnectTestCaseParams
	konnectExtension    *konnectv1alpha2.KonnectExtension
	secret              *corev1.Secret
	authConfigName      string
	konnectControlPlane *konnectv1alpha2.KonnectGatewayControlPlane
	namespace           string
	client              client.Client
}

func deployKonnectEntitiesForKonnectExtension(
	t *testing.T,
	params KonnectTestCaseParams,
) {
	ks := deploy.KongService(t, GetCtx(), params.client,
		deploy.WithKonnectNamespacedRefControlPlaneRef(params.konnectControlPlane),
		func(obj client.Object) {
			ks, ok := obj.(*configurationv1alpha1.KongService)
			require.True(t, ok)
			ks.Spec.KongServiceAPISpec = configurationv1alpha1.KongServiceAPISpec{
				Name: lo.ToPtr("httpbin"),
				URL:  lo.ToPtr(fmt.Sprintf("http://%s.%s.svc.cluster.local/", params.service.Name, params.namespace)),
				Host: fmt.Sprintf("%s.%s.svc.cluster.local", params.service.Name, params.namespace),
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
		err := GetClients().MgrClient.Get(GetCtx(), k8stypes.NamespacedName{Name: kr.Name, Namespace: kr.Namespace}, kr)
		require.NoError(t, err)

		assertKonnectEntityProgrammed(t, kr)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, kr))
}

func KonnectExtensionTestCases(t *testing.T, params KonnectTestCaseParams, testhandler func(t *testing.T, params KonnectTestBodyParams)) {
	cert, key := certificate.MustGenerateSelfSignedCertPEMFormat()

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
			t.Cleanup(deleteObjectAndWaitForDeletionFn(t, secretCert.DeepCopy()))

			konnectExtension := deploy.KonnectExtension(
				t, ctx, params.client,
				deploy.WithKonnectExtensionKonnectNamespacedRefControlPlaneRef(params.konnectControlPlane),
				setKonnectExtensionDPCertSecretRef(t, secretCert),
			)
			t.Cleanup(deleteObjectAndWaitForDeletionFn(t, konnectExtension.DeepCopy()))

			params := KonnectTestBodyParams{
				konnectControlPlane: params.konnectControlPlane,
				konnectExtension:    konnectExtension,
				secret:              secretCert,
				client:              params.client,
				authConfigName:      params.authConfigName,
				namespace:           params.namespace,
			}
			testhandler(t, params)
		})

		t.Run("automatic secret provisioning", func(t *testing.T) {
			konnectExtension := deploy.KonnectExtension(
				t, ctx, params.client,
				deploy.WithKonnectExtensionKonnectNamespacedRefControlPlaneRef(params.konnectControlPlane),
			)
			t.Cleanup(deleteObjectAndWaitForDeletionFn(t, konnectExtension.DeepCopy()))
			params := KonnectTestBodyParams{
				konnectControlPlane: params.konnectControlPlane,
				konnectExtension:    konnectExtension,
				secret:              nil, // automatic provisioning
				client:              params.client,
				authConfigName:      params.authConfigName,
				namespace:           params.namespace,
			}
			testhandler(t, params)
		})
	})
}

func setKonnectExtensionDPCertSecretRef(t *testing.T, s *corev1.Secret) deploy.ObjOption {
	return func(obj client.Object) {
		ke, ok := obj.(*konnectv1alpha2.KonnectExtension)
		require.True(t, ok)
		ke.Spec.ClientAuth = &konnectv1alpha2.KonnectExtensionClientAuth{
			CertificateSecret: konnectv1alpha2.CertificateSecret{
				Provisioning: lo.ToPtr(konnectv1alpha2.ManualSecretProvisioning),
				CertificateSecretRef: &konnectv1alpha2.SecretRef{
					Name: s.Name,
				},
			},
		}
	}
}

func checkKonnectExtensionConditions(
	t *assert.CollectT,
	ke *konnectv1alpha2.KonnectExtension,
	checker helpers.ConditionsChecker,
	conditions ...kcfgconsts.ConditionType,
) (bool, string) {
	err := GetClients().MgrClient.Get(GetCtx(), k8stypes.NamespacedName{Name: ke.Name, Namespace: ke.Namespace}, ke)
	require.NoError(t, err)

	return checker(ke, conditions...)
}

func checkKonnectExtensionStatus(
	ke *konnectv1alpha2.KonnectExtension,
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
		if expectedDPCertificateSecretName != "" {
			assert.Equal(t, expectedDPCertificateSecretName, ke.Status.DataPlaneClientAuth.CertificateSecretRef.Name,
				"status.dataPlaneClientAuth.certiifcateSecretRef should have the expected secret name")
		}
	}
}
