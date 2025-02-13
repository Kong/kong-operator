package integration

import (
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/controller/konnect"
	testutils "github.com/kong/gateway-operator/pkg/utils/test"
	"github.com/kong/gateway-operator/test"
	"github.com/kong/gateway-operator/test/helpers"
	"github.com/kong/gateway-operator/test/helpers/deploy"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
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

	ks := deploy.KongService(t, ctx, clientNamespaced,
		deploy.WithKonnectIDControlPlaneRef(cp),
		deploy.WithTestIDLabel(testID),
	)

	t.Logf("Waiting for KongService to be updated with Konnect ID")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: ks.Name, Namespace: ks.Namespace}, ks)
		require.NoError(t, err)
		assertKonnectEntityProgrammed(t, ks)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

	kr := deploy.KongRouteAttachedToService(t, ctx, clientNamespaced, ks,
		deploy.WithTestIDLabel(testID),
		func(obj client.Object) {
			kr := obj.(*configurationv1alpha1.KongRoute)
			kr.Spec.KongRouteAPISpec.Paths = []string{"/kr-" + testID}
			kr.Spec.Headers = map[string][]string{
				"KongTestHeader": {"example.com", "example.org"},
			}
		},
	)
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, kr.DeepCopy()))

	t.Logf("Waiting for KongRoute to be updated with Konnect ID")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: kr.Name, Namespace: kr.Namespace}, kr)
		require.NoError(t, err)

		assertKonnectEntityProgrammed(t, kr)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

	kcg := deploy.KongConsumerGroupAttachedToCP(t, ctx, clientNamespaced,
		deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
		deploy.WithTestIDLabel(testID),
	)

	t.Logf("Waiting for KongConsumerGroup to be updated with Konnect ID")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: kcg.Name, Namespace: ns.Name}, kcg)
		require.NoError(t, err)

		assertKonnectEntityProgrammed(t, kcg)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

	kc := deploy.KongConsumer(t, ctx, clientNamespaced, "kc-"+testID,
		deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
		deploy.WithTestIDLabel(testID),
		func(obj client.Object) {
			kc := obj.(*configurationv1.KongConsumer)
			kc.ConsumerGroups = []string{kcg.Name}
			kc.Spec.ControlPlaneRef = &configurationv1alpha1.ControlPlaneRef{
				Type:                 configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{Name: cp.Name},
			}
		},
	)

	t.Logf("Waiting for KongConsumer to be updated with Konnect ID and Programmed")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: kc.Name, Namespace: ns.Name}, kc)
		require.NoError(t, err)

		assertKonnectEntityProgrammed(t, kc)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

	kp := deploy.ProxyCachePlugin(t, ctx, clientNamespaced)
	kpb := deploy.KongPluginBinding(t, ctx, clientNamespaced,
		konnect.NewKongPluginBindingBuilder().
			WithServiceTarget(ks.Name).
			WithPluginRef(kp.Name).
			WithControlPlaneRefKonnectNamespaced(cp.Name).
			Build(),
		deploy.WithTestIDLabel(testID),
	)

	t.Logf("Waiting for KongPluginBinding to be updated with Konnect ID")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: kpb.Name, Namespace: ns.Name}, kpb)
		require.NoError(t, err)

		assertKonnectEntityProgrammed(t, kpb)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

	globalKPB := deploy.KongPluginBinding(t, ctx, clientNamespaced,
		konnect.NewKongPluginBindingBuilder().
			WithPluginRef(kp.Name).
			WithControlPlaneRefKonnectNamespaced(cp.Name).
			WithScope(configurationv1alpha1.KongPluginBindingScopeGlobalInControlPlane).
			Build(),
		deploy.WithTestIDLabel(testID),
	)

	t.Logf("Waiting for KongPluginBinding to be updated with Konnect ID")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: globalKPB.Name, Namespace: globalKPB.Namespace}, globalKPB)
		require.NoError(t, err)
		assertKonnectEntityProgrammed(t, globalKPB)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

	kup := deploy.KongUpstream(t, ctx, clientNamespaced,
		deploy.WithKonnectNamespacedRefControlPlaneRef(cp),
		deploy.WithTestIDLabel(testID),
		func(obj client.Object) {
			kup := obj.(*configurationv1alpha1.KongUpstream)
			kup.Spec.KongUpstreamAPISpec.Name = ks.Spec.Host
			kup.Spec.KongUpstreamAPISpec.Slots = lo.ToPtr(int64(16384))
			kup.Spec.KongUpstreamAPISpec.Algorithm = sdkkonnectcomp.UpstreamAlgorithmConsistentHashing.ToPointer()
		},
	)

	t.Log("Waiting for KongUpstream to be updated with Konnect ID")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: kup.Name, Namespace: ns.Name}, kup)
		require.NoError(t, err)

		assertKonnectEntityProgrammed(t, kup)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

	kt := deploy.KongTargetAttachedToUpstream(t, ctx, clientNamespaced, kup,
		deploy.WithTestIDLabel(testID),
		func(obj client.Object) {
			kt := obj.(*configurationv1alpha1.KongTarget)
			kt.Spec.KongTargetAPISpec.Target = "example.com"
			kt.Spec.KongTargetAPISpec.Weight = 100
		},
	)

	t.Log("Waiting for KongTarget to be updated with Konnect ID")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: kt.Name, Namespace: ns.Name}, kt)
		require.NoError(t, err)

		assertKonnectEntityProgrammed(t, kt)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

	// Should delete KongTarget because it will block deletion of KongUpstream owning it.
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, kt.DeepCopy()))

	kv := deploy.KongVaultAttachedToCP(t, ctx, clientNamespaced, "env", "env-vault", []byte(`{"prefix":"env-vault"}`), cp)
	t.Logf("Waiting for KongVault to be updated with Konnect ID")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: kv.Name}, kv)
		require.NoError(t, err)

		assertKonnectEntityProgrammed(t, kv)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

	kcert := deploy.KongCertificateAttachedToCP(t, ctx, clientNamespaced, cp)

	t.Logf("Waiting for KongCertificate to get Konnect ID")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{
			Name:      kcert.Name,
			Namespace: ns.Name,
		}, kcert)
		require.NoError(t, err)

		assertKonnectEntityProgrammed(t, kcert)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

	ksni := deploy.KongSNIAttachedToCertificate(t, ctx, clientNamespaced, kcert,
		deploy.WithTestIDLabel(testID),
		func(obj client.Object) {
			ksni := obj.(*configurationv1alpha1.KongSNI)
			ksni.Spec.KongSNIAPISpec.Name = "test.kong-sni.example.com"
		},
	)

	t.Logf("Waiting for KongSNI to get Konnect ID")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{
			Name:      ksni.Name,
			Namespace: ns.Name,
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
		err := GetClients().MgrClient.Delete(GetCtx(), obj)
		require.NoError(t, err)

		require.EventuallyWithT(t, func(t *assert.CollectT) {
			err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: obj.GetName(), Namespace: obj.GetNamespace()}, obj)
			assert.True(t, k8serrors.IsNotFound(err))
		}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)
	}
}

// assertKonnectEntityProgrammed asserts that the KonnectEntityProgrammed condition is set to true and the Konnect
// status fields are populated.
func assertKonnectEntityProgrammed(
	t assert.TestingT,
	obj interface {
		GetKonnectStatus() *konnectv1alpha1.KonnectEntityStatus
		GetConditions() []metav1.Condition
	},
) {
	konnectStatus := obj.GetKonnectStatus()
	if !assert.NotNil(t, konnectStatus) {
		return
	}
	assert.NotEmpty(t, konnectStatus.GetKonnectID())
	assert.NotEmpty(t, konnectStatus.GetOrgID())
	assert.NotEmpty(t, konnectStatus.GetServerURL())

	assert.True(t, lo.ContainsBy(obj.GetConditions(), func(condition metav1.Condition) bool {
		return condition.Type == konnectv1alpha1.KonnectEntityProgrammedConditionType &&
			condition.Status == metav1.ConditionTrue
	}))
}
