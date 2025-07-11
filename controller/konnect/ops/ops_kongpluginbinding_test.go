package ops

import (
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kong/kong-operator/modules/manager/scheme"

	configurationv1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha1"
	"github.com/kong/kubernetes-configuration/v2/pkg/metadata"
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

func TestKongPluginWithTargetsToKongPluginInput(t *testing.T) {
	plugin := &configurationv1.KongPlugin{
		PluginName:   "basic-auth",
		InstanceName: "my-plugin",
		Disabled:     false,
		Protocols:    []configurationv1.KongProtocol{"http", "https"},
	}

	pluginWithoutInstanceName := &configurationv1.KongPlugin{
		PluginName: "basic-auth",
		Disabled:   false,
	}

	binding := &configurationv1alpha1.KongPluginBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-binding",
			Namespace: "default",
		},
		Spec: configurationv1alpha1.KongPluginBindingSpec{},
	}

	tests := []struct {
		name          string
		binding       *configurationv1alpha1.KongPluginBinding
		plugin        *configurationv1.KongPlugin
		targets       []pluginTarget
		tags          []string
		expected      *sdkkonnectcomp.Plugin
		expectedError bool
	}{
		{
			name:    "no targets with global scope",
			binding: binding,
			plugin:  plugin,
			targets: nil,
			tags:    []string{"tag1", "tag2"},
			expected: &sdkkonnectcomp.Plugin{
				Name:         "basic-auth",
				Config:       map[string]any{},
				Enabled:      lo.ToPtr(true),
				Tags:         []string{"tag1", "tag2"},
				InstanceName: lo.ToPtr("my-plugin"),
				Protocols:    []sdkkonnectcomp.Protocols{"http", "https"},
			},
		},
		{
			name: "no targets with target-only scope should fail",
			binding: &configurationv1alpha1.KongPluginBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-binding",
					Namespace: "default",
				},
				Spec: configurationv1alpha1.KongPluginBindingSpec{
					Scope: configurationv1alpha1.KongPluginBindingScopeOnlyTargets,
				},
			},
			plugin:        plugin,
			targets:       nil,
			tags:          []string{"tag1", "tag2"},
			expectedError: true,
		},
		{
			name:    "with service target",
			binding: binding,
			plugin:  plugin,
			targets: []pluginTarget{
				&configurationv1alpha1.KongService{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-1",
						Namespace: "default",
					},
					Status: configurationv1alpha1.KongServiceStatus{
						Konnect: &konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef{
							KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{
								ID: "service-id-123",
							},
						},
					},
				},
			},
			tags: []string{"tag1", "tag2"},
			expected: &sdkkonnectcomp.Plugin{
				Name:         "basic-auth",
				Config:       map[string]any{},
				Enabled:      lo.ToPtr(true),
				Tags:         []string{"tag1", "tag2"},
				InstanceName: lo.ToPtr("my-plugin"),
				Protocols:    []sdkkonnectcomp.Protocols{"http", "https"},
				Service: &sdkkonnectcomp.PluginService{
					ID: lo.ToPtr("service-id-123"),
				},
			},
		},
		{
			name:    "with route target",
			binding: binding,
			plugin:  plugin,
			targets: []pluginTarget{
				&configurationv1alpha1.KongRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "route-1",
						Namespace: "default",
					},
					Status: configurationv1alpha1.KongRouteStatus{
						Konnect: &konnectv1alpha1.KonnectEntityStatusWithControlPlaneAndServiceRefs{
							KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{
								ID: "route-id-123",
							},
						},
					},
				},
			},
			tags: []string{"tag1", "tag2"},
			expected: &sdkkonnectcomp.Plugin{
				Name:         "basic-auth",
				Config:       map[string]any{},
				Enabled:      lo.ToPtr(true),
				Tags:         []string{"tag1", "tag2"},
				InstanceName: lo.ToPtr("my-plugin"),
				Protocols:    []sdkkonnectcomp.Protocols{"http", "https"},
				Route: &sdkkonnectcomp.PluginRoute{
					ID: lo.ToPtr("route-id-123"),
				},
			},
		},
		{
			name:    "with consumer target",
			binding: binding,
			plugin:  plugin,
			targets: []pluginTarget{
				&configurationv1.KongConsumer{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "consumer-1",
						Namespace: "default",
					},
					Status: configurationv1.KongConsumerStatus{
						Konnect: &konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef{
							KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{
								ID: "consumer-id-123",
							},
						},
					},
				},
			},
			tags: []string{"tag1", "tag2"},
			expected: &sdkkonnectcomp.Plugin{
				Name:         "basic-auth",
				Config:       map[string]any{},
				Enabled:      lo.ToPtr(true),
				Tags:         []string{"tag1", "tag2"},
				InstanceName: lo.ToPtr("my-plugin"),
				Protocols:    []sdkkonnectcomp.Protocols{"http", "https"},
				Consumer: &sdkkonnectcomp.PluginConsumer{
					ID: lo.ToPtr("consumer-id-123"),
				},
			},
		},
		{
			name:    "with multiple targets",
			binding: binding,
			plugin:  plugin,
			targets: []pluginTarget{
				&configurationv1alpha1.KongService{
					ObjectMeta: metav1.ObjectMeta{Name: "service-1"},
					Status: configurationv1alpha1.KongServiceStatus{
						Konnect: &konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef{
							KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{ID: "service-id-123"},
						},
					},
				},
				&configurationv1.KongConsumer{
					ObjectMeta: metav1.ObjectMeta{Name: "consumer-1"},
					Status: configurationv1.KongConsumerStatus{
						Konnect: &konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef{
							KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{ID: "consumer-id-123"},
						},
					},
				},
			},
			tags: []string{"tag1", "tag2"},
			expected: &sdkkonnectcomp.Plugin{
				Name:         "basic-auth",
				Config:       map[string]any{},
				Enabled:      lo.ToPtr(true),
				Tags:         []string{"tag1", "tag2"},
				InstanceName: lo.ToPtr("my-plugin"),
				Protocols:    []sdkkonnectcomp.Protocols{"http", "https"},
				Service: &sdkkonnectcomp.PluginService{
					ID: lo.ToPtr("service-id-123"),
				},
				Consumer: &sdkkonnectcomp.PluginConsumer{
					ID: lo.ToPtr("consumer-id-123"),
				},
			},
		},
		{
			name:    "target without konnect id",
			binding: binding,
			plugin:  plugin,
			targets: []pluginTarget{
				&configurationv1alpha1.KongService{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-1",
						Namespace: "default",
					},
					Status: configurationv1alpha1.KongServiceStatus{},
				},
			},
			tags:          []string{"tag1", "tag2"},
			expectedError: true,
		},
		{
			name:    "plugin without instance name",
			binding: binding,
			plugin:  pluginWithoutInstanceName,
			targets: []pluginTarget{
				&configurationv1alpha1.KongService{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service-1",
						Namespace: "default",
					},
					Status: configurationv1alpha1.KongServiceStatus{
						Konnect: &konnectv1alpha1.KonnectEntityStatusWithControlPlaneRef{
							KonnectEntityStatus: konnectv1alpha1.KonnectEntityStatus{
								ID: "service-id-123",
							},
						},
					},
				},
			},
			expected: &sdkkonnectcomp.Plugin{
				Name:    "basic-auth",
				Config:  map[string]any{},
				Enabled: lo.ToPtr(true),
				Service: &sdkkonnectcomp.PluginService{
					ID: lo.ToPtr("service-id-123"),
				},
			},
		},
		// TODO Add test cases for plugin ordering https://github.com/kong/kong-operator/issues/1682
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := kongPluginWithTargetsToKongPluginInput(tc.binding, tc.plugin, tc.targets, tc.tags)
			if tc.expectedError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.expected, result)
		})
	}
}
