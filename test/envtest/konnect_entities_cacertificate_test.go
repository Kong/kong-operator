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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/gateway-operator/controller/konnect/conditions"
	"github.com/kong/gateway-operator/controller/konnect/ops"

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

var kongCACertificateTestCases = []konnectEntityReconcilerTestCase{
	{
		name: "should create a KongCACertificate",
		objectOps: func(ctx context.Context, t *testing.T, cl client.Client, ns *corev1.Namespace) {
			cpName := prepareValidKonnectCP(ctx, t, ns.Name, cl)

			caCert := &configurationv1alpha1.KongCACertificate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cert-1",
					Namespace: ns.Name,
				},
				Spec: configurationv1alpha1.KongCACertificateSpec{
					ControlPlaneRef: &configurationv1alpha1.ControlPlaneRef{
						Type: configurationv1alpha1.ControlPlaneRefKonnectNamespacedRef,
						KonnectNamespacedRef: &configurationv1alpha1.KonnectNamespacedRef{
							Name: cpName,
						},
					},
					KongCACertificateAPISpec: configurationv1alpha1.KongCACertificateAPISpec{
						Cert: dummyValidCACertPEM,
						Tags: []string{"tag1", "tag2"},
					},
				},
			}
			require.NoError(t, cl.Create(ctx, caCert))
		},
		mockExpectations: func(t *testing.T, sdk *ops.MockSDKWrapper, ns *corev1.Namespace) {
			sdk.CACertificatesSDK.EXPECT().CreateCaCertificate(mock.Anything, testCpID,
				mock.MatchedBy(func(input sdkkonnectcomp.CACertificateInput) bool {
					return input.Cert != nil &&
						*input.Cert == dummyValidCACertPEM
				}),
			).Return(&sdkkonnectops.CreateCaCertificateResponse{
				CACertificate: &sdkkonnectcomp.CACertificate{
					ID: lo.ToPtr("12345"),
				},
			}, nil)
		},
		eventuallyPredicate: func(ctx context.Context, t *assert.CollectT, cl client.Client, ns *corev1.Namespace) {
			caCert := &configurationv1alpha1.KongCACertificate{}
			if !assert.NoError(t, cl.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: "cert-1"}, caCert)) {
				return
			}
			assert.Equal(t, "12345", caCert.Status.Konnect.ID)
			assert.True(t,
				lo.ContainsBy(caCert.Status.Conditions, func(condition metav1.Condition) bool {
					return condition.Type == conditions.KonnectEntityProgrammedConditionType &&
						condition.Status == metav1.ConditionTrue
				}),
				"Programmed condition should be set and it status should be true",
			)
		},
	},
}
