package watch

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/event"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/modules/manager/scheme"
)

func TestFilterBy(t *testing.T) {
	s := scheme.Get()

	tests := []struct {
		name        string
		obj         client.Object
		expectError bool
		expectNil   bool
	}{
		{
			name: "HTTPRoute returns predicate funcs",
			obj: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "default",
				},
			},
			expectError: false,
			expectNil:   false,
		},
		{
			name: "Gateway returns predicate funcs",
			obj: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
				},
			},
			expectError: false,
			expectNil:   false,
		},
		{
			name: "unsupported type returns error",
			obj: &gatewayv1.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-class",
				},
			},
			expectError: true,
			expectNil:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := fake.NewClientBuilder().
				WithScheme(s).
				Build()

			predicates, err := FilterBy(context.Background(), cl, tt.obj)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			if tt.expectNil {
				assert.Nil(t, predicates)
			} else {
				assert.NotNil(t, predicates)
			}
		})
	}
}

func TestFilterByHTTPRoute(t *testing.T) {
	s := scheme.Get()

	gatewayGroup := gwtypes.Group(gwtypes.GroupName)
	gatewayKind := gwtypes.Kind("Gateway")

	tests := []struct {
		name           string
		object         client.Object
		objectOld      client.Object
		existingObjs   []client.Object
		eventType      string
		expectedResult bool
	}{
		{
			name: "CreateFunc allows event when object is not HTTPRoute",
			object: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
				},
			},
			eventType:      "create",
			expectedResult: true,
		},
		{
			name: "CreateFunc filters out when gateway not found",
			object: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "default",
				},
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{
							{
								Group: &gatewayGroup,
								Kind:  &gatewayKind,
								Name:  "non-existent-gateway",
							},
						},
					},
				},
			},
			eventType:      "create",
			expectedResult: false,
		},
		{
			name: "CreateFunc allows when error occurs during GetNamespacedRefs",
			object: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "default",
				},
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{
							{
								Group: &gatewayGroup,
								Kind:  &gatewayKind,
								Name:  "gateway-with-error",
							},
						},
					},
				},
			},
			existingObjs: []client.Object{
				&gwtypes.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gateway-with-error",
						Namespace: "default",
						UID:       "gw-error-uid",
					},
					Spec: gwtypes.GatewaySpec{
						GatewayClassName: "kong",
					},
				},
				&konnectv1alpha2.KonnectExtension{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ext1",
						Namespace: "default",
						Labels: map[string]string{
							"gateway-operator.konghq.com/managed-by": "gateway",
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: "gateway.networking.k8s.io/v1",
								Kind:       "Gateway",
								Name:       "gateway-with-error",
								UID:        "gw-error-uid",
							},
						},
					},
					Spec: konnectv1alpha2.KonnectExtensionSpec{},
				},
				&konnectv1alpha2.KonnectExtension{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ext2",
						Namespace: "default",
						Labels: map[string]string{
							"gateway-operator.konghq.com/managed-by": "gateway",
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: "gateway.networking.k8s.io/v1",
								Kind:       "Gateway",
								Name:       "gateway-with-error",
								UID:        "gw-error-uid",
							},
						},
					},
					Spec: konnectv1alpha2.KonnectExtensionSpec{},
				},
			},
			eventType:      "create",
			expectedResult: true,
		},
		{
			name: "CreateFunc filters out HTTPRoute without Konnect control plane reference",
			object: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "default",
				},
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{
							{
								Group: &gatewayGroup,
								Kind:  &gatewayKind,
								Name:  "test-gateway",
							},
						},
					},
				},
			},
			existingObjs: []client.Object{
				&gwtypes.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-gateway",
						Namespace: "default",
					},
					Spec: gwtypes.GatewaySpec{
						GatewayClassName: "kong",
					},
				},
			},
			eventType:      "create",
			expectedResult: false,
		},
		{
			name: "UpdateFunc allows when object is not HTTPRoute",
			object: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
				},
			},
			objectOld: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
				},
			},
			eventType:      "update",
			expectedResult: true,
		},
		{
			name: "UpdateFunc filters out when neither old nor new has CP ref",
			object: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "default",
				},
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{
							{
								Group: &gatewayGroup,
								Kind:  &gatewayKind,
								Name:  "test-gateway",
							},
						},
					},
				},
			},
			objectOld: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "default",
				},
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{
							{
								Group: &gatewayGroup,
								Kind:  &gatewayKind,
								Name:  "test-gateway",
							},
						},
					},
				},
			},
			existingObjs: []client.Object{
				&gwtypes.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-gateway",
						Namespace: "default",
					},
					Spec: gwtypes.GatewaySpec{
						GatewayClassName: "kong",
					},
				},
			},
			eventType:      "update",
			expectedResult: false,
		},
		{
			name: "UpdateFunc allows when old object has CP ref but new doesn't",
			object: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "default",
				},
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{
							{
								Group: &gatewayGroup,
								Kind:  &gatewayKind,
								Name:  "gateway-without-cp",
							},
						},
					},
				},
			},
			objectOld: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "default",
				},
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{
							{
								Group: &gatewayGroup,
								Kind:  &gatewayKind,
								Name:  "gateway-with-cp",
							},
						},
					},
				},
			},
			existingObjs: []client.Object{
				&gwtypes.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gateway-without-cp",
						Namespace: "default",
						UID:       "gw-no-cp-uid",
					},
					Spec: gwtypes.GatewaySpec{
						GatewayClassName: "kong",
					},
				},
				&gwtypes.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gateway-with-cp",
						Namespace: "default",
						UID:       "gw-with-cp-uid",
					},
					Spec: gwtypes.GatewaySpec{
						GatewayClassName: "kong",
					},
				},
				&konnectv1alpha2.KonnectExtension{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-konnect-extension",
						Namespace: "default",
						Labels: map[string]string{
							"gateway-operator.konghq.com/managed-by": "gateway",
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: "gateway.networking.k8s.io/v1",
								Kind:       "Gateway",
								Name:       "gateway-with-cp",
								UID:        "gw-with-cp-uid",
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
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cp",
						Namespace: "default",
					},
				},
			},
			eventType:      "update",
			expectedResult: true,
		},
		{
			name: "DeleteFunc allows event when object is not HTTPRoute",
			object: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
				},
			},
			eventType:      "delete",
			expectedResult: true,
		},
		{
			name: "DeleteFunc filters out HTTPRoute without CP ref",
			object: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "default",
				},
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{
							{
								Group: &gatewayGroup,
								Kind:  &gatewayKind,
								Name:  "test-gateway",
							},
						},
					},
				},
			},
			existingObjs: []client.Object{
				&gwtypes.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-gateway",
						Namespace: "default",
					},
					Spec: gwtypes.GatewaySpec{
						GatewayClassName: "kong",
					},
				},
			},
			eventType:      "delete",
			expectedResult: false,
		},
		{
			name: "GenericFunc allows event when object is not HTTPRoute",
			object: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
				},
			},
			eventType:      "generic",
			expectedResult: true,
		},
		{
			name: "GenericFunc filters out HTTPRoute without CP ref",
			object: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "default",
				},
				Spec: gwtypes.HTTPRouteSpec{
					CommonRouteSpec: gwtypes.CommonRouteSpec{
						ParentRefs: []gwtypes.ParentReference{
							{
								Group: &gatewayGroup,
								Kind:  &gatewayKind,
								Name:  "test-gateway",
							},
						},
					},
				},
			},
			existingObjs: []client.Object{
				&gwtypes.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-gateway",
						Namespace: "default",
					},
					Spec: gwtypes.GatewaySpec{
						GatewayClassName: "kong",
					},
				},
			},
			eventType:      "generic",
			expectedResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := fake.NewClientBuilder().WithScheme(s)
			if len(tt.existingObjs) > 0 {
				builder = builder.WithObjects(tt.existingObjs...)
			}
			cl := builder.Build()

			predicates := filterByHTTPRoute(context.Background(), cl)
			require.NotNil(t, predicates)

			var result bool
			switch tt.eventType {
			case "create":
				result = predicates.CreateFunc(event.CreateEvent{Object: tt.object})
			case "update":
				result = predicates.UpdateFunc(event.UpdateEvent{
					ObjectOld: tt.objectOld,
					ObjectNew: tt.object,
				})
			case "delete":
				result = predicates.DeleteFunc(event.DeleteEvent{Object: tt.object})
			case "generic":
				result = predicates.GenericFunc(event.GenericEvent{Object: tt.object})
			}

			assert.Equal(t, tt.expectedResult, result)
		})
	}
}

func TestFilterByGateway(t *testing.T) {
	s := scheme.Get()

	tests := []struct {
		name             string
		object           client.Object
		objectOld        client.Object
		existingObjs     []client.Object
		interceptorFuncs *interceptor.Funcs
		eventType        string
		expectedResult   bool
	}{
		{
			name: "CreateFunc allows event when object is not Gateway",
			object: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "default",
				},
			},
			eventType:      "create",
			expectedResult: true,
		},
		{
			name: "CreateFunc filters out Gateway with non-existent GatewayClass",
			object: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
				},
				Spec: gwtypes.GatewaySpec{
					GatewayClassName: "non-existent-class",
				},
			},
			eventType:      "create",
			expectedResult: false,
		},
		{
			name: "CreateFunc filters out Gateway with unsupported controller",
			object: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
				},
				Spec: gwtypes.GatewaySpec{
					GatewayClassName: "other-class",
				},
			},
			existingObjs: []client.Object{
				&gwtypes.GatewayClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: "other-class",
					},
					Spec: gwtypes.GatewayClassSpec{
						ControllerName: "example.com/other-controller",
					},
				},
			},
			eventType:      "create",
			expectedResult: false,
		},
		{
			name: "CreateFunc allows supported Gateway",
			object: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
				},
				Spec: gwtypes.GatewaySpec{
					GatewayClassName: "kong",
				},
			},
			existingObjs: []client.Object{
				&gwtypes.GatewayClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: "kong",
					},
					Spec: gwtypes.GatewayClassSpec{
						ControllerName: "konghq.com/gateway-operator",
					},
				},
			},
			eventType:      "create",
			expectedResult: true,
		},
		{
			name: "CreateFunc allows when API error occurs",
			object: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
				},
				Spec: gwtypes.GatewaySpec{
					GatewayClassName: "kong",
				},
			},
			existingObjs: []client.Object{
				&gwtypes.GatewayClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: "kong",
					},
					Spec: gwtypes.GatewayClassSpec{
						ControllerName: "konghq.com/gateway-operator",
					},
				},
			},
			interceptorFuncs: &interceptor.Funcs{
				Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					return errors.New("simulated API error")
				},
			},
			eventType:      "create",
			expectedResult: true,
		},
		{
			name: "UpdateFunc allows when object is not Gateway",
			object: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "default",
				},
			},
			objectOld: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "default",
				},
			},
			eventType:      "update",
			expectedResult: true,
		},
		{
			name: "UpdateFunc filters out when neither old nor new is supported",
			object: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
				},
				Spec: gwtypes.GatewaySpec{
					GatewayClassName: "non-existent",
				},
			},
			objectOld: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
				},
				Spec: gwtypes.GatewaySpec{
					GatewayClassName: "non-existent-old",
				},
			},
			eventType:      "update",
			expectedResult: false,
		},
		{
			name: "UpdateFunc allows when new object is supported",
			object: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
				},
				Spec: gwtypes.GatewaySpec{
					GatewayClassName: "kong",
				},
			},
			objectOld: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
				},
				Spec: gwtypes.GatewaySpec{
					GatewayClassName: "non-existent",
				},
			},
			existingObjs: []client.Object{
				&gwtypes.GatewayClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: "kong",
					},
					Spec: gwtypes.GatewayClassSpec{
						ControllerName: "konghq.com/gateway-operator",
					},
				},
			},
			eventType:      "update",
			expectedResult: true,
		},
		{
			name: "UpdateFunc allows when old object is supported but new is not",
			object: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
				},
				Spec: gwtypes.GatewaySpec{
					GatewayClassName: "non-existent",
				},
			},
			objectOld: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
				},
				Spec: gwtypes.GatewaySpec{
					GatewayClassName: "kong",
				},
			},
			existingObjs: []client.Object{
				&gwtypes.GatewayClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: "kong",
					},
					Spec: gwtypes.GatewayClassSpec{
						ControllerName: "konghq.com/gateway-operator",
					},
				},
			},
			eventType:      "update",
			expectedResult: true,
		},
		{
			name: "DeleteFunc allows event when object is not Gateway",
			object: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "default",
				},
			},
			eventType:      "delete",
			expectedResult: true,
		},
		{
			name: "DeleteFunc filters out unsupported Gateway",
			object: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
				},
				Spec: gwtypes.GatewaySpec{
					GatewayClassName: "non-existent",
				},
			},
			eventType:      "delete",
			expectedResult: false,
		},
		{
			name: "DeleteFunc allows supported Gateway",
			object: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
				},
				Spec: gwtypes.GatewaySpec{
					GatewayClassName: "kong",
				},
			},
			existingObjs: []client.Object{
				&gwtypes.GatewayClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: "kong",
					},
					Spec: gwtypes.GatewayClassSpec{
						ControllerName: "konghq.com/gateway-operator",
					},
				},
			},
			eventType:      "delete",
			expectedResult: true,
		},
		{
			name: "GenericFunc allows event when object is not Gateway",
			object: &gwtypes.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: "default",
				},
			},
			eventType:      "generic",
			expectedResult: true,
		},
		{
			name: "GenericFunc filters out unsupported Gateway",
			object: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
				},
				Spec: gwtypes.GatewaySpec{
					GatewayClassName: "non-existent",
				},
			},
			eventType:      "generic",
			expectedResult: false,
		},
		{
			name: "GenericFunc allows supported Gateway",
			object: &gwtypes.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
				},
				Spec: gwtypes.GatewaySpec{
					GatewayClassName: "kong",
				},
			},
			existingObjs: []client.Object{
				&gwtypes.GatewayClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: "kong",
					},
					Spec: gwtypes.GatewayClassSpec{
						ControllerName: "konghq.com/gateway-operator",
					},
				},
			},
			eventType:      "generic",
			expectedResult: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := fake.NewClientBuilder().WithScheme(s)
			if len(tt.existingObjs) > 0 {
				builder = builder.WithObjects(tt.existingObjs...)
			}
			if tt.interceptorFuncs != nil {
				builder = builder.WithInterceptorFuncs(*tt.interceptorFuncs)
			}
			cl := builder.Build()

			predicates := filterByGateway(context.Background(), cl)
			require.NotNil(t, predicates)

			var result bool
			switch tt.eventType {
			case "create":
				result = predicates.CreateFunc(event.CreateEvent{Object: tt.object})
			case "update":
				result = predicates.UpdateFunc(event.UpdateEvent{
					ObjectOld: tt.objectOld,
					ObjectNew: tt.object,
				})
			case "delete":
				result = predicates.DeleteFunc(event.DeleteEvent{Object: tt.object})
			case "generic":
				result = predicates.GenericFunc(event.GenericEvent{Object: tt.object})
			}

			assert.Equal(t, tt.expectedResult, result)
		})
	}
}
