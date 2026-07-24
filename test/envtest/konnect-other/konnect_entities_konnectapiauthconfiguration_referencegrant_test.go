package konnectother

import (
	"context"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiwatch "k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	"github.com/kong/kong-operator/v2/controller/konnect"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/test/envtest"
	"github.com/kong/kong-operator/v2/test/helpers/deploy"
	"github.com/kong/kong-operator/v2/test/mocks/sdkmocks"
)

// TestKonnectAPIAuthConfigurationReferenceGrant verifies that a cross-namespace
// reference from a KonnectAPIAuthConfiguration to a Secret requires a
// KongReferenceGrant in the Secret's namespace.
func TestKonnectAPIAuthConfigurationReferenceGrant(t *testing.T) {
	t.Parallel()
	ctx, cancel := envtest.Context(t, t.Context())
	defer cancel()
	cfg, ns := envtest.Setup(t, ctx, scheme.Get(), envtest.WithInstallGatewayCRDs(true))

	t.Log("Setting up the manager with reconcilers")
	mgr, logs := envtest.NewManager(t, ctx, cfg, scheme.Get())
	factory := sdkmocks.NewMockSDKFactory(t)
	sdk := factory.SDK
	envtest.StartReconcilers(ctx, t, mgr, logs,
		konnect.NewKonnectAPIAuthConfigurationReconciler(controller.Options{}, factory, logging.DevelopmentMode, mgr.GetClient()),
	)

	t.Log("Setting up clients")
	cl, err := client.NewWithWatch(mgr.GetConfig(), client.Options{
		Scheme: scheme.Get(),
	})
	require.NoError(t, err)
	clientNamespaced := client.NewNamespacedClient(mgr.GetClient(), ns.Name)

	// The Konnect API is only reached once the cross-namespace reference is
	// permitted; return a valid organization so APIAuthValid can become True.
	sdk.MeSDK.EXPECT().
		GetOrganizationsMe(mock.Anything, mock.Anything).
		Return(
			&sdkkonnectops.GetOrganizationsMeResponse{
				MeOrganization: &sdkkonnectcomp.MeOrganization{
					ID:   new("12345"),
					Name: new("org-12345"),
				},
			},
			nil,
		)

	t.Log("Creating a Secret holding a Konnect token in a different namespace")
	secretNS := deploy.Namespace(t, ctx, cl)
	secret := deploy.Secret(t, ctx, cl,
		map[string][]byte{
			konnect.SecretTokenKey: []byte("kpat_xxxxxx"),
		},
		func(obj client.Object) {
			obj.SetNamespace(secretNS.Name)
			obj.SetLabels(map[string]string{
				konnect.SecretCredentialLabel: konnect.SecretCredentialLabelValueKonnect,
			})
		},
	)

	w := envtest.SetupWatch[konnectv1alpha1.KonnectAPIAuthConfigurationList](t, ctx, cl, client.InNamespace(ns.Name))

	t.Log("Creating a KonnectAPIAuthConfiguration referencing the Secret cross-namespace (no grant yet)")
	apiAuth := &konnectv1alpha1.KonnectAPIAuthConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "api-auth-xns-",
			Namespace:    ns.Name,
		},
		Spec: konnectv1alpha1.KonnectAPIAuthConfigurationSpec{
			Type: konnectv1alpha1.KonnectAPIAuthTypeSecretRef,
			SecretRef: &corev1.SecretReference{
				Name:      secret.Name,
				Namespace: secretNS.Name,
			},
			ServerURL: sdkmocks.SDKServerURL,
		},
	}
	require.NoError(t, clientNamespaced.Create(ctx, apiAuth))

	t.Log("Waiting for KonnectAPIAuthConfiguration to have ResolvedRefs=False/RefNotPermitted (no grant)")
	envtest.WatchFor(t, ctx, w, apiwatch.Modified, func(a *konnectv1alpha1.KonnectAPIAuthConfiguration) bool {
		if a.GetName() != apiAuth.GetName() {
			return false
		}
		return lo.ContainsBy(a.Status.Conditions, func(condition metav1.Condition) bool {
			return condition.Type == configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs &&
				condition.Status == metav1.ConditionFalse &&
				condition.Reason == configurationv1alpha1.KongReferenceGrantReasonRefNotPermitted
		})
	}, "KonnectAPIAuthConfiguration should have ResolvedRefs=False/RefNotPermitted without a KongReferenceGrant")

	t.Log("Creating a KongReferenceGrant to allow the cross-namespace reference")
	deploy.KongReferenceGrant(t, ctx, cl,
		func(obj client.Object) {
			obj.SetNamespace(secretNS.Name)
		},
		deploy.KongReferenceGrantFroms(configurationv1alpha1.ReferenceGrantFrom{
			Group:     configurationv1alpha1.Group(konnectv1alpha1.GroupVersion.Group),
			Kind:      "KonnectAPIAuthConfiguration",
			Namespace: configurationv1alpha1.Namespace(ns.Name),
		}),
		deploy.KongReferenceGrantTos(configurationv1alpha1.ReferenceGrantTo{
			Group: "core",
			Kind:  "Secret",
			Name:  new(configurationv1alpha1.ObjectName(secret.Name)),
		}),
	)

	t.Log("Waiting for KonnectAPIAuthConfiguration to become ResolvedRefs=True and APIAuthValid=True after the grant")
	envtest.WatchFor(t, ctx, w, apiwatch.Modified, func(a *konnectv1alpha1.KonnectAPIAuthConfiguration) bool {
		if a.GetName() != apiAuth.GetName() {
			return false
		}
		hasResolvedRefs := lo.ContainsBy(a.Status.Conditions, func(condition metav1.Condition) bool {
			return condition.Type == configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs &&
				condition.Status == metav1.ConditionTrue &&
				condition.Reason == configurationv1alpha1.KongReferenceGrantReasonResolvedRefs
		})
		hasValid := lo.ContainsBy(a.Status.Conditions, func(condition metav1.Condition) bool {
			return condition.Type == konnectv1alpha1.KonnectEntityAPIAuthConfigurationValidConditionType &&
				condition.Status == metav1.ConditionTrue
		})
		return hasResolvedRefs && hasValid
	}, "KonnectAPIAuthConfiguration should have ResolvedRefs=True and APIAuthValid=True after the KongReferenceGrant is created")

	// Use a fresh context for cleanup: t.Context() is canceled before top-level
	// cleanup functions run.
	t.Cleanup(func() { assert.NoError(t, client.IgnoreNotFound(cl.Delete(context.Background(), apiAuth))) })
}
