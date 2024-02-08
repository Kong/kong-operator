package dataplane

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	operatorv1beta1 "github.com/kong/gateway-operator/apis/v1beta1"
	"github.com/kong/gateway-operator/controllers/pkg/builder"
	"github.com/kong/gateway-operator/controllers/pkg/dataplane"
	"github.com/kong/gateway-operator/controllers/pkg/op"
	"github.com/kong/gateway-operator/internal/consts"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
	k8sresources "github.com/kong/gateway-operator/internal/utils/kubernetes/resources"
)

func TestEnsureIngressServiceForDataPlane(t *testing.T) {
	testCases := []struct {
		name                     string
		dataplane                *operatorv1beta1.DataPlane
		additionalLabels         map[string]string
		existingServiceModifier  func(*testing.T, context.Context, client.Client, *corev1.Service)
		expectedCreatedOrUpdated op.CreatedUpdatedOrNoop
		expectedServiceType      corev1.ServiceType
		expectedServicePorts     []corev1.ServicePort
		expectedAnnotations      map[string]string
		expectedLabels           map[string]string
	}{
		{
			name: "should create a new service if service does not exist",
			dataplane: builder.NewDataPlaneBuilder().WithObjectMeta(metav1.ObjectMeta{
				Namespace: "default",
				Name:      "dp-1",
			}).WithIngressServiceType(corev1.ServiceTypeLoadBalancer).Build(),
			existingServiceModifier: func(t *testing.T, ctx context.Context, c client.Client, svc *corev1.Service) {
				require.NoError(t, dataplane.OwnedObjectPreDeleteHook(ctx, c, svc))
				require.NoError(t, c.Delete(ctx, svc))
			},
			expectedCreatedOrUpdated: op.Created,
			expectedServiceType:      corev1.ServiceTypeLoadBalancer,
			expectedServicePorts:     k8sresources.DefaultDataPlaneIngressServicePorts,
		},
		{
			name: "should not update when a service exists",
			dataplane: builder.NewDataPlaneBuilder().WithObjectMeta(metav1.ObjectMeta{
				Namespace: "default",
				Name:      "dp-1",
			}).WithIngressServiceType(corev1.ServiceTypeLoadBalancer).Build(),
			expectedCreatedOrUpdated: op.Noop,
			expectedServiceType:      corev1.ServiceTypeLoadBalancer,
			expectedServicePorts:     k8sresources.DefaultDataPlaneIngressServicePorts,
		},
		{
			name: "should add annotations to existing service",
			dataplane: builder.NewDataPlaneBuilder().WithObjectMeta(metav1.ObjectMeta{
				Namespace: "default",
				Name:      "dp-1",
			}).WithIngressServiceType(corev1.ServiceTypeLoadBalancer).
				WithIngressServiceAnnotations(map[string]string{"foo": "bar"}).Build(),
			existingServiceModifier: func(t *testing.T, ctx context.Context, c client.Client, svc *corev1.Service) {
				svc.Annotations = nil
				require.NoError(t, c.Update(ctx, svc))
			},
			expectedCreatedOrUpdated: op.Updated,
			expectedServiceType:      corev1.ServiceTypeLoadBalancer,
			expectedServicePorts:     k8sresources.DefaultDataPlaneIngressServicePorts,
			expectedAnnotations: map[string]string{
				"foo": "bar",
				// should be annotated with last applied annotations
				consts.AnnotationLastAppliedAnnotations: `{"foo":"bar"}`,
			},
		},
		{
			name: "should remove outdated annotations",
			existingServiceModifier: func(t *testing.T, ctx context.Context, c client.Client, svc *corev1.Service) {
				svc.Annotations = map[string]string{
					"foo":                                   "bar",
					"foo2":                                  "bar2",
					"added-by-other-controller":             "just-preserve-it",
					consts.AnnotationLastAppliedAnnotations: `{"foo":"bar","foo2":"bar2"}`,
				}
				require.NoError(t, c.Update(ctx, svc))
			},
			dataplane: builder.NewDataPlaneBuilder().WithObjectMeta(metav1.ObjectMeta{
				Namespace: "default",
				Name:      "dp-1",
			}).WithIngressServiceType(corev1.ServiceTypeLoadBalancer).
				WithIngressServiceAnnotations(map[string]string{"foo": "bar"}).Build(),
			expectedCreatedOrUpdated: op.Updated,
			expectedServiceType:      corev1.ServiceTypeLoadBalancer,
			expectedServicePorts:     k8sresources.DefaultDataPlaneIngressServicePorts,
			expectedAnnotations: map[string]string{
				"foo": "bar",
				// "foo2":                      "bar2", // this one should be removed
				"added-by-other-controller": "just-preserve-it",
				// should be annotated with last applied annotations
				consts.AnnotationLastAppliedAnnotations: `{"foo":"bar"}`,
			},
		},
		{
			name:             "should create service when service does not contain additional labels",
			additionalLabels: map[string]string{"foo": "bar"},
			dataplane: builder.NewDataPlaneBuilder().WithObjectMeta(metav1.ObjectMeta{
				Namespace: "default",
				Name:      "dp-1",
			}).WithIngressServiceType(corev1.ServiceTypeLoadBalancer).Build(),
			existingServiceModifier: func(t *testing.T, ctx context.Context, c client.Client, svc *corev1.Service) {
				if svc.Labels != nil {
					delete(svc.Labels, "foo")
				}
				require.NoError(t, c.Update(ctx, svc))
			},
			expectedCreatedOrUpdated: op.Created,
			expectedServiceType:      corev1.ServiceTypeLoadBalancer,
			expectedServicePorts:     k8sresources.DefaultDataPlaneIngressServicePorts,
			expectedLabels:           map[string]string{"foo": "bar"},
		},
		{
			name: "should update ports",
			dataplane: builder.NewDataPlaneBuilder().WithObjectMeta(metav1.ObjectMeta{
				Namespace: "default",
				Name:      "dp-1",
			}).WithIngressServiceType(corev1.ServiceTypeLoadBalancer).WithIngressServicePorts([]operatorv1beta1.DataPlaneServicePort{
				{
					Name:       "http",
					Port:       8080,
					TargetPort: intstr.FromInt(8000),
				},
			}).Build(),
			expectedCreatedOrUpdated: op.Updated,
			expectedServiceType:      corev1.ServiceTypeLoadBalancer,
			expectedServicePorts: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       8080,
					TargetPort: intstr.FromInt(8000),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			fakeClient := fakectrlruntimeclient.
				NewClientBuilder().
				WithScheme(scheme.Scheme).
				Build()

			ctx := context.Background()
			existingSvc, err := k8sresources.GenerateNewIngressServiceForDataPlane(tc.dataplane)
			require.NoError(t, err)
			k8sutils.SetOwnerForObject(existingSvc, tc.dataplane)
			err = fakeClient.Create(ctx, existingSvc)
			require.NoError(t, err)
			if tc.existingServiceModifier != nil {
				tc.existingServiceModifier(t, ctx, fakeClient, existingSvc)
			}
			// create dataplane resource.
			err = fakeClient.Create(ctx, tc.dataplane)
			require.NoError(t, err, "should create dataplane successfully")
			res, svc, err := ensureIngressServiceForDataPlane(
				ctx,
				logr.Discard(),
				fakeClient,
				tc.dataplane,
				tc.additionalLabels,
				k8sresources.ServicePortsFromDataPlaneIngressOpt(tc.dataplane),
			)
			require.NoError(t, err)
			require.Equal(t, tc.expectedCreatedOrUpdated, res)
			// check service type.
			require.Equal(t, tc.expectedServiceType, svc.Spec.Type, "should have the same service type")
			// check service ports.
			require.Equal(t, tc.expectedServicePorts, svc.Spec.Ports, "should have the same service ports")
			// check service annotations.
			require.Equal(t, tc.expectedAnnotations, svc.Annotations, "should have the same annotations")
			// check service labels.
			for k, v := range tc.expectedLabels {
				actualValue := svc.Labels[k]
				require.Equalf(t, v, actualValue, "should have label %s:%s in service", k, v)
			}
		})
	}
}
