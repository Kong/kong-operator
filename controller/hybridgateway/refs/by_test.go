package refs

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
	gwtypes "github.com/kong/kong-operator/internal/types"
)

func newTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = gatewayv1.Install(s)
	_ = konnectv1alpha2.AddToScheme(s)
	return s
}

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
	gatewayGroup := gwtypes.Group(gwtypes.GroupName)
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
			wantErr:     true,
			description: "should return error for invalid group",
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
			wantErr:     true,
			description: "should return error for invalid kind",
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
		{
			name: "gateway without konnect extension",
			setup: func() (client.Client, *gwtypes.HTTPRoute, gwtypes.ParentReference) {
				// Create a Gateway without any KonnectExtension.
				gateway := &gwtypes.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-gateway",
						Namespace: "test-namespace",
					},
					Spec: gwtypes.GatewaySpec{
						GatewayClassName: "test-gateway-class",
					},
				}

				gatewayClass := &gwtypes.GatewayClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-gateway-class",
					},
					Spec: gwtypes.GatewayClassSpec{
						ControllerName: "konghq.com/gateway-operator",
					},
				}

				cl := fake.NewClientBuilder().
					WithScheme(newTestScheme()).
					WithObjects(gateway, gatewayClass).
					Build()

				route := &gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-route",
						Namespace: "test-namespace",
					},
				}
				return cl, route, gwtypes.ParentReference{
					Group: &gatewayGroup,
					Kind:  &gatewayKind,
					Name:  "test-gateway",
				}
			},
			expected:    nil,
			wantErr:     false,
			description: "should return nil when gateway has no konnect extension",
		},
		{
			name: "gateway with konnect extension but no control plane",
			setup: func() (client.Client, *gwtypes.HTTPRoute, gwtypes.ParentReference) {
				// Create a Gateway with a KonnectExtension that references a non-existent ControlPlane.
				gateway := &gwtypes.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-gateway",
						Namespace: "test-namespace",
						UID:       "gateway-uid-456",
					},
					Spec: gwtypes.GatewaySpec{
						GatewayClassName: "test-gateway-class",
					},
				}

				gatewayClass := &gwtypes.GatewayClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-gateway-class",
					},
					Spec: gwtypes.GatewayClassSpec{
						ControllerName: "konghq.com/gateway-operator",
					},
				}

				konnectExtension := &konnectv1alpha2.KonnectExtension{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-konnect-extension",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"gateway-operator.konghq.com/managed-by": "gateway",
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: "gateway.networking.k8s.io/v1",
								Kind:       "Gateway",
								Name:       "test-gateway",
								UID:        "gateway-uid-456",
							},
						},
					},
					Spec: konnectv1alpha2.KonnectExtensionSpec{
						Konnect: konnectv1alpha2.KonnectExtensionKonnectSpec{
							ControlPlane: konnectv1alpha2.KonnectExtensionControlPlane{
								Ref: commonv1alpha1.KonnectExtensionControlPlaneRef{
									Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
									KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
										Name: "non-existent-cp",
									},
								},
							},
						},
					},
				}

				cl := fake.NewClientBuilder().
					WithScheme(newTestScheme()).
					WithObjects(gateway, gatewayClass, konnectExtension).
					Build()

				route := &gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-route",
						Namespace: "test-namespace",
					},
				}
				return cl, route, gwtypes.ParentReference{
					Group: &gatewayGroup,
					Kind:  &gatewayKind,
					Name:  "test-gateway",
				}
			},
			expected:    nil,
			wantErr:     false,
			description: "should return nil when control plane doesn't exist",
		},
		{
			name: "successful case with valid konnect extension and control plane",
			setup: func() (client.Client, *gwtypes.HTTPRoute, gwtypes.ParentReference) {
				// Create a complete setup with Gateway, KonnectExtension, and ControlPlane.
				gateway := &gwtypes.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-gateway",
						Namespace: "test-namespace",
						UID:       "gateway-uid-123",
					},
					Spec: gwtypes.GatewaySpec{
						GatewayClassName: "test-gateway-class",
					},
				}

				gatewayClass := &gwtypes.GatewayClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-gateway-class",
					},
					Spec: gwtypes.GatewayClassSpec{
						ControllerName: "konghq.com/gateway-operator",
					},
				}

				konnectExtension := &konnectv1alpha2.KonnectExtension{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-konnect-extension",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"gateway-operator.konghq.com/managed-by": "gateway",
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: "gateway.networking.k8s.io/v1",
								Kind:       "Gateway",
								Name:       "test-gateway",
								UID:        "gateway-uid-123",
							},
						},
					},
					Spec: konnectv1alpha2.KonnectExtensionSpec{
						Konnect: konnectv1alpha2.KonnectExtensionKonnectSpec{
							ControlPlane: konnectv1alpha2.KonnectExtensionControlPlane{
								Ref: commonv1alpha1.KonnectExtensionControlPlaneRef{
									Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
									KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
										Name: "test-cp",
									},
								},
							},
						},
					},
				}

				controlPlane := &konnectv1alpha2.KonnectGatewayControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cp",
						Namespace: "test-namespace",
					},
				}

				cl := fake.NewClientBuilder().
					WithScheme(newTestScheme()).
					WithObjects(gateway, gatewayClass, konnectExtension, controlPlane).
					Build()

				route := &gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-route",
						Namespace: "test-namespace",
					},
				}
				return cl, route, gwtypes.ParentReference{
					Group: &gatewayGroup,
					Kind:  &gatewayKind,
					Name:  "test-gateway",
				}
			},
			expected: &commonv1alpha1.ControlPlaneRef{
				Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
				KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
					Name:      "test-cp",
					Namespace: "",
				},
			},
			wantErr:     false,
			description: "should return valid ControlPlaneRef for complete setup",
		},
		{
			name: "error from byGateway, multiple konnect extensions",
			setup: func() (client.Client, *gwtypes.HTTPRoute, gwtypes.ParentReference) {
				// Create a Gateway with multiple KonnectExtensions (should cause an error).
				gateway := &gwtypes.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-gateway",
						Namespace: "test-namespace",
						UID:       "gateway-uid-789",
					},
					Spec: gwtypes.GatewaySpec{
						GatewayClassName: "test-gateway-class",
					},
				}

				gatewayClass := &gwtypes.GatewayClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-gateway-class",
					},
					Spec: gwtypes.GatewayClassSpec{
						ControllerName: "konghq.com/gateway-operator",
					},
				}

				// Create two KonnectExtensions - this should cause an error.
				konnectExtension1 := &konnectv1alpha2.KonnectExtension{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-konnect-extension-1",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"gateway-operator.konghq.com/managed-by": "gateway",
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: "gateway.networking.k8s.io/v1",
								Kind:       "Gateway",
								Name:       "test-gateway",
								UID:        "gateway-uid-789",
							},
						},
					},
					Spec: konnectv1alpha2.KonnectExtensionSpec{
						Konnect: konnectv1alpha2.KonnectExtensionKonnectSpec{
							ControlPlane: konnectv1alpha2.KonnectExtensionControlPlane{
								Ref: commonv1alpha1.KonnectExtensionControlPlaneRef{
									Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
									KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
										Name: "test-cp-1",
									},
								},
							},
						},
					},
				}

				konnectExtension2 := &konnectv1alpha2.KonnectExtension{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-konnect-extension-2",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"gateway-operator.konghq.com/managed-by": "gateway",
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: "gateway.networking.k8s.io/v1",
								Kind:       "Gateway",
								Name:       "test-gateway",
								UID:        "gateway-uid-789",
							},
						},
					},
					Spec: konnectv1alpha2.KonnectExtensionSpec{
						Konnect: konnectv1alpha2.KonnectExtensionKonnectSpec{
							ControlPlane: konnectv1alpha2.KonnectExtensionControlPlane{
								Ref: commonv1alpha1.KonnectExtensionControlPlaneRef{
									Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
									KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
										Name: "test-cp-2",
									},
								},
							},
						},
					},
				}

				cl := fake.NewClientBuilder().
					WithScheme(newTestScheme()).
					WithObjects(gateway, gatewayClass, konnectExtension1, konnectExtension2).
					Build()

				route := &gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-route",
						Namespace: "test-namespace",
					},
				}
				return cl, route, gwtypes.ParentReference{
					Group: &gatewayGroup,
					Kind:  &gatewayKind,
					Name:  "test-gateway",
				}
			},
			expected:    nil,
			wantErr:     true,
			description: "should return error when multiple konnect extensions exist for gateway",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl, route, parentRef := tt.setup()
			result, err := GetControlPlaneRefByParentRef(t.Context(), logr.Discard(), cl, route, parentRef)

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
	gatewayGroup := gwtypes.Group(gwtypes.GroupName)
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
