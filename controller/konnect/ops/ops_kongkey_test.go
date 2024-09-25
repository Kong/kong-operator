package ops

import (
	"testing"

	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"

	konnectconsts "github.com/kong/gateway-operator/controller/konnect/consts"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

func TestKongKeyToKeyInput(t *testing.T) {
	key := &configurationv1alpha1.KongKey{
		TypeMeta: metav1.TypeMeta{
			Kind:       "KongKey",
			APIVersion: "configuration.konghq.com/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "key-1",
			Namespace:  "default",
			Generation: 2,
			UID:        k8stypes.UID(uuid.NewString()),
			Annotations: map[string]string{
				konnectconsts.AnnotationTags: "tag1,tag2,duplicate",
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
	}
	output := kongKeyToKeyInput(key)
	expectedTags := []string{
		"k8s-generation:2",
		"k8s-kind:KongKey",
		"k8s-name:key-1",
		"k8s-uid:" + string(key.GetUID()),
		"k8s-version:v1alpha1",
		"k8s-group:configuration.konghq.com",
		"k8s-namespace:default",
		"tag1",
		"tag2",
		"tag3",
		"tag4",
		"duplicate",
	}
	require.ElementsMatch(t, expectedTags, output.Tags)
	require.Equal(t, "kid", output.Kid)
	require.Equal(t, "name", *output.Name)
	require.Equal(t, "jwk", *output.Jwk)
	require.Equal(t, "public", *output.Pem.PublicKey)
	require.Equal(t, "private", *output.Pem.PrivateKey)
}
