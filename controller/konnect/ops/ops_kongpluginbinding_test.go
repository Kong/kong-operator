package ops

import (
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	sdkkonnectops "github.com/Kong/sdk-konnect-go/models/operations"
	sdkkonnecterrs "github.com/Kong/sdk-konnect-go/models/sdkerrors"
	"github.com/Kong/sdk-konnect-go/test/mocks"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/metadata"
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
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
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
				Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
					KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
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
						Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
							KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
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
						Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneAndServiceRefs{
							KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
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
						Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
							KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
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
						Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
							KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{ID: "service-id-123"},
						},
					},
				},
				&configurationv1.KongConsumer{
					ObjectMeta: metav1.ObjectMeta{Name: "consumer-1"},
					Status: configurationv1.KongConsumerStatus{
						Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
							KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{ID: "consumer-id-123"},
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
						Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
							KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
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

func TestAdoptKongPluginBindingOverride(t *testing.T) {
	ctx := t.Context()
	const (
		cpID             = "cp-1234"
		pluginKonnectID  = "plugin-5678"
		serviceKonnectID = "svc-9012"
	)

	plugin := &configurationv1.KongPlugin{
		TypeMeta: metav1.TypeMeta{
			Kind:       "KongPlugin",
			APIVersion: "configuration.konghq.com/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rate-limit",
			Namespace: "default",
		},
		PluginName:   "rate-limiting",
		InstanceName: "rl-instance",
		Protocols:    []configurationv1.KongProtocol{"http", "https"},
		Config: apiextensionsv1.JSON{
			Raw: []byte(`{"minute": 42}`),
		},
	}
	service := &configurationv1alpha1.KongService{
		TypeMeta: metav1.TypeMeta{
			Kind:       "KongService",
			APIVersion: "configuration.konghq.com/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-override",
			Namespace: "default",
		},
		Status: configurationv1alpha1.KongServiceStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
				KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
					ID: serviceKonnectID,
				},
			},
		},
	}
	binding := &configurationv1alpha1.KongPluginBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "binding-override",
			Namespace: "default",
			UID:       k8stypes.UID("uid-binding-override"),
		},
		Spec: configurationv1alpha1.KongPluginBindingSpec{
			PluginReference: configurationv1alpha1.PluginRef{
				Name: plugin.Name,
				Kind: lo.ToPtr("KongPlugin"),
			},
			Targets: &configurationv1alpha1.KongPluginBindingTargets{
				ServiceReference: &configurationv1alpha1.TargetRefWithGroupKind{
					Name: service.Name,
					Kind: "KongService",
				},
			},
			Adopt: &commonv1alpha1.AdoptOptions{
				From: commonv1alpha1.AdoptSourceKonnect,
				Mode: commonv1alpha1.AdoptModeOverride,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{
					ID: pluginKonnectID,
				},
			},
		},
		Status: configurationv1alpha1.KongPluginBindingStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
				KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{},
				ControlPlaneID:      cpID,
			},
		},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(plugin, service).Build()
	sdk := mocks.NewMockPluginsSDK(t)
	sdk.EXPECT().GetPlugin(mock.Anything,
		sdkkonnectops.GetPluginRequest{
			PluginID:       pluginKonnectID,
			ControlPlaneID: cpID,
		}).Return(
		&sdkkonnectops.GetPluginResponse{
			Plugin: &sdkkonnectcomp.Plugin{
				ID: lo.ToPtr(pluginKonnectID),
			},
		},
		nil,
	)
	sdk.EXPECT().UpsertPlugin(mock.Anything, mock.MatchedBy(func(req sdkkonnectops.UpsertPluginRequest) bool {
		return req.PluginID == pluginKonnectID &&
			req.ControlPlaneID == cpID &&
			req.Plugin.Service != nil &&
			req.Plugin.Service.ID != nil &&
			*req.Plugin.Service.ID == serviceKonnectID
	})).Return(&sdkkonnectops.UpsertPluginResponse{}, nil)

	err := adoptPluginBinding(ctx, sdk, cl, binding)
	require.NoError(t, err)
	require.Equal(t, pluginKonnectID, binding.GetKonnectID())
}

func TestAdoptKongPluginBindingMatch(t *testing.T) {
	ctx := t.Context()
	const (
		cpID             = "cp-1234"
		pluginKonnectID  = "plugin-match"
		serviceKonnectID = "svc-match"
	)

	plugin := &configurationv1.KongPlugin{
		TypeMeta: metav1.TypeMeta{
			Kind:       "KongPlugin",
			APIVersion: "configuration.konghq.com/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "key-auth",
			Namespace: "default",
		},
		PluginName: "key-auth",
		Config: apiextensionsv1.JSON{
			Raw: []byte(`{"key_in_header": true}`),
		},
	}
	service := &configurationv1alpha1.KongService{
		TypeMeta: metav1.TypeMeta{
			Kind:       "KongService",
			APIVersion: "configuration.konghq.com/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc-match",
			Namespace: "default",
		},
		Status: configurationv1alpha1.KongServiceStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
				KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
					ID: serviceKonnectID,
				},
			},
		},
	}
	binding := &configurationv1alpha1.KongPluginBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "binding-match",
			Namespace: "default",
			UID:       k8stypes.UID("uid-binding-match"),
		},
		Spec: configurationv1alpha1.KongPluginBindingSpec{
			PluginReference: configurationv1alpha1.PluginRef{
				Name: plugin.Name,
				Kind: lo.ToPtr("KongPlugin"),
			},
			Targets: &configurationv1alpha1.KongPluginBindingTargets{
				ServiceReference: &configurationv1alpha1.TargetRefWithGroupKind{
					Name: service.Name,
					Kind: "KongService",
				},
			},
			Adopt: &commonv1alpha1.AdoptOptions{
				From: commonv1alpha1.AdoptSourceKonnect,
				Mode: commonv1alpha1.AdoptModeMatch,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{
					ID: pluginKonnectID,
				},
			},
		},
		Status: configurationv1alpha1.KongPluginBindingStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
				KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{},
				ControlPlaneID:      cpID,
			},
		},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(plugin, service).Build()
	desiredPlugin, err := kongPluginBindingToSDKPluginInput(ctx, cl, binding)
	require.NoError(t, err)
	remotePlugin := *desiredPlugin
	remotePlugin.ID = lo.ToPtr(pluginKonnectID)

	sdk := mocks.NewMockPluginsSDK(t)
	sdk.EXPECT().GetPlugin(mock.Anything,
		sdkkonnectops.GetPluginRequest{
			PluginID:       pluginKonnectID,
			ControlPlaneID: cpID,
		}).Return(
		&sdkkonnectops.GetPluginResponse{
			Plugin: &remotePlugin,
		},
		nil,
	)

	err = adoptPluginBinding(ctx, sdk, cl, binding)
	require.NoError(t, err)
	require.Equal(t, pluginKonnectID, binding.GetKonnectID())
}

func TestAdoptKongPluginBindingMatchMismatch(t *testing.T) {
	ctx := t.Context()
	const (
		cpID            = "cp-1234"
		pluginKonnectID = "plugin-mismatch"
	)

	plugin := &configurationv1.KongPlugin{
		TypeMeta: metav1.TypeMeta{
			Kind:       "KongPlugin",
			APIVersion: "configuration.konghq.com/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "acl",
			Namespace: "default",
		},
		PluginName: "acl",
		Config: apiextensionsv1.JSON{
			Raw: []byte(`{"allow": ["group1"]}`),
		},
	}
	binding := &configurationv1alpha1.KongPluginBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "binding-mismatch",
			Namespace: "default",
			UID:       k8stypes.UID("uid-binding-mismatch"),
		},
		Spec: configurationv1alpha1.KongPluginBindingSpec{
			PluginReference: configurationv1alpha1.PluginRef{
				Name: plugin.Name,
				Kind: lo.ToPtr("KongPlugin"),
			},
			Adopt: &commonv1alpha1.AdoptOptions{
				From: commonv1alpha1.AdoptSourceKonnect,
				Mode: commonv1alpha1.AdoptModeMatch,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{
					ID: pluginKonnectID,
				},
			},
		},
		Status: configurationv1alpha1.KongPluginBindingStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
				KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{},
				ControlPlaneID:      cpID,
			},
		},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(plugin).Build()
	sdk := mocks.NewMockPluginsSDK(t)
	sdk.EXPECT().GetPlugin(mock.Anything,
		sdkkonnectops.GetPluginRequest{
			PluginID:       pluginKonnectID,
			ControlPlaneID: cpID,
		}).Return(
		&sdkkonnectops.GetPluginResponse{
			Plugin: &sdkkonnectcomp.Plugin{
				ID:     lo.ToPtr(pluginKonnectID),
				Name:   "acl",
				Config: map[string]any{"allow": []any{"group2"}},
			},
		},
		nil,
	)

	err := adoptPluginBinding(ctx, sdk, cl, binding)
	require.Error(t, err)
	require.ErrorAs(t, err, &KonnectEntityAdoptionNotMatchError{})
	require.Empty(t, binding.GetKonnectID())
}

func TestAdoptKongPluginBindingFetchError(t *testing.T) {
	ctx := t.Context()
	const (
		cpID            = "cp-1234"
		pluginKonnectID = "plugin-fetch-error"
	)

	plugin := &configurationv1.KongPlugin{
		TypeMeta: metav1.TypeMeta{
			Kind:       "KongPlugin",
			APIVersion: "configuration.konghq.com/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "jwt",
			Namespace: "default",
		},
		PluginName: "jwt",
	}
	binding := &configurationv1alpha1.KongPluginBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "binding-fetch",
			Namespace: "default",
			UID:       k8stypes.UID("uid-binding-fetch"),
		},
		Spec: configurationv1alpha1.KongPluginBindingSpec{
			PluginReference: configurationv1alpha1.PluginRef{
				Name: plugin.Name,
				Kind: lo.ToPtr("KongPlugin"),
			},
			Adopt: &commonv1alpha1.AdoptOptions{
				From: commonv1alpha1.AdoptSourceKonnect,
				Mode: commonv1alpha1.AdoptModeOverride,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{
					ID: pluginKonnectID,
				},
			},
		},
		Status: configurationv1alpha1.KongPluginBindingStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
				KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{},
				ControlPlaneID:      cpID,
			},
		},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(plugin).Build()
	sdk := mocks.NewMockPluginsSDK(t)
	sdk.EXPECT().GetPlugin(mock.Anything,
		sdkkonnectops.GetPluginRequest{
			PluginID:       pluginKonnectID,
			ControlPlaneID: cpID,
		}).Return(
		(*sdkkonnectops.GetPluginResponse)(nil),
		&sdkkonnecterrs.NotFoundError{},
	)

	err := adoptPluginBinding(ctx, sdk, cl, binding)
	require.Error(t, err)
	require.ErrorAs(t, err, &KonnectEntityAdoptionFetchError{})
	require.Empty(t, binding.GetKonnectID())
}

func TestAdoptKongPluginBindingUIDConflict(t *testing.T) {
	ctx := t.Context()
	const (
		cpID            = "cp-1234"
		pluginKonnectID = "plugin-uid-conflict"
	)

	plugin := &configurationv1.KongPlugin{
		TypeMeta: metav1.TypeMeta{
			Kind:       "KongPlugin",
			APIVersion: "configuration.konghq.com/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cors",
			Namespace: "default",
		},
		PluginName: "cors",
	}
	binding := &configurationv1alpha1.KongPluginBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "binding-uid",
			Namespace: "default",
			UID:       k8stypes.UID("uid-binding-original"),
		},
		Spec: configurationv1alpha1.KongPluginBindingSpec{
			PluginReference: configurationv1alpha1.PluginRef{
				Name: plugin.Name,
				Kind: lo.ToPtr("KongPlugin"),
			},
			Adopt: &commonv1alpha1.AdoptOptions{
				From: commonv1alpha1.AdoptSourceKonnect,
				Mode: commonv1alpha1.AdoptModeOverride,
				Konnect: &commonv1alpha1.AdoptKonnectOptions{
					ID: pluginKonnectID,
				},
			},
		},
		Status: configurationv1alpha1.KongPluginBindingStatus{
			Konnect: &konnectv1alpha2.KonnectEntityStatusWithControlPlaneRef{
				KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{},
				ControlPlaneID:      cpID,
			},
		},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme.Get()).WithObjects(plugin).Build()
	sdk := mocks.NewMockPluginsSDK(t)
	sdk.EXPECT().GetPlugin(mock.Anything,
		sdkkonnectops.GetPluginRequest{
			PluginID:       pluginKonnectID,
			ControlPlaneID: cpID,
		}).Return(
		&sdkkonnectops.GetPluginResponse{
			Plugin: &sdkkonnectcomp.Plugin{
				ID:   lo.ToPtr(pluginKonnectID),
				Name: "cors",
				Tags: []string{"k8s-uid:other-uid"},
			},
		},
		nil,
	)

	err := adoptPluginBinding(ctx, sdk, cl, binding)
	require.Error(t, err)
	require.ErrorAs(t, err, &KonnectEntityAdoptionUIDTagConflictError{})
	require.Empty(t, binding.GetKonnectID())
}
