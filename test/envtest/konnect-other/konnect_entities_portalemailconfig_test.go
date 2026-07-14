package konnectother

import (
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiwatch "k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/konnect"
	"github.com/kong/kong-operator/v2/controller/konnect/ops"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/envtest"
	"github.com/kong/kong-operator/v2/test/envtest/consts"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
	"github.com/kong/kong-operator/v2/test/helpers/eventually"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

func TestPortalEmailConfig(t *testing.T) {
	t.Parallel()
	ctx, cancel := envtest.Context(t, t.Context())
	defer cancel()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get(), envtest.WithInstallGatewayCRDs(true))

	t.Log("Setting up the manager with reconcilers")
	mgr, logs := envtest.NewManager(t, ctx, cfg, scheme.Get())
	factory := sdkmocks.NewMockSDKFactory(t)
	sdk := factory.SDK
	reconcilers := []envtest.Reconciler{
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[konnectv1alpha1.Portal](consts.KonnectInfiniteSyncTime),
			konnect.WithMetricRecorder[konnectv1alpha1.Portal](&metricsmocks.MockRecorder{}),
		),
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[konnectv1alpha1.PortalEmailConfig](consts.KonnectInfiniteSyncTime),
			konnect.WithMetricRecorder[konnectv1alpha1.PortalEmailConfig](&metricsmocks.MockRecorder{}),
		),
	}
	envtest.StartReconcilers(ctx, t, mgr, logs, reconcilers...)

	t.Log("Setting up clients")
	cl, err := client.NewWithWatch(mgr.GetConfig(), client.Options{
		Scheme: scheme.Get(),
	})
	require.NoError(t, err)
	clientNamespaced := client.NewNamespacedClient(mgr.GetClient(), ns.Name)

	t.Log("Creating KonnectAPIAuthConfiguration")
	apiAuth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced)

	t.Run("should create, update and delete PortalEmailConfig successfully", func(t *testing.T) {
		const (
			portalID           = "portal-12345"
			emailConfigID      = "portal-email-config-12345"
			displayName        = "Developer Portal"
			initialDomainName  = "example.com"
			updatedDomainName  = "developer.example.com"
			initialFromEmail   = "noreply@example.com"
			updatedFromEmail   = "support@example.com"
			fromName           = "Example Developer Portal"
			initialReplyToMail = "support@example.com"
		)

		portalWatch := envtest.SetupWatch[konnectv1alpha1.PortalList](t, ctx, cl, client.InNamespace(ns.Name))
		sdk.PortalsSDK.EXPECT().
			CreatePortal(mock.Anything, mock.MatchedBy(func(req sdkkonnectcomp.CreatePortal) bool {
				return req.DisplayName != nil && *req.DisplayName == displayName &&
					req.Labels != nil &&
					req.Labels[ops.KubernetesUIDLabelKey] != nil && *req.Labels[ops.KubernetesUIDLabelKey] != ""
			})).
			Return(&sdkkonnectops.CreatePortalResponse{
				PortalResponse: &sdkkonnectcomp.PortalResponse{
					ID: portalID,
				},
			}, nil)

		t.Log("Creating Portal")
		portal := deploy.Portal(t, ctx, clientNamespaced, apiAuth, func(obj client.Object) {
			p := obj.(*konnectv1alpha1.Portal)
			p.Spec.APISpec.DisplayName = displayName
		})

		t.Log("Waiting for Portal to be programmed")
		envtest.WatchFor(t, ctx, portalWatch, apiwatch.Modified,
			envtest.AssertsAnd(
				envtest.ObjectMatchesName(portal),
				envtest.ObjectMatchesKonnectID[*konnectv1alpha1.Portal](portalID),
				envtest.ObjectHasConditionProgrammedSetToTrue[*konnectv1alpha1.Portal](),
			),
			"Portal didn't get Programmed status condition or Konnect ID",
		)
		envtest.EventuallyAssertSDKExpectations(t, sdk.PortalsSDK, consts.WaitTime, consts.TickTime)

		emailConfigWatch := envtest.SetupWatch[konnectv1alpha1.PortalEmailConfigList](t, ctx, cl, client.InNamespace(ns.Name))
		emailConfig := testEnvtestPortalEmailConfig(
			ns.Name,
			portal.GetName(),
			initialDomainName,
			initialFromEmail,
			fromName,
			initialReplyToMail,
		)
		expectedCreateRequest, err := emailConfig.Spec.APISpec.ToPostPortalEmailConfig()
		require.NoError(t, err)

		sdk.PortalEmailsSDK.EXPECT().
			CreatePortalEmailConfig(mock.Anything, portalID, *expectedCreateRequest).
			Return(&sdkkonnectops.CreatePortalEmailConfigResponse{
				PortalEmailConfig: &sdkkonnectcomp.PortalEmailConfig{
					ID: emailConfigID,
				},
			}, nil)

		t.Log("Creating PortalEmailConfig")
		require.NoError(t, clientNamespaced.Create(ctx, emailConfig))

		t.Log("Waiting for PortalEmailConfig to be programmed")
		envtest.WatchFor(t, ctx, emailConfigWatch, apiwatch.Modified,
			envtest.AssertsAnd(
				envtest.ObjectMatchesName(emailConfig),
				envtest.ObjectMatchesKonnectID[*konnectv1alpha1.PortalEmailConfig](emailConfigID),
				envtest.ObjectHasConditionProgrammedSetToTrue[*konnectv1alpha1.PortalEmailConfig](),
				func(p *konnectv1alpha1.PortalEmailConfig) bool {
					return p.GetPortalID() == portalID &&
						controllerutil.ContainsFinalizer(p, konnect.KonnectCleanupFinalizer)
				},
			),
			"PortalEmailConfig didn't get Programmed status condition, Portal ID, Konnect ID, or cleanup finalizer",
		)
		envtest.EventuallyAssertSDKExpectations(t, sdk.PortalEmailsSDK, consts.WaitTime, consts.TickTime)

		t.Log("Setting up SDK expectations on PortalEmailConfig update")
		emailConfigToPatch := emailConfig.DeepCopy()
		emailConfigToPatch.Spec.APISpec.DomainName = new(updatedDomainName)
		emailConfigToPatch.Spec.APISpec.FromEmail = new(updatedFromEmail)
		expectedUpdateRequest, err := emailConfigToPatch.Spec.APISpec.ToPatchPortalEmailConfig()
		require.NoError(t, err)

		sdk.PortalEmailsSDK.EXPECT().
			UpdatePortalEmailConfig(mock.Anything, portalID, expectedUpdateRequest).
			Return(&sdkkonnectops.UpdatePortalEmailConfigResponse{}, nil)

		t.Log("Patching PortalEmailConfig")
		require.NoError(t, clientNamespaced.Patch(ctx, emailConfigToPatch, client.MergeFrom(emailConfig)))

		t.Log("Waiting for PortalEmailConfig to be patched")
		envtest.WatchFor(t, ctx, emailConfigWatch, apiwatch.Modified,
			envtest.AssertsAnd(
				envtest.ObjectMatchesName(emailConfig),
				envtest.ObjectMatchesKonnectID[*konnectv1alpha1.PortalEmailConfig](emailConfigID),
				envtest.ObjectHasConditionProgrammedSetToTrue[*konnectv1alpha1.PortalEmailConfig](),
				func(p *konnectv1alpha1.PortalEmailConfig) bool {
					return p.Spec.APISpec.DomainName != nil && *p.Spec.APISpec.DomainName == updatedDomainName &&
						p.Spec.APISpec.FromEmail != nil && *p.Spec.APISpec.FromEmail == updatedFromEmail
				},
			),
			"PortalEmailConfig didn't get patched",
		)
		envtest.EventuallyAssertSDKExpectations(t, sdk.PortalEmailsSDK, consts.WaitTime, consts.TickTime)

		t.Log("Setting up SDK expectations on PortalEmailConfig deletion")
		sdk.PortalEmailsSDK.EXPECT().
			DeletePortalEmailConfig(mock.Anything, portalID).
			Return(&sdkkonnectops.DeletePortalEmailConfigResponse{}, nil)

		t.Log("Deleting PortalEmailConfig")
		require.NoError(t, clientNamespaced.Delete(ctx, emailConfig))
		eventually.WaitForObjectToNotExist(t, ctx, clientNamespaced, emailConfig, consts.WaitTime, consts.TickTime)
		envtest.EventuallyAssertSDKExpectations(t, sdk.PortalEmailsSDK, consts.WaitTime, consts.TickTime)
	})
}

func testEnvtestPortalEmailConfig(
	namespace, portalName, domainName, fromEmail, fromName, replyToEmail string,
) *konnectv1alpha1.PortalEmailConfig {
	return &konnectv1alpha1.PortalEmailConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "portal-email-config",
			Namespace: namespace,
		},
		Spec: konnectv1alpha1.PortalEmailConfigSpec{
			PortalRef: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: portalName,
				},
			},
			APISpec: konnectv1alpha1.PortalEmailConfigAPISpec{
				DomainName:   new(domainName),
				FromEmail:    new(fromEmail),
				FromName:     new(fromName),
				ReplyToEmail: new(replyToEmail),
			},
		},
	}
}
