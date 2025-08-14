package envtest

import (
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	apiwatch "k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha1"

	"github.com/kong/kong-operator/controller/konnect"
	"github.com/kong/kong-operator/modules/manager/logging"
	"github.com/kong/kong-operator/modules/manager/scheme"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
	"github.com/kong/kong-operator/test/helpers/deploy"
	"github.com/kong/kong-operator/test/mocks/sdkmocks"
)

func TestKonnectAPIAuthConfiguration(t *testing.T) {
	t.Parallel()
	ctx, cancel := Context(t, t.Context())
	defer cancel()
	cfg, ns := Setup(t, ctx, scheme.Get())

	t.Log("Setting up the manager with reconcilers")
	mgr, logs := NewManager(t, ctx, cfg, scheme.Get())
	factory := sdkmocks.NewMockSDKFactory(t)
	sdk := factory.SDK
	StartReconcilers(ctx, t, mgr, logs,
		konnect.NewKonnectAPIAuthConfigurationReconciler(factory, logging.DevelopmentMode, mgr.GetClient()),
	)

	t.Log("Setting up clients")
	cl, err := client.NewWithWatch(mgr.GetConfig(), client.Options{
		Scheme: scheme.Get(),
	})
	require.NoError(t, err)
	clientNamespaced := client.NewNamespacedClient(mgr.GetClient(), ns.Name)

	call := sdk.MeSDK.EXPECT().
		GetOrganizationsMe(mock.Anything, mock.Anything)

	t.Run("gets APIAuthValid=true status condition set on success", func(t *testing.T) {
		call = call.
			Return(
				&sdkkonnectops.GetOrganizationsMeResponse{
					MeOrganization: &sdkkonnectcomp.MeOrganization{
						ID:   lo.ToPtr("12345"),
						Name: lo.ToPtr("org-12345"),
					},
				},
				nil,
			)

		w := setupWatch[konnectv1alpha1.KonnectAPIAuthConfigurationList](t, ctx, cl, client.InNamespace(ns.Name))
		apiAuth := deploy.KonnectAPIAuthConfiguration(t, ctx, clientNamespaced)
		t.Cleanup(func() { assert.NoError(t, cl.Delete(ctx, apiAuth)) })

		t.Log("Waiting for KonnectAPIAuthConfiguration to be APIAuthValid=true")
		watchFor(t, ctx, w, apiwatch.Modified, func(r *konnectv1alpha1.KonnectAPIAuthConfiguration) bool {
			return client.ObjectKeyFromObject(r) == client.ObjectKeyFromObject(apiAuth) &&
				r.Status.OrganizationID == "12345" &&
				k8sutils.HasConditionTrue("APIAuthValid", r)
		}, "KonnectAPIAuthConfiguration didn't get APIAuthValid status condition set to true or didn't get the Org ID set")
	})

	t.Run("gets APIAuthValid=false status condition set on invalid token", func(t *testing.T) {
		call = call.
			Return(
				nil,
				&sdkkonnecterrs.UnauthorizedError{
					Status: 401,
					Title:  "Unauthenticated",
					Detail: "A valid token is required",
				},
			)

		w := setupWatch[konnectv1alpha1.KonnectAPIAuthConfigurationList](t, ctx, cl, client.InNamespace(ns.Name))
		apiAuth := deploy.KonnectAPIAuthConfiguration(t, ctx, clientNamespaced)
		t.Cleanup(func() { assert.NoError(t, cl.Delete(ctx, apiAuth)) })

		t.Log("Waiting for KonnectAPIAuthConfiguration to be APIAuthValid=true")
		watchFor(t, ctx, w, apiwatch.Modified, func(r *konnectv1alpha1.KonnectAPIAuthConfiguration) bool {
			return client.ObjectKeyFromObject(r) == client.ObjectKeyFromObject(apiAuth) &&
				k8sutils.HasConditionFalse("APIAuthValid", r)
		}, "KonnectAPIAuthConfiguration didn't get APIAuthValid status condition set to false")
	})

	t.Run("does not panic when response MeOrganization has no ID", func(t *testing.T) {
		call = call.
			Return(
				&sdkkonnectops.GetOrganizationsMeResponse{
					MeOrganization: &sdkkonnectcomp.MeOrganization{},
				},
				nil,
			)

		w := setupWatch[konnectv1alpha1.KonnectAPIAuthConfigurationList](t, ctx, cl, client.InNamespace(ns.Name))
		apiAuth := deploy.KonnectAPIAuthConfiguration(t, ctx, clientNamespaced)
		t.Cleanup(func() { assert.NoError(t, cl.Delete(ctx, apiAuth)) })

		t.Log("Waiting for KonnectAPIAuthConfiguration to be APIAuthValid=false")
		watchFor(t, ctx, w, apiwatch.Modified, func(r *konnectv1alpha1.KonnectAPIAuthConfiguration) bool {
			return client.ObjectKeyFromObject(r) == client.ObjectKeyFromObject(apiAuth) &&
				k8sutils.HasConditionFalse("APIAuthValid", r)
		}, "KonnectAPIAuthConfiguration didn't get APIAuthValid status condition set to false")
	})

	t.Run("does not panic when response MeOrganization is nil", func(t *testing.T) {
		call = call.
			Return(
				&sdkkonnectops.GetOrganizationsMeResponse{
					MeOrganization: nil,
				},
				nil,
			)

		w := setupWatch[konnectv1alpha1.KonnectAPIAuthConfigurationList](t, ctx, cl, client.InNamespace(ns.Name))
		apiAuth := deploy.KonnectAPIAuthConfiguration(t, ctx, clientNamespaced)
		t.Cleanup(func() { assert.NoError(t, cl.Delete(ctx, apiAuth)) })

		t.Log("Waiting for KonnectAPIAuthConfiguration to be APIAuthValid=false")
		watchFor(t, ctx, w, apiwatch.Modified, func(r *konnectv1alpha1.KonnectAPIAuthConfiguration) bool {
			return client.ObjectKeyFromObject(r) == client.ObjectKeyFromObject(apiAuth) &&
				k8sutils.HasConditionFalse("APIAuthValid", r)
		}, "KonnectAPIAuthConfiguration didn't get APIAuthValid status condition set to false")
	})

	t.Run("does not panic when response is nil", func(t *testing.T) {
		call = call.
			Return(
				nil,
				nil,
			)

		w := setupWatch[konnectv1alpha1.KonnectAPIAuthConfigurationList](t, ctx, cl, client.InNamespace(ns.Name))
		apiAuth := deploy.KonnectAPIAuthConfiguration(t, ctx, clientNamespaced)
		t.Cleanup(func() { assert.NoError(t, cl.Delete(ctx, apiAuth)) })

		t.Log("Waiting for KonnectAPIAuthConfiguration to be APIAuthValid=false")
		watchFor(t, ctx, w, apiwatch.Modified, func(r *konnectv1alpha1.KonnectAPIAuthConfiguration) bool {
			return client.ObjectKeyFromObject(r) == client.ObjectKeyFromObject(apiAuth) &&
				k8sutils.HasConditionFalse("APIAuthValid", r)
		}, "KonnectAPIAuthConfiguration didn't get APIAuthValid status condition set to false")
	})
}
