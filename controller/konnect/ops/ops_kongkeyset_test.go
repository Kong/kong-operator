package ops

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1alpha1"
	"github.com/kong/kubernetes-configuration/v2/pkg/metadata"
)

func TestKongKeySetToKeySetInput(t *testing.T) {
	keySet := &configurationv1alpha1.KongKeySet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "KongKeySet",
			APIVersion: "configuration.konghq.com/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "keySet-1",
			Namespace:  "default",
			Generation: 2,
			UID:        k8stypes.UID(uuid.NewString()),
			Annotations: map[string]string{
				metadata.AnnotationKeyTags: "tag1,tag2,duplicate",
			},
		},
		Spec: configurationv1alpha1.KongKeySetSpec{
			KongKeySetAPISpec: configurationv1alpha1.KongKeySetAPISpec{
				Name: "name",
				Tags: []string{"tag3", "tag4", "duplicate"},
			},
		},
	}
	output := kongKeySetToKeySetInput(keySet)
	expectedTags := []string{
		"k8s-generation:2",
		"k8s-kind:KongKeySet",
		"k8s-name:keySet-1",
		"k8s-uid:" + string(keySet.GetUID()),
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
	require.Equal(t, "name", *output.Name)
}
