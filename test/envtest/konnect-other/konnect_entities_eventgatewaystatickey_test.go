package konnectother

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiwatch "k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/konnect"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/envtest"
	"github.com/kong/kong-operator/v2/test/envtest/consts"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

func TestEventGatewayStaticKey(t *testing.T) {
	t.Parallel()
	ctx, cancel := envtest.Context(t, t.Context())
	defer cancel()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get(), envtest.WithInstallGatewayCRDs(true))

	t.Log("Setting up the manager with reconcilers")
	mgr, logs := envtest.NewManager(t, ctx, cfg, scheme.Get())
	factory := sdkmocks.NewMockSDKFactory(t)
	sdk := factory.SDK
	envtest.StartReconcilers(ctx, t, mgr, logs,
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.EventGatewayStaticKey](consts.KonnectInfiniteSyncTime),
			konnect.WithMetricRecorder[configurationv1alpha1.EventGatewayStaticKey](&metricsmocks.MockRecorder{}),
		),
	)

	t.Log("Setting up clients")
	cl, err := client.NewWithWatch(mgr.GetConfig(), client.Options{
		Scheme: scheme.Get(),
	})
	require.NoError(t, err)
	clientNamespaced := client.NewNamespacedClient(mgr.GetClient(), ns.Name)

	t.Log("Creating KonnectAPIAuthConfiguration and parent KonnectEventGateway")
	apiAuth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced)
	gateway := deploy.KonnectEventGateway(t, ctx, clientNamespaced, apiAuth)

	const eventGatewayID = "event-gateway-12345"
	envtest.UpdateKonnectEventGatewayStatusWithProgrammed(t, ctx, clientNamespaced, gateway, eventGatewayID)

	// Konnect refuses to delete a static key while produce policies use it:
	// the operator keeps the cleanup finalizer, reports DeletionBlocked naming
	// the policies, and retries as soon as one of them changes or is deleted.
	t.Run("should create from a Secret and block deletion while produce policies use it", func(t *testing.T) {
		const (
			staticKeyID       = "static-key-12345"
			staticKeyKN       = "encryption-key"
			keyValue          = "Y2hhaW5zYXctc3RhdGljLWtleS0zMi1ieXRlcy1vayE="
			virtualClusterID  = "virtual-cluster-12345"
			policyKonnectID   = "produce-policy-12345"
			policyKonnectName = "encrypt"
		)

		w := envtest.SetupWatch[configurationv1alpha1.EventGatewayStaticKeyList](t, ctx, cl, client.InNamespace(ns.Name))

		t.Log("Creating the Secret holding the key value")
		keySecret := &corev1.Secret{
			GenerateName: "static-key-",
			Labels:       map[string]string{"konghq.com/secret": "true"},
			StringData:   map[string]string{"key": keyValue},
		}
		require.NoError(t, clientNamespaced.Create(ctx, keySecret))

		t.Log("Setting up SDK expectations on EventGatewayStaticKey creation")
		sdk.EventGatewayStaticKeysSDK.EXPECT().
			CreateEventGatewayStaticKey(mock.Anything, eventGatewayID, mock.MatchedBy(func(req *sdkkonnectcomp.EventGatewayStaticKeyCreate) bool {
				return req != nil && req.Name == staticKeyKN && req.Value == keyValue
			})).
			Return(&sdkkonnectops.CreateEventGatewayStaticKeyResponse{
				EventGatewayStaticKey: &sdkkonnectcomp.EventGatewayStaticKey{ID: staticKeyID},
			}, nil)

		t.Log("Creating EventGatewayStaticKey")
		staticKey := &configurationv1alpha1.EventGatewayStaticKey{
			GenerateName: "static-key-",
			Spec: configurationv1alpha1.EventGatewayStaticKeySpec{
				GatewayRef: commonv1alpha1.ObjectRef{
					Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
					NamespacedRef: &commonv1alpha1.NamespacedRef{Name: gateway.Name},
				},
				APISpec: configurationv1alpha1.EventGatewayStaticKeyAPISpec{
					Name: staticKeyKN,
					Value: configurationv1alpha1.SensitiveDataSource{
						Type:      configurationv1alpha1.SensitiveDataSourceTypeSecretRef,
						SecretRef: &configurationv1alpha1.SensitiveDataSecretRef{Name: keySecret.Name, Key: "key"},
					},
				},
			},
		}
		require.NoError(t, clientNamespaced.Create(ctx, staticKey))

		t.Log("Waiting for EventGatewayStaticKey to be programmed")
		envtest.WatchFor(t, ctx, w, apiwatch.Modified,
			envtest.AssertsAnd(
				envtest.ObjectMatchesName(staticKey),
				envtest.ObjectMatchesKonnectID[*configurationv1alpha1.EventGatewayStaticKey](staticKeyID),
				envtest.ObjectHasConditionProgrammedSetToTrue[*configurationv1alpha1.EventGatewayStaticKey](),
				func(sk *configurationv1alpha1.EventGatewayStaticKey) bool {
					return controllerutil.ContainsFinalizer(sk, konnect.KonnectCleanupFinalizer)
				},
			),
			"EventGatewayStaticKey didn't get Programmed status condition, Konnect ID, or cleanup finalizer",
		)
		envtest.EventuallyAssertSDKExpectations(t, sdk.EventGatewayStaticKeysSDK, consts.WaitTime, consts.TickTime)

		t.Log("Setting up SDK expectations: Konnect refuses the deletion while the produce policy uses the static key")
		// Whether the Konnect produce policy still uses the static key.
		var policyUsesStaticKey atomic.Bool
		policyUsesStaticKey.Store(true)
		sdk.EventGatewayStaticKeysSDK.EXPECT().
			DeleteEventGatewayStaticKey(mock.Anything, eventGatewayID, staticKeyID).
			RunAndReturn(func(context.Context, string, string, ...sdkkonnectops.Option) (*sdkkonnectops.DeleteEventGatewayStaticKeyResponse, error) {
				if policyUsesStaticKey.Load() {
					return nil, &sdkkonnecterrs.BadRequestError{
						Status: http.StatusBadRequest,
						Title:  "Bad Request",
						Detail: "static_key: entity is still in use by at least one policy",
					}
				}
				return &sdkkonnectops.DeleteEventGatewayStaticKeyResponse{}, nil
			})
		sdk.EventGatewayVirtualClustersSDK.EXPECT().
			ListEventGatewayVirtualClusters(mock.Anything, mock.MatchedBy(func(req sdkkonnectops.ListEventGatewayVirtualClustersRequest) bool {
				return req.GatewayID == eventGatewayID
			})).
			Return(&sdkkonnectops.ListEventGatewayVirtualClustersResponse{
				ListVirtualClustersResponse: &sdkkonnectcomp.ListVirtualClustersResponse{
					Data: []sdkkonnectcomp.VirtualCluster{{ID: virtualClusterID, Name: "orders"}},
				},
			}, nil).
			Maybe()
		sdk.EventGatewayVirtualClusterProducePoliciesSDK.EXPECT().
			ListEventGatewayVirtualClusterProducePolicies(mock.Anything, sdkkonnectops.ListEventGatewayVirtualClusterProducePoliciesRequest{
				GatewayID:        eventGatewayID,
				VirtualClusterID: virtualClusterID,
			}).
			RunAndReturn(func(context.Context, sdkkonnectops.ListEventGatewayVirtualClusterProducePoliciesRequest, ...sdkkonnectops.Option) (*sdkkonnectops.ListEventGatewayVirtualClusterProducePoliciesResponse, error) {
				body := fmt.Sprintf(`[{"id":%q,"name":%q,"type":"encrypt","config":{"failure_mode":"error","part_of_record":["value"],"encryption_key":{"type":"static","key":{"id":%q}}}}]`,
					policyKonnectID, policyKonnectName, staticKeyID)
				return &sdkkonnectops.ListEventGatewayVirtualClusterProducePoliciesResponse{
					RawResponse: &http.Response{Body: io.NopCloser(strings.NewReader(body))},
				}, nil
			}).
			Maybe()

		t.Log("Creating an EventGatewayVirtualClusterProducePolicy, created in Konnect, using the static key")
		policy := &configurationv1alpha1.EventGatewayVirtualClusterProducePolicy{
			GenerateName: "encrypt-",
			Spec: configurationv1alpha1.EventGatewayVirtualClusterProducePolicySpec{
				EventGatewayVirtualClusterRef: commonv1alpha1.ObjectRef{
					Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
					NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "virtual-cluster"},
				},
				APISpec: configurationv1alpha1.EventGatewayVirtualClusterProducePolicyAPISpec{
					EventGatewayVirtualClusterProducePolicyConfig: &configurationv1alpha1.EventGatewayVirtualClusterProducePolicyConfig{
						Type: configurationv1alpha1.EventGatewayVirtualClusterProducePolicyConfigTypeEncryptPolicy,
						EncryptPolicy: &configurationv1alpha1.EventGatewayEncryptPolicy{
							Name: policyKonnectName,
							Config: configurationv1alpha1.EventGatewayEncryptConfig{
								FailureMode:  "error",
								PartOfRecord: []configurationv1alpha1.EncryptionRecordPart{"value"},
								EncryptionKey: &configurationv1alpha1.EventGatewayEncryptConfigEncryptionKey{
									Type: configurationv1alpha1.EventGatewayEncryptConfigEncryptionKeyTypeStatic,
									Static: &configurationv1alpha1.EncryptionKeyStatic{
										Key: &configurationv1alpha1.EncryptionKeyStaticReference{
											NamespacedRef: &configurationv1alpha1.EventGatewayStaticKeyRef{Name: staticKey.Name},
										},
									},
								},
							},
						},
					},
				},
			},
		}
		require.NoError(t, clientNamespaced.Create(ctx, policy))
		policy.SetKonnectID(policyKonnectID)
		require.NoError(t, clientNamespaced.Status().Update(ctx, policy))

		t.Log("Deleting EventGatewayStaticKey while the produce policy uses it")
		require.NoError(t, clientNamespaced.Delete(ctx, staticKey))

		t.Log("Waiting for EventGatewayStaticKey to report the DeletionBlocked condition")
		envtest.WatchFor(t, ctx, w, apiwatch.Modified, func(sk *configurationv1alpha1.EventGatewayStaticKey) bool {
			if sk.GetName() != staticKey.GetName() {
				return false
			}
			for _, c := range sk.GetConditions() {
				if c.Type == konnectv1alpha1.KonnectEntityProgrammedConditionType {
					return c.Status == metav1.ConditionFalse &&
						c.Reason == konnectv1alpha1.KonnectEntityProgrammedReasonDeletionBlocked &&
						strings.Contains(c.Message, ns.Name+"/"+policy.Name) &&
						controllerutil.ContainsFinalizer(sk, konnect.KonnectCleanupFinalizer)
				}
			}
			return false
		}, "EventGatewayStaticKey should get the Programmed condition set to status=False with reason DeletionBlocked naming the policy")

		// The blocked deletion is retried on a fixed one-minute period, longer
		// than the 45s wait below: the deletion must resume through the watch
		// on produce policies. A policy is gone from Konnect before its object
		// is (its finalizer deletes it there first).
		t.Log("Deleting the produce policy, gone from Konnect: the static key deletion resumes")
		policyUsesStaticKey.Store(false)
		require.NoError(t, clientNamespaced.Delete(ctx, policy))
		envtest.WatchFor(t, ctx, w, apiwatch.Deleted,
			envtest.ObjectMatchesName(staticKey),
			"EventGatewayStaticKey should be deleted once no produce policy uses it",
		)
	})
}
