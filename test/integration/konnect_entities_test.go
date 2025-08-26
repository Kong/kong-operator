package integration

import (
	"strings"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/common/v1alpha1"
	configurationv1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha2"

	"github.com/kong/kong-operator/controller/konnect"
	testutils "github.com/kong/kong-operator/pkg/utils/test"
	"github.com/kong/kong-operator/test"
	"github.com/kong/kong-operator/test/helpers"
	"github.com/kong/kong-operator/test/helpers/deploy"
	"github.com/kong/kong-operator/test/helpers/eventually"
)

func TestKonnectEntities(t *testing.T) {
	// A cleaner is created underneath anyway, and a whole namespace is deleted eventually.
	// We can't use a cleaner to delete objects because it handles deletes in FIFO order and that won't work in this
	// case: KonnectAPIAuthConfiguration shouldn't be deleted before any other object as that is required for others to
	// complete their finalizer which is deleting a reflecting entity in Konnect. That's why we're only cleaning up a
	// KonnectGatewayControlPlane and waiting for its deletion synchronously with deleteObjectAndWaitForDeletionFn to ensure it
	// was successfully deleted along with its children. The KonnectAPIAuthConfiguration is implicitly deleted along
	// with the namespace.
	ns, _ := helpers.SetupTestEnv(t, GetCtx(), GetEnv())

	// Let's generate a unique test ID that we can refer to in Konnect entities.
	// Using only the first 8 characters of the UUID to keep the ID short enough for Konnect to accept it as a part
	// of an entity name.
	testID := uuid.NewString()[:8]
	t.Logf("Running Konnect entities test with ID: %s", testID)

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

	cp := deploy.KonnectGatewayControlPlane(t, GetCtx(), clientNamespaced, authCfg,
		deploy.WithTestIDLabel(testID),
	)

	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, cp.DeepCopy()))

	t.Logf("Waiting for Konnect ID to be assigned to ControlPlane %s/%s", cp.Namespace, cp.Name)
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: cp.Name, Namespace: cp.Namespace}, cp)
		require.NoError(t, err)
		assertKonnectEntityProgrammed(t, cp)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

	t.Logf("Waiting for ControlPlane and telemetry endpoints to be assigned to ControlPlane %s/%s", cp.Namespace, cp.Name)
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: cp.Name, Namespace: cp.Namespace}, cp)
		require.NoError(t, err)
		require.NotEmpty(t, cp.Status.Endpoints)
		// Example: https://e7b5c7de43.us.cp0.konghq.tech - always it will include ".cp0.".
		require.True(t, strings.HasPrefix(cp.Status.Endpoints.ControlPlaneEndpoint, "https://"), "must start with https://")
		require.Contains(t, cp.Status.Endpoints.ControlPlaneEndpoint, ".cp0.", "must contain .cp0.")
		// Example: https://e7b5c7de43.us.tp0.konghq.tech - always it will include ".tp0.".
		require.True(t, strings.HasPrefix(cp.Status.Endpoints.TelemetryEndpoint, "https://"), "must start with https://")
		require.Contains(t, cp.Status.Endpoints.TelemetryEndpoint, ".tp0.", "must contain .tp0.")
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

	t.Run("with Origin ControlPlane", func(t *testing.T) {
		KonnectEntitiesTestCase(t, konnectEntitiesTestCaseParams{
			cp:     cp,
			client: clientNamespaced,
			ns:     ns.Name,
			testID: testID,
		})
	})

	t.Run("with Mirror ControlPlane", func(t *testing.T) {
		// Create a Mirror Konnect control plane for the Konnect Entities test case.
		mirrorCP := deploy.KonnectGatewayControlPlane(t, GetCtx(), clientNamespaced, authCfg,
			deploy.WithTestIDLabel(testID),
			deploy.WithMirrorSource(cp.GetKonnectID()),
		)
		t.Cleanup(deleteObjectAndWaitForDeletionFn(t, mirrorCP.DeepCopy()))

		t.Logf("Waiting for Konnect ID to be assigned to ControlPlane %s/%s", mirrorCP.Namespace, mirrorCP.Name)
		require.EventuallyWithT(t, func(t *assert.CollectT) {
			err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: mirrorCP.Name, Namespace: mirrorCP.Namespace}, mirrorCP)
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

		KonnectEntitiesTestCase(t, konnectEntitiesTestCaseParams{
			cp:     mirrorCP,
			client: clientNamespaced,
			ns:     ns.Name,
			testID: testID,
		})
	})
}

type konnectEntitiesTestCaseParams struct {
	cp     *konnectv1alpha2.KonnectGatewayControlPlane
	client client.Client
	ns     string
	testID string
}

func KonnectEntitiesTestCase(t *testing.T, params konnectEntitiesTestCaseParams) {
	subID := uuid.NewString()[:8]
	params.testID = params.testID + "-" + subID

	ks := deploy.KongService(t, ctx, params.client,
		deploy.WithKonnectNamespacedRefControlPlaneRef(params.cp),
		deploy.WithTestIDLabel(params.testID),
	)

	t.Logf("Waiting for KongService to be updated with Konnect ID")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: ks.Name, Namespace: ks.Namespace}, ks)
		require.NoError(t, err)
		assertKonnectEntityProgrammed(t, ks)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

	kr := deploy.KongRoute(t, ctx, params.client,
		deploy.WithKonnectNamespacedRefControlPlaneRef(params.cp),
		deploy.WithTestIDLabel(params.testID),
		func(obj client.Object) {
			kr := obj.(*configurationv1alpha1.KongRoute)
			kr.Spec.Paths = []string{"/kr-" + params.testID}
			kr.Spec.Headers = map[string][]string{
				"KongTestHeader": {"example.com", "example.org"},
			}
		},
	)
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, kr.DeepCopy()))

	t.Logf("Waiting for KongRoute attached to ControlPlane (serviceless) to be updated with Konnect ID")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: kr.Name, Namespace: kr.Namespace}, kr)
		require.NoError(t, err)
		assertKonnectEntityProgrammed(t, kr)
		require.NotNil(t, kr.Status.Konnect)
		require.Equal(t, params.cp.Status.ID, kr.Status.Konnect.ControlPlaneID, "ControlPlaneID should be set")
		require.Empty(t, kr.Status.Konnect.ServiceID, "ServiceID should not be set")
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

	t.Log("Making KongRoute service bound")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: kr.Name, Namespace: kr.Namespace}, kr)
		require.NoError(t, err)
		kr.Spec.ServiceRef = &configurationv1alpha1.ServiceRef{
			Type: configurationv1alpha1.ServiceRefNamespacedRef,
			NamespacedRef: &commonv1alpha1.NameRef{
				Name: ks.Name,
			},
		}
		err = GetClients().MgrClient.Update(GetCtx(), kr)
		require.NoError(t, err)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: kr.Name, Namespace: kr.Namespace}, kr)
		require.NoError(t, err)

		assertKonnectEntityProgrammed(t, kr)
		require.Equal(t, params.cp.Status.ID, kr.Status.Konnect.ControlPlaneID, "ControlPlaneID should be set")
		require.Equal(t, ks.Status.Konnect.ID, kr.Status.Konnect.ServiceID, "ServiceID should be set")
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

	t.Log("Making KongRoute serviceless again")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: kr.Name, Namespace: kr.Namespace}, kr)
		require.NoError(t, err)
		kr.Spec.ServiceRef = nil
		err = GetClients().MgrClient.Update(GetCtx(), kr)
		require.NoError(t, err)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: kr.Name, Namespace: kr.Namespace}, kr)
		require.NoError(t, err)
		assertKonnectEntityProgrammed(t, kr)
		require.Equal(t, params.cp.Status.ID, kr.Status.Konnect.ControlPlaneID, "ControlPlaneID should be set")
		require.Empty(t, kr.Status.Konnect.ServiceID, "ServiceID should not be set")
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

	kcg := deploy.KongConsumerGroupAttachedToCP(t, ctx, params.client,
		deploy.WithKonnectNamespacedRefControlPlaneRef(params.cp),
		deploy.WithTestIDLabel(params.testID),
	)

	t.Logf("Waiting for KongConsumerGroup to be updated with Konnect ID")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: kcg.Name, Namespace: params.ns}, kcg)
		require.NoError(t, err)

		assertKonnectEntityProgrammed(t, kcg)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

	kc := deploy.KongConsumer(t, ctx, params.client, "kc-"+params.testID,
		deploy.WithKonnectNamespacedRefControlPlaneRef(params.cp),
		deploy.WithTestIDLabel(params.testID),
		func(obj client.Object) {
			kc := obj.(*configurationv1.KongConsumer)
			kc.ConsumerGroups = []string{kcg.Name}
			kc.Spec.ControlPlaneRef = &commonv1alpha1.ControlPlaneRef{
				Type:                 configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{Name: params.cp.Name},
			}
		},
	)
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, kc.DeepCopy()))

	t.Logf("Waiting for KongConsumer to be updated with Konnect ID and Programmed")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: kc.Name, Namespace: params.ns}, kc)
		require.NoError(t, err)

		assertKonnectEntityProgrammed(t, kc)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

	kp := deploy.ProxyCachePlugin(t, ctx, params.client)
	kpb := deploy.KongPluginBinding(t, ctx, params.client,
		konnect.NewKongPluginBindingBuilder().
			WithServiceTarget(ks.Name).
			WithPluginRef(kp.Name).
			WithControlPlaneRefKonnectNamespaced(params.cp.Name).
			Build(),
		deploy.WithTestIDLabel(params.testID),
	)
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, kp.DeepCopy()))

	t.Logf("Waiting for KongPluginBinding to be updated with Konnect ID")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: kpb.Name, Namespace: params.ns}, kpb)
		require.NoError(t, err)

		assertKonnectEntityProgrammed(t, kpb)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

	globalKPB := deploy.KongPluginBinding(t, ctx, params.client,
		konnect.NewKongPluginBindingBuilder().
			WithPluginRef(kp.Name).
			WithControlPlaneRefKonnectNamespaced(params.cp.Name).
			WithScope(configurationv1alpha1.KongPluginBindingScopeGlobalInControlPlane).
			Build(),
		deploy.WithTestIDLabel(params.testID),
	)
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, globalKPB.DeepCopy()))

	t.Logf("Waiting for KongPluginBinding to be updated with Konnect ID")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: globalKPB.Name, Namespace: globalKPB.Namespace}, globalKPB)
		require.NoError(t, err)
		assertKonnectEntityProgrammed(t, globalKPB)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

	kup := deploy.KongUpstream(t, ctx, params.client,
		deploy.WithKonnectNamespacedRefControlPlaneRef(params.cp),
		deploy.WithTestIDLabel(params.testID),
		func(obj client.Object) {
			kup := obj.(*configurationv1alpha1.KongUpstream)
			kup.Spec.Name = ks.Spec.Host
			kup.Spec.Slots = lo.ToPtr(int64(16384))
			kup.Spec.Algorithm = sdkkonnectcomp.UpstreamAlgorithmConsistentHashing.ToPointer()
		},
	)
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, kup.DeepCopy()))

	t.Log("Waiting for KongUpstream to be updated with Konnect ID")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: kup.Name, Namespace: params.ns}, kup)
		require.NoError(t, err)

		assertKonnectEntityProgrammed(t, kup)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

	kt := deploy.KongTargetAttachedToUpstream(t, ctx, params.client, kup,
		deploy.WithTestIDLabel(params.testID),
		func(obj client.Object) {
			kt := obj.(*configurationv1alpha1.KongTarget)
			kt.Spec.Target = "example.com"
			kt.Spec.Weight = 100
		},
	)

	t.Log("Waiting for KongTarget to be updated with Konnect ID")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: kt.Name, Namespace: params.ns}, kt)
		require.NoError(t, err)

		assertKonnectEntityProgrammed(t, kt)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

	// Should delete KongTarget because it will block deletion of KongUpstream owning it.
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, kt.DeepCopy()))

	kv := deploy.KongVaultAttachedToCP(t, ctx, params.client, "env", "env-vault", []byte(`{"prefix":"env-vault"}`), params.cp)
	t.Logf("Waiting for KongVault to be updated with Konnect ID")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: kv.Name}, kv)
		require.NoError(t, err)

		assertKonnectEntityProgrammed(t, kv)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, kv.DeepCopy()))

	kcert := deploy.KongCertificateAttachedToCP(t, ctx, params.client, params.cp)
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, kcert.DeepCopy()))

	t.Logf("Waiting for KongCertificate to get Konnect ID")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{
			Name:      kcert.Name,
			Namespace: params.ns,
		}, kcert)
		require.NoError(t, err)

		assertKonnectEntityProgrammed(t, kcert)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

	ksni := deploy.KongSNIAttachedToCertificate(t, ctx, params.client, kcert,
		deploy.WithTestIDLabel(params.testID),
		func(obj client.Object) {
			ksni := obj.(*configurationv1alpha1.KongSNI)
			ksni.Spec.Name = "test.kong-sni.example.com"
		},
	)
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, ksni.DeepCopy()))

	t.Logf("Waiting for KongSNI to get Konnect ID")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{
			Name:      ksni.Name,
			Namespace: params.ns,
		}, ksni)
		require.NoError(t, err)

		assertKonnectEntityProgrammed(t, ksni)
		assert.Equal(t, kcert.GetKonnectID(), ksni.Status.Konnect.CertificateID)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)
}

// deleteObjectAndWaitForDeletionFn returns a function that deletes the given object and waits for it to be gone.
// It's designed to be used with t.Cleanup() to ensure the object is properly deleted (it's not stuck with finalizers, etc.).
func deleteObjectAndWaitForDeletionFn(t *testing.T, obj client.Object) func() {
	return func() {
		t.Logf("Deleting %s/%s and waiting for it gone",
			obj.GetNamespace(), obj.GetName(),
		)
		cl := GetClients().MgrClient
		require.NoError(t, cl.Delete(GetCtx(), obj))
		eventually.WaitForObjectToNotExist(t, ctx, cl, obj,
			testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick,
		)
	}
}

// assertKonnectEntityProgrammed asserts that the KonnectEntityProgrammed condition is set to true and the Konnect
// status fields are populated.
func assertKonnectEntityProgrammed(
	t assert.TestingT,
	obj interface {
		GetKonnectStatus() *konnectv1alpha2.KonnectEntityStatus
		GetConditions() []metav1.Condition
	},
) {
	konnectStatus := obj.GetKonnectStatus()
	if !assert.NotNil(t, konnectStatus) {
		return
	}
	assert.NotEmpty(t, konnectStatus.GetKonnectID(), "empty Konnect ID")
	assert.NotEmpty(t, konnectStatus.GetOrgID(), "empty Org ID")
	assert.NotEmpty(t, konnectStatus.GetServerURL(), "empty Server URL")

	assert.True(t, lo.ContainsBy(obj.GetConditions(), func(condition metav1.Condition) bool {
		return condition.Type == konnectv1alpha1.KonnectEntityProgrammedConditionType &&
			condition.Status == metav1.ConditionTrue
	}), "condition %s is not set to True", konnectv1alpha1.KonnectEntityProgrammedConditionType)
}
