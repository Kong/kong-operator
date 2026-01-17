package crossnamespace

import (
	"context"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/modules/manager/scheme"
)

func TestIsXNamespaceRefGranted(t *testing.T) {
	ctx := context.Background()
	scheme := scheme.Get()

	testCases := []struct {
		name          string
		grants        []configurationv1alpha1.KongReferenceGrant
		fromNamespace string
		toNamespace   string
		toName        string
		fromGVK       metav1.GroupVersionKind
		toGVK         metav1.GroupVersionKind
		expected      bool
		expectError   bool
	}{
		{
			name: "grant allows reference from KongCertificate to Secret",
			grants: []configurationv1alpha1.KongReferenceGrant{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "allow-cert-to-secret",
						Namespace: "secret-ns",
					},
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     "configuration.konghq.com",
								Kind:      "KongCertificate",
								Namespace: "cert-ns",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "core",
								Kind:  "Secret",
								Name:  lo.ToPtr(configurationv1alpha1.ObjectName("my-secret")),
							},
						},
					},
				},
			},
			fromNamespace: "cert-ns",
			toNamespace:   "secret-ns",
			toName:        "my-secret",
			fromGVK: metav1.GroupVersionKind{
				Group:   "configuration.konghq.com",
				Version: "v1alpha1",
				Kind:    "KongCertificate",
			},
			toGVK: metav1.GroupVersionKind{
				Group:   "core",
				Version: "v1",
				Kind:    "Secret",
			},
			expected: true,
		},
		{
			name: "grant allows reference from KonnectGatewayControlPlane to KonnectAPIAuthConfiguration",
			grants: []configurationv1alpha1.KongReferenceGrant{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "allow-cp-to-auth",
						Namespace: "auth-ns",
					},
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     "konnect.konghq.com",
								Kind:      "KonnectGatewayControlPlane",
								Namespace: "cp-ns",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "konnect.konghq.com",
								Kind:  "KonnectAPIAuthConfiguration",
								Name:  lo.ToPtr(configurationv1alpha1.ObjectName("my-auth")),
							},
						},
					},
				},
			},
			fromNamespace: "cp-ns",
			toNamespace:   "auth-ns",
			toName:        "my-auth",
			fromGVK: metav1.GroupVersionKind{
				Group:   "konnect.konghq.com",
				Version: "v1alpha2",
				Kind:    "KonnectGatewayControlPlane",
			},
			toGVK: metav1.GroupVersionKind{
				Group:   "konnect.konghq.com",
				Version: "v1alpha1",
				Kind:    "KonnectAPIAuthConfiguration",
			},
			expected: true,
		},
		{
			name: "grant allows reference from cluster-scoped KongVault to KonnectGatewayControlPlane",
			grants: []configurationv1alpha1.KongReferenceGrant{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "allow-vault-to-cp",
						Namespace: "cp-ns",
					},
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     "configuration.konghq.com",
								Kind:      "KongVault",
								Namespace: "",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "konnect.konghq.com",
								Kind:  "KonnectGatewayControlPlane",
								Name:  lo.ToPtr(configurationv1alpha1.ObjectName("my-cp")),
							},
						},
					},
				},
			},
			fromNamespace: "",
			toNamespace:   "cp-ns",
			toName:        "my-cp",
			fromGVK: metav1.GroupVersionKind{
				Group:   "configuration.konghq.com",
				Version: "v1alpha1",
				Kind:    "KongVault",
			},
			toGVK: metav1.GroupVersionKind{
				Group:   "konnect.konghq.com",
				Version: "v1alpha2",
				Kind:    "KonnectGatewayControlPlane",
			},
			expected: true,
		},
		{
			name:          "no grant exists - denies reference",
			fromNamespace: "cert-ns",
			toNamespace:   "secret-ns",
			toName:        "my-secret",
			fromGVK: metav1.GroupVersionKind{
				Group:   "configuration.konghq.com",
				Version: "v1alpha1",
				Kind:    "KongCertificate",
			},
			toGVK: metav1.GroupVersionKind{
				Group:   "core",
				Version: "v1",
				Kind:    "Secret",
			},
			expected: false,
		},
		{
			name: "grant exists but from namespace doesn't match",
			grants: []configurationv1alpha1.KongReferenceGrant{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "allow-cert-to-secret",
						Namespace: "secret-ns",
					},
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     "configuration.konghq.com",
								Kind:      "KongCertificate",
								Namespace: "other-cert-ns",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "core",
								Kind:  "Secret",
								Name:  lo.ToPtr(configurationv1alpha1.ObjectName("my-secret")),
							},
						},
					},
				},
			},
			fromNamespace: "cert-ns",
			toNamespace:   "secret-ns",
			toName:        "my-secret",
			fromGVK: metav1.GroupVersionKind{
				Group:   "configuration.konghq.com",
				Version: "v1alpha1",
				Kind:    "KongCertificate",
			},
			toGVK: metav1.GroupVersionKind{
				Group:   "core",
				Version: "v1",
				Kind:    "Secret",
			},
			expected: false,
		},
		{
			name: "grant exists but from kind doesn't match",
			grants: []configurationv1alpha1.KongReferenceGrant{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "allow-cp-to-secret",
						Namespace: "secret-ns",
					},
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     "konnect.konghq.com",
								Kind:      "KonnectGatewayControlPlane",
								Namespace: "cert-ns",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "core",
								Kind:  "Secret",
								Name:  lo.ToPtr(configurationv1alpha1.ObjectName("my-secret")),
							},
						},
					},
				},
			},
			fromNamespace: "cert-ns",
			toNamespace:   "secret-ns",
			toName:        "my-secret",
			fromGVK: metav1.GroupVersionKind{
				Group:   "configuration.konghq.com",
				Version: "v1alpha1",
				Kind:    "KongCertificate",
			},
			toGVK: metav1.GroupVersionKind{
				Group:   "core",
				Version: "v1",
				Kind:    "Secret",
			},
			expected: false,
		},
		{
			name: "grant exists but from group doesn't match",
			grants: []configurationv1alpha1.KongReferenceGrant{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "allow-cp-to-secret",
						Namespace: "secret-ns",
					},
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     "konnect.konghq.com",
								Kind:      "KonnectGatewayControlPlane",
								Namespace: "cert-ns",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "core",
								Kind:  "Secret",
								Name:  lo.ToPtr(configurationv1alpha1.ObjectName("my-secret")),
							},
						},
					},
				},
			},
			fromNamespace: "cert-ns",
			toNamespace:   "secret-ns",
			toName:        "my-secret",
			fromGVK: metav1.GroupVersionKind{
				Group:   "configuration.konghq.com",
				Version: "v1alpha1",
				Kind:    "KonnectGatewayControlPlane",
			},
			toGVK: metav1.GroupVersionKind{
				Group:   "core",
				Version: "v1",
				Kind:    "Secret",
			},
			expected: false,
		},
		{
			name: "grant exists but to name doesn't match",
			grants: []configurationv1alpha1.KongReferenceGrant{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "allow-cert-to-secret",
						Namespace: "secret-ns",
					},
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     "configuration.konghq.com",
								Kind:      "KongCertificate",
								Namespace: "cert-ns",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "core",
								Kind:  "Secret",
								Name:  lo.ToPtr(configurationv1alpha1.ObjectName("other-secret")),
							},
						},
					},
				},
			},
			fromNamespace: "cert-ns",
			toNamespace:   "secret-ns",
			toName:        "my-secret",
			fromGVK: metav1.GroupVersionKind{
				Group:   "configuration.konghq.com",
				Version: "v1alpha1",
				Kind:    "KongCertificate",
			},
			toGVK: metav1.GroupVersionKind{
				Group:   "core",
				Version: "v1",
				Kind:    "Secret",
			},
			expected: false,
		},
		{
			name: "grant exists but to kind doesn't match",
			grants: []configurationv1alpha1.KongReferenceGrant{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "allow-cert-to-auth",
						Namespace: "secret-ns",
					},
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     "configuration.konghq.com",
								Kind:      "KongCertificate",
								Namespace: "cert-ns",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "konnect.konghq.com",
								Kind:  "KonnectAPIAuthConfiguration",
								Name:  lo.ToPtr(configurationv1alpha1.ObjectName("my-secret")),
							},
						},
					},
				},
			},
			fromNamespace: "cert-ns",
			toNamespace:   "secret-ns",
			toName:        "my-secret",
			fromGVK: metav1.GroupVersionKind{
				Group:   "configuration.konghq.com",
				Version: "v1alpha1",
				Kind:    "KongCertificate",
			},
			toGVK: metav1.GroupVersionKind{
				Group:   "core",
				Version: "v1",
				Kind:    "Secret",
			},
			expected: false,
		},
		{
			name: "grant exists but to group doesn't match",
			grants: []configurationv1alpha1.KongReferenceGrant{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "allow-cert-to-auth",
						Namespace: "secret-ns",
					},
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     "configuration.konghq.com",
								Kind:      "KongCertificate",
								Namespace: "cert-ns",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "konnect.konghq.com",
								Kind:  "Secret",
								Name:  lo.ToPtr(configurationv1alpha1.ObjectName("my-secret")),
							},
						},
					},
				},
			},
			fromNamespace: "cert-ns",
			toNamespace:   "secret-ns",
			toName:        "my-secret",
			fromGVK: metav1.GroupVersionKind{
				Group:   "configuration.konghq.com",
				Version: "v1alpha1",
				Kind:    "KongCertificate",
			},
			toGVK: metav1.GroupVersionKind{
				Group:   "core",
				Version: "v1",
				Kind:    "Secret",
			},
			expected: false,
		},
		{
			name: "multiple grants - first one matches",
			grants: []configurationv1alpha1.KongReferenceGrant{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "allow-cert-to-secret",
						Namespace: "secret-ns",
					},
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     "configuration.konghq.com",
								Kind:      "KongCertificate",
								Namespace: "cert-ns",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "core",
								Kind:  "Secret",
								Name:  lo.ToPtr(configurationv1alpha1.ObjectName("my-secret")),
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "allow-other",
						Namespace: "secret-ns",
					},
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     "konnect.konghq.com",
								Kind:      "KonnectGatewayControlPlane",
								Namespace: "other-ns",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "core",
								Kind:  "Secret",
								Name:  lo.ToPtr(configurationv1alpha1.ObjectName("other-secret")),
							},
						},
					},
				},
			},
			fromNamespace: "cert-ns",
			toNamespace:   "secret-ns",
			toName:        "my-secret",
			fromGVK: metav1.GroupVersionKind{
				Group:   "configuration.konghq.com",
				Version: "v1alpha1",
				Kind:    "KongCertificate",
			},
			toGVK: metav1.GroupVersionKind{
				Group:   "core",
				Version: "v1",
				Kind:    "Secret",
			},
			expected: true,
		},
		{
			name: "multiple grants - second one matches",
			grants: []configurationv1alpha1.KongReferenceGrant{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "allow-other",
						Namespace: "secret-ns",
					},
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     "konnect.konghq.com",
								Kind:      "KonnectGatewayControlPlane",
								Namespace: "other-ns",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "core",
								Kind:  "Secret",
								Name:  lo.ToPtr(configurationv1alpha1.ObjectName("other-secret")),
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "allow-cert-to-secret",
						Namespace: "secret-ns",
					},
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     "configuration.konghq.com",
								Kind:      "KongCertificate",
								Namespace: "cert-ns",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "core",
								Kind:  "Secret",
								Name:  lo.ToPtr(configurationv1alpha1.ObjectName("my-secret")),
							},
						},
					},
				},
			},
			fromNamespace: "cert-ns",
			toNamespace:   "secret-ns",
			toName:        "my-secret",
			fromGVK: metav1.GroupVersionKind{
				Group:   "configuration.konghq.com",
				Version: "v1alpha1",
				Kind:    "KongCertificate",
			},
			toGVK: metav1.GroupVersionKind{
				Group:   "core",
				Version: "v1",
				Kind:    "Secret",
			},
			expected: true,
		},
		{
			name: "grant with multiple from entries - matches second entry",
			grants: []configurationv1alpha1.KongReferenceGrant{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "allow-multiple-from",
						Namespace: "secret-ns",
					},
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     "configuration.konghq.com",
								Kind:      "KongCertificate",
								Namespace: "other-ns",
							},
							{
								Group:     "configuration.konghq.com",
								Kind:      "KongCertificate",
								Namespace: "cert-ns",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "core",
								Kind:  "Secret",
								Name:  lo.ToPtr(configurationv1alpha1.ObjectName("my-secret")),
							},
						},
					},
				},
			},
			fromNamespace: "cert-ns",
			toNamespace:   "secret-ns",
			toName:        "my-secret",
			fromGVK: metav1.GroupVersionKind{
				Group:   "configuration.konghq.com",
				Version: "v1alpha1",
				Kind:    "KongCertificate",
			},
			toGVK: metav1.GroupVersionKind{
				Group:   "core",
				Version: "v1",
				Kind:    "Secret",
			},
			expected: true,
		},
		{
			name: "grant with multiple to entries - matches second entry",
			grants: []configurationv1alpha1.KongReferenceGrant{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "allow-multiple-to",
						Namespace: "secret-ns",
					},
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     "configuration.konghq.com",
								Kind:      "KongCertificate",
								Namespace: "cert-ns",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "core",
								Kind:  "Secret",
								Name:  lo.ToPtr(configurationv1alpha1.ObjectName("other-secret")),
							},
							{
								Group: "core",
								Kind:  "Secret",
								Name:  lo.ToPtr(configurationv1alpha1.ObjectName("my-secret")),
							},
						},
					},
				},
			},
			fromNamespace: "cert-ns",
			toNamespace:   "secret-ns",
			toName:        "my-secret",
			fromGVK: metav1.GroupVersionKind{
				Group:   "configuration.konghq.com",
				Version: "v1alpha1",
				Kind:    "KongCertificate",
			},
			toGVK: metav1.GroupVersionKind{
				Group:   "core",
				Version: "v1",
				Kind:    "Secret",
			},
			expected: true,
		},
		{
			name:          "empty grants list - denies reference",
			grants:        []configurationv1alpha1.KongReferenceGrant{},
			fromNamespace: "cert-ns",
			toNamespace:   "secret-ns",
			toName:        "my-secret",
			fromGVK: metav1.GroupVersionKind{
				Group:   "configuration.konghq.com",
				Version: "v1alpha1",
				Kind:    "KongCertificate",
			},
			toGVK: metav1.GroupVersionKind{
				Group:   "core",
				Version: "v1",
				Kind:    "Secret",
			},
			expected: false,
		},
		{
			name: "grant with nil name allows reference to any resource",
			grants: []configurationv1alpha1.KongReferenceGrant{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "allow-cert-to-any-secret",
						Namespace: "secret-ns",
					},
					Spec: configurationv1alpha1.KongReferenceGrantSpec{
						From: []configurationv1alpha1.ReferenceGrantFrom{
							{
								Group:     "configuration.konghq.com",
								Kind:      "KongCertificate",
								Namespace: "cert-ns",
							},
						},
						To: []configurationv1alpha1.ReferenceGrantTo{
							{
								Group: "core",
								Kind:  "Secret",
								Name:  nil,
							},
						},
					},
				},
			},
			fromNamespace: "cert-ns",
			toNamespace:   "secret-ns",
			toName:        "any-secret-name",
			fromGVK: metav1.GroupVersionKind{
				Group:   "configuration.konghq.com",
				Version: "v1alpha1",
				Kind:    "KongCertificate",
			},
			toGVK: metav1.GroupVersionKind{
				Group:   "core",
				Version: "v1",
				Kind:    "Secret",
			},
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var objs []client.Object
			for i := range tc.grants {
				objs = append(objs, &tc.grants[i])
			}

			cl := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objs...).
				Build()

			result, err := isReferenceGranted(
				cl,
				ctx,
				tc.fromNamespace,
				tc.toNamespace,
				tc.toName,
				tc.fromGVK,
				tc.toGVK,
			)

			if tc.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.expected, result)
			}
		})
	}
}
