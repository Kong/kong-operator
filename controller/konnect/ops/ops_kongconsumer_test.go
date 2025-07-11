package ops

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"

	configurationv1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1"
	"github.com/kong/kubernetes-configuration/v2/pkg/metadata"
)

func TestKongConsumerToSDKConsumerInput_Tags(t *testing.T) {
	cg := &configurationv1.KongConsumer{
		TypeMeta: metav1.TypeMeta{
			Kind:       "KongConsumer",
			APIVersion: "configuration.konghq.com/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "cg-1",
			Namespace:  "default",
			Generation: 2,
			UID:        k8stypes.UID(uuid.NewString()),
			Annotations: map[string]string{
				metadata.AnnotationKeyTags: "tag1,tag2",
			},
		},
	}
	output := kongConsumerToSDKConsumerInput(cg)
	expectedTags := []string{
		"k8s-generation:2",
		"k8s-kind:KongConsumer",
		"k8s-name:cg-1",
		"k8s-uid:" + string(cg.GetUID()),
		"k8s-version:v1beta1",
		"k8s-group:configuration.konghq.com",
		"k8s-namespace:default",
		"tag1",
		"tag2",
	}
	require.ElementsMatch(t, expectedTags, output.Tags)
}
