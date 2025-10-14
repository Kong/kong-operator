package refs

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
	gwtypes "github.com/kong/kong-operator/internal/types"
)

func TestGetNamespacedRefs(t *testing.T) {
	tests := []struct {
		name        string
		setup       func() (client.Client, runtime.Object)
		expected    map[string]GatewaysByNamespacedRef
		wantErr     bool
		description string
	}{
		{
			name: "HTTPRoute with no references",
			setup: func() (client.Client, runtime.Object) {
				cl := fake.NewClientBuilder().Build()
				httpRoute := &gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-route",
						Namespace: "test-namespace",
					},
				}
				return cl, httpRoute
			},
			expected:    map[string]GatewaysByNamespacedRef{},
			wantErr:     false,
			description: "should return empty map for HTTPRoute without references",
		},
		{
			name: "unsupported object type",
			setup: func() (client.Client, runtime.Object) {
				cl := fake.NewClientBuilder().Build()
				obj := &gwtypes.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-route",
						Namespace: "test-namespace",
					},
				}
				return cl, obj
			},
			expected:    nil,
			wantErr:     false,
			description: "should return nil for unsupported object type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl, obj := tt.setup()
			result, err := GetNamespacedRefs(context.Background(), cl, obj)

			if tt.wantErr {
				assert.Error(t, err, tt.description)
			} else {
				assert.NoError(t, err, tt.description)
				assert.Equal(t, tt.expected, result, tt.description)
			}
		})
	}
}

func TestGetControlPlaneRefByParentRef(t *testing.T) {
	gatewayGroup := gwtypes.Group("gateway.networking.k8s.io")
	gatewayKind := gwtypes.Kind("Gateway")

	tests := []struct {
		name        string
		setup       func() (client.Client, *gwtypes.HTTPRoute, gwtypes.ParentReference)
		expected    *commonv1alpha1.ControlPlaneRef
		wantErr     bool
		description string
	}{
		{
			name: "invalid group",
			setup: func() (client.Client, *gwtypes.HTTPRoute, gwtypes.ParentReference) {
				cl := fake.NewClientBuilder().Build()
				route := &gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-route",
						Namespace: "test-namespace",
					},
				}
				invalidGroup := gwtypes.Group("invalid.group")
				return cl, route, gwtypes.ParentReference{
					Group: &invalidGroup,
				}
			},
			expected:    nil,
			wantErr:     false,
			description: "should return nil for invalid group",
		},
		{
			name: "invalid kind",
			setup: func() (client.Client, *gwtypes.HTTPRoute, gwtypes.ParentReference) {
				cl := fake.NewClientBuilder().Build()
				route := &gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-route",
						Namespace: "test-namespace",
					},
				}
				invalidKind := gwtypes.Kind("invalid.kind")
				return cl, route, gwtypes.ParentReference{
					Group: &gatewayGroup,
					Kind:  &invalidKind,
				}
			},
			expected:    nil,
			wantErr:     false,
			description: "should return nil for invalid kind",
		},
		{
			name: "gateway not found",
			setup: func() (client.Client, *gwtypes.HTTPRoute, gwtypes.ParentReference) {
				cl := fake.NewClientBuilder().Build()
				route := &gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-route",
						Namespace: "test-namespace",
					},
				}
				return cl, route, gwtypes.ParentReference{
					Group: &gatewayGroup,
					Kind:  &gatewayKind,
					Name:  "non-existent-gateway",
				}
			},
			expected:    nil,
			wantErr:     true,
			description: "should return error when gateway not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl, route, parentRef := tt.setup()
			result, err := GetControlPlaneRefByParentRef(context.Background(), cl, route, parentRef)

			if tt.wantErr {
				assert.Error(t, err, tt.description)
			} else {
				assert.NoError(t, err, tt.description)
				assert.Equal(t, tt.expected, result, tt.description)
			}
		})
	}
}

func TestGetListenersByParentRef(t *testing.T) {
	gatewayGroup := gwtypes.Group("gateway.networking.k8s.io")
	gatewayKind := gwtypes.Kind("Gateway")

	tests := []struct {
		name        string
		setup       func() (client.Client, *gwtypes.HTTPRoute, gwtypes.ParentReference)
		expected    []gwtypes.Listener
		wantErr     bool
		description string
	}{
		{
			name: "invalid group",
			setup: func() (client.Client, *gwtypes.HTTPRoute, gwtypes.ParentReference) {
				cl := fake.NewClientBuilder().Build()
				route := &gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-route",
						Namespace: "test-namespace",
					},
				}
				invalidGroup := gwtypes.Group("invalid.group")
				return cl, route, gwtypes.ParentReference{
					Group: &invalidGroup,
				}
			},
			expected:    nil,
			wantErr:     false,
			description: "should return nil for invalid group",
		},
		{
			name: "invalid kind",
			setup: func() (client.Client, *gwtypes.HTTPRoute, gwtypes.ParentReference) {
				cl := fake.NewClientBuilder().Build()
				route := &gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-route",
						Namespace: "test-namespace",
					},
				}
				invalidKind := gwtypes.Kind("InvalidKind")
				return cl, route, gwtypes.ParentReference{
					Group: &gatewayGroup,
					Kind:  &invalidKind,
				}
			},
			expected:    nil,
			wantErr:     false,
			description: "should return nil for invalid kind",
		},
		{
			name: "gateway not found",
			setup: func() (client.Client, *gwtypes.HTTPRoute, gwtypes.ParentReference) {
				cl := fake.NewClientBuilder().Build()
				route := &gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-route",
						Namespace: "test-namespace",
					},
				}
				return cl, route, gwtypes.ParentReference{
					Group: &gatewayGroup,
					Kind:  &gatewayKind,
					Name:  "non-existent-gateway",
				}
			},
			expected:    nil,
			wantErr:     true,
			description: "should return error when gateway not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl, route, parentRef := tt.setup()
			result, err := GetListenersByParentRef(context.Background(), cl, route, parentRef)

			if tt.wantErr {
				assert.Error(t, err, tt.description)
			} else {
				assert.NoError(t, err, tt.description)
				assert.Equal(t, tt.expected, result, tt.description)
			}
		})
	}
}

func TestByKonnectExtension(t *testing.T) {
	tests := []struct {
		name        string
		setup       func() (client.Client, konnectv1alpha2.KonnectExtension)
		expected    *commonv1alpha1.KonnectNamespacedRef
		wantErr     bool
		description string
	}{
		{
			name: "KonnectExtension without ControlPlaneRef",
			setup: func() (client.Client, konnectv1alpha2.KonnectExtension) {
				cl := fake.NewClientBuilder().Build()
				konnectExtension := konnectv1alpha2.KonnectExtension{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-konnect-extension",
						Namespace: "test-namespace",
					},
				}
				return cl, konnectExtension
			},
			expected:    nil,
			wantErr:     false,
			description: "should return nil when no ControlPlaneRef is set",
		},
		{
			name: "KonnectExtension with cross-namespace reference",
			setup: func() (client.Client, konnectv1alpha2.KonnectExtension) {
				cl := fake.NewClientBuilder().Build()
				konnectExtension := konnectv1alpha2.KonnectExtension{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-konnect-extension",
						Namespace: "test-namespace",
					},
					Spec: konnectv1alpha2.KonnectExtensionSpec{
						Konnect: konnectv1alpha2.KonnectExtensionKonnectSpec{
							ControlPlane: konnectv1alpha2.KonnectExtensionControlPlane{
								Ref: commonv1alpha1.KonnectExtensionControlPlaneRef{
									Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
									KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
										Name:      "test-cp",
										Namespace: "other-namespace",
									},
								},
							},
						},
					},
				}
				return cl, konnectExtension
			},
			expected:    nil,
			wantErr:     true,
			description: "should return error for cross-namespace references",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl, konnectExt := tt.setup()
			result, err := byKonnectExtension(context.Background(), cl, konnectExt)

			if tt.wantErr {
				assert.Error(t, err, tt.description)
			} else {
				assert.NoError(t, err, tt.description)
				assert.Equal(t, tt.expected, result, tt.description)
			}
		})
	}
}

func TestByHTTPRoute(t *testing.T) {
	tests := []struct {
		name        string
		setup       func() (client.Client, gwtypes.HTTPRoute)
		expected    map[string]GatewaysByNamespacedRef
		wantErr     bool
		description string
	}{
		{
			name: "HTTPRoute without parentRefs",
			setup: func() (client.Client, gwtypes.HTTPRoute) {
				cl := fake.NewClientBuilder().Build()
				httpRoute := gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-route",
						Namespace: "test-namespace",
					},
				}
				return cl, httpRoute
			},
			expected:    map[string]GatewaysByNamespacedRef{},
			wantErr:     false,
			description: "should return empty map for HTTPRoute without parentRefs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl, httpRoute := tt.setup()
			result, err := byHTTPRoute(context.Background(), cl, httpRoute)

			if tt.wantErr {
				assert.Error(t, err, tt.description)
			} else {
				assert.NoError(t, err, tt.description)
				assert.Equal(t, tt.expected, result, tt.description)
			}
		})
	}
}

func TestGatewaysByNamespacedRefStructure(t *testing.T) {
	ref := commonv1alpha1.KonnectNamespacedRef{
		Name:      "test-ref",
		Namespace: "test-namespace",
	}

	gateways := []gwtypes.Gateway{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "gateway1",
				Namespace: "test-namespace",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "gateway2",
				Namespace: "test-namespace",
			},
		},
	}

	gatewaysByRef := GatewaysByNamespacedRef{
		Ref:      ref,
		Gateways: gateways,
	}

	t.Run("stores correct reference", func(t *testing.T) {
		assert.Equal(t, ref, gatewaysByRef.Ref, "should store the correct reference")
	})

	t.Run("stores correct gateways", func(t *testing.T) {
		assert.Equal(t, gateways, gatewaysByRef.Gateways, "should store the correct gateways")
		assert.Len(t, gatewaysByRef.Gateways, 2, "should have the right number of gateways")
	})
}
