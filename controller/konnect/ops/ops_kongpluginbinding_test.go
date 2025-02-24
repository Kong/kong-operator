package ops

import (
	"testing"

	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kong/gateway-operator/modules/manager/scheme"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
	"github.com/kong/kubernetes-configuration/pkg/metadata"
)

func TestKongPluginBindingToSDKPluginInput_Tags(t *testing.T) {
	ctx := t.Context()
	pb := &configurationv1alpha1.KongPluginBinding{
		TypeMeta: metav1.TypeMeta{
			Kind:       "KongPluginBinding",
			APIVersion: "configuration.konghq.com/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "plugin-binding-1",
			Namespace:  "default",
			UID:        k8stypes.UID(uuid.NewString()),
			Generation: 2,
			Annotations: map[string]string{
				metadata.AnnotationKeyTags: "tag1,tag2,duplicate-tag",
			},
		},
		Spec: configurationv1alpha1.KongPluginBindingSpec{
			PluginReference: configurationv1alpha1.PluginRef{
				Name: "plugin-1",
				Kind: lo.ToPtr("KongPlugin"),
			},
			Targets: &configurationv1alpha1.KongPluginBindingTargets{
				ServiceReference: &configurationv1alpha1.TargetRefWithGroupKind{
					Name: "service-1",
					Kind: "KongService",
				},
			},
		},
		Status: configurationv1alpha1.KongPluginBindingStatus{
			Konnect: &konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef{
				ControlPlaneID: uuid.NewString(),
			},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(
		&configurationv1.KongPlugin{
			TypeMeta: metav1.TypeMeta{
				Kind:       "KongPlugin",
				APIVersion: "configuration.konghq.com/v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "plugin-1",
				Namespace: "default",
				Annotations: map[string]string{
					metadata.AnnotationKeyTags: "tag3,tag4,duplicate-tag",
				},
			},
			PluginName: "basic-auth",
		},
		&configurationv1alpha1.KongService{
			TypeMeta: metav1.TypeMeta{
				Kind:       "KongService",
				APIVersion: "configuration.konghq.com/v1alpha1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "service-1",
				Namespace: "default",
			},
			Status: configurationv1alpha1.KongServiceStatus{
				Konnect: &konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef{
					KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{
						ID: "12345",
					},
				},
			},
		},
	).Build()
	output, err := kongPluginBindingToSDKPluginInput(ctx, cl, pb)
	require.NoError(t, err)
	expectedTags := []string{
		"k8s-kind:KongPluginBinding",
		"k8s-name:plugin-binding-1",
		"k8s-namespace:default",
		"k8s-uid:" + string(pb.GetUID()),
		"k8s-version:v1alpha1",
		"k8s-group:configuration.konghq.com",
		"k8s-generation:2",
		"tag1",
		"tag2",
		"duplicate-tag",
		"tag3",
		"tag4",
	}
	require.ElementsMatch(t, expectedTags, output.Tags)
}
