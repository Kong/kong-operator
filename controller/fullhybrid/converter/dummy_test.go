package converter

import (
	"context"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/configuration/v1alpha1"

	"github.com/kong/kong-operator/controller/fullhybrid/utils"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/modules/manager/scheme"
)

func TestDummyTranslation(t *testing.T) {
	testCases := []struct {
		name           string
		service        corev1.Service
		httpRoutes     []client.Object
		expectedOutput []client.Object
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
			expectedOutput: []client.Object{},
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
			expectedOutput: []client.Object{
				&configurationv1alpha1.KongService{
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
			expectedOutput: []client.Object{
				&configurationv1alpha1.KongService{
					Spec: configurationv1alpha1.KongServiceSpec{
						KongServiceAPISpec: configurationv1alpha1.KongServiceAPISpec{
							Name: lo.ToPtr("test-service-80"),
							Port: 80,
						},
					},
				},
				&configurationv1alpha1.KongService{
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

			dummyConverter := NewDummyConverter(cl)
			dummyConverter.SetRootObject(&tc.service)

			for _, svc := range tc.expectedOutput {
				require.NoError(t, dummyConverter.setMetadata(svc.(*configurationv1alpha1.KongService)))
			}
			expectedUnstructured := make([]unstructured.Unstructured, len(tc.expectedOutput))
			for i, obj := range tc.expectedOutput {
				u, err := utils.ToUnstructured(obj)
				require.NoError(t, err)
				expectedUnstructured[i] = u
			}

			require.NoError(t, dummyConverter.LoadInputStore(context.Background()))
			require.NoError(t, dummyConverter.Translate())
			require.ElementsMatch(t, expectedUnstructured, dummyConverter.GetOutputStore(context.Background()))
		})
	}
}
