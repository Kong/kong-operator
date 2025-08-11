package ops

import (
	"sort"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1alpha1"
	konnectv1alpha2 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha2"
	"github.com/kong/kubernetes-configuration/v2/pkg/metadata"
)

func TestKongKeyToKeyInput(t *testing.T) {
	testCases := []struct {
		name           string
		key            *configurationv1alpha1.KongKey
		expectedOutput sdkkonnectcomp.Key
	}{
		{
			name: "kong key with all fields set without key set",
			key: &configurationv1alpha1.KongKey{
				TypeMeta: metav1.TypeMeta{
					Kind:       "KongKey",
					APIVersion: "configuration.konghq.com/v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:       "key-1",
					Namespace:  "default",
					Generation: 2,
					UID:        k8stypes.UID("key-uid"),
					Annotations: map[string]string{
						metadata.AnnotationKeyTags: "tag1,tag2,duplicate",
					},
				},
				Spec: configurationv1alpha1.KongKeySpec{
					KongKeyAPISpec: configurationv1alpha1.KongKeyAPISpec{
						KID:  "kid",
						Name: lo.ToPtr("name"),
						JWK:  lo.ToPtr("jwk"),
						PEM: &configurationv1alpha1.PEMKeyPair{
							PublicKey:  "public",
							PrivateKey: "private",
						},
						Tags: []string{"tag3", "tag4", "duplicate"},
					},
				},
			},
			expectedOutput: sdkkonnectcomp.Key{
				Kid:  "kid",
				Name: lo.ToPtr("name"),
				Jwk:  lo.ToPtr("jwk"),
				Pem: &sdkkonnectcomp.Pem{
					PublicKey:  lo.ToPtr("public"),
					PrivateKey: lo.ToPtr("private"),
				},
				Tags: []string{
					"duplicate",
					"k8s-generation:2",
					"k8s-group:configuration.konghq.com",
					"k8s-kind:KongKey",
					"k8s-name:key-1",
					"k8s-namespace:default",
					"k8s-uid:key-uid",
					"k8s-version:v1alpha1",
					"tag1",
					"tag2",
					"tag3",
					"tag4",
				},
			},
		},
		{
			name: "kong key with all fields set with key set",
			key: &configurationv1alpha1.KongKey{
				TypeMeta: metav1.TypeMeta{
					Kind:       "KongKey",
					APIVersion: "configuration.konghq.com/v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:       "key-1",
					Namespace:  "default",
					Generation: 2,
					UID:        k8stypes.UID("key-uid"),
					Annotations: map[string]string{
						metadata.AnnotationKeyTags: "tag1,tag2,duplicate",
					},
				},
				Spec: configurationv1alpha1.KongKeySpec{
					KongKeyAPISpec: configurationv1alpha1.KongKeyAPISpec{
						KID:  "kid",
						Name: lo.ToPtr("name"),
						JWK:  lo.ToPtr("jwk"),
						PEM: &configurationv1alpha1.PEMKeyPair{
							PublicKey:  "public",
							PrivateKey: "private",
						},
						Tags: []string{"tag3", "tag4", "duplicate"},
					},
				},
				Status: configurationv1alpha1.KongKeyStatus{
					Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndKeySetRef{
						KeySetID: "key-set-id",
					},
				},
			},
			expectedOutput: sdkkonnectcomp.Key{
				Kid:  "kid",
				Name: lo.ToPtr("name"),
				Jwk:  lo.ToPtr("jwk"),
				Pem: &sdkkonnectcomp.Pem{
					PublicKey:  lo.ToPtr("public"),
					PrivateKey: lo.ToPtr("private"),
				},
				Set: &sdkkonnectcomp.Set{
					ID: lo.ToPtr("key-set-id"),
				},
				Tags: []string{
					"duplicate",
					"k8s-generation:2",
					"k8s-group:configuration.konghq.com",
					"k8s-kind:KongKey",
					"k8s-name:key-1",
					"k8s-namespace:default",
					"k8s-uid:key-uid",
					"k8s-version:v1alpha1",
					"tag1",
					"tag2",
					"tag3",
					"tag4",
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			output := kongKeyToKeyInput(tc.key)

			// Tags order is not guaranteed, so we need to sort them before comparing.
			sort.Strings(output.Tags)
			require.Equal(t, tc.expectedOutput, output)
		})
	}
}
