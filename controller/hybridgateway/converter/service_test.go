package converter

import (
	"context"
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

commonv1alpha1 "github.com/kong/kong-operator/api/common/v1alpha1"
konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"

	"github.com/kong/kong-operator/controller/hybridgateway/route"
	"github.com/kong/kong-operator/controller/hybridgateway/utils"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/internal/utils/index"
	"github.com/kong/kong-operator/modules/manager/scheme"
)

func TestServiceTranslation(t *testing.T) {
	testCases := []struct {
		name           string
		service        corev1.Service
		objects        []client.Object
		expectedOutput []*configurationv1alpha1.KongService
		expectedErr    error
	}{
		{
			name: "service with no ports",
			service: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{},
				},
			},
			objects: []client.Object{
				&gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-route",
						Namespace: "default",
					},
					Spec: gwtypes.HTTPRouteSpec{
						Rules: []gwtypes.HTTPRouteRule{
							{
								BackendRefs: []gwtypes.HTTPBackendRef{
									{
										BackendRef: gwtypes.BackendRef{
											BackendObjectReference: gwtypes.BackendObjectReference{
												Name: "test-service",
												Port: lo.ToPtr(gwtypes.PortNumber(80)),
											},
										},
									},
								},
							},
						},
					},
				},
			},
			expectedOutput: []*configurationv1alpha1.KongService{},
		},
		{
			name: "service with matching port, single ControlPlane,",
			service: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Port: 80,
						},
					},
				},
			},
			objects: []client.Object{
				&gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-route",
						Namespace: "default",
					},
					Spec: gwtypes.HTTPRouteSpec{
						Rules: []gwtypes.HTTPRouteRule{
							{
								BackendRefs: []gwtypes.HTTPBackendRef{
									{
										BackendRef: gwtypes.BackendRef{
											BackendObjectReference: gwtypes.BackendObjectReference{
												Name: "test-service",
												Port: lo.ToPtr(gwtypes.PortNumber(80)),
											},
										},
									},
								},
							},
						},
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{
									Group: lo.ToPtr(gwtypes.Group("gateway.networking.k8s.io")),
									Kind:  lo.ToPtr(gwtypes.Kind("Gateway")),
									Name:  "test-gateway",
								},
							},
						},
					},
				},
				&gwtypes.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-gateway",
						Namespace: "default",
					},
					Spec: gwtypes.GatewaySpec{
						GatewayClassName: "test-gatewayclass",
					},
				},
				&gwtypes.GatewayClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-gatewayclass",
					},
					Spec: gwtypes.GatewayClassSpec{
						ParametersRef: &gwtypes.ParametersReference{
							Group:     "gateway-operator.konghq.com",
							Kind:      "GatewayConfiguration",
							Name:      "test-gatewayconfig",
							Namespace: lo.ToPtr(gwtypes.Namespace("default")),
						},
					},
				},
				&gwtypes.GatewayConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-gatewayconfig",
						Namespace: "default",
					},
					Spec: gwtypes.GatewayConfigurationSpec{
						Extensions: []commonv1alpha1.ExtensionRef{
							{
								Group: konnectv1alpha2.SchemeGroupVersion.Group,
								Kind:  konnectv1alpha2.KonnectExtensionKind,
								NamespacedRef: commonv1alpha1.NamespacedRef{
									Name: "test-konnectextension",
								},
							},
						},
					},
				},
				&konnectv1alpha2.KonnectExtension{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-konnectextension",
						Namespace: "default",
					},
					Spec: konnectv1alpha2.KonnectExtensionSpec{
						Konnect: konnectv1alpha2.KonnectExtensionKonnectSpec{
							ControlPlane: konnectv1alpha2.KonnectExtensionControlPlane{
								Ref: commonv1alpha1.KonnectExtensionControlPlaneRef{
									Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
									KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
										Name:      "test-konnectcontrolplane",
										Namespace: "default",
									},
								},
							},
						},
					},
				},
				&konnectv1alpha2.KonnectGatewayControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-konnectcontrolplane",
						Namespace: "default",
					},
					Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: &sdkkonnectcomp.CreateControlPlaneRequest{
							Name: "test-controlplane",
						},
					},
				},
			},
			expectedOutput: []*configurationv1alpha1.KongService{
				{
					TypeMeta: metav1.TypeMeta{
						APIVersion: configurationv1alpha1.GroupVersion.String(),
						Kind:       "KongService",
					},
					Spec: configurationv1alpha1.KongServiceSpec{
						KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
							Name: lo.ToPtr("test-service-80"),
							Port: 80,
							Host: "api.kong-air.com",
						},
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnectcontrolplane",
							},
						},
					},
				},
			},
		},
		{
			name: "multiple HTTPRoutes, single ControlPlane, multiple ports",
			service: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Port: 80,
						},
						{
							Port: 443,
						},
					},
				},
			},
			objects: []client.Object{
				&gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-route-1",
						Namespace: "default",
					},
					Spec: gwtypes.HTTPRouteSpec{
						Rules: []gwtypes.HTTPRouteRule{
							{
								BackendRefs: []gwtypes.HTTPBackendRef{
									{
										BackendRef: gwtypes.BackendRef{
											BackendObjectReference: gwtypes.BackendObjectReference{
												Name: "test-service",
												Port: lo.ToPtr(gwtypes.PortNumber(80)),
											},
										},
									},
									{
										BackendRef: gwtypes.BackendRef{
											BackendObjectReference: gwtypes.BackendObjectReference{
												Name: "test-service",
												Port: lo.ToPtr(gwtypes.PortNumber(8080)),
											},
										},
									},
								},
							},
						},
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{
									Group: lo.ToPtr(gwtypes.Group("gateway.networking.k8s.io")),
									Kind:  lo.ToPtr(gwtypes.Kind("Gateway")),
									Name:  "test-gateway",
								},
							},
						},
					},
				},
				&gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-route-2",
						Namespace: "default",
					},
					Spec: gwtypes.HTTPRouteSpec{
						Rules: []gwtypes.HTTPRouteRule{
							{
								BackendRefs: []gwtypes.HTTPBackendRef{
									{
										BackendRef: gwtypes.BackendRef{
											BackendObjectReference: gwtypes.BackendObjectReference{
												Name: "test-service",
												Port: lo.ToPtr(gwtypes.PortNumber(443)),
											},
										},
									},
									{
										BackendRef: gwtypes.BackendRef{
											BackendObjectReference: gwtypes.BackendObjectReference{
												Name: "test-service",
												Port: lo.ToPtr(gwtypes.PortNumber(8443)),
											},
										},
									},
								},
							},
						},
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{
									Group: lo.ToPtr(gwtypes.Group("gateway.networking.k8s.io")),
									Kind:  lo.ToPtr(gwtypes.Kind("Gateway")),
									Name:  "test-gateway",
								},
							},
						},
					},
				},
				&gwtypes.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-gateway",
						Namespace: "default",
					},
					Spec: gwtypes.GatewaySpec{
						GatewayClassName: "test-gatewayclass",
					},
				},
				&gwtypes.GatewayClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-gatewayclass",
					},
					Spec: gwtypes.GatewayClassSpec{
						ParametersRef: &gwtypes.ParametersReference{
							Group:     "gateway-operator.konghq.com",
							Kind:      "GatewayConfiguration",
							Name:      "test-gatewayconfig",
							Namespace: lo.ToPtr(gwtypes.Namespace("default")),
						},
					},
				},
				&gwtypes.GatewayConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-gatewayconfig",
						Namespace: "default",
					},
					Spec: gwtypes.GatewayConfigurationSpec{
						Extensions: []commonv1alpha1.ExtensionRef{
							{
								Group: konnectv1alpha2.SchemeGroupVersion.Group,
								Kind:  konnectv1alpha2.KonnectExtensionKind,
								NamespacedRef: commonv1alpha1.NamespacedRef{
									Name: "test-konnectextension",
								},
							},
						},
					},
				},
				&konnectv1alpha2.KonnectExtension{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-konnectextension",
						Namespace: "default",
					},
					Spec: konnectv1alpha2.KonnectExtensionSpec{
						Konnect: konnectv1alpha2.KonnectExtensionKonnectSpec{
							ControlPlane: konnectv1alpha2.KonnectExtensionControlPlane{
								Ref: commonv1alpha1.KonnectExtensionControlPlaneRef{
									Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
									KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
										Name:      "test-konnectcontrolplane",
										Namespace: "default",
									},
								},
							},
						},
					},
				},
				&konnectv1alpha2.KonnectGatewayControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-konnectcontrolplane",
						Namespace: "default",
					},
					Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: &sdkkonnectcomp.CreateControlPlaneRequest{
							Name: "test-controlplane",
						},
					},
				},
			},
			expectedOutput: []*configurationv1alpha1.KongService{
				{
					TypeMeta: metav1.TypeMeta{
						APIVersion: configurationv1alpha1.GroupVersion.String(),
						Kind:       "KongService",
					},
					Spec: configurationv1alpha1.KongServiceSpec{
						KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
							Name: lo.ToPtr("test-service-80"),
							Port: 80,
							Host: "api.kong-air.com",
						},
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnectcontrolplane",
							},
						},
					},
				},
				{
					TypeMeta: metav1.TypeMeta{
						APIVersion: configurationv1alpha1.GroupVersion.String(),
						Kind:       "KongService",
					},
					Spec: configurationv1alpha1.KongServiceSpec{
						KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
							Name: lo.ToPtr("test-service-443"),
							Port: 443,
							Host: "api.kong-air.com",
						},
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnectcontrolplane",
							},
						},
					},
				},
			},
		},
		{
			name: "multiple HTTPRoutes, multiple ControlPlanes, multiple ports",
			service: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Port: 80,
						},
						{
							Port: 443,
						},
					},
				},
			},
			objects: []client.Object{
				&gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-route-1",
						Namespace: "default",
					},
					Spec: gwtypes.HTTPRouteSpec{
						Rules: []gwtypes.HTTPRouteRule{
							{
								BackendRefs: []gwtypes.HTTPBackendRef{
									{
										BackendRef: gwtypes.BackendRef{
											BackendObjectReference: gwtypes.BackendObjectReference{
												Name: "test-service",
												Port: lo.ToPtr(gwtypes.PortNumber(80)),
											},
										},
									},
									{
										BackendRef: gwtypes.BackendRef{
											BackendObjectReference: gwtypes.BackendObjectReference{
												Name: "test-service",
												Port: lo.ToPtr(gwtypes.PortNumber(8080)),
											},
										},
									},
								},
							},
						},
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{
									Group: lo.ToPtr(gwtypes.Group("gateway.networking.k8s.io")),
									Kind:  lo.ToPtr(gwtypes.Kind("Gateway")),
									Name:  "test-gateway",
								},
							},
						},
					},
				},
				&gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-route-2",
						Namespace: "default",
					},
					Spec: gwtypes.HTTPRouteSpec{
						Rules: []gwtypes.HTTPRouteRule{
							{
								BackendRefs: []gwtypes.HTTPBackendRef{
									{
										BackendRef: gwtypes.BackendRef{
											BackendObjectReference: gwtypes.BackendObjectReference{
												Name: "test-service",
												Port: lo.ToPtr(gwtypes.PortNumber(443)),
											},
										},
									},
									{
										BackendRef: gwtypes.BackendRef{
											BackendObjectReference: gwtypes.BackendObjectReference{
												Name: "test-service",
												Port: lo.ToPtr(gwtypes.PortNumber(8443)),
											},
										},
									},
								},
							},
						},
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{
									Group: lo.ToPtr(gwtypes.Group("gateway.networking.k8s.io")),
									Kind:  lo.ToPtr(gwtypes.Kind("Gateway")),
									Name:  "test-gateway-2",
								},
							},
						},
					},
				},
				&gwtypes.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-gateway",
						Namespace: "default",
					},
					Spec: gwtypes.GatewaySpec{
						GatewayClassName: "test-gatewayclass",
					},
				},
				&gwtypes.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-gateway-2",
						Namespace: "default",
					},
					Spec: gwtypes.GatewaySpec{
						GatewayClassName: "test-gatewayclass-2",
					},
				},
				&gwtypes.GatewayClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-gatewayclass",
					},
					Spec: gwtypes.GatewayClassSpec{
						ParametersRef: &gwtypes.ParametersReference{
							Group:     "gateway-operator.konghq.com",
							Kind:      "GatewayConfiguration",
							Name:      "test-gatewayconfig",
							Namespace: lo.ToPtr(gwtypes.Namespace("default")),
						},
					},
				},
				&gwtypes.GatewayClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-gatewayclass-2",
					},
					Spec: gwtypes.GatewayClassSpec{
						ParametersRef: &gwtypes.ParametersReference{
							Group:     "gateway-operator.konghq.com",
							Kind:      "GatewayConfiguration",
							Name:      "test-gatewayconfig-2",
							Namespace: lo.ToPtr(gwtypes.Namespace("default")),
						},
					},
				},
				&gwtypes.GatewayConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-gatewayconfig",
						Namespace: "default",
					},
					Spec: gwtypes.GatewayConfigurationSpec{
						Extensions: []commonv1alpha1.ExtensionRef{
							{
								Group: konnectv1alpha2.SchemeGroupVersion.Group,
								Kind:  konnectv1alpha2.KonnectExtensionKind,
								NamespacedRef: commonv1alpha1.NamespacedRef{
									Name: "test-konnectextension",
								},
							},
						},
					},
				},
				&gwtypes.GatewayConfiguration{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-gatewayconfig-2",
						Namespace: "default",
					},
					Spec: gwtypes.GatewayConfigurationSpec{
						Extensions: []commonv1alpha1.ExtensionRef{
							{
								Group: konnectv1alpha2.SchemeGroupVersion.Group,
								Kind:  konnectv1alpha2.KonnectExtensionKind,
								NamespacedRef: commonv1alpha1.NamespacedRef{
									Name: "test-konnectextension-2",
								},
							},
						},
					},
				},
				&konnectv1alpha2.KonnectExtension{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-konnectextension",
						Namespace: "default",
					},
					Spec: konnectv1alpha2.KonnectExtensionSpec{
						Konnect: konnectv1alpha2.KonnectExtensionKonnectSpec{
							ControlPlane: konnectv1alpha2.KonnectExtensionControlPlane{
								Ref: commonv1alpha1.KonnectExtensionControlPlaneRef{
									Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
									KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
										Name:      "test-konnectcontrolplane",
										Namespace: "default",
									},
								},
							},
						},
					},
				},
				&konnectv1alpha2.KonnectExtension{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-konnectextension-2",
						Namespace: "default",
					},
					Spec: konnectv1alpha2.KonnectExtensionSpec{
						Konnect: konnectv1alpha2.KonnectExtensionKonnectSpec{
							ControlPlane: konnectv1alpha2.KonnectExtensionControlPlane{
								Ref: commonv1alpha1.KonnectExtensionControlPlaneRef{
									Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
									KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
										Name:      "test-konnectcontrolplane-2",
										Namespace: "default",
									},
								},
							},
						},
					},
				},
				&konnectv1alpha2.KonnectGatewayControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-konnectcontrolplane",
						Namespace: "default",
					},
					Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: &sdkkonnectcomp.CreateControlPlaneRequest{
							Name: "test-controlplane",
						},
					},
				},
				&konnectv1alpha2.KonnectGatewayControlPlane{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-konnectcontrolplane-2",
						Namespace: "default",
					},
					Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
						CreateControlPlaneRequest: &sdkkonnectcomp.CreateControlPlaneRequest{
							Name: "test-controlplane",
						},
					},
				},
			},
			expectedOutput: []*configurationv1alpha1.KongService{
				{
					TypeMeta: metav1.TypeMeta{
						APIVersion: configurationv1alpha1.GroupVersion.String(),
						Kind:       "KongService",
					},
					Spec: configurationv1alpha1.KongServiceSpec{
						KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
							Name: lo.ToPtr("test-service-80"),
							Port: 80,
							Host: "api.kong-air.com",
						},
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnectcontrolplane",
							},
						},
					},
				},
				{
					TypeMeta: metav1.TypeMeta{
						APIVersion: configurationv1alpha1.GroupVersion.String(),
						Kind:       "KongService",
					},
					Spec: configurationv1alpha1.KongServiceSpec{
						KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
							Name: lo.ToPtr("test-service-443"),
							Port: 443,
							Host: "api.kong-air.com",
						},
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnectcontrolplane",
							},
						},
					},
				},
				{
					TypeMeta: metav1.TypeMeta{
						APIVersion: configurationv1alpha1.GroupVersion.String(),
						Kind:       "KongService",
					},
					Spec: configurationv1alpha1.KongServiceSpec{
						KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
							Name: lo.ToPtr("test-service-80"),
							Port: 80,
							Host: "api.kong-air.com",
						},
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnectcontrolplane-2",
							},
						},
					},
				},
				{
					TypeMeta: metav1.TypeMeta{
						APIVersion: configurationv1alpha1.GroupVersion.String(),
						Kind:       "KongService",
					},
					Spec: configurationv1alpha1.KongServiceSpec{
						KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
							Name: lo.ToPtr("test-service-443"),
							Port: 443,
							Host: "api.kong-air.com",
						},
						ControlPlaneRef: &commonv1alpha1.ControlPlaneRef{
							Type: commonv1alpha1.ControlPlaneRefKonnectNamespacedRef,
							KonnectNamespacedRef: &commonv1alpha1.KonnectNamespacedRef{
								Name: "test-konnectcontrolplane-2",
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			clBuilder := fake.NewClientBuilder().
				WithScheme(scheme.Get()).
				WithObjects(&tc.service).
				WithObjects(tc.objects...)

			for _, index := range index.OptionsForHTTPRoute() {
				clBuilder.WithIndex(index.Object, index.Field, index.ExtractValueFn)
			}

			cl := clBuilder.Build()

			statusMap := route.NewSharedStatusMap()
			serviceConverter := newServiceConverter(&tc.service, cl, statusMap)
			for _, svc := range tc.expectedOutput {
				hashSpec := utils.Hash64(svc.Spec)
				require.NoError(t, utils.SetMetadata(&tc.service, svc, hashSpec, "", ""))
			}

			expectedUnstructured := make([]unstructured.Unstructured, len(tc.expectedOutput))
			for i, obj := range tc.expectedOutput {
				u, err := utils.ToUnstructured(obj, cl.Scheme())
				require.NoError(t, err)
				expectedUnstructured[i] = u
			}

			err := serviceConverter.Translate()
			if tc.expectedErr != nil {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			store := serviceConverter.GetOutputStore(context.Background())

			// TODO(mlavacca): change this test to check for equality of the
			// contents of store and expectedUnstructured
			require.Len(t, store, len(tc.expectedOutput))
		})
	}
}
