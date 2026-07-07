package envtest

import (
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	apiwatch "k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/konnect"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
	"github.com/kong/kong-operator/v2/test/helpers/eventually"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

func TestAIGatewayPolicy(t *testing.T) {
	t.Parallel()
	ctx, cancel := Context(t, t.Context())
	defer cancel()
	cfg, ns := Setup(t, ctx, scheme.Get(), WithInstallGatewayCRDs(true))

	t.Log("Setting up the manager with reconcilers")
	mgr, logs := NewManager(t, ctx, cfg, scheme.Get())
	factory := sdkmocks.NewMockSDKFactory(t)
	sdk := factory.SDK
	StartReconcilers(ctx, t, mgr, logs,
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[konnectv1alpha1.AIGatewayPolicy](konnectInfiniteSyncTime),
			konnect.WithMetricRecorder[konnectv1alpha1.AIGatewayPolicy](&metricsmocks.MockRecorder{}),
		),
	)

	t.Log("Setting up client")
	clientOptions := client.Options{
		Scheme: scheme.Get(),
	}
	cl, err := client.NewWithWatch(mgr.GetConfig(), clientOptions)
	require.NoError(t, err)
	clientNamespaced := client.NewNamespacedClient(mgr.GetClient(), ns.Name)

	t.Log("Creating KonnectAPIAuthConfiguration and AIGatewayControlPlane")
	apiAuth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced)
	cp := deploy.AIGatewayControlPlaneWithID(t, ctx, clientNamespaced, apiAuth)

	t.Run("adding, patching and deleting AIGatewayPolicy", func(t *testing.T) {
		const (
			policyID    = "policy-12345"
			displayName = "Test AI Gateway Policy"
			updatedName = "Updated AI Gateway Policy"
		)

		w := setupWatch[konnectv1alpha1.AIGatewayPolicyList](t, ctx, cl, client.InNamespace(ns.Name))

		t.Log("Setting up SDK expectations on AIGatewayPolicy creation")
		sdk.AIGatewayPoliciesSDK.EXPECT().
			CreateAiGatewayPolicy(
				mock.Anything,
				cp.GetKonnectID(),
				mock.Anything,
			).
			Return(
				&sdkkonnectops.CreateAiGatewayPolicyResponse{
					AIGatewayPolicy: &sdkkonnectcomp.AIGatewayPolicy{
						ID: policyID,
					},
				},
				nil,
			)

		t.Log("Creating an AIGatewayPolicy")
		createdPolicy := deploy.AIGatewayPolicy(t, ctx, clientNamespaced,
			deploy.WithAIGatewayControlPlaneRef(cp),
			func(obj client.Object) {
				p := obj.(*konnectv1alpha1.AIGatewayPolicy)
				p.Spec.APISpec.DisplayName = displayName
			},
		)

		t.Log("Waiting for AIGatewayPolicy to be programmed and get Konnect ID")
		watchFor(t, ctx, w, apiwatch.Modified, func(p *konnectv1alpha1.AIGatewayPolicy) bool {
			return p.GetKonnectID() == policyID && k8sutils.IsProgrammed(p)
		}, "AIGatewayPolicy didn't get Programmed status condition or didn't get the correct (policy-12345) Konnect ID assigned")

		eventuallyAssertSDKExpectations(t, factory.SDK.AIGatewayPoliciesSDK, waitTime, tickTime)

		t.Log("Setting up SDK expectations on AIGatewayPolicy update")
		sdk.AIGatewayPoliciesSDK.EXPECT().
			UpdateAiGatewayPolicy(
				mock.Anything,
				mock.MatchedBy(func(req sdkkonnectops.UpdateAiGatewayPolicyRequest) bool {
					return req.GatewayID == cp.GetKonnectID() && req.PolicyIDOrName == policyID
				}),
			).
			Return(&sdkkonnectops.UpdateAiGatewayPolicyResponse{}, nil)

		t.Log("Patching AIGatewayPolicy")
		policyToPatch := createdPolicy.DeepCopy()
		policyToPatch.Spec.APISpec.DisplayName = updatedName
		require.NoError(t, clientNamespaced.Patch(ctx, policyToPatch, client.MergeFrom(createdPolicy)))

		t.Log("Waiting for AIGatewayPolicy to be patched")
		watchFor(t, ctx, w, apiwatch.Modified,
			assertsAnd(
				objectMatchesName(createdPolicy),
				objectMatchesKonnectID[*konnectv1alpha1.AIGatewayPolicy](policyID),
				objectHasConditionProgrammedSetToTrue[*konnectv1alpha1.AIGatewayPolicy](),
				func(p *konnectv1alpha1.AIGatewayPolicy) bool {
					return p.Spec.APISpec.DisplayName == updatedName
				},
			),
			"AIGatewayPolicy didn't get patched",
		)
		eventuallyAssertSDKExpectations(t, factory.SDK.AIGatewayPoliciesSDK, waitTime, tickTime)

		t.Log("Setting up SDK expectations on AIGatewayPolicy deletion")
		sdk.AIGatewayPoliciesSDK.EXPECT().
			DeleteAiGatewayPolicy(
				mock.Anything,
				cp.GetKonnectID(),
				policyID,
			).
			Return(&sdkkonnectops.DeleteAiGatewayPolicyResponse{}, nil)

		t.Log("Deleting AIGatewayPolicy")
		require.NoError(t, clientNamespaced.Delete(ctx, createdPolicy))
		eventually.WaitForObjectToNotExist(t, ctx, cl, createdPolicy, waitTime, tickTime)

		eventuallyAssertSDKExpectations(t, factory.SDK.AIGatewayPoliciesSDK, waitTime, tickTime)
	})
}
