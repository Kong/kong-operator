package konnectother

import (
	"encoding/json"
	"testing"
	"time"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/konnect"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/envtest"
	"github.com/kong/kong-operator/v2/test/envtest/consts"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

type eventGatewayTestPolicy interface {
	client.Object
	GetKonnectID() string
	GetConditions() []metav1.Condition
}

func TestEventGatewayParentPolicyLifecycle(t *testing.T) {
	t.Parallel()
	ctx, cancel := envtest.Context(t, t.Context())
	defer cancel()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get(), envtest.WithInstallGatewayCRDs(true))
	mgr, logs := envtest.NewManager(t, ctx, cfg, scheme.Get())
	factory := sdkmocks.NewMockSDKFactory(t)
	envtest.StartReconcilers(ctx, t, mgr, logs,
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.EventGatewayVirtualClusterProducePolicy](consts.KonnectInfiniteSyncTime),
			konnect.WithMetricRecorder[configurationv1alpha1.EventGatewayVirtualClusterProducePolicy](&metricsmocks.MockRecorder{}),
		),
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy](consts.KonnectInfiniteSyncTime),
			konnect.WithMetricRecorder[configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy](&metricsmocks.MockRecorder{}),
		),
	)
	cl, err := client.New(cfg, client.Options{Scheme: scheme.Get()})
	require.NoError(t, err)
	nsClient := client.NewNamespacedClient(cl, ns.Name)
	auth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, nsClient)
	gateway := deploy.KonnectEventGateway(t, ctx, nsClient, auth)
	envtest.UpdateKonnectEventGatewayStatusWithProgrammed(t, ctx, nsClient, gateway, "gateway-id")
	backend := deploy.EventGatewayBackendCluster(t, ctx, nsClient, gateway, deploy.WithName("backend"))
	backend.SetKonnectID("backend-id")
	backend.SetGatewayID("gateway-id")
	backend.Status.Conditions = []metav1.Condition{envtest.ProgrammedCondition(backend.Generation)}
	require.NoError(t, cl.Status().Update(ctx, backend))
	vc := deploy.EventGatewayVirtualCluster(t, ctx, nsClient, backend, deploy.WithName("vc"))
	vc.SetKonnectID("vc-id")
	vc.SetGatewayID("gateway-id")
	vc.Status.Conditions = []metav1.Condition{envtest.ProgrammedCondition(vc.Generation)}
	require.NoError(t, cl.Status().Update(ctx, vc))

	for _, side := range []string{"produce", "consume"} {
		t.Run(side, func(t *testing.T) {
			var child, parent eventGatewayTestPolicy
			childName, parentName := side+"-child", side+"-parent"
			if side == "produce" {
				child = &configurationv1alpha1.EventGatewayVirtualClusterProducePolicy{}
				parent = &configurationv1alpha1.EventGatewayVirtualClusterProducePolicy{}
				require.NoError(t, json.Unmarshal([]byte(`{"spec":{"apiSpec":{"type":"encryptFields","parentPolicyRef":{"name":"produce-parent"},"encryptFields":{"config":{"failureMode":"reject","encryptFields":[{"paths":{"type":"array","array":[{"match":"secret"}]},"encryptionKey":{"type":"static","static":{"key":{"name":"test-key"}}}}]}}}}}`), child))
			} else {
				child = &configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy{}
				parent = &configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy{}
				require.NoError(t, json.Unmarshal([]byte(`{"spec":{"apiSpec":{"type":"decryptFields","parentPolicyRef":{"name":"consume-parent"},"decryptFields":{"config":{"failureMode":"error","decryptFields":{"paths":{"type":"array","array":[{"match":"secret"}]}},"keySources":[{"type":"static","static":{}}]}}}}}`), child))
			}
			require.NoError(t, json.Unmarshal([]byte(`{"spec":{"apiSpec":{"type":"schemaValidation","schemaValidation":{"config":{"type":"json","json":{}}}}}}`), parent))
			for _, obj := range []eventGatewayTestPolicy{child, parent} {
				obj.SetNamespace(ns.Name)
				ref := commonv1alpha1.ObjectRef{
					Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
					NamespacedRef: &commonv1alpha1.NamespacedRef{Name: vc.Name},
				}
				if p, ok := obj.(*configurationv1alpha1.EventGatewayVirtualClusterProducePolicy); ok {
					p.Spec.EventGatewayVirtualClusterRef = ref
				} else {
					p := obj.(*configurationv1alpha1.EventGatewayVirtualClusterConsumePolicy)
					p.Spec.EventGatewayVirtualClusterRef = ref
				}
			}
			child.SetName(childName)
			parent.SetName(parentName)
			require.NoError(t, cl.Create(ctx, child))
			waitForPolicyCondition(t, cl, child, "KonnectReferencesResolved", "ReferenceNotFound")
			require.Empty(t, child.GetKonnectID())

			if side == "produce" {
				sdk := factory.SDK.EventGatewayVirtualClusterProducePoliciesSDK
				sdk.EXPECT().CreateEventGatewayVirtualClusterProducePolicy(mock.Anything,
					mock.MatchedBy(func(req sdkkonnectops.CreateEventGatewayVirtualClusterProducePolicyRequest) bool {
						return req.EventGatewayProducePolicyCreate.EventGatewayProduceSchemaValidationPolicy != nil
					})).Return(&sdkkonnectops.CreateEventGatewayVirtualClusterProducePolicyResponse{
					EventGatewayPolicy: &sdkkonnectcomp.EventGatewayPolicy{ID: "produce-parent-id"},
				}, nil).Once()
				sdk.EXPECT().CreateEventGatewayVirtualClusterProducePolicy(mock.Anything,
					mock.MatchedBy(func(req sdkkonnectops.CreateEventGatewayVirtualClusterProducePolicyRequest) bool {
						p := req.EventGatewayProducePolicyCreate.EventGatewayParsedRecordEncryptFieldsPolicyCreate
						return p != nil && p.ParentPolicyID == "produce-parent-id"
					})).Return(&sdkkonnectops.CreateEventGatewayVirtualClusterProducePolicyResponse{
					EventGatewayPolicy: &sdkkonnectcomp.EventGatewayPolicy{ID: "produce-child-id"},
				}, nil).Once()
			} else {
				sdk := factory.SDK.EventGatewayVirtualClusterConsumePoliciesSDK
				sdk.EXPECT().CreateEventGatewayVirtualClusterConsumePolicy(mock.Anything,
					mock.MatchedBy(func(req sdkkonnectops.CreateEventGatewayVirtualClusterConsumePolicyRequest) bool {
						return req.EventGatewayConsumePolicyCreate.EventGatewayConsumeSchemaValidationPolicy != nil
					})).Return(&sdkkonnectops.CreateEventGatewayVirtualClusterConsumePolicyResponse{
					EventGatewayPolicy: &sdkkonnectcomp.EventGatewayPolicy{ID: "consume-parent-id"},
				}, nil).Once()
				sdk.EXPECT().CreateEventGatewayVirtualClusterConsumePolicy(mock.Anything,
					mock.MatchedBy(func(req sdkkonnectops.CreateEventGatewayVirtualClusterConsumePolicyRequest) bool {
						p := req.EventGatewayConsumePolicyCreate.EventGatewayParsedRecordDecryptFieldsPolicyCreate
						return p != nil && p.ParentPolicyID == "consume-parent-id"
					})).Return(&sdkkonnectops.CreateEventGatewayVirtualClusterConsumePolicyResponse{
					EventGatewayPolicy: &sdkkonnectcomp.EventGatewayPolicy{ID: "consume-child-id"},
				}, nil).Once()
			}
			require.NoError(t, cl.Create(ctx, parent))
			waitForPolicyCondition(t, cl, child, "Programmed", "Programmed")
			require.Equal(t, side+"-child-id", child.GetKonnectID())
			require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(parent), parent))
			require.NoError(t, cl.Delete(ctx, parent))
			waitForPolicyCondition(t, cl, parent, "Programmed", "DeletionBlocked")

			// Deletion is idempotent: reconciliation may repeat before the
			// cache observes that the cleanup finalizer has been removed.
			if side == "produce" {
				factory.SDK.EventGatewayVirtualClusterProducePoliciesSDK.EXPECT().
					DeleteEventGatewayVirtualClusterProducePolicy(mock.Anything, mock.MatchedBy(func(req sdkkonnectops.DeleteEventGatewayVirtualClusterProducePolicyRequest) bool {
						return req.PolicyID == "produce-child-id"
					})).Return(&sdkkonnectops.DeleteEventGatewayVirtualClusterProducePolicyResponse{}, nil)
				factory.SDK.EventGatewayVirtualClusterProducePoliciesSDK.EXPECT().
					DeleteEventGatewayVirtualClusterProducePolicy(mock.Anything, mock.MatchedBy(func(req sdkkonnectops.DeleteEventGatewayVirtualClusterProducePolicyRequest) bool {
						return req.PolicyID == "produce-parent-id"
					})).Return(&sdkkonnectops.DeleteEventGatewayVirtualClusterProducePolicyResponse{}, nil)
			} else {
				factory.SDK.EventGatewayVirtualClusterConsumePoliciesSDK.EXPECT().
					DeleteEventGatewayVirtualClusterConsumePolicy(mock.Anything, mock.MatchedBy(func(req sdkkonnectops.DeleteEventGatewayVirtualClusterConsumePolicyRequest) bool {
						return req.PolicyID == "consume-child-id"
					})).Return(&sdkkonnectops.DeleteEventGatewayVirtualClusterConsumePolicyResponse{}, nil)
				factory.SDK.EventGatewayVirtualClusterConsumePoliciesSDK.EXPECT().
					DeleteEventGatewayVirtualClusterConsumePolicy(mock.Anything, mock.MatchedBy(func(req sdkkonnectops.DeleteEventGatewayVirtualClusterConsumePolicyRequest) bool {
						return req.PolicyID == "consume-parent-id"
					})).Return(&sdkkonnectops.DeleteEventGatewayVirtualClusterConsumePolicyResponse{}, nil)
			}
			require.NoError(t, cl.Delete(ctx, child))
			// Shorter than the blocked-deletion retry period: the child watch,
			// not a periodic retry, must unblock the parent.
			require.EventuallyWithT(t, func(ct *assert.CollectT) {
				assert.True(ct, apierrors.IsNotFound(cl.Get(ctx, client.ObjectKeyFromObject(parent), parent)))
			}, 10*time.Second, consts.TickTime)
			require.True(t, apierrors.IsNotFound(cl.Get(ctx, client.ObjectKeyFromObject(child), child)))
		})
	}
}

func waitForPolicyCondition(t *testing.T, cl client.Client, obj eventGatewayTestPolicy, conditionType, reason string) {
	t.Helper()
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		if !assert.NoError(ct, cl.Get(t.Context(), client.ObjectKeyFromObject(obj), obj)) {
			return
		}
		for _, c := range obj.GetConditions() {
			if c.Type == conditionType {
				assert.Equal(ct, reason, c.Reason, "%s", c.Message)
				return
			}
		}
		assert.Fail(ct, "condition missing", "%s", conditionType)
	}, consts.WaitTime, consts.TickTime)
}
