//go:build envtest

package envtest

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/kong/kong-operator/v2/ingress-controller/test/adminapi"
	"github.com/kong/kong-operator/v2/ingress-controller/test/util/builder"
)

func TestDiscoverer_GetAdminAPIsForServiceReturnsAllAddressesCorrectlyPagingThroughResults(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := Scheme(t)
	cfg, _ := Setup(t, ctx, scheme)
	client := NewControllerClient(t, scheme, cfg)

	// In tests below we use a deferred cancel to stop the manager and not wait
	// for its timeout.

	testcases := []struct {
		subnetC int
		subnetD int
	}{
		{subnetC: 1, subnetD: 100},
		{subnetC: 1, subnetD: 101},
		{subnetC: 1, subnetD: 250},
		{subnetC: 2, subnetD: 250},
		{subnetC: 5, subnetD: 250},
	}

	for _, tc := range testcases {
		t.Run(fmt.Sprintf("%dx%d", tc.subnetC, tc.subnetD), func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			const (
				serviceName = "test-service"
				portName    = "admin"
				portNumber  = 8444
			)
			var (
				ns         = CreateNamespace(ctx, t, client)
				serviceObj = corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: ns.Name,
						Name:      serviceName,
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: "v1",
								Kind:       "Service",
								Name:       ns.Name,
								UID:        ns.UID,
							},
						},
					},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{
							{
								Name:     portName,
								Protocol: corev1.ProtocolTCP,
								Port:     portNumber,
							},
						},
					},
				}
			)
			require.NoError(t, client.Create(ctx, &serviceObj))

			for i := range tc.subnetC {
				for j := range tc.subnetD {
					es := discoveryv1.EndpointSlice{
						ObjectMeta: metav1.ObjectMeta{
							Name:      uuid.NewString(),
							Namespace: ns.Name,
							Labels: map[string]string{
								"kubernetes.io/service-name": serviceName,
							},
							OwnerReferences: []metav1.OwnerReference{
								{
									APIVersion: "v1",
									Kind:       "Service",
									Name:       serviceName,
									UID:        serviceObj.UID,
								},
							},
						},
						AddressType: discoveryv1.AddressTypeIPv4,
						Endpoints: []discoveryv1.Endpoint{
							{
								Addresses: []string{fmt.Sprintf("10.0.%d.%d", i, j)},
								Conditions: discoveryv1.EndpointConditions{
									Ready:       new(true),
									Terminating: new(false),
								},
								TargetRef: testPodReference("pod-1", ns.Name),
							},
						},
						Ports: builder.NewEndpointPort(portNumber).WithName(portName).IntoSlice(),
					}
					require.NoError(t, client.Create(ctx, &es))
				}
			}

			discoverer, err := adminapi.NewDiscoverer(sets.New(portName))
			require.NoError(t, err)

			got, err := discoverer.GetAdminAPIsForService(ctx, client, k8stypes.NamespacedName{Name: serviceObj.Name, Namespace: serviceObj.Namespace})
			require.NoError(t, err)
			require.Len(t, got, tc.subnetD*tc.subnetC, "GetAdminAPIsForService should return all valid addresses")
		})
	}
}

func testPodReference(name, ns string) *corev1.ObjectReference {
	return &corev1.ObjectReference{
		Kind:      "Pod",
		Namespace: ns,
		Name:      name,
	}
}
