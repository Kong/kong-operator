package konnect

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	configurationv1 "github.com/kong/kong-operator/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/internal/utils/index"
	"github.com/kong/kong-operator/modules/manager/scheme"
)

func TestEnqueueKongPluginBindingForKongPlugin(t *testing.T) {
	tests := []struct {
		name     string
		plugin   client.Object
		bindings []client.Object
		expected []ctrl.Request
	}{
		{
			name: "object is not a KongPlugin",
			plugin: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "not-a-plugin",
					Namespace: "default",
				},
			},
			bindings: []client.Object{},
			expected: nil,
		},
		{
			name: "no KongPluginBindings reference the KongPlugin",
			plugin: &configurationv1.KongPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rate-limiting",
					Namespace: "default",
				},
			},
			bindings: []client.Object{
				&configurationv1alpha1.KongPluginBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "binding1",
						Namespace: "default",
					},
					Spec: configurationv1alpha1.KongPluginBindingSpec{
						PluginReference: configurationv1alpha1.PluginRef{
							Name: "different-plugin",
						},
						ControlPlaneRef: commonv1alpha1.ControlPlaneRef{
							Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "cp1",
							},
						},
					},
				},
			},
			expected: []ctrl.Request{},
		},
		{
			name: "single KongPluginBinding with ControlPlane reference",
			plugin: &configurationv1.KongPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rate-limiting",
					Namespace: "default",
				},
			},
			bindings: []client.Object{
				&configurationv1alpha1.KongPluginBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "binding1",
						Namespace: "default",
					},
					Spec: configurationv1alpha1.KongPluginBindingSpec{
						PluginReference: configurationv1alpha1.PluginRef{
							Name: "rate-limiting",
						},
						ControlPlaneRef: commonv1alpha1.ControlPlaneRef{
							Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "cp1",
							},
						},
					},
				},
			},
			expected: []ctrl.Request{
				{
					NamespacedName: types.NamespacedName{
						Name:      "binding1",
						Namespace: "default",
					},
				},
			},
		},
		{
			name: "single KongPluginBinding without ControlPlane reference is filtered out",
			plugin: &configurationv1.KongPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rate-limiting",
					Namespace: "default",
				},
			},
			bindings: []client.Object{
				&configurationv1alpha1.KongPluginBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "binding1",
						Namespace: "default",
					},
					Spec: configurationv1alpha1.KongPluginBindingSpec{
						PluginReference: configurationv1alpha1.PluginRef{
							Name: "rate-limiting",
						},
					},
				},
			},
			expected: []ctrl.Request{},
		},
		{
			name: "multiple KongPluginBindings, some with ControlPlane reference",
			plugin: &configurationv1.KongPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rate-limiting",
					Namespace: "default",
				},
			},
			bindings: []client.Object{
				&configurationv1alpha1.KongPluginBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "binding1",
						Namespace: "default",
					},
					Spec: configurationv1alpha1.KongPluginBindingSpec{
						PluginReference: configurationv1alpha1.PluginRef{
							Name: "rate-limiting",
						},
						ControlPlaneRef: commonv1alpha1.ControlPlaneRef{
							Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "cp1",
							},
						},
					},
				},
				&configurationv1alpha1.KongPluginBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "binding2",
						Namespace: "default",
					},
					Spec: configurationv1alpha1.KongPluginBindingSpec{
						PluginReference: configurationv1alpha1.PluginRef{
							Name: "rate-limiting",
						},
					},
				},
				&configurationv1alpha1.KongPluginBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "binding3",
						Namespace: "default",
					},
					Spec: configurationv1alpha1.KongPluginBindingSpec{
						PluginReference: configurationv1alpha1.PluginRef{
							Name: "rate-limiting",
						},
						ControlPlaneRef: commonv1alpha1.ControlPlaneRef{
							Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "cp2",
							},
						},
					},
				},
			},
			expected: []ctrl.Request{
				{
					NamespacedName: types.NamespacedName{
						Name:      "binding1",
						Namespace: "default",
					},
				},
				{
					NamespacedName: types.NamespacedName{
						Name:      "binding3",
						Namespace: "default",
					},
				},
			},
		},
		{
			name: "cross-namespace plugin reference",
			plugin: &configurationv1.KongPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rate-limiting",
					Namespace: "plugin-ns",
				},
			},
			bindings: []client.Object{
				&configurationv1alpha1.KongPluginBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "binding1",
						Namespace: "app-ns",
					},
					Spec: configurationv1alpha1.KongPluginBindingSpec{
						PluginReference: configurationv1alpha1.PluginRef{
							Name:      "rate-limiting",
							Namespace: "plugin-ns",
						},
						ControlPlaneRef: commonv1alpha1.ControlPlaneRef{
							Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "cp1",
							},
						},
					},
				},
				&configurationv1alpha1.KongPluginBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "binding2",
						Namespace: "plugin-ns",
					},
					Spec: configurationv1alpha1.KongPluginBindingSpec{
						PluginReference: configurationv1alpha1.PluginRef{
							Name: "rate-limiting",
						},
						ControlPlaneRef: commonv1alpha1.ControlPlaneRef{
							Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "cp1",
							},
						},
					},
				},
			},
			expected: []ctrl.Request{
				{
					NamespacedName: types.NamespacedName{
						Name:      "binding1",
						Namespace: "app-ns",
					},
				},
				{
					NamespacedName: types.NamespacedName{
						Name:      "binding2",
						Namespace: "plugin-ns",
					},
				},
			},
		},
		{
			name: "plugin reference with explicit Kind set to KongPlugin",
			plugin: &configurationv1.KongPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rate-limiting",
					Namespace: "default",
				},
			},
			bindings: []client.Object{
				&configurationv1alpha1.KongPluginBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "binding1",
						Namespace: "default",
					},
					Spec: configurationv1alpha1.KongPluginBindingSpec{
						PluginReference: configurationv1alpha1.PluginRef{
							Name: "rate-limiting",
							Kind: func() *string {
								kind := "KongPlugin"
								return &kind
							}(),
						},
						ControlPlaneRef: commonv1alpha1.ControlPlaneRef{
							Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "cp1",
							},
						},
					},
				},
			},
			expected: []ctrl.Request{
				{
					NamespacedName: types.NamespacedName{
						Name:      "binding1",
						Namespace: "default",
					},
				},
			},
		},
		{
			name: "plugin reference with Kind set to KongClusterPlugin is filtered out by index",
			plugin: &configurationv1.KongPlugin{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rate-limiting",
					Namespace: "default",
				},
			},
			bindings: []client.Object{
				&configurationv1alpha1.KongPluginBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "binding1",
						Namespace: "default",
					},
					Spec: configurationv1alpha1.KongPluginBindingSpec{
						PluginReference: configurationv1alpha1.PluginRef{
							Name: "rate-limiting",
							Kind: func() *string {
								kind := "KongClusterPlugin"
								return &kind
							}(),
						},
						ControlPlaneRef: commonv1alpha1.ControlPlaneRef{
							Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "cp1",
							},
						},
					},
				},
			},
			expected: []ctrl.Request{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			// Build a client with the plugin and bindings
			objs := append([]client.Object{tt.plugin}, tt.bindings...)
			cl := fakectrlruntimeclient.NewClientBuilder().
				WithScheme(scheme.Get()).
				WithObjects(objs...).
				WithIndex(&configurationv1alpha1.KongPluginBinding{},
					index.IndexFieldKongPluginBindingKongPluginReference,
					index.OptionsForKongPluginBinding()[0].ExtractValueFn,
				).
				Build()
			require.NotNil(t, cl)

			// Create the watch mapper function
			mapperFunc := enqueueKongPluginBindingForKongPlugin(cl)

			// Execute the mapper
			requests := mapperFunc(ctx, tt.plugin)

			// Assert
			require.Len(t, requests, len(tt.expected))
			if len(tt.expected) > 0 {
				require.ElementsMatch(t, tt.expected, requests)
			}
		})
	}
}

func TestEnqueueKongPluginBindingForKongPlugin_CrossNamespace(t *testing.T) {
	ctx := context.Background()

	// Create a KongPlugin in namespace "plugin-ns"
	plugin := &configurationv1.KongPlugin{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rate-limiting",
			Namespace: "plugin-ns",
		},
	}

	// Create KongPluginBindings in different namespaces
	bindings := []client.Object{
		// Binding in app-ns-1 referencing plugin in plugin-ns
		&configurationv1alpha1.KongPluginBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "binding1",
				Namespace: "app-ns-1",
			},
			Spec: configurationv1alpha1.KongPluginBindingSpec{
				PluginReference: configurationv1alpha1.PluginRef{
					Name:      "rate-limiting",
					Namespace: "plugin-ns",
				},
				ControlPlaneRef: commonv1alpha1.ControlPlaneRef{
					Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
					KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
						Name: "cp1",
					},
				},
			},
		},
		// Binding in app-ns-2 referencing plugin in plugin-ns
		&configurationv1alpha1.KongPluginBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "binding2",
				Namespace: "app-ns-2",
			},
			Spec: configurationv1alpha1.KongPluginBindingSpec{
				PluginReference: configurationv1alpha1.PluginRef{
					Name:      "rate-limiting",
					Namespace: "plugin-ns",
				},
				ControlPlaneRef: commonv1alpha1.ControlPlaneRef{
					Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
					KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
						Name: "cp1",
					},
				},
			},
		},
		// Binding in plugin-ns referencing plugin in same namespace
		&configurationv1alpha1.KongPluginBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "binding3",
				Namespace: "plugin-ns",
			},
			Spec: configurationv1alpha1.KongPluginBindingSpec{
				PluginReference: configurationv1alpha1.PluginRef{
					Name: "rate-limiting",
				},
				ControlPlaneRef: commonv1alpha1.ControlPlaneRef{
					Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
					KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
						Name: "cp1",
					},
				},
			},
		},
		// Binding in app-ns-1 referencing different plugin - should not be returned
		&configurationv1alpha1.KongPluginBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "binding4",
				Namespace: "app-ns-1",
			},
			Spec: configurationv1alpha1.KongPluginBindingSpec{
				PluginReference: configurationv1alpha1.PluginRef{
					Name:      "different-plugin",
					Namespace: "plugin-ns",
				},
				ControlPlaneRef: commonv1alpha1.ControlPlaneRef{
					Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
					KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
						Name: "cp1",
					},
				},
			},
		},
	}

	// Build client with proper indexing
	objs := append([]client.Object{plugin}, bindings...)
	cl := fakectrlruntimeclient.NewClientBuilder().
		WithScheme(scheme.Get()).
		WithObjects(objs...).
		WithIndex(&configurationv1alpha1.KongPluginBinding{},
			index.IndexFieldKongPluginBindingKongPluginReference,
			index.OptionsForKongPluginBinding()[0].ExtractValueFn,
		).
		Build()
	require.NotNil(t, cl)

	// Create the watch mapper function
	mapperFunc := enqueueKongPluginBindingForKongPlugin(cl)

	// Execute the mapper
	requests := mapperFunc(ctx, plugin)

	// Assert: Should return all bindings referencing this plugin across all namespaces
	expected := []ctrl.Request{
		{
			NamespacedName: types.NamespacedName{
				Name:      "binding1",
				Namespace: "app-ns-1",
			},
		},
		{
			NamespacedName: types.NamespacedName{
				Name:      "binding2",
				Namespace: "app-ns-2",
			},
		},
		{
			NamespacedName: types.NamespacedName{
				Name:      "binding3",
				Namespace: "plugin-ns",
			},
		},
	}

	require.Len(t, requests, len(expected))
	require.ElementsMatch(t, expected, requests)
}
