package converter_test

import (
	"context"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1alpha1"

	"github.com/kong/kong-operator/controller/fullhybrid/converter"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/modules/manager/scheme"
)

func TestDummyConverter(t *testing.T) {

	testCases := []struct {
		name           string
		service        corev1.Service
		httpRoutes     []client.Object
		expectedOutput []configurationv1alpha1.KongService
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
			httpRoutes: []client.Object{
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
			expectedOutput: []configurationv1alpha1.KongService{},
		},
		{
			name: "service with matching port",
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
			httpRoutes: []client.Object{
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
			expectedOutput: []configurationv1alpha1.KongService{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-service-80",
						Namespace: "default",
					},
					Spec: configurationv1alpha1.KongServiceSpec{
						KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
							Name: lo.ToPtr("test-service-80"),
							Port: 80,
						},
					},
				},
			},
		},
		{
			name: "multiple HTTPRoutes, multiple ports",
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
			httpRoutes: []client.Object{
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
					},
				},
			},
			expectedOutput: []configurationv1alpha1.KongService{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-service-80",
						Namespace: "default",
					},
					Spec: configurationv1alpha1.KongServiceSpec{
						KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
							Name: lo.ToPtr("test-service-80"),
							Port: 80,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-service-443",
						Namespace: "default",
					},
					Spec: configurationv1alpha1.KongServiceSpec{
						KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
							Name: lo.ToPtr("test-service-443"),
							Port: 443,
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cl := fake.NewClientBuilder().
				WithScheme(scheme.Get()).
				WithObjects(&tc.service).
				WithObjects(tc.httpRoutes...).
				Build()

			dummyConverter := converter.NewDummyConverter(cl)
			dummyConverter.SetRootObject(tc.service)
			require.NoError(t, dummyConverter.LoadStore(context.Background()))
			require.NoError(t, dummyConverter.Translate())
			require.EqualValues(t, tc.expectedOutput, dummyConverter.DumpOutputStore())
		})
	}
}
