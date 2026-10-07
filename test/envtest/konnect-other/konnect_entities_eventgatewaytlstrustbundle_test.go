package konnectother

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
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

func TestEventGatewayTLSTrustBundle(t *testing.T) {
	t.Parallel()
	ctx, cancel := envtest.Context(t, t.Context())
	defer cancel()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get(), envtest.WithInstallGatewayCRDs(true))

	t.Log("Setting up the manager with reconcilers")
	mgr, logs := envtest.NewManager(t, ctx, cfg, scheme.Get())
	factory := sdkmocks.NewMockSDKFactory(t)
	sdk := factory.SDK
	// A Secret change doesn't bump the trust bundle's generation, so the
	// trusted CA certificates it holds reach Konnect at the next sync: use a
	// short sync period to observe it.
	const syncPeriod = 2 * time.Second
	envtest.StartReconcilers(ctx, t, mgr, logs,
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.EventGatewayTLSTrustBundle](syncPeriod),
			konnect.WithMetricRecorder[configurationv1alpha1.EventGatewayTLSTrustBundle](&metricsmocks.MockRecorder{}),
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

	// Konnect allows deleting a TLS trust bundle used by listener policies,
	// leaving them referencing a missing trust bundle, so the operator keeps
	// the cleanup finalizer and reports DeletionBlocked while Konnect listener
	// policies reference it, and deletes it as soon as none does.
	t.Run("should sync the trusted CA Secret and block deletion while listener policies reference it", func(t *testing.T) {
		const (
			trustBundleID   = "trust-bundle-12345"
			trustBundleKN   = "client-ca"
			listenerID      = "listener-12345"
			policyKonnectID = "listener-policy-12345"
			caPEM           = "ca-pem-data"
			rotatedCAPEM    = "ca-pem-data-rotated"
		)

		w := envtest.SetupWatch[configurationv1alpha1.EventGatewayTLSTrustBundleList](t, ctx, cl, client.InNamespace(ns.Name))

		t.Log("Creating the Secret holding the trusted CA certificates")
		caSecret := &corev1.Secret{
			GenerateName: "client-ca-",
			Labels:       map[string]string{"konghq.com/secret": "true"},
			StringData:   map[string]string{"ca.crt": caPEM},
		}
		require.NoError(t, clientNamespaced.Create(ctx, caSecret))

		t.Log("Setting up SDK expectations on EventGatewayTLSTrustBundle creation")
		sdk.EventGatewayTLSTrustBundlesSDK.EXPECT().
			CreateEventGatewayTLSTrustBundle(mock.Anything, eventGatewayID, mock.MatchedBy(func(req sdkkonnectcomp.CreateTLSTrustBundleRequest) bool {
				return req.Name == trustBundleKN && req.Config.TrustedCa == caPEM
			})).
			Return(&sdkkonnectops.CreateEventGatewayTLSTrustBundleResponse{
				TLSTrustBundle: &sdkkonnectcomp.TLSTrustBundle{ID: trustBundleID},
			}, nil)
		// Periodic syncs (and a stale cache right after creation) send the
		// unchanged spec.
		sdk.EventGatewayTLSTrustBundlesSDK.EXPECT().
			UpdateEventGatewayTLSTrustBundle(mock.Anything, mock.MatchedBy(func(req sdkkonnectops.UpdateEventGatewayTLSTrustBundleRequest) bool {
				return req.UpdateTLSTrustBundleRequest.Config != nil && req.UpdateTLSTrustBundleRequest.Config.TrustedCa == caPEM
			})).
			Return(&sdkkonnectops.UpdateEventGatewayTLSTrustBundleResponse{}, nil).
			Maybe()

		t.Log("Creating EventGatewayTLSTrustBundle")
		trustBundle := &configurationv1alpha1.EventGatewayTLSTrustBundle{
			GenerateName: "trust-bundle-",
			Spec: configurationv1alpha1.EventGatewayTLSTrustBundleSpec{
				GatewayRef: commonv1alpha1.ObjectRef{
					Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
					NamespacedRef: &commonv1alpha1.NamespacedRef{Name: gateway.Name},
				},
				APISpec: configurationv1alpha1.EventGatewayTLSTrustBundleAPISpec{
					Name: trustBundleKN,
					Config: configurationv1alpha1.TLSTrustBundleConfig{
						TrustedCa: configurationv1alpha1.SensitiveDataSource{
							Type: configurationv1alpha1.SensitiveDataSourceTypeSecretRef,
							SecretRef: &configurationv1alpha1.SensitiveDataSecretRef{
								Name: caSecret.Name,
								Key:  "ca.crt",
							},
						},
					},
				},
			},
		}
		require.NoError(t, clientNamespaced.Create(ctx, trustBundle))

		t.Log("Waiting for EventGatewayTLSTrustBundle to be programmed")
		envtest.WatchFor(t, ctx, w, apiwatch.Modified,
			envtest.AssertsAnd(
				envtest.ObjectMatchesName(trustBundle),
				envtest.ObjectMatchesKonnectID[*configurationv1alpha1.EventGatewayTLSTrustBundle](trustBundleID),
				envtest.ObjectHasConditionProgrammedSetToTrue[*configurationv1alpha1.EventGatewayTLSTrustBundle](),
				func(tb *configurationv1alpha1.EventGatewayTLSTrustBundle) bool {
					return controllerutil.ContainsFinalizer(tb, konnect.KonnectCleanupFinalizer)
				},
			),
			"EventGatewayTLSTrustBundle didn't get Programmed status condition, Konnect ID, or cleanup finalizer",
		)

		t.Log("Rotating the trusted CA certificates in the Secret: Konnect gets the new ones at the next sync")
		var rotated atomic.Bool
		sdk.EventGatewayTLSTrustBundlesSDK.EXPECT().
			UpdateEventGatewayTLSTrustBundle(mock.Anything, mock.MatchedBy(func(req sdkkonnectops.UpdateEventGatewayTLSTrustBundleRequest) bool {
				return req.GatewayID == eventGatewayID &&
					req.TLSTrustBundleID == trustBundleID &&
					req.UpdateTLSTrustBundleRequest.Config != nil &&
					req.UpdateTLSTrustBundleRequest.Config.TrustedCa == rotatedCAPEM
			})).
			Run(func(context.Context, sdkkonnectops.UpdateEventGatewayTLSTrustBundleRequest, ...sdkkonnectops.Option) {
				rotated.Store(true)
			}).
			Return(&sdkkonnectops.UpdateEventGatewayTLSTrustBundleResponse{}, nil)
		caSecret.StringData = map[string]string{"ca.crt": rotatedCAPEM}
		require.NoError(t, clientNamespaced.Update(ctx, caSecret))
		require.Eventually(t, rotated.Load, consts.WaitTime, consts.TickTime,
			"the rotated trusted CA certificates were not sent to Konnect")

		t.Log("Setting up SDK expectations on the Konnect listener policies using the trust bundle")
		sdk.EventGatewayTLSTrustBundlesSDK.EXPECT().
			GetEventGatewayTLSTrustBundle(mock.Anything, eventGatewayID, trustBundleID).
			Return(&sdkkonnectops.GetEventGatewayTLSTrustBundleResponse{
				TLSTrustBundle: &sdkkonnectcomp.TLSTrustBundle{ID: trustBundleID, Name: trustBundleKN},
			}, nil).
			Maybe()
		// Whether the Konnect listener policy still references the trust bundle.
		var policyUsesTrustBundle atomic.Bool
		policyUsesTrustBundle.Store(true)
		sdk.EventGatewayListenersSDK.EXPECT().
			ListEventGatewayListeners(mock.Anything, mock.MatchedBy(func(req sdkkonnectops.ListEventGatewayListenersRequest) bool {
				return req.GatewayID == eventGatewayID
			})).
			Return(&sdkkonnectops.ListEventGatewayListenersResponse{
				ListEventGatewayListenersResponse: &sdkkonnectcomp.ListEventGatewayListenersResponse{
					Data: []sdkkonnectcomp.EventGatewayListener{{ID: listenerID, Name: "listener"}},
				},
			}, nil).
			Maybe()
		sdk.EventGatewayListenerPoliciesSDK.EXPECT().
			ListEventGatewayListenerPolicies(mock.Anything, sdkkonnectops.ListEventGatewayListenerPoliciesRequest{
				GatewayID:  eventGatewayID,
				ListenerID: listenerID,
			}).
			RunAndReturn(func(context.Context, sdkkonnectops.ListEventGatewayListenerPoliciesRequest, ...sdkkonnectops.Option) (*sdkkonnectops.ListEventGatewayListenerPoliciesResponse, error) {
				ref := `{"id":"another-trust-bundle-id"}`
				if policyUsesTrustBundle.Load() {
					ref = fmt.Sprintf(`{"id":%q}`, trustBundleID)
				}
				body := fmt.Sprintf(`[{"id":%q,"name":"tls-policy","type":"tls_server","config":{"client_authentication":{"mode":"required","tls_trust_bundles":[%s]}}}]`, policyKonnectID, ref)
				return &sdkkonnectops.ListEventGatewayListenerPoliciesResponse{
					RawResponse: &http.Response{Body: io.NopCloser(strings.NewReader(body))},
				}, nil
			}).
			Maybe()

		t.Log("Creating an EventGatewayListenerPolicy, created in Konnect, referencing the trust bundle")
		policy := &configurationv1alpha1.EventGatewayListenerPolicy{
			GenerateName: "listener-policy-",
			Spec: configurationv1alpha1.EventGatewayListenerPolicySpec{
				EventGatewayListenerRef: commonv1alpha1.ObjectRef{
					Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
					NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "listener"},
				},
				APISpec: configurationv1alpha1.EventGatewayListenerPolicyAPISpec{
					EventGatewayListenerPolicyConfig: &configurationv1alpha1.EventGatewayListenerPolicyConfig{
						Type: configurationv1alpha1.EventGatewayListenerPolicyConfigTypeEventGatewayTLSListen,
						EventGatewayTLSListen: &configurationv1alpha1.EventGatewayTLSListenerPolicy{
							Name: "tls-policy",
							Config: configurationv1alpha1.EventGatewayTLSListenerPolicyConfig{
								Certificates: []configurationv1alpha1.TLSCertificate{{
									Certificate: configurationv1alpha1.SensitiveDataSource{Type: configurationv1alpha1.SensitiveDataSourceTypeInline, Value: new("cert")},
									Key:         configurationv1alpha1.SensitiveDataSource{Type: configurationv1alpha1.SensitiveDataSourceTypeInline, Value: new("key")},
								}},
								ClientAuthentication: configurationv1alpha1.EventGatewayTLSListenerPolicyConfigClientAuthentication{
									Mode: "required",
									TLSTrustBundles: []configurationv1alpha1.TLSTrustBundleReference{{
										NamespacedRef: &configurationv1alpha1.EventGatewayTLSTrustBundleRef{Name: trustBundle.Name},
									}},
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

		t.Log("Deleting EventGatewayTLSTrustBundle while the Konnect policy references it")
		require.NoError(t, clientNamespaced.Delete(ctx, trustBundle))

		t.Log("Waiting for EventGatewayTLSTrustBundle to report the DeletionBlocked condition")
		envtest.WatchFor(t, ctx, w, apiwatch.Modified, func(tb *configurationv1alpha1.EventGatewayTLSTrustBundle) bool {
			if tb.GetName() != trustBundle.GetName() {
				return false
			}
			for _, c := range tb.GetConditions() {
				if c.Type == konnectv1alpha1.KonnectEntityProgrammedConditionType {
					return c.Status == metav1.ConditionFalse &&
						c.Reason == konnectv1alpha1.KonnectEntityProgrammedReasonDeletionBlocked &&
						strings.Contains(c.Message, ns.Name+"/"+policy.Name) &&
						controllerutil.ContainsFinalizer(tb, konnect.KonnectCleanupFinalizer)
				}
			}
			return false
		}, "EventGatewayTLSTrustBundle should get the Programmed condition set to status=False with reason DeletionBlocked naming the policy")

		t.Log("Setting up SDK expectations on EventGatewayTLSTrustBundle deletion")
		sdk.EventGatewayTLSTrustBundlesSDK.EXPECT().
			DeleteEventGatewayTLSTrustBundle(mock.Anything, eventGatewayID, trustBundleID).
			Return(&sdkkonnectops.DeleteEventGatewayTLSTrustBundleResponse{}, nil)

		// The blocked deletion is retried on a fixed one-minute period, longer
		// than the 45s wait below: the deletion must resume through the watch
		// on listener policies. A policy is gone from Konnect before its object
		// is (its finalizer deletes it there first), so by the time the object
		// is deleted, Konnect no longer has it.
		t.Log("Deleting the policy, gone from Konnect: the trust bundle deletion resumes")
		policyUsesTrustBundle.Store(false)
		require.NoError(t, clientNamespaced.Delete(ctx, policy))
		envtest.WatchFor(t, ctx, w, apiwatch.Deleted,
			envtest.ObjectMatchesName(trustBundle),
			"EventGatewayTLSTrustBundle should be deleted once no Konnect listener policy references it",
		)
		envtest.EventuallyAssertSDKExpectations(t, sdk.EventGatewayTLSTrustBundlesSDK, consts.WaitTime, consts.TickTime)
	})
}
