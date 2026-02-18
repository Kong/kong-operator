package refs

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	hybridgatewayerrors "github.com/kong/kong-operator/v2/controller/hybridgateway/errors"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/vars"
)

func Test_GetSupportedGatewayForParentRef(t *testing.T) {
	ctx := context.Background()
	logger := logr.Discard()

	controllerName := vars.DefaultControllerName
	vars.SetControllerName(controllerName)

	s := scheme.Get()

	gateway := &gwtypes.Gateway{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "gateway.networking.k8s.io/v1",
			Kind:       "Gateway",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "my-gateway",
			UID:       "gateway-uid",
		},
		Spec: gwtypes.GatewaySpec{
			GatewayClassName: "my-class",
		},
	}
	gateway.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   gwtypes.GroupName,
		Version: "v1",
		Kind:    "Gateway",
	})

	konnectExtension := &konnectv1alpha2.KonnectExtension{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "konnect.konghq.com/v1alpha2",
			Kind:       "KonnectExtension",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "my-gateway",
			Labels: map[string]string{
				"gateway-operator.konghq.com/managed-by": "gateway",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "gateway.networking.k8s.io/v1",
					Kind:       "Gateway",
					Name:       "my-gateway",
					UID:        "gateway-uid",
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

	konnectGatewayControlPlane := &konnectv1alpha2.KonnectGatewayControlPlane{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "konnect.konghq.com/v1alpha2",
			Kind:       "KonnectGatewayControlPlane",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "test-cp",
		},
	}

	tests := []struct {
		name            string
		pRef            gwtypes.ParentReference
		routeNS         string
		objs            []client.Object
		controllerVal   string
		interceptorFunc interceptor.Funcs
		wantErr         error
		wantFound       bool
	}{
		{
			name:      "unsupported kind",
			pRef:      gwtypes.ParentReference{Kind: kindPtr("OtherKind"), Name: "my-gateway"},
			routeNS:   "default",
			objs:      []client.Object{gateway, konnectExtension, konnectGatewayControlPlane},
			wantErr:   hybridgatewayerrors.ErrUnsupportedKind,
			wantFound: false,
		},
		{
			name:      "unsupported group",
			pRef:      gwtypes.ParentReference{Kind: kindPtr("Gateway"), Group: groupPtr("other.group"), Name: "my-gateway"},
			routeNS:   "default",
			objs:      []client.Object{gateway, konnectExtension, konnectGatewayControlPlane},
			wantErr:   hybridgatewayerrors.ErrUnsupportedGroup,
			wantFound: false,
		},
		{
			name:      "gateway not found",
			pRef:      gwtypes.ParentReference{Kind: kindPtr("Gateway"), Group: groupPtr(gwtypes.GroupName), Name: "notfound"},
			routeNS:   "default",
			objs:      []client.Object{},
			wantErr:   hybridgatewayerrors.ErrNoGatewayFound,
			wantFound: false,
		},
		{
			name:      "gateway without konnect extension",
			pRef:      gwtypes.ParentReference{Kind: kindPtr("Gateway"), Group: groupPtr(gwtypes.GroupName), Name: "my-gateway"},
			routeNS:   "default",
			objs:      []client.Object{gateway},
			wantErr:   nil,
			wantFound: false,
		},
		{
			name:    "gateway with konnect extension but no control plane",
			pRef:    gwtypes.ParentReference{Kind: kindPtr("Gateway"), Group: groupPtr(gwtypes.GroupName), Name: "my-gateway"},
			routeNS: "default",
			objs: []client.Object{gateway, &konnectv1alpha2.KonnectExtension{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "konnect.konghq.com/v1alpha2",
					Kind:       "KonnectExtension",
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "my-gateway",
					Labels: map[string]string{
						"gateway-operator.konghq.com/managed-by": "gateway",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "gateway.networking.k8s.io/v1",
							Kind:       "Gateway",
							Name:       "my-gateway",
							UID:        "gateway-uid",
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
			}},
			wantErr:   nil,
			wantFound: false,
		},
		{
			name:      "supported parent ref",
			pRef:      gwtypes.ParentReference{Kind: kindPtr("Gateway"), Group: groupPtr(gwtypes.GroupName), Name: "my-gateway"},
			routeNS:   "default",
			objs:      []client.Object{gateway, konnectExtension, konnectGatewayControlPlane},
			wantFound: true,
		},
		{
			name:    "gateway get generic error",
			pRef:    gwtypes.ParentReference{Kind: kindPtr("Gateway"), Group: groupPtr(gwtypes.GroupName), Name: "my-gateway"},
			routeNS: "default",
			objs:    []client.Object{gateway, konnectExtension, konnectGatewayControlPlane},
			interceptorFunc: interceptor.Funcs{
				Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if key.Name == "my-gateway" && key.Namespace == "default" {
						return fmt.Errorf("generic gateway error")
					}
					return client.Get(ctx, key, obj, opts...)
				},
			},
			wantErr:   fmt.Errorf("failed to get gateway for ParentRef"),
			wantFound: false,
		},
		{
			name:    "konnect extension listing error",
			pRef:    gwtypes.ParentReference{Kind: kindPtr("Gateway"), Group: groupPtr(gwtypes.GroupName), Name: "my-gateway"},
			routeNS: "default",
			objs:    []client.Object{gateway, konnectExtension, konnectGatewayControlPlane},
			interceptorFunc: interceptor.Funcs{
				List: func(ctx context.Context, client client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					return fmt.Errorf("generic list error")
				},
			},
			wantErr:   fmt.Errorf("failed to determine if Gateway"),
			wantFound: false,
		},
		{
			name:    "parentRef with custom namespace",
			pRef:    gwtypes.ParentReference{Kind: kindPtr("Gateway"), Group: groupPtr(gwtypes.GroupName), Name: "my-gateway", Namespace: nsPtr("custom-ns")},
			routeNS: "default",
			objs: []client.Object{
				&gwtypes.Gateway{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "gateway.networking.k8s.io/v1",
						Kind:       "Gateway",
					},
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "custom-ns",
						Name:      "my-gateway",
						UID:       "custom-gateway-uid",
					},
					Spec: gwtypes.GatewaySpec{
						GatewayClassName: "my-class",
					},
				},
				&konnectv1alpha2.KonnectExtension{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "konnect.konghq.com/v1alpha2",
						Kind:       "KonnectExtension",
					},
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "custom-ns",
						Name:      "my-gateway",
						Labels: map[string]string{
							"gateway-operator.konghq.com/managed-by": "gateway",
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: "gateway.networking.k8s.io/v1",
								Kind:       "Gateway",
								Name:       "my-gateway",
								UID:        "custom-gateway-uid",
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
				},
				&konnectv1alpha2.KonnectGatewayControlPlane{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "konnect.konghq.com/v1alpha2",
						Kind:       "KonnectGatewayControlPlane",
					},
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "custom-ns",
						Name:      "test-cp",
					},
				},
			},
			wantFound: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clientBuilder := fake.NewClientBuilder().WithScheme(s).WithObjects(tt.objs...)
			if tt.interceptorFunc.Get != nil || tt.interceptorFunc.List != nil {
				clientBuilder = clientBuilder.WithInterceptorFuncs(tt.interceptorFunc)
			}
			cl := clientBuilder.Build()
			gw, found, err := GetSupportedGatewayForParentRef(ctx, logger, cl, tt.pRef, tt.routeNS)
			if tt.wantErr != nil {
				require.Error(t, err)
				if errors.Is(err, hybridgatewayerrors.ErrNoGatewayFound) || errors.Is(err, hybridgatewayerrors.ErrUnsupportedKind) || errors.Is(err, hybridgatewayerrors.ErrUnsupportedGroup) {
					// Specific error type matches
					require.ErrorIs(t, err, tt.wantErr)
					return
				}
				require.Contains(t, err.Error(), tt.wantErr.Error())
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantFound, found)
			if tt.wantFound {
				require.NotNil(t, gw)
			}
		})
	}
}

func groupPtr(s string) *gatewayv1.Group  { g := gatewayv1.Group(s); return &g }
func kindPtr(s string) *gatewayv1.Kind    { k := gatewayv1.Kind(s); return &k }
func nsPtr(s string) *gatewayv1.Namespace { n := gatewayv1.Namespace(s); return &n }

func TestIsGatewayInKonnect(t *testing.T) {
	ctx := context.Background()

	s := scheme.Get()

	tests := []struct {
		name           string
		gateway        *gwtypes.Gateway
		setupObjs      func(*gwtypes.Gateway) []client.Object
		expectedResult bool
		expectError    bool
		errorContains  string
	}{
		{
			name: "gateway with konnect extension and control plane",
			gateway: &gwtypes.Gateway{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "gateway.networking.k8s.io/v1",
					Kind:       "Gateway",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
					UID:       "test-gateway-uid",
				},
			},
			setupObjs: func(gw *gwtypes.Gateway) []client.Object {
				return []client.Object{
					&konnectv1alpha2.KonnectExtension{
						TypeMeta: metav1.TypeMeta{
							APIVersion: "konnect.konghq.com/v1alpha2",
							Kind:       "KonnectExtension",
						},
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-gateway",
							Namespace: "default",
							Labels: map[string]string{
								"gateway-operator.konghq.com/managed-by": "gateway",
							},
							OwnerReferences: []metav1.OwnerReference{
								{
									APIVersion: "gateway.networking.k8s.io/v1",
									Kind:       "Gateway",
									Name:       gw.Name,
									UID:        gw.UID,
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
					},
					&konnectv1alpha2.KonnectGatewayControlPlane{
						TypeMeta: metav1.TypeMeta{
							APIVersion: "konnect.konghq.com/v1alpha2",
							Kind:       "KonnectGatewayControlPlane",
						},
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-cp",
							Namespace: "default",
						},
					},
				}
			},
			expectedResult: true,
			expectError:    false,
		},
		{
			name: "gateway without konnect extension",
			gateway: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
					UID:       "test-gateway-uid",
				},
			},
			setupObjs: func(gw *gwtypes.Gateway) []client.Object {
				return []client.Object{}
			},
			expectedResult: false,
			expectError:    false,
		},
		{
			name: "gateway with konnect extension but no control plane",
			gateway: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
					UID:       "test-gateway-uid",
				},
			},
			setupObjs: func(gw *gwtypes.Gateway) []client.Object {
				return []client.Object{
					&konnectv1alpha2.KonnectExtension{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-gateway",
							Namespace: "default",
							Labels: map[string]string{
								"gateway-operator.konghq.com/managed-by": "gateway",
							},
							OwnerReferences: []metav1.OwnerReference{
								{
									APIVersion: "gateway.networking.k8s.io/v1",
									Kind:       "Gateway",
									Name:       gw.Name,
									UID:        gw.UID,
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
					},
				}
			},
			expectedResult: false,
			expectError:    false,
		},
		{
			name: "error listing konnect extensions",
			gateway: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
					UID:       "test-gateway-uid",
				},
			},
			setupObjs: func(gw *gwtypes.Gateway) []client.Object {
				return []client.Object{}
			},
			expectedResult: false,
			expectError:    true,
			errorContains:  "failed to determine if Gateway",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			existingObjs := tt.setupObjs(tt.gateway)
			// Add the gateway itself to the objects
			existingObjs = append(existingObjs, tt.gateway)

			clientBuilder := fake.NewClientBuilder().WithScheme(s)

			if len(existingObjs) > 0 {
				clientBuilder = clientBuilder.WithObjects(existingObjs...)
			}

			// For the error test case, add an interceptor to simulate API errors
			if tt.expectError && tt.errorContains != "" {
				clientBuilder = clientBuilder.WithInterceptorFuncs(interceptor.Funcs{
					List: func(ctx context.Context, client client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
						return fmt.Errorf("simulated API error")
					},
				})
			}

			cl := clientBuilder.Build()

			result, err := IsGatewayInKonnect(ctx, cl, tt.gateway)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					require.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
			}

			require.Equal(t, tt.expectedResult, result)
		})
	}
}
