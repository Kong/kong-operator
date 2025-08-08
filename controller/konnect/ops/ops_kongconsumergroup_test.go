package ops

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"

	configurationv1beta1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1beta1"
	konnectv1alpha2 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha2"
	"github.com/kong/kubernetes-configuration/v2/pkg/metadata"
)

func TestKongConsumerGroupToSDKConsumerGroupInput_Tags(t *testing.T) {
	cg := &configurationv1beta1.KongConsumerGroup{
		TypeMeta: metav1.TypeMeta{
			Kind:       "KongConsumerGroup",
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
		Status: configurationv1beta1.KongConsumerGroupStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
				ControlPlaneID: uuid.NewString(),
			},
		},
	}
	expectedTags := []string{
		"k8s-kind:KongConsumerGroup",
		"k8s-name:cg-1",
		"k8s-namespace:default",
		"k8s-uid:" + string(cg.GetUID()),
		"k8s-version:v1beta1",
		"k8s-group:configuration.konghq.com",
		"k8s-generation:2",
		"tag1",
		"tag2",
	}
	output := kongConsumerGroupToSDKConsumerGroupInput(cg)
	require.ElementsMatch(t, expectedTags, output.Tags)
}
