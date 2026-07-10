package envtest

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
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
	"github.com/kong/kong-operator/v2/test/helpers/eventually"
	"github.com/kong/kong-operator/v2/test/mocks/metricsmocks"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

func TestPortalCustomDomain(t *testing.T) {
	t.Parallel()
	ctx, cancel := Context(t, t.Context())
	defer cancel()
	cfg, ns := Setup(t, ctx, scheme.Get(), WithInstallGatewayCRDs(true))

	t.Log("Setting up the manager with reconcilers")
	mgr, logs := NewManager(t, ctx, cfg, scheme.Get())
	factory := sdkmocks.NewMockSDKFactory(t)
	sdk := factory.SDK
	reconcilers := []Reconciler{
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[konnectv1alpha1.Portal](konnectInfiniteSyncTime),
			konnect.WithMetricRecorder[konnectv1alpha1.Portal](&metricsmocks.MockRecorder{}),
		),
		konnect.NewKonnectEntityReconciler(factory, logging.DevelopmentMode, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[konnectv1alpha1.PortalCustomDomain](konnectInfiniteSyncTime),
			konnect.WithMetricRecorder[konnectv1alpha1.PortalCustomDomain](&metricsmocks.MockRecorder{}),
		),
	}
	StartReconcilers(ctx, t, mgr, logs, reconcilers...)

	t.Log("Setting up clients")
	cl, err := client.NewWithWatch(mgr.GetConfig(), client.Options{
		Scheme: scheme.Get(),
	})
	require.NoError(t, err)
	clientNamespaced := client.NewNamespacedClient(mgr.GetClient(), ns.Name)

	t.Log("Creating KonnectAPIAuthConfiguration")
	apiAuth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced)

	t.Run("should create, update and delete PortalCustomDomain successfully", func(t *testing.T) {
		const (
			portalID        = "portal-12345"
			displayName     = "Developer Portal"
			initialHostname = "developer.example.com"
		)

		portalWatch := SetupWatch[konnectv1alpha1.PortalList](t, ctx, cl, client.InNamespace(ns.Name))
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
		WatchFor(t, ctx, portalWatch, apiwatch.Modified,
			AssertsAnd(
				ObjectMatchesName(portal),
				ObjectMatchesKonnectID[*konnectv1alpha1.Portal](portalID),
				ObjectHasConditionProgrammedSetToTrue[*konnectv1alpha1.Portal](),
			),
			"Portal didn't get Programmed status condition or Konnect ID",
		)
		EventuallyAssertSDKExpectations(t, sdk.PortalsSDK, waitTime, tickTime)

		domainWatch := SetupWatch[konnectv1alpha1.PortalCustomDomainList](t, ctx, cl, client.InNamespace(ns.Name))
		domain := testEnvtestPortalCustomDomain(ns.Name, portal.GetName(), "Enabled", initialHostname)
		expectedCreateRequest, err := domain.Spec.APISpec.ToCreatePortalCustomDomainRequest()
		require.NoError(t, err)

		sdk.PortalCustomDomainsSDK.EXPECT().
			CreatePortalCustomDomain(mock.Anything, portalID, *expectedCreateRequest).
			Return(&sdkkonnectops.CreatePortalCustomDomainResponse{
				PortalCustomDomain: &sdkkonnectcomp.PortalCustomDomain{
					Hostname: initialHostname,
				},
			}, nil)

		t.Log("Creating PortalCustomDomain")
		require.NoError(t, clientNamespaced.Create(ctx, domain))

		t.Log("Waiting for PortalCustomDomain to be programmed")
		WatchFor(t, ctx, domainWatch, apiwatch.Modified,
			AssertsAnd(
				ObjectMatchesName(domain),
				ObjectHasConditionProgrammedSetToTrue[*konnectv1alpha1.PortalCustomDomain](),
				func(p *konnectv1alpha1.PortalCustomDomain) bool {
					return p.GetPortalID() == portalID &&
						p.GetKonnectID() == "" &&
						controllerutil.ContainsFinalizer(p, konnect.KonnectCleanupFinalizer)
				},
			),
			"PortalCustomDomain didn't get Programmed status condition, Portal ID, or cleanup finalizer",
		)
		EventuallyAssertSDKExpectations(t, sdk.PortalCustomDomainsSDK, waitTime, tickTime)

		t.Log("Setting up SDK expectations on PortalCustomDomain update")
		domainToPatch := domain.DeepCopy()
		domainToPatch.Spec.APISpec.Enabled = "Disabled"
		expectedUpdateRequest, err := domainToPatch.Spec.APISpec.ToUpdatePortalCustomDomainRequest()
		require.NoError(t, err)

		sdk.PortalCustomDomainsSDK.EXPECT().
			UpdatePortalCustomDomain(mock.Anything, portalID, *expectedUpdateRequest).
			Return(&sdkkonnectops.UpdatePortalCustomDomainResponse{}, nil)

		t.Log("Patching PortalCustomDomain")
		require.NoError(t, clientNamespaced.Patch(ctx, domainToPatch, client.MergeFrom(domain)))

		t.Log("Waiting for PortalCustomDomain to be patched")
		WatchFor(t, ctx, domainWatch, apiwatch.Modified,
			AssertsAnd(
				ObjectMatchesName(domain),
				ObjectHasConditionProgrammedSetToTrue[*konnectv1alpha1.PortalCustomDomain](),
				func(p *konnectv1alpha1.PortalCustomDomain) bool {
					return p.Spec.APISpec.Enabled == "Disabled" &&
						p.GetPortalID() == portalID &&
						p.GetKonnectID() == ""
				},
			),
			"PortalCustomDomain didn't get patched",
		)
		EventuallyAssertSDKExpectations(t, sdk.PortalCustomDomainsSDK, waitTime, tickTime)

		t.Log("Setting up SDK expectations on PortalCustomDomain deletion")
		sdk.PortalCustomDomainsSDK.EXPECT().
			DeletePortalCustomDomain(mock.Anything, portalID).
			Return(&sdkkonnectops.DeletePortalCustomDomainResponse{}, nil)

		t.Log("Deleting PortalCustomDomain")
		require.NoError(t, clientNamespaced.Delete(ctx, domain))
		eventually.WaitForObjectToNotExist(t, ctx, clientNamespaced, domain, waitTime, tickTime)
		EventuallyAssertSDKExpectations(t, sdk.PortalCustomDomainsSDK, waitTime, tickTime)
	})
}

func testEnvtestPortalCustomDomain(
	namespace, portalName, enabled, hostname string,
) *konnectv1alpha1.PortalCustomDomain {
	return &konnectv1alpha1.PortalCustomDomain{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "portal-custom-domain",
			Namespace: namespace,
		},
		Spec: konnectv1alpha1.PortalCustomDomainSpec{
			PortalRef: commonv1alpha1.ObjectRef{
				Type: commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{
					Name: portalName,
				},
			},
			APISpec: konnectv1alpha1.PortalCustomDomainAPISpec{
				Enabled:  enabled,
				Hostname: hostname,
				SSL: &konnectv1alpha1.PortalCustomDomainSSL{
					Type: konnectv1alpha1.PortalCustomDomainSSLTypeStandard,
					Standard: &konnectv1alpha1.CreatePortalCustomDomainSSLStandard{
						DomainVerificationMethod: "http",
					},
				},
			},
		},
	}
}
