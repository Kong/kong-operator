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

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1 "github.com/kong/kong-operator/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/controller/konnect"
	testutils "github.com/kong/kong-operator/pkg/utils/test"
	"github.com/kong/kong-operator/test"
	"github.com/kong/kong-operator/test/helpers"
	"github.com/kong/kong-operator/test/helpers/conditions"
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
		deploy.KonnectGatewayControlPlaneLabel(deploy.KonnectTestIDLabel, testID),
	)

	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, cp.DeepCopy()))

	t.Logf("Waiting for Konnect ID to be assigned to ControlPlane %s/%s", cp.Namespace, cp.Name)
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: cp.Name, Namespace: cp.Namespace}, cp)
		require.NoError(t, err)
		conditions.KonnectEntityIsProgrammed(t, cp)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

	t.Logf("Waiting for ControlPlane and telemetry endpoints to be assigned to ControlPlane %s/%s", cp.Namespace, cp.Name)
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: cp.Name, Namespace: cp.Namespace}, cp)
		require.NoError(t, err)
		require.NotEmpty(t, cp.Status.Endpoints)
		// We will not test the domain name to prevent flakes when the URLs change in Konnect.
		// Example: https://e7b5c7de43.us.cp.konghq.tech
		require.True(t, strings.HasPrefix(cp.Status.Endpoints.ControlPlaneEndpoint, "https://"), "must start with https://")
		// Example: https://e7b5c7de43.us.tp.konghq.tech
		require.True(t, strings.HasPrefix(cp.Status.Endpoints.TelemetryEndpoint, "https://"), "must start with https://")
		// Check if the status.clusterType is set.
		require.Equal(t, sdkkonnectcomp.ControlPlaneClusterTypeClusterTypeControlPlane, cp.Status.ClusterType,
			"status.clusterType must be set to "+sdkkonnectcomp.ControlPlaneClusterTypeClusterTypeControlPlane)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

	// Create KongReferenceGrant to allow KongVault (cluster-scoped) to reference KonnectGatewayControlPlane (namespace-scoped).
	_ = deploy.KongReferenceGrant(t, GetCtx(), clientNamespaced,
		deploy.KongReferenceGrantFroms(configurationv1alpha1.ReferenceGrantFrom{
			Group:     configurationv1alpha1.Group(configurationv1alpha1.GroupVersion.Group),
			Kind:      "KongVault",
			Namespace: configurationv1alpha1.Namespace(""),
		}),
		deploy.KongReferenceGrantTos(configurationv1alpha1.ReferenceGrantTo{
			Group: configurationv1alpha1.Group(konnectv1alpha1.GroupVersion.Group),
			Kind:  "KonnectGatewayControlPlane",
		}),
	)

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
			conditions.KonnectEntityIsProgrammed(t, mirrorCP)
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

		// Check if the status.clusterType is set.
		require.Equal(t, sdkkonnectcomp.ControlPlaneClusterTypeClusterTypeControlPlane, cp.Status.ClusterType,
			"status.clusterType must be set to "+sdkkonnectcomp.ControlPlaneClusterTypeClusterTypeControlPlane)

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
	ks = eventually.KonnectEntityGetsProgrammed(t, ctx, params.client, ks)

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
	eventually.KonnectEntityGetsProgrammed(t, ctx, params.client, kr,
		func(t *assert.CollectT, kr *configurationv1alpha1.KongRoute) {
			require.NotNil(t, kr.Status.Konnect, "Status.Konnect should not be nil")
			require.Equal(t, params.cp.Status.ID, kr.Status.Konnect.ControlPlaneID, "ControlPlaneID should be set")
			require.Empty(t, kr.Status.Konnect.ServiceID, "ServiceID should not be set")
		},
	)

	t.Log("Making KongRoute service bound")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: kr.Name, Namespace: kr.Namespace}, kr)
		require.NoError(t, err)
		kr.Spec.ServiceRef = &configurationv1alpha1.ServiceRef{
			Type: configurationv1alpha1.ServiceRefNamespacedRef,
			NamespacedRef: &commonv1alpha1.NamespacedRef{
				Name: ks.Name,
			},
		}
		err = GetClients().MgrClient.Update(GetCtx(), kr)
		require.NoError(t, err)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)
	eventually.KonnectEntityGetsProgrammed(t, ctx, params.client, kr,
		func(t *assert.CollectT, kr *configurationv1alpha1.KongRoute) {
			require.Equal(t, params.cp.Status.ID, kr.Status.Konnect.ControlPlaneID, "ControlPlaneID should be set")
			require.Equal(t, ks.Status.Konnect.ID, kr.Status.Konnect.ServiceID, "ServiceID should be set")
		},
	)

	t.Log("Making KongRoute serviceless again")
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := GetClients().MgrClient.Get(GetCtx(), types.NamespacedName{Name: kr.Name, Namespace: kr.Namespace}, kr)
		require.NoError(t, err)
		kr.Spec.ServiceRef = nil
		err = GetClients().MgrClient.Update(GetCtx(), kr)
		require.NoError(t, err)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)
	eventually.KonnectEntityGetsProgrammed(t, ctx, params.client, kr,
		func(t *assert.CollectT, kr *configurationv1alpha1.KongRoute) {
			require.Equal(t, params.cp.Status.ID, kr.Status.Konnect.ControlPlaneID, "ControlPlaneID should be set")
			require.Empty(t, kr.Status.Konnect.ServiceID, "ServiceID should not be set")
		},
	)

	kcg := deploy.KongConsumerGroupAttachedToCP(t, ctx, params.client,
		deploy.WithKonnectNamespacedRefControlPlaneRef(params.cp),
		deploy.WithTestIDLabel(params.testID),
	)

	t.Logf("Waiting for KongConsumerGroup to be updated with Konnect ID")
	eventually.KonnectEntityGetsProgrammed(t, ctx, params.client, kcg)

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
	eventually.KonnectEntityGetsProgrammed(t, ctx, params.client, kc)

	kp := deploy.ProxyCachePlugin(t, ctx, params.client)
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, kp.DeepCopy()))

	kpb := deploy.KongPluginBinding(t, ctx, params.client,
		konnect.NewKongPluginBindingBuilder().
			WithServiceTarget(ks.Name).
			WithPluginRefName(kp.Name).
			WithControlPlaneRefKonnectNamespaced(params.cp.Name).
			Build(),
		deploy.WithTestIDLabel(params.testID),
	)
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, kpb.DeepCopy()))

	t.Logf("Waiting for KongPluginBinding to be updated with Konnect ID")
	eventually.KonnectEntityGetsProgrammed(t, ctx, params.client, kpb)

	globalKPB := deploy.KongPluginBinding(t, ctx, params.client,
		konnect.NewKongPluginBindingBuilder().
			WithPluginRefName(kp.Name).
			WithControlPlaneRefKonnectNamespaced(params.cp.Name).
			WithScope(configurationv1alpha1.KongPluginBindingScopeGlobalInControlPlane).
			Build(),
		deploy.WithTestIDLabel(params.testID),
	)
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, globalKPB.DeepCopy()))

	t.Logf("Waiting for KongPluginBinding to be updated with Konnect ID")
	eventually.KonnectEntityGetsProgrammed(t, ctx, params.client, globalKPB)

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
	eventually.KonnectEntityGetsProgrammed(t, ctx, params.client, kup)

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

		conditions.KonnectEntityIsProgrammed(t, kt)
	}, testutils.ObjectUpdateTimeout, testutils.ObjectUpdateTick)

	// Should delete KongTarget because it will block deletion of KongUpstream owning it.
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, kt.DeepCopy()))

	kv := deploy.KongVaultAttachedToCP(t, ctx, params.client, "env", "env-vault", []byte(`{"prefix":"env-vault"}`), params.cp)
	t.Logf("Waiting for KongVault to be updated with Konnect ID")

	eventually.KonnectEntityGetsProgrammed(t, ctx, params.client, kv)
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, kv.DeepCopy()))

	kcert := deploy.KongCertificateAttachedToCP(t, ctx, params.client, params.cp)
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, kcert.DeepCopy()))

	t.Logf("Waiting for KongCertificate to get Konnect ID")
	kcert = eventually.KonnectEntityGetsProgrammed(t, ctx, params.client, kcert)

	ksni := deploy.KongSNIAttachedToCertificate(t, ctx, params.client, kcert,
		deploy.WithTestIDLabel(params.testID),
		func(obj client.Object) {
			ksni := obj.(*configurationv1alpha1.KongSNI)
			ksni.Spec.Name = "test.kong-sni.example.com"
		},
	)
	t.Cleanup(deleteObjectAndWaitForDeletionFn(t, ksni.DeepCopy()))

	t.Logf("Waiting for KongSNI to get Konnect ID")
	eventually.KonnectEntityGetsProgrammed(t, ctx, params.client, ksni,
		func(t *assert.CollectT, obj *configurationv1alpha1.KongSNI) {
			require.NotNil(t, obj.Status.Konnect, "Status.Konnect should not be nil")
			assert.Equal(t, kcert.GetKonnectID(), obj.Status.Konnect.CertificateID, "CertificateID should be set")
		},
	)
}
