package integration

import (
	"testing"
	"time"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/controller/konnect/conditions"
	testutils "github.com/kong/gateway-operator/pkg/utils/test"
	"github.com/kong/gateway-operator/test"
	"github.com/kong/gateway-operator/test/helpers"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kubernetes-configuration/api/configuration/v1beta1"
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

	t.Logf("Creating KonnectAPIAuthConfiguration")
	authCfg := &konnectv1alpha1.KonnectAPIAuthConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "auth-" + testID,
			Namespace: ns.Name,
		},
		Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
			Type:      konnectv1alpha1.KonnectAPIAuthTypeToken,
			Token:     test.KonnectAccessToken(),
			ServerURL: test.KonnectServerURL(),
		},
	}
	err := GetClients().MgrClient.Create(GetCtx(), authCfg)
	require.NoError(t, err)

	cpName := "cp-" + testID
	t.Logf("Creating KonnectGatewayControlPlane %s", cpName)
	cp := &konnectv1alpha1.KonnectGatewayControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cpName,
			Namespace: ns.Name,
		},
		Spec: konnectv1alpha1.KonnectGatewayControlPlaneSpec{
			CreateControlPlaneRequest: sdkkonnectcomp.CreateControlPlaneRequest{
				Name:        cpName,
				ClusterType: lo.ToPtr(sdkkonnectcomp.ClusterTypeClusterTypeControlPlane),
				Labels:      map[string]string{"test_id": testID},
			},
			KonnectConfiguration: konnectv1alpha1.KonnectConfiguration{
				APIAuthConfigurationRef: konnectv1alpha1.KonnectAPIAuthConfigurationRef{
					Name: authCfg.Name,
				},
			},
		},
	}
	err = GetClients().MgrClient.Create(GetCtx(), cp)
	require.NoError(t, err)
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, cp))

	t.Logf("Waiting for Konnect ID to be assigned to ControlPlane %s/%s", cp.Namespace, cp.Name)
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: cp.Name, Namespace: cp.Namespace}, cp)
		require.NoError(t, err)
		assertKonnectEntityProgrammed(t, cp.GetConditions(), cp.GetKonnectStatus())
	}, testutils.ObjectUpdateTimeout, time.Second)

	t.Logf("Creating KongService")
	ksName := "ks-" + testID
	ks := &configurationv1alpha1.KongService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ks-" + testID,
			Namespace: ns.Name,
		},
		Spec: configurationv1alpha1.KongServiceSpec{
			ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
				Type:                 configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{Name: cp.Name},
			},
			KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
				Name: lo.ToPtr(ksName),
				URL:  lo.ToPtr("http://example.com"),
				Host: "example.com",
			},
		},
	}
	err = GetClients().MgrClient.Create(GetCtx(), ks)
	require.NoError(t, err)

	t.Logf("Waiting for KongService to be updated with Konnect ID")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: ks.Name, Namespace: ks.Namespace}, ks)
		require.NoError(t, err)
		assertKonnectEntityProgrammed(t, ks.GetConditions(), ks.GetKonnectStatus())
	}, testutils.ObjectUpdateTimeout, time.Second)

	t.Logf("Creating KongRoute")
	krName := "kr-" + testID
	kr := configurationv1alpha1.KongRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      krName,
			Namespace: ns.Name,
		},
		Spec: configurationv1alpha1.KongRouteSpec{
			ServiceRef: &configurationv1alpha1.ServiceRef{
				Type: configurationv1alpha1.ServiceRefNamespacedRef,
				NamespacedRef: &configurationv1alpha1.NamespacedServiceRef{
					Name: ks.Name,
				},
			},
			KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{
				Name:  lo.ToPtr(krName),
				Paths: []string{"/kr-" + testID},
			},
		},
	}
	err = GetClients().MgrClient.Create(GetCtx(), &kr)
	require.NoError(t, err)
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, &kr))

	t.Logf("Waiting for KongRoute to be updated with Konnect ID")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: kr.Name, Namespace: kr.Namespace}, &kr)
		require.NoError(t, err)
		assertKonnectEntityProgrammed(t, kr.GetConditions(), kr.GetKonnectStatus())
	}, testutils.ObjectUpdateTimeout, time.Second)

	t.Logf("Creating KongConsumerGroup")
	kcgName := "kcg-" + testID
	kcg := configurationv1beta1.KongConsumerGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      kcgName,
			Namespace: ns.Name,
		},
		Spec: configurationv1beta1.KongConsumerGroupSpec{
			Name: kcgName,
			ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
				Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
					Name: cp.Name,
				},
			},
		},
	}
	err = GetClients().MgrClient.Create(GetCtx(), &kcg)
	require.NoError(t, err)

	t.Logf("Waiting for KongConsumerGroup to be updated with Konnect ID")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: kcg.Name, Namespace: ns.Name}, &kcg)
		require.NoError(t, err)
		assertKonnectEntityProgrammed(t, kcg.GetConditions(), kcg.GetKonnectStatus())
	}, testutils.ObjectUpdateTimeout, time.Second)

	t.Logf("Creating KongConsumer")
	kcName := "kc-" + testID
	kc := configurationv1.KongConsumer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      kcName,
			Namespace: ns.Name,
		},
		Username: kcName,
		ConsumerGroups: []string{
			kcg.Name,
		},
		Spec: configurationv1.KongConsumerSpec{
			ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
				Type:                 configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{Name: cp.Name},
			},
		},
	}
	require.NoError(t, GetClients().MgrClient.Create(GetCtx(), &kc))

	t.Logf("Waiting for KongConsumer to be updated with Konnect ID and Programmed")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: kc.Name, Namespace: ns.Name}, &kc)
		require.NoError(t, err)
		assertKonnectEntityProgrammed(t, kc.GetConditions(), kc.GetKonnectStatus())
	}, testutils.ObjectUpdateTimeout, time.Second)

	t.Logf("Creating KongPlugin and KongPluginBinding")
	kpName := "kp-" + testID
	kp := configurationv1.KongPlugin{
		ObjectMeta: metav1.ObjectMeta{
			Name:      kpName,
			Namespace: ns.Name,
		},
		PluginName: "key-auth",
	}
	err = GetClients().MgrClient.Create(GetCtx(), &kp)
	require.NoError(t, err)

	kpbName := "kpb-" + testID
	kpb := configurationv1alpha1.KongPluginBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      kpbName,
			Namespace: ns.Name,
		},
		Spec: configurationv1alpha1.KongPluginBindingSpec{
			PluginReference: configurationv1alpha1.PluginRef{
				Name: kp.Name,
				Kind: lo.ToPtr("KongPlugin"),
			},
			Targets: configurationv1alpha1.KongPluginBindingTargets{
				ServiceReference: &configurationv1alpha1.TargetRefWithGroupKind{
					Name:  ks.Name,
					Kind:  "KongService",
					Group: "configuration.konghq.com",
				},
			},
			ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
				Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
					Name: cp.Name,
				},
			},
		},
	}
	err = GetClients().MgrClient.Create(GetCtx(), &kpb)
	require.NoError(t, err)

	t.Logf("Waiting for KongPluginBinding to be updated with Konnect ID")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: kpb.Name, Namespace: ns.Name}, &kpb)
		require.NoError(t, err)
		assertKonnectEntityProgrammed(t, kpb.GetConditions(), kpb.GetKonnectStatus())
	}, testutils.ObjectUpdateTimeout, time.Second)

	t.Log("Creating KongUpstream")
	kupName := "kup-" + testID
	kup := &configurationv1alpha1.KongUpstream{
		ObjectMeta: metav1.ObjectMeta{
			Name:      kupName,
			Namespace: ns.Name,
		},
		Spec: configurationv1alpha1.KongUpstreamSpec{
			ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
				Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
					Name: cp.Name,
				},
			},
			KongUpstreamAPISpec: configurationv1alpha1.KongUpstreamAPISpec{
				Name:      ks.Spec.Host,
				Slots:     lo.ToPtr(int64(16384)),
				Algorithm: sdkkonnectcomp.UpstreamAlgorithmConsistentHashing.ToPointer(),
			},
		},
	}
	err = GetClients().MgrClient.Create(GetCtx(), kup)
	require.NoError(t, err)

	t.Log("Waiting for KongUpstream to be updated with Konnect ID")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: kup.Name, Namespace: ns.Name}, kup)
		require.NoError(t, err)

		if !assert.NotNil(t, kup.Status.Konnect) {
			return
		}
		assert.NotEmpty(t, kup.Status.Konnect.KonnectEntityStatus.GetKonnectID())
		assert.NotEmpty(t, kup.Status.Konnect.KonnectEntityStatus.GetOrgID())
		assert.NotEmpty(t, kup.Status.Konnect.KonnectEntityStatus.GetServerURL())
	}, testutils.ObjectUpdateTimeout, time.Second)

	t.Log("Creating KongTarget")
	ktName := "kt-" + testID
	kt := &configurationv1alpha1.KongTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ktName,
			Namespace: ns.Name,
		},
		Spec: configurationv1alpha1.KongTargetSpec{
			UpstreamRef: configurationv1alpha1.TargetRef{
				Name: kupName,
			},
			KongTargetAPISpec: configurationv1alpha1.KongTargetAPISpec{
				Target: "example.com",
				Weight: 100,
			},
		},
	}
	err = GetClients().MgrClient.Create(GetCtx(), kt)
	require.NoError(t, err)

	t.Log("Waiting for KongTarget to be updated with Konnect ID")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: kt.Name, Namespace: ns.Name}, kt)
		require.NoError(t, err)
		if !assert.NotNil(t, kt.Status.Konnect) {
			return
		}
		assert.NotEmpty(t, kt.Status.Konnect.KonnectEntityStatus.GetKonnectID())
		assert.NotEmpty(t, kt.Status.Konnect.KonnectEntityStatus.GetOrgID())
		assert.NotEmpty(t, kt.Status.Konnect.KonnectEntityStatus.GetServerURL())
	}, testutils.ObjectUpdateTimeout, time.Second)

	// Should delete KongTarget because it will block deletion of KongUpstream owning it.
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, kt))

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
		}, testutils.ObjectUpdateTimeout, time.Second)
	}
}

// assertKonnectEntityProgrammed asserts that the KonnectEntityProgrammed condition is set to true and the Konnect
// status fields are populated.
func assertKonnectEntityProgrammed(t assert.TestingT, cs []metav1.Condition, konnectStatus *konnectv1alpha1.KonnectEntityStatus) {
	if !assert.NotNil(t, konnectStatus) {
		return
	}
	assert.NotEmpty(t, konnectStatus.GetKonnectID())
	assert.NotEmpty(t, konnectStatus.GetOrgID())
	assert.NotEmpty(t, konnectStatus.GetServerURL())

	assert.True(t, lo.ContainsBy(cs, func(condition metav1.Condition) bool {
		return condition.Type == conditions.KonnectEntityProgrammedConditionType &&
			condition.Status == metav1.ConditionTrue
	}))
}
