package envtest

import (
	"context"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/controller/konnect"
	sdkmocks "github.com/kong/gateway-operator/controller/konnect/ops/sdk/mocks"
	"github.com/kong/gateway-operator/modules/manager/scheme"
	"github.com/kong/gateway-operator/test/helpers/deploy"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

func TestKongCACertificate(t *testing.T) {
	t.Parallel()
	ctx, cancel := Context(t, context.Background())
	defer cancel()
	cfg, ns := Setup(t, ctx, scheme.Get())

	t.Log("Setting up the manager with reconcilers")
	mgr, logs := NewManager(t, ctx, cfg, scheme.Get())
	factory := sdkmocks.NewMockSDKFactory(t)
	sdk := factory.SDK
	StartReconcilers(ctx, t, mgr, logs,
		konnect.NewKonnectEntityReconciler(factory, false, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.KongCACertificate](konnectInfiniteSyncTime),
		),
	)

	t.Log("Setting up clients")
	cl, err := client.NewWithWatch(mgr.GetConfig(), client.Options{
		Scheme: scheme.Get(),
	})
	require.NoError(t, err)
	clientNamespaced := client.NewNamespacedClient(mgr.GetClient(), ns.Name)

	t.Log("Creating KonnectAPIAuthConfiguration and KonnectGatewayControlPlane")
	apiAuth := deploy.KonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced)
	cp := deploy.KonnectGatewayControlPlaneWithID(t, ctx, clientNamespaced, apiAuth)

	t.Log("Setting up SDK expectations on KongCACertificate creation")
	sdk.CACertificatesSDK.EXPECT().CreateCaCertificate(mock.Anything, cp.GetKonnectStatus().GetKonnectID(),
		mock.MatchedBy(func(input sdkkonnectcomp.CACertificateInput) bool {
			return input.Cert == deploy.TestValidCACertPEM
		}),
	).Return(&sdkkonnectops.CreateCaCertificateResponse{
		CACertificate: &sdkkonnectcomp.CACertificate{
			ID: lo.ToPtr("12345"),
		},
	}, nil)

	t.Log("Setting up a watch for KongCACertificate events")
	w := setupWatch[configurationv1alpha1.KongCACertificateList](t, ctx, cl, client.InNamespace(ns.Name))

	t.Log("Creating KongCACertificate")
	createdCert := deploy.KongCACertificateAttachedToCP(t, ctx, clientNamespaced, cp)

	t.Log("Waiting for KongCACertificate to be programmed")
	watchFor(t, ctx, w, watch.Modified, func(c *configurationv1alpha1.KongCACertificate) bool {
		if c.GetName() != createdCert.GetName() {
			return false
		}
		return lo.ContainsBy(c.Status.Conditions, func(condition metav1.Condition) bool {
			return condition.Type == konnectv1alpha1.KonnectEntityProgrammedConditionType &&
				condition.Status == metav1.ConditionTrue
		})
	}, "KongCACertificate's Programmed condition should be true eventually")

	t.Log("Waiting for KongCACertificate to be created in the SDK")
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.True(c, factory.SDK.CACertificatesSDK.AssertExpectations(t))
	}, waitTime, tickTime)

	t.Log("Setting up SDK expectations on KongCACertificate update")
	sdk.CACertificatesSDK.EXPECT().UpsertCaCertificate(mock.Anything, mock.MatchedBy(func(r sdkkonnectops.UpsertCaCertificateRequest) bool {
		return r.CACertificateID == "12345" &&
			lo.Contains(r.CACertificate.Tags, "addedTag")
	})).Return(&sdkkonnectops.UpsertCaCertificateResponse{}, nil)

	t.Log("Patching KongCACertificate")
	certToPatch := createdCert.DeepCopy()
	certToPatch.Spec.Tags = append(certToPatch.Spec.Tags, "addedTag")
	require.NoError(t, clientNamespaced.Patch(ctx, certToPatch, client.MergeFrom(createdCert)))

	t.Log("Waiting for KongCACertificate to be updated in the SDK")
	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.True(c, factory.SDK.CACertificatesSDK.AssertExpectations(t))
	}, waitTime, tickTime)

	t.Log("Setting up SDK expectations on KongCACertificate deletion")
	sdk.CACertificatesSDK.EXPECT().DeleteCaCertificate(mock.Anything, cp.GetKonnectStatus().GetKonnectID(), "12345").
		Return(&sdkkonnectops.DeleteCaCertificateResponse{}, nil)

	t.Log("Deleting KongCACertificate")
	require.NoError(t, cl.Delete(ctx, createdCert))

	t.Log("Waiting for KongCACertificate to be deleted in the SDK")
	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.True(c, factory.SDK.CACertificatesSDK.AssertExpectations(t))
	}, waitTime, tickTime)
}
