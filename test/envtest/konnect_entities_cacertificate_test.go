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
	"github.com/kong/gateway-operator/controller/konnect/conditions"
	"github.com/kong/gateway-operator/controller/konnect/ops"
	"github.com/kong/gateway-operator/modules/manager/scheme"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

const dummyValidCACertPEM = `-----BEGIN CERTIFICATE-----
MIIDPTCCAiWgAwIBAgIUcNKAk2icWRJGwZ5QDpdSkkeF5kUwDQYJKoZIhvcNAQEL
BQAwLjELMAkGA1UEBhMCVVMxCzAJBgNVBAgMAkNBMRIwEAYDVQQKDAlLb25nIElu
Yy4wHhcNMjQwOTE5MDkwODEzWhcNMjkwOTE4MDkwODEzWjAuMQswCQYDVQQGEwJV
UzELMAkGA1UECAwCQ0ExEjAQBgNVBAoMCUtvbmcgSW5jLjCCASIwDQYJKoZIhvcN
AQEBBQADggEPADCCAQoCggEBAMvDhLM0vTw0QXmgE+sB6gvKx2PUWzvd2tRZoamH
h4RAxYRjgJsJe6WEeAk0tjWQqwAq0Y2MQioMCC4X+L13kpdtomI+4PKjBozg+iTd
ThyV0oQSVHHWzayUzcSODnGR524H9YxmkXV5ImrXwbEqXwiUESPVtjnf/ZzWS01v
gtbu4x3YW+z8kRoXOTpJHKcEoI90SU9F4yeuQsCtbJHeJZRqPr6Kz84ZuHsZ2MeU
os4j1GdMaH3dSysqFv6o1hJ2+6bsrE/ONiGtBb4+tyhivgf+u+ixQwqIERlEJzhI
z/csoAAnfMBY401j2NNUgPpwx5sTQdCz5aFDmanol5152M8CAwEAAaNTMFEwHQYD
VR0OBBYEFK2qd3oRF37acVvgfDeLakx66ioTMB8GA1UdIwQYMBaAFK2qd3oRF37a
cVvgfDeLakx66ioTMA8GA1UdEwEB/wQFMAMBAf8wDQYJKoZIhvcNAQELBQADggEB
AAuul+rAztaueTpPIM63nrS4bSZsIatCgAQ5Pihm0+rZ+13BJk4K2GxkS+T0qkB5
34+F3eVhUB4cC+kVkWZrlEzD9BsJwWjnoJK+848znTg+ufTeaOQWslYNqFKjmy2k
K6NE7E6r+JLdNvafJzeDybSTXI1tCzDRWUdj5m+bgruX07B13KIJKrAweCTD1927
WvvfJYxsg8P7dYD9DPlcuOm22ggAaPPu4P/MsnApiq3kJEI/nSGSsboKyjBO2hcz
VF1CYr6Epfyw/47kwuJLCVHjlTgT4haOChW1S8rZILCLXfb8ukM/g3XVYIeEwzsr
KU74cm8lTFCdxlcXePbMdHc=
-----END CERTIFICATE-----
`

func TestKongCACertificate(t *testing.T) {
	t.Parallel()
	ctx, cancel := Context(t, context.Background())
	defer cancel()
	cfg, ns := Setup(t, ctx, scheme.Get())

	t.Log("Setting up the manager with reconcilers")
	mgr, logs := NewManager(t, ctx, cfg, scheme.Get())
	factory := ops.NewMockSDKFactory(t)
	sdk := factory.SDK
	reconcilers := []Reconciler{
		konnect.NewKonnectEntityReconciler(factory, false, mgr.GetClient(),
			konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.KongCACertificate](konnectInfiniteSyncTime),
		),
	}
	StartReconcilers(ctx, t, mgr, logs, reconcilers...)

	t.Log("Setting up clients")
	cl, err := client.NewWithWatch(mgr.GetConfig(), client.Options{
		Scheme: scheme.Get(),
	})
	require.NoError(t, err)
	clientNamespaced := client.NewNamespacedClient(mgr.GetClient(), ns.Name)

	t.Log("Creating KonnectAPIAuthConfiguration and KonnectGatewayControlPlane")
	apiAuth := deployKonnectAPIAuthConfigurationWithProgrammed(t, ctx, clientNamespaced)
	cp := deployKonnectGatewayControlPlaneWithID(t, ctx, clientNamespaced, apiAuth)

	t.Log("Setting up SDK expectations on KongCACertificate creation")
	sdk.CACertificatesSDK.EXPECT().CreateCaCertificate(mock.Anything, cp.GetKonnectStatus().GetKonnectID(),
		mock.MatchedBy(func(input sdkkonnectcomp.CACertificateInput) bool {
			return input.Cert != nil &&
				*input.Cert == dummyValidCACertPEM
		}),
	).Return(&sdkkonnectops.CreateCaCertificateResponse{
		CACertificate: &sdkkonnectcomp.CACertificate{
			ID: lo.ToPtr("12345"),
		},
	}, nil)

	t.Log("Setting up a watch for KongCACertificate events")
	w := setupWatch[configurationv1alpha1.KongCACertificateList](t, ctx, cl, client.InNamespace(ns.Name))

	t.Log("Creating KongCACertificate")
	createdCert := &configurationv1alpha1.KongCACertificate{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "cert-",
			Namespace:    ns.Name,
		},
		Spec: configurationv1alpha1.KongCACertificateSpec{
			ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
				Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
					Name: cp.GetName(),
				},
			},
			KongCACertificateAPISpec: configurationv1alpha1.KongCACertificateAPISpec{
				Cert: dummyValidCACertPEM,
				Tags: []string{"tag1", "tag2"},
			},
		},
	}
	require.NoError(t, cl.Create(ctx, createdCert))

	t.Log("Waiting for KongCACertificate to be programmed")
	watchFor(t, ctx, w, watch.Modified, func(c *configurationv1alpha1.KongCACertificate) bool {
		if c.GetName() != createdCert.GetName() {
			return false
		}
		return lo.ContainsBy(c.Status.Conditions, func(condition metav1.Condition) bool {
			return condition.Type == conditions.KonnectEntityProgrammedConditionType &&
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
