package dataplane

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	"github.com/kong/kong-operator/v2/controller/pkg/builder"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	"github.com/kong/kong-operator/v2/pkg/consts"
	"github.com/kong/kong-operator/v2/pkg/ipfamily"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
	k8sreduce "github.com/kong/kong-operator/v2/pkg/utils/kubernetes/reduce"
	k8sresources "github.com/kong/kong-operator/v2/pkg/utils/kubernetes/resources"
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
				require.NoError(t, k8sreduce.OwnedObjectPreDeleteHook(ctx, c, svc))
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
				"foo": "bar",
				// "foo2" was tracked in last-applied-annotations and removed from the
				// DataPlane spec, so it should be removed from the service.
				// "added-by-other-controller" was NOT tracked in last-applied, so it
				// is preserved (it was set by something other than the operator).
				"added-by-other-controller":             "just-preserve-it",
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
				require.NoError(t, k8sreduce.OwnedObjectPreDeleteHook(ctx, c, svc))
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

func TestEnsureIngressServiceForDataPlaneIPFamily(t *testing.T) {
	testCases := []struct {
		name string
		// operator's IP family, fed to IPFamilyPolicyServiceOpt.
		ipFamily ipfamily.IPFamily
		// ipFamilyPolicy set in the DataPlane spec (nil = not set).
		dataplanePolicy *corev1.IPFamilyPolicy
		// ipFamilies set in the DataPlane spec (nil = not set).
		dataplaneFamilies []corev1.IPFamily
		// existingPolicy and existingPolicySet control the ipFamilyPolicy of
		// the existing Service before reconcile: existingPolicySet=false leaves
		// the Service as generated; true sets it to existingPolicy (nil
		// included, since the fake client applies no defaulting).
		existingPolicy    *corev1.IPFamilyPolicy
		existingPolicySet bool
		// existingFamilies and existingFamiliesSet control the ipFamilies of
		// the existing Service before reconcile, e.g. to simulate the API
		// server expanding a dual-policy Service with the secondary family.
		existingFamilies    []corev1.IPFamily
		existingFamiliesSet bool
		expectedResult      op.Result
		expectedSvcPolicy   *corev1.IPFamilyPolicy
		expectedSvcIPs      []corev1.IPFamily
	}{
		{
			name:              "should not update when the existing non-default ipFamilyPolicy equals the generated one",
			dataplanePolicy:   new(corev1.IPFamilyPolicyPreferDualStack),
			expectedResult:    op.Noop,
			expectedSvcPolicy: new(corev1.IPFamilyPolicyPreferDualStack),
		},
		{
			name:              "should leave the existing ipFamilyPolicy untouched when the generated Service sets no policy",
			existingPolicy:    new(corev1.IPFamilyPolicyRequireDualStack),
			existingPolicySet: true,
			expectedResult:    op.Noop,
			expectedSvcPolicy: new(corev1.IPFamilyPolicyRequireDualStack),
		},
		{
			name:              "should update ipFamilyPolicy when changed in the DataPlane spec",
			dataplanePolicy:   new(corev1.IPFamilyPolicyPreferDualStack),
			existingPolicy:    new(corev1.IPFamilyPolicyRequireDualStack),
			existingPolicySet: true,
			expectedResult:    op.Updated,
			expectedSvcPolicy: new(corev1.IPFamilyPolicyPreferDualStack),
		},
		{
			name:              "should not update when ipFamilyPolicy is unset in both the DataPlane spec and the Service",
			existingPolicySet: true,
			expectedResult:    op.Noop,
		},
		{
			name:              "should apply ipFamilyPolicy when set in the DataPlane spec and unset on the Service",
			dataplanePolicy:   new(corev1.IPFamilyPolicyPreferDualStack),
			existingPolicySet: true,
			expectedResult:    op.Updated,
			expectedSvcPolicy: new(corev1.IPFamilyPolicyPreferDualStack),
		},
		{
			name:              "should apply the dual operator default to an existing single-stack Service",
			ipFamily:          ipfamily.Dual,
			existingPolicySet: true,
			expectedResult:    op.Updated,
			expectedSvcPolicy: new(corev1.IPFamilyPolicyPreferDualStack),
		},
		{
			name:              "should not churn when the dual operator default is already applied",
			ipFamily:          ipfamily.Dual,
			existingPolicy:    new(corev1.IPFamilyPolicyPreferDualStack),
			existingPolicySet: true,
			expectedResult:    op.Noop,
			expectedSvcPolicy: new(corev1.IPFamilyPolicyPreferDualStack),
		},
		{
			name:              "should sync user-set ipFamilies without churn when already applied",
			ipFamily:          ipfamily.Dual,
			dataplaneFamilies: []corev1.IPFamily{corev1.IPv4Protocol, corev1.IPv6Protocol},
			existingPolicy:    new(corev1.IPFamilyPolicyPreferDualStack),
			existingPolicySet: true,
			expectedResult:    op.Noop,
			expectedSvcPolicy: new(corev1.IPFamilyPolicyPreferDualStack),
			expectedSvcIPs:    []corev1.IPFamily{corev1.IPv4Protocol, corev1.IPv6Protocol},
		},
		{
			name:              "should apply a single user-set ipFamilies entry as SingleStack on a dual cluster",
			ipFamily:          ipfamily.Dual,
			dataplaneFamilies: []corev1.IPFamily{corev1.IPv4Protocol},
			expectedResult:    op.Noop,
			expectedSvcPolicy: new(corev1.IPFamilyPolicySingleStack),
			expectedSvcIPs:    []corev1.IPFamily{corev1.IPv4Protocol},
		},
		{
			name:                "should preserve the API-defaulted secondary ipFamily when the spec requests a dual policy with a single family",
			ipFamily:            ipfamily.Dual,
			dataplaneFamilies:   []corev1.IPFamily{corev1.IPv4Protocol},
			dataplanePolicy:     new(corev1.IPFamilyPolicyPreferDualStack),
			existingFamilies:    []corev1.IPFamily{corev1.IPv4Protocol, corev1.IPv6Protocol},
			existingFamiliesSet: true,
			expectedResult:      op.Noop,
			expectedSvcPolicy:   new(corev1.IPFamilyPolicyPreferDualStack),
			expectedSvcIPs:      []corev1.IPFamily{corev1.IPv4Protocol, corev1.IPv6Protocol},
		},
		{
			name:                "should release the secondary ipFamily when the spec requests SingleStack with a single family",
			ipFamily:            ipfamily.Dual,
			dataplaneFamilies:   []corev1.IPFamily{corev1.IPv4Protocol},
			dataplanePolicy:     new(corev1.IPFamilyPolicySingleStack),
			existingFamilies:    []corev1.IPFamily{corev1.IPv4Protocol, corev1.IPv6Protocol},
			existingFamiliesSet: true,
			expectedResult:      op.Updated,
			expectedSvcPolicy:   new(corev1.IPFamilyPolicySingleStack),
			expectedSvcIPs:      []corev1.IPFamily{corev1.IPv4Protocol},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dataplane := builder.
				NewDataPlaneBuilder().
				WithObjectMeta(metav1.ObjectMeta{
					Namespace: "default",
					Name:      "dp-1",
				}).
				Build()
			dataplane.Spec.Network = operatorv1beta1.DataPlaneNetworkOptions{
				Services: &operatorv1beta1.DataPlaneServices{
					Ingress: &operatorv1beta1.DataPlaneServiceOptions{
						IPFamilies:     tc.dataplaneFamilies,
						IPFamilyPolicy: tc.dataplanePolicy,
					},
				},
			}

			fakeClient := fakectrlruntimeclient.
				NewClientBuilder().
				WithScheme(scheme.Scheme).
				Build()

			ctx := t.Context()
			existingSvc, err := k8sresources.GenerateNewIngressServiceForDataPlane(dataplane)
			require.NoError(t, err)
			k8sutils.SetOwnerForObject(existingSvc, dataplane)
			require.NoError(t, fakeClient.Create(ctx, existingSvc))
			if tc.existingPolicySet {
				existingSvc.Spec.IPFamilyPolicy = tc.existingPolicy
				require.NoError(t, fakeClient.Update(ctx, existingSvc))
			}
			if tc.existingFamiliesSet {
				existingSvc.Spec.IPFamilies = tc.existingFamilies
				require.NoError(t, fakeClient.Update(ctx, existingSvc))
			}
			require.NoError(t, fakeClient.Create(ctx, dataplane), "should create dataplane successfully")

			res, svc, err := ensureIngressServiceForDataPlane(
				ctx,
				logr.Discard(),
				fakeClient,
				dataplane,
				nil,
				k8sresources.IPFamilyPolicyServiceOpt(tc.ipFamily),
			)
			require.NoError(t, err)
			require.Equal(t, tc.expectedResult, res)
			require.Equal(t, tc.expectedSvcPolicy, svc.Spec.IPFamilyPolicy)
			require.Equal(t, tc.expectedSvcIPs, svc.Spec.IPFamilies)
		})
	}
}

func TestEnsureIngressServiceForDataPlaneClusterIP(t *testing.T) {
	testCases := []struct {
		name string
		// clusterIP of the existing Service, as allocated by the API server.
		existingClusterIP string
		// clusterIP set in the DataPlane spec (nil = not set).
		dataplaneClusterIP *string
		// expectRecreate is true when the existing Service is expected to be
		// deleted (and created on the next reconcile) because spec.clusterIP
		// is immutable.
		expectRecreate    bool
		expectedClusterIP string
	}{
		{
			name:              "should not recreate a Service with an allocated cluster IP when clusterIP is unset",
			existingClusterIP: "10.96.0.10",
			expectedClusterIP: "10.96.0.10",
		},
		{
			name:               "should not recreate a headless Service when clusterIP is None",
			existingClusterIP:  corev1.ClusterIPNone,
			dataplaneClusterIP: new(corev1.ClusterIPNone),
			expectedClusterIP:  corev1.ClusterIPNone,
		},
		{
			name:               "should recreate the Service as headless when clusterIP is set to None",
			existingClusterIP:  "10.96.0.10",
			dataplaneClusterIP: new(corev1.ClusterIPNone),
			expectRecreate:     true,
			expectedClusterIP:  corev1.ClusterIPNone,
		},
		{
			name:              "should recreate the Service as non-headless when clusterIP is unset",
			existingClusterIP: corev1.ClusterIPNone,
			expectRecreate:    true,
			expectedClusterIP: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dataplane := builder.
				NewDataPlaneBuilder().
				WithObjectMeta(metav1.ObjectMeta{
					Namespace: "default",
					Name:      "dp-1",
				}).
				Build()
			dataplane.Spec.Network = operatorv1beta1.DataPlaneNetworkOptions{
				Services: &operatorv1beta1.DataPlaneServices{
					Ingress: &operatorv1beta1.DataPlaneServiceOptions{
						Type:      corev1.ServiceTypeClusterIP,
						ClusterIP: tc.dataplaneClusterIP,
					},
				},
			}

			fakeClient := fakectrlruntimeclient.
				NewClientBuilder().
				WithScheme(scheme.Scheme).
				Build()

			ctx := t.Context()
			existingSvc, err := k8sresources.GenerateNewIngressServiceForDataPlane(dataplane)
			require.NoError(t, err)
			existingSvc.Spec.ClusterIP = tc.existingClusterIP
			k8sutils.SetOwnerForObject(existingSvc, dataplane)
			require.NoError(t, fakeClient.Create(ctx, existingSvc))
			require.NoError(t, fakeClient.Create(ctx, dataplane), "should create dataplane successfully")

			res, svc, err := ensureIngressServiceForDataPlane(ctx, logr.Discard(), fakeClient, dataplane, nil)
			if !tc.expectRecreate {
				require.NoError(t, err)
				require.Equal(t, op.Noop, res)
				require.Equal(t, tc.expectedClusterIP, svc.Spec.ClusterIP)
				return
			}

			require.Error(t, err)
			require.Equal(t, op.Noop, res)
			err = fakeClient.Get(ctx, client.ObjectKeyFromObject(existingSvc), &corev1.Service{})
			require.True(t, apierrors.IsNotFound(err), "existing Service should be deleted, got: %v", err)

			res, svc, err = ensureIngressServiceForDataPlane(ctx, logr.Discard(), fakeClient, dataplane, nil)
			require.NoError(t, err)
			require.Equal(t, op.Created, res)
			require.Equal(t, tc.expectedClusterIP, svc.Spec.ClusterIP)
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

func TestDeleteAdminServicesForDataPlane(t *testing.T) {
	dp := &operatorv1beta1.DataPlane{
		Namespace: "default", Name: "dp-1", UID: "dp-uid",
	}
	ownerRef := func(uid string) []metav1.OwnerReference {
		return []metav1.OwnerReference{{
			APIVersion: operatorv1beta1.SchemeGroupVersion.String(),
			Kind:       "DataPlane",
			Name:       "owner",
			UID:        k8stypes.UID(uid),
		}}
	}
	svc := func(name, ownerUID, svcType, state string) *corev1.Service {
		return &corev1.Service{
			Namespace: "default",
			Name:      name,
			Labels: map[string]string{
				consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
				consts.DataPlaneServiceTypeLabel:     svcType,
				consts.DataPlaneServiceStateLabel:    state,
			},
			OwnerReferences: ownerRef(ownerUID),
			Finalizers:      []string{consts.DataPlaneOwnedWaitForOwnerFinalizer},
		}
	}
	admin := string(consts.DataPlaneAdminServiceLabelValue)
	ingress := string(consts.DataPlaneIngressServiceLabelValue)
	live := consts.DataPlaneStateLabelValueLive
	preview := consts.DataPlaneStateLabelValuePreview

	testCases := []struct {
		name             string
		objects          []client.Object
		additionalLabels client.MatchingLabels
		expectedDeleted  bool
		expectedGone     []string
		expectedKept     []string
	}{
		{
			name: "owned admin service is deleted, others untouched",
			objects: []client.Object{
				svc("admin", "dp-uid", admin, live),
				svc("ingress", "dp-uid", ingress, live),
				svc("other-admin", "other-uid", admin, live),
			},
			additionalLabels: client.MatchingLabels{consts.DataPlaneServiceStateLabel: live},
			expectedDeleted:  true,
			expectedGone:     []string{"admin"},
			expectedKept:     []string{"ingress", "other-admin"},
		},
		{
			name: "live label filter leaves preview admin service",
			objects: []client.Object{
				svc("admin-live", "dp-uid", admin, live),
				svc("admin-preview", "dp-uid", admin, preview),
			},
			additionalLabels: client.MatchingLabels{consts.DataPlaneServiceStateLabel: live},
			expectedDeleted:  true,
			expectedGone:     []string{"admin-live"},
			expectedKept:     []string{"admin-preview"},
		},
		{
			name: "preview label filter leaves live admin service",
			objects: []client.Object{
				svc("admin-live", "dp-uid", admin, live),
				svc("admin-preview", "dp-uid", admin, preview),
			},
			additionalLabels: client.MatchingLabels{consts.DataPlaneServiceStateLabel: preview},
			expectedDeleted:  true,
			expectedGone:     []string{"admin-preview"},
			expectedKept:     []string{"admin-live"},
		},
		{
			name: "nothing to delete",
			objects: []client.Object{
				svc("ingress", "dp-uid", ingress, live),
			},
			additionalLabels: client.MatchingLabels{consts.DataPlaneServiceStateLabel: live},
			expectedDeleted:  false,
			expectedKept:     []string{"ingress"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			cl := fakectrlruntimeclient.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(tc.objects...).
				Build()

			deleted, err := deleteAdminServicesForDataPlane(ctx, cl, dp, tc.additionalLabels)
			require.NoError(t, err)
			require.Equal(t, tc.expectedDeleted, deleted)

			for _, name := range tc.expectedGone {
				err := cl.Get(ctx, client.ObjectKey{Namespace: "default", Name: name}, &corev1.Service{})
				require.True(t, apierrors.IsNotFound(err), "expected Service %s to be deleted", name)
			}
			for _, name := range tc.expectedKept {
				require.NoError(t, cl.Get(ctx, client.ObjectKey{Namespace: "default", Name: name}, &corev1.Service{}))
			}
		})
	}
}

func TestDeleteUnreferencedAdminCertSecrets(t *testing.T) {
	dp := &operatorv1beta1.DataPlane{
		Namespace: "default", Name: "dp-1", UID: "dp-uid",
	}
	ownerRefs := []metav1.OwnerReference{{
		APIVersion: operatorv1beta1.SchemeGroupVersion.String(),
		Kind:       "DataPlane",
		Name:       dp.Name,
		UID:        dp.UID,
	}}
	secret := func(name string, labels map[string]string) *corev1.Secret {
		return &corev1.Secret{
			Namespace:       "default",
			Name:            name,
			Labels:          labels,
			OwnerReferences: ownerRefs,
			Finalizers:      []string{consts.DataPlaneOwnedWaitForOwnerFinalizer},
		}
	}
	adminCertLabels := map[string]string{consts.ServiceSecretLabel: "admin-svc"}
	deployment := func(name string, volumes ...corev1.Volume) *appsv1.Deployment {
		return &appsv1.Deployment{
			Namespace:       "default",
			Name:            name,
			OwnerReferences: ownerRefs,
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{Volumes: volumes},
				},
			},
			Status: appsv1.DeploymentStatus{Replicas: 1, UpdatedReplicas: 1},
		}
	}
	rollingOut := func(d *appsv1.Deployment) *appsv1.Deployment {
		d.Status.Replicas = 2
		return d
	}
	secretVolume := func(secretName string) corev1.Volume {
		return corev1.Volume{
			Name:   secretName,
			Secret: &corev1.SecretVolumeSource{SecretName: secretName},
		}
	}
	projectedVolume := func(secretName string) corev1.Volume {
		return corev1.Volume{
			Name: secretName,
			Projected: &corev1.ProjectedVolumeSource{
				Sources: []corev1.VolumeProjection{{
					Secret: &corev1.SecretProjection{
						Name: secretName,
					},
				}},
			},
		}
	}

	testCases := []struct {
		name         string
		objects      []client.Object
		expectedGone []string
		expectedKept []string
	}{
		{
			name: "referenced kept, unreferenced deleted, unlabeled untouched",
			objects: []client.Object{
				secret("referenced", adminCertLabels),
				secret("unreferenced", adminCertLabels),
				secret("no-label", nil),
				deployment("dp-live", secretVolume("referenced"), secretVolume("no-label")),
			},
			expectedGone: []string{"unreferenced"},
			expectedKept: []string{"referenced", "no-label"},
		},
		{
			name: "secrets referenced by preview deployment or projected volume are kept",
			objects: []client.Object{
				secret("live-cert", adminCertLabels),
				secret("preview-cert", adminCertLabels),
				deployment("dp-live", projectedVolume("live-cert")),
				deployment("dp-preview", secretVolume("preview-cert")),
			},
			expectedKept: []string{"live-cert", "preview-cert"},
		},
		{
			name: "all labeled secrets deleted when no deployment references them",
			objects: []client.Object{
				secret("cert-1", adminCertLabels),
				secret("cert-2", adminCertLabels),
				secret("no-label", nil),
				deployment("dp-live"),
			},
			expectedGone: []string{"cert-1", "cert-2"},
			expectedKept: []string{"no-label"},
		},
		{
			name: "unreferenced secrets kept while a deployment is rolling out",
			objects: []client.Object{
				secret("cert-1", adminCertLabels),
				rollingOut(deployment("dp-live")),
			},
			expectedKept: []string{"cert-1"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			cl := fakectrlruntimeclient.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(tc.objects...).
				Build()

			require.NoError(t, deleteUnreferencedAdminCertSecrets(ctx, cl, dp))

			for _, name := range tc.expectedGone {
				err := cl.Get(ctx, client.ObjectKey{Namespace: "default", Name: name}, &corev1.Secret{})
				require.True(t, apierrors.IsNotFound(err), "expected Secret %s to be deleted", name)
			}
			for _, name := range tc.expectedKept {
				require.NoError(t, cl.Get(ctx, client.ObjectKey{Namespace: "default", Name: name}, &corev1.Secret{}))
			}
		})
	}
}
