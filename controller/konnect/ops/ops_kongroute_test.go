package ops

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"

	commonv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/common/v1alpha1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha1"
	"github.com/kong/kubernetes-configuration/v2/pkg/metadata"
)

func TestKongRouteToSDKRouteInput_Tags(t *testing.T) {
	route := &configurationv1alpha1.KongRoute{
		TypeMeta: metav1.TypeMeta{
			Kind:       "KongRoute",
			APIVersion: "configuration.konghq.com/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "route-1",
			Namespace:  "default",
			UID:        k8stypes.UID(uuid.NewString()),
			Generation: 2,
			Annotations: map[string]string{
				metadata.AnnotationKeyTags: "tag1,tag2,duplicate-tag",
			},
		},
		Spec: configurationv1alpha1.KongRouteSpec{
			ServiceRef: &configurationv1alpha1.ServiceRef{
				Type: configurationv1alpha1.ServiceRefNamespacedRef,
				NamespacedRef: &commonv1alpha1.NameRef{
					Name: "service-1",
				},
			},
			KongRouteAPISpec: configurationv1alpha1.KongRouteAPISpec{
				Tags: []string{"tag3", "tag4", "duplicate-tag"},
			},
		},
		Status: configurationv1alpha1.KongRouteStatus{
			Konnect: &konnectv1alpha1.KonnectEntityStatusWithControlPlaneAndServiceRefs{
				ControlPlaneID: "12345",
			},
		},
	}

	output := kongRouteToSDKRouteInput(route)
	expectedTags := []string{
		"k8s-kind:KongRoute",
		"k8s-name:route-1",
		"k8s-namespace:default",
		"k8s-uid:" + string(route.GetUID()),
		"k8s-version:v1alpha1",
		"k8s-group:configuration.konghq.com",
		"k8s-generation:2",
		"tag1",
		"tag2",
		"tag3",
		"tag4",
		"duplicate-tag",
	}
	require.ElementsMatch(t, expectedTags, output.RouteJSON.Tags)
}
