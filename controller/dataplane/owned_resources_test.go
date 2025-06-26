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

	"github.com/kong/kong-operator/controller/pkg/builder"
	"github.com/kong/kong-operator/controller/pkg/dataplane"
	"github.com/kong/kong-operator/controller/pkg/op"
	"github.com/kong/kong-operator/pkg/consts"
	k8sutils "github.com/kong/kong-operator/pkg/utils/kubernetes"
	k8sresources "github.com/kong/kong-operator/pkg/utils/kubernetes/resources"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
)

func TestEnsureIngressServiceForDataPlane(t *testing.T) {
	testCases := []struct {
		name                     string
		dataplane                *operatorv1beta1.DataPlane
		additionalLabels         map[string]string
		existingServiceModifier  func(*testing.T, context.Context, client.Client, *corev1.Service)
		expectedCreatedOrUpdated op.Result
		expectedServiceType      corev1.ServiceType
		expectedServiceName      string
		expectedServicePorts     []corev1.ServicePort
		expectedAnnotations      map[string]string
		expectedLabels           map[string]string
	}{
		{
			name: "should create a new service if service does not exist",
			dataplane: builder.
				NewDataPlaneBuilder().
				WithObjectMeta(metav1.ObjectMeta{
					Namespace: "default",
					Name:      "dp-1",
				}).
				WithIngressServiceType(corev1.ServiceTypeLoadBalancer).
				Build(),
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
			dataplane: builder.
				NewDataPlaneBuilder().
				WithObjectMeta(metav1.ObjectMeta{
					Namespace: "default",
					Name:      "dp-1",
				}).
				WithIngressServiceType(corev1.ServiceTypeLoadBalancer).
				Build(),
			expectedCreatedOrUpdated: op.Noop,
			expectedServiceType:      corev1.ServiceTypeLoadBalancer,
			expectedServicePorts:     k8sresources.DefaultDataPlaneIngressServicePorts,
		},
		{
			name: "should add annotations to existing service",
			dataplane: builder.
				NewDataPlaneBuilder().
				WithObjectMeta(metav1.ObjectMeta{
					Namespace: "default",
					Name:      "dp-1",
				}).
				WithIngressServiceType(corev1.ServiceTypeLoadBalancer).
				WithIngressServiceAnnotations(map[string]string{"foo": "bar"}).
				Build(),
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
			dataplane: builder.
				NewDataPlaneBuilder().
				WithObjectMeta(metav1.ObjectMeta{
					Namespace: "default",
					Name:      "dp-1",
				}).
				WithIngressServiceType(corev1.ServiceTypeLoadBalancer).
				WithIngressServiceAnnotations(map[string]string{"foo": "bar"}).
				Build(),
			expectedCreatedOrUpdated: op.Updated,
			expectedServiceType:      corev1.ServiceTypeLoadBalancer,
			expectedServicePorts:     k8sresources.DefaultDataPlaneIngressServicePorts,
			expectedAnnotations: map[string]string{
				"foo":                       "bar",
				"foo2":                      "bar2", // This one should be preserved as some other controller might have added it.
				"added-by-other-controller": "just-preserve-it",
				// should be annotated with last applied annotations
				consts.AnnotationLastAppliedAnnotations: `{"foo":"bar"}`,
			},
		},
		{
			name:             "should create service when service does not contain additional labels",
			additionalLabels: map[string]string{"foo": "bar"},
			dataplane: builder.
				NewDataPlaneBuilder().
				WithObjectMeta(metav1.ObjectMeta{
					Namespace: "default",
					Name:      "dp-1",
				}).
				WithIngressServiceType(corev1.ServiceTypeLoadBalancer).
				Build(),
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
			dataplane: builder.
				NewDataPlaneBuilder().
				WithObjectMeta(metav1.ObjectMeta{
					Namespace: "default",
					Name:      "dp-1",
				}).
				WithIngressServiceType(corev1.ServiceTypeLoadBalancer).
				WithIngressServicePorts([]operatorv1beta1.DataPlaneServicePort{
					{
						Name:       "http",
						Port:       8080,
						TargetPort: intstr.FromInt(8000),
					},
				}).
				Build(),
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
		{
			name: "should not need to update the service (LB) when it already has the cluster external traffic policy",
			dataplane: builder.
				NewDataPlaneBuilder().
				WithObjectMeta(metav1.ObjectMeta{
					Namespace: "default",
					Name:      "dp-1",
				}).
				WithIngressServiceType(corev1.ServiceTypeLoadBalancer).
				Build(),
			existingServiceModifier: func(t *testing.T, ctx context.Context, c client.Client, svc *corev1.Service) {
				svc.Spec.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicyTypeCluster
				require.NoError(t, c.Update(ctx, svc))
			},
			expectedCreatedOrUpdated: op.Noop,
			expectedServiceType:      corev1.ServiceTypeLoadBalancer,
			expectedServicePorts:     k8sresources.DefaultDataPlaneIngressServicePorts,
		},
		{
			name: "should not need to update the service (LB) when it already has the cluster external traffic policy and dp spec has the same",
			dataplane: builder.
				NewDataPlaneBuilder().
				WithObjectMeta(metav1.ObjectMeta{
					Namespace: "default",
					Name:      "dp-1",
				}).
				WithIngressServiceType(corev1.ServiceTypeLoadBalancer).
				WithIngressServiceExternalTrafficPolicy(corev1.ServiceExternalTrafficPolicyCluster).
				Build(),
			existingServiceModifier: func(t *testing.T, ctx context.Context, c client.Client, svc *corev1.Service) {
				svc.Spec.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicyCluster
				require.NoError(t, c.Update(ctx, svc))
			},
			expectedCreatedOrUpdated: op.Noop,
			expectedServiceType:      corev1.ServiceTypeLoadBalancer,
			expectedServicePorts:     k8sresources.DefaultDataPlaneIngressServicePorts,
		},
		{
			name: "should update the service (LB) when it has the cluster external traffic policy and dp spec has local",
			dataplane: builder.
				NewDataPlaneBuilder().
				WithObjectMeta(metav1.ObjectMeta{
					Namespace: "default",
					Name:      "dp-1",
				}).
				WithIngressServiceType(corev1.ServiceTypeLoadBalancer).
				WithIngressServiceExternalTrafficPolicy(corev1.ServiceExternalTrafficPolicyLocal).
				Build(),
			existingServiceModifier: func(t *testing.T, ctx context.Context, c client.Client, svc *corev1.Service) {
				svc.Spec.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicyCluster
				require.NoError(t, c.Update(ctx, svc))
			},
			expectedCreatedOrUpdated: op.Updated,
			expectedServiceType:      corev1.ServiceTypeLoadBalancer,
			expectedServicePorts:     k8sresources.DefaultDataPlaneIngressServicePorts,
		},
		{
			name: "should update the service (LB) when it has the local external traffic policy and dp spec not specified it",
			dataplane: builder.
				NewDataPlaneBuilder().
				WithObjectMeta(metav1.ObjectMeta{
					Namespace: "default",
					Name:      "dp-1",
				}).
				WithIngressServiceType(corev1.ServiceTypeLoadBalancer).
				Build(),
			existingServiceModifier: func(t *testing.T, ctx context.Context, c client.Client, svc *corev1.Service) {
				svc.Spec.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicyLocal
				require.NoError(t, c.Update(ctx, svc))
			},
			expectedCreatedOrUpdated: op.Updated,
			expectedServiceType:      corev1.ServiceTypeLoadBalancer,
			expectedServicePorts:     k8sresources.DefaultDataPlaneIngressServicePorts,
		},
		{
			name: "should not need to update the service (LB) when it has the local external traffic policy and dp spec has also local",
			dataplane: builder.
				NewDataPlaneBuilder().
				WithObjectMeta(metav1.ObjectMeta{
					Namespace: "default",
					Name:      "dp-1",
				}).
				WithIngressServiceType(corev1.ServiceTypeLoadBalancer).
				WithIngressServiceExternalTrafficPolicy(corev1.ServiceExternalTrafficPolicyLocal).
				Build(),
			existingServiceModifier: func(t *testing.T, ctx context.Context, c client.Client, svc *corev1.Service) {
				svc.Spec.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicyLocal
				require.NoError(t, c.Update(ctx, svc))
			},
			expectedCreatedOrUpdated: op.Noop,
			expectedServiceType:      corev1.ServiceTypeLoadBalancer,
			expectedServicePorts:     k8sresources.DefaultDataPlaneIngressServicePorts,
		},
		{
			name: "should create service with specified name if name is specified",
			dataplane: builder.NewDataPlaneBuilder().
				WithObjectMeta(metav1.ObjectMeta{
					Namespace: "default",
					Name:      "ingress-service-specified",
				}).WithIngressServiceName("ingress-service-1").
				WithIngressServiceType(corev1.ServiceTypeLoadBalancer).
				Build(),
			existingServiceModifier: func(t *testing.T, ctx context.Context, c client.Client, svc *corev1.Service) {
				require.NoError(t, dataplane.OwnedObjectPreDeleteHook(ctx, c, svc))
				require.NoError(t, c.Delete(ctx, svc))
			},
			expectedCreatedOrUpdated: op.Created,
			expectedServiceType:      corev1.ServiceTypeLoadBalancer,
			expectedServiceName:      "ingress-service-1",
			expectedServicePorts:     k8sresources.DefaultDataPlaneIngressServicePorts,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeClient := fakectrlruntimeclient.
				NewClientBuilder().
				WithScheme(scheme.Scheme).
				Build()

			ctx := t.Context()
			existingSvc, err := k8sresources.GenerateNewIngressServiceForDataPlane(tc.dataplane)
			require.NoError(t, err)
			k8sutils.SetOwnerForObject(existingSvc, tc.dataplane)
			require.NoError(t, fakeClient.Create(ctx, existingSvc))
			if tc.existingServiceModifier != nil {
				tc.existingServiceModifier(t, ctx, fakeClient, existingSvc)
			}
			// create dataplane resource.
			require.NoError(t, fakeClient.Create(ctx, tc.dataplane), "should create dataplane successfully")
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
			// check service name.
			if tc.expectedServiceName != "" {
				require.Equal(t, tc.expectedServiceName, svc.Name, "should have the same name")
			}
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

func TestComparePorts(t *testing.T) {
	testCases := []struct {
		name      string
		a         []corev1.ServicePort
		b         []corev1.ServicePort
		dataPlane *operatorv1beta1.DataPlane
		expected  bool
	}{
		{
			name: "should return true when NodePort differs but not specified in DataPlane spec",
			a:    []corev1.ServicePort{{Name: "port-80", Port: 80, NodePort: 30080}},
			b:    []corev1.ServicePort{{Name: "port-80", Port: 80, NodePort: 30081}},
			dataPlane: builder.NewDataPlaneBuilder().
				WithIngressServicePorts([]operatorv1beta1.DataPlaneServicePort{
					{
						Name:       "http",
						Port:       80,
						TargetPort: intstr.FromInt(8000),
						// NodePort not specified
					},
				}).Build(),
			expected: true,
		},
		{
			name: "should return false when NodePort differs and is specified in DataPlane spec",
			a:    []corev1.ServicePort{{Name: "port-80", Port: 80, NodePort: 30080}},
			b:    []corev1.ServicePort{{Name: "port-80", Port: 80, NodePort: 30081}},
			dataPlane: builder.NewDataPlaneBuilder().
				WithIngressServicePorts([]operatorv1beta1.DataPlaneServicePort{
					{
						Name:       "http",
						Port:       80,
						TargetPort: intstr.FromInt(8000),
						NodePort:   30080,
					},
				}).Build(),
			expected: false,
		},
		{
			name: "should return true when multiple ports match except NodePort which is not specified",
			a: []corev1.ServicePort{
				{Name: "port-80", Port: 80, NodePort: 30080},
				{Name: "port-443", Port: 443, NodePort: 30443},
			},
			b: []corev1.ServicePort{
				{Name: "port-80", Port: 80, NodePort: 30081},
				{Name: "port-443", Port: 443, NodePort: 30444},
			},
			dataPlane: builder.NewDataPlaneBuilder().
				WithIngressServicePorts([]operatorv1beta1.DataPlaneServicePort{
					{
						Name:       "http",
						Port:       80,
						TargetPort: intstr.FromInt(8000),
					},
					{
						Name:       "https",
						Port:       443,
						TargetPort: intstr.FromInt(8443),
					},
				}).Build(),
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual := comparePorts(tc.a, tc.b, tc.dataPlane)
			require.Equal(t, tc.expected, actual)
		})
	}
}
