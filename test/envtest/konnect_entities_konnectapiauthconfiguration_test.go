package envtest

import (
	"context"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/controller/konnect"
	sdkmocks "github.com/kong/gateway-operator/controller/konnect/ops/sdk/mocks"
	"github.com/kong/gateway-operator/modules/manager/scheme"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"
	"github.com/kong/gateway-operator/test/helpers/deploy"

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

func TestKonnectAPIAuthConfiguration(t *testing.T) {
	t.Parallel()
	ctx, cancel := Context(t, context.Background())
	defer cancel()
	cfg, ns := Setup(t, ctx, scheme.Get())

	t.Log("Setting up the manager with reconcilers")
	mgr, logs := NewManager(t, ctx, cfg, scheme.Get())
	factory := sdkmocks.NewMockSDKFactory(t)
	sdk := factory.SDK
	StartReconcilers(ctx, t, mgr, logs,
		konnect.NewKonnectAPIAuthConfigurationReconciler(factory, false, mgr.GetClient()),
	)

	t.Log("Setting up clients")
	cl, err := client.NewWithWatch(mgr.GetConfig(), client.Options{
		Scheme: scheme.Get(),
	})
	require.NoError(t, err)
	clientNamespaced := client.NewNamespacedClient(mgr.GetClient(), ns.Name)

	t.Run("gets APIAuthValid=true status condition set on success", func(t *testing.T) {
		call := sdk.MeSDK.EXPECT().
			GetOrganizationsMe(mock.Anything, mock.Anything).
			Return(
				&sdkkonnectops.GetOrganizationsMeResponse{
					MeOrganization: &sdkkonnectcomp.MeOrganization{
						ID:   lo.ToPtr("12345"),
						Name: lo.ToPtr("org-12345"),
					},
				},
				nil,
			)
		t.Cleanup(func() { call.Unset() })

		w := setupWatch[konnectv1alpha1.KonnectAPIAuthConfigurationList](t, ctx, cl, client.InNamespace(ns.Name))
		apiAuth := deploy.KonnectAPIAuthConfiguration(t, ctx, clientNamespaced)
		t.Cleanup(func() { assert.NoError(t, cl.Delete(ctx, apiAuth)) })

		t.Log("Waiting for KonnectAPIAuthConfiguration to be APIAuthValid=true")
		watchFor(t, ctx, w, watch.Modified, func(r *konnectv1alpha1.KonnectAPIAuthConfiguration) bool {
			return client.ObjectKeyFromObject(r) == client.ObjectKeyFromObject(apiAuth) &&
				r.Status.OrganizationID == "12345" &&
				k8sutils.IsConditionTrue("APIAuthValid", r)
		}, "KonnectAPIAuthConfiguration didn't get APIAuthValid status condition set to true or didn't get the Org ID set")
	})

	t.Run("gets APIAuthValid=false status condition set on invalid token", func(t *testing.T) {
		call := sdk.MeSDK.EXPECT().
			GetOrganizationsMe(mock.Anything, mock.Anything).
			Return(
				nil,
				&sdkkonnecterrs.UnauthorizedError{
					Status: 401,
					Title:  "Unauthenticated",
					Detail: "A valid token is required",
				},
			)
		t.Cleanup(func() { call.Unset() })

		w := setupWatch[konnectv1alpha1.KonnectAPIAuthConfigurationList](t, ctx, cl, client.InNamespace(ns.Name))
		apiAuth := deploy.KonnectAPIAuthConfiguration(t, ctx, clientNamespaced)
		t.Cleanup(func() { assert.NoError(t, cl.Delete(ctx, apiAuth)) })

		t.Log("Waiting for KonnectAPIAuthConfiguration to be APIAuthValid=true")
		watchFor(t, ctx, w, watch.Modified, func(r *konnectv1alpha1.KonnectAPIAuthConfiguration) bool {
			return client.ObjectKeyFromObject(r) == client.ObjectKeyFromObject(apiAuth) &&
				k8sutils.IsConditionFalse("APIAuthValid", r)
		}, "KonnectAPIAuthConfiguration didn't get APIAuthValid status condition set to false")
	})

	t.Run("does not panic when response MeOrganization has no ID", func(t *testing.T) {
		call := sdk.MeSDK.EXPECT().
			GetOrganizationsMe(mock.Anything, mock.Anything).
			Return(
				&sdkkonnectops.GetOrganizationsMeResponse{
					MeOrganization: &sdkkonnectcomp.MeOrganization{},
				},
				nil,
			)
		t.Cleanup(func() { call.Unset() })

		w := setupWatch[konnectv1alpha1.KonnectAPIAuthConfigurationList](t, ctx, cl, client.InNamespace(ns.Name))
		apiAuth := deploy.KonnectAPIAuthConfiguration(t, ctx, clientNamespaced)
		t.Cleanup(func() { assert.NoError(t, cl.Delete(ctx, apiAuth)) })

		t.Log("Waiting for KonnectAPIAuthConfiguration to be APIAuthValid=false")
		watchFor(t, ctx, w, watch.Modified, func(r *konnectv1alpha1.KonnectAPIAuthConfiguration) bool {
			return client.ObjectKeyFromObject(r) == client.ObjectKeyFromObject(apiAuth) &&
				k8sutils.IsConditionFalse("APIAuthValid", r)
		}, "KonnectAPIAuthConfiguration didn't get APIAuthValid status condition set to false")
	})

	t.Run("does not panic when response MeOrganization is nil", func(t *testing.T) {
		call := sdk.MeSDK.EXPECT().
			GetOrganizationsMe(mock.Anything, mock.Anything).
			Return(
				&sdkkonnectops.GetOrganizationsMeResponse{
					MeOrganization: nil,
				},
				nil,
			)
		t.Cleanup(func() { call.Unset() })

		w := setupWatch[konnectv1alpha1.KonnectAPIAuthConfigurationList](t, ctx, cl, client.InNamespace(ns.Name))
		apiAuth := deploy.KonnectAPIAuthConfiguration(t, ctx, clientNamespaced)
		t.Cleanup(func() { assert.NoError(t, cl.Delete(ctx, apiAuth)) })

		t.Log("Waiting for KonnectAPIAuthConfiguration to be APIAuthValid=false")
		watchFor(t, ctx, w, watch.Modified, func(r *konnectv1alpha1.KonnectAPIAuthConfiguration) bool {
			return client.ObjectKeyFromObject(r) == client.ObjectKeyFromObject(apiAuth) &&
				k8sutils.IsConditionFalse("APIAuthValid", r)
		}, "KonnectAPIAuthConfiguration didn't get APIAuthValid status condition set to false")
	})

	t.Run("does not panic when response is nil", func(t *testing.T) {
		call := sdk.MeSDK.EXPECT().
			GetOrganizationsMe(mock.Anything, mock.Anything).
			Return(
				nil,
				nil,
			)
		t.Cleanup(func() { call.Unset() })

		w := setupWatch[konnectv1alpha1.KonnectAPIAuthConfigurationList](t, ctx, cl, client.InNamespace(ns.Name))
		apiAuth := deploy.KonnectAPIAuthConfiguration(t, ctx, clientNamespaced)
		t.Cleanup(func() { assert.NoError(t, cl.Delete(ctx, apiAuth)) })

		t.Log("Waiting for KonnectAPIAuthConfiguration to be APIAuthValid=false")
		watchFor(t, ctx, w, watch.Modified, func(r *konnectv1alpha1.KonnectAPIAuthConfiguration) bool {
			return client.ObjectKeyFromObject(r) == client.ObjectKeyFromObject(apiAuth) &&
				k8sutils.IsConditionFalse("APIAuthValid", r)
		}, "KonnectAPIAuthConfiguration didn't get APIAuthValid status condition set to false")
	})
}
