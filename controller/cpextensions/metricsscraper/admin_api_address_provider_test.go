package metricsscraper

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	adminapi "github.com/kong/kong-operator/v2/internal/adminapi"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

func adminEndpointSlice(
	name string,
	app string,
	addressType discoveryv1.AddressType,
	ports []discoveryv1.EndpointPort,
	endpoints ...discoveryv1.Endpoint,
) discoveryv1.EndpointSlice {
	return discoveryv1.EndpointSlice{
		Name:      name,
		Namespace: "default",
		Labels: map[string]string{
			consts.DataPlaneServiceStateLabel:    consts.DataPlaneStateLabelValueLive,
			consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneAdminServiceLabelValue),
			consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
			"app":                                app,
		},
		OwnerReferences: []metav1.OwnerReference{{
			APIVersion: "v1",
			Kind:       "Service",
			Name:       "servicename",
		}},
		AddressType: addressType,
		Ports:       ports,
		Endpoints:   endpoints,
	}
}

func adminEndpoint(address, podName string) discoveryv1.Endpoint {
	return discoveryv1.Endpoint{
		Addresses: []string{address},
		TargetRef: &corev1.ObjectReference{
			Kind:      "Pod",
			Namespace: "default",
			Name:      podName,
		},
	}
}

func discoveredAdminAPI(address, podName string) adminapi.DiscoveredAdminAPI {
	return adminapi.DiscoveredAdminAPI{
		Address:       address,
		TLSServerName: "pod.servicename.default.svc",
		PodRef:        types.NamespacedName{Namespace: "default", Name: podName},
	}
}

func TestAdminAPIAddressProvider_AdminAddressesFromDP(t *testing.T) {
	adminPorts := []discoveryv1.EndpointPort{{
		Name:     new(consts.DataPlaneAdminServicePortName),
		Protocol: new(corev1.ProtocolTCP),
		Port:     new(int32(8001)),
	}}
	dataplane := &operatorv1beta1.DataPlane{Name: "dataplane-1", Namespace: "default"}

	testCases := []struct {
		name           string
		endpointSlices []discoveryv1.EndpointSlice
		expectedResult []adminapi.DiscoveredAdminAPI
	}{
		{
			name: "single EndpointSlice",
			endpointSlices: []discoveryv1.EndpointSlice{adminEndpointSlice(
				"endpoint-slice-1",
				"dataplane-1",
				discoveryv1.AddressTypeIPv4,
				adminPorts,
				adminEndpoint("192.168.0.1", "pod-1"),
				adminEndpoint("192.168.0.2", "pod-2"),
			)},
			expectedResult: []adminapi.DiscoveredAdminAPI{
				discoveredAdminAPI("https://192.168.0.1:8001", "pod-1"),
				discoveredAdminAPI("https://192.168.0.2:8001", "pod-2"),
			},
		},
		{
			name: "unready, unserving, terminating and addressless endpoints are skipped",
			endpointSlices: []discoveryv1.EndpointSlice{adminEndpointSlice(
				"endpoint-slice-1",
				"dataplane-1",
				discoveryv1.AddressTypeIPv4,
				adminPorts,
				discoveryv1.Endpoint{
					Addresses:  []string{"192.168.0.1"},
					Conditions: discoveryv1.EndpointConditions{Ready: new(false)},
					TargetRef:  &corev1.ObjectReference{Kind: "Pod", Namespace: "default", Name: "pod-1"},
				},
				discoveryv1.Endpoint{
					Addresses:  []string{"192.168.0.2"},
					Conditions: discoveryv1.EndpointConditions{Serving: new(false)},
					TargetRef:  &corev1.ObjectReference{Kind: "Pod", Namespace: "default", Name: "pod-2"},
				},
				discoveryv1.Endpoint{
					Addresses:  []string{"192.168.0.3"},
					Conditions: discoveryv1.EndpointConditions{Terminating: new(true)},
					TargetRef:  &corev1.ObjectReference{Kind: "Pod", Namespace: "default", Name: "pod-3"},
				},
				discoveryv1.Endpoint{
					TargetRef: &corev1.ObjectReference{Kind: "Pod", Namespace: "default", Name: "pod-4"},
				},
			)},
		},
		{
			name: "IPv6 is not supported",
			endpointSlices: []discoveryv1.EndpointSlice{adminEndpointSlice(
				"endpoint-slice-1",
				"dataplane-1",
				discoveryv1.AddressTypeIPv6,
				adminPorts,
				adminEndpoint("fd69:2f34:87f7:8411:48e:92f4:f3ec:dd01", "pod-1"),
			)},
		},
		{
			name: "EndpointSlices of another DataPlane are ignored",
			endpointSlices: []discoveryv1.EndpointSlice{
				adminEndpointSlice(
					"endpoint-slice-1",
					"dataplane-1",
					discoveryv1.AddressTypeIPv4,
					adminPorts,
					adminEndpoint("192.168.0.1", "pod-1"),
				),
				adminEndpointSlice(
					"endpoint-slice-2",
					"dataplane-2",
					discoveryv1.AddressTypeIPv4,
					adminPorts,
					adminEndpoint("192.168.100.1", "pod-2"),
				),
			},
			expectedResult: []adminapi.DiscoveredAdminAPI{
				discoveredAdminAPI("https://192.168.0.1:8001", "pod-1"),
			},
		},
		{
			name: "only the first address of an endpoint is used",
			endpointSlices: []discoveryv1.EndpointSlice{adminEndpointSlice(
				"endpoint-slice-1",
				"dataplane-1",
				discoveryv1.AddressTypeIPv4,
				adminPorts,
				discoveryv1.Endpoint{
					Addresses: []string{"192.168.0.1", "192.168.0.2", "192.168.0.3"},
					TargetRef: &corev1.ObjectReference{Kind: "Pod", Namespace: "default", Name: "pod-1"},
				},
				adminEndpoint("192.168.100.1", "pod-2"),
			)},
			expectedResult: []adminapi.DiscoveredAdminAPI{
				discoveredAdminAPI("https://192.168.0.1:8001", "pod-1"),
				discoveredAdminAPI("https://192.168.100.1:8001", "pod-2"),
			},
		},
		{
			name: "EndpointSlice without a Service owner is ignored",
			endpointSlices: []discoveryv1.EndpointSlice{func() discoveryv1.EndpointSlice {
				es := adminEndpointSlice(
					"endpoint-slice-1",
					"dataplane-1",
					discoveryv1.AddressTypeIPv4,
					adminPorts,
					adminEndpoint("192.168.0.1", "pod-1"),
				)
				es.OwnerReferences = nil
				return es
			}()},
		},
		{
			name: "non-admin ports are ignored",
			endpointSlices: []discoveryv1.EndpointSlice{adminEndpointSlice(
				"endpoint-slice-1",
				"dataplane-1",
				discoveryv1.AddressTypeIPv4,
				[]discoveryv1.EndpointPort{{
					Name:     new("http"),
					Protocol: new(corev1.ProtocolTCP),
					Port:     new(int32(8001)),
				}},
				adminEndpoint("192.168.0.1", "pod-1"),
			)},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().WithLists(&discoveryv1.EndpointSliceList{Items: tc.endpointSlices}).Build()
			provider, err := NewAdminAPIAddressProvider(fakeClient)
			require.NoError(t, err)

			addresses, err := provider.AdminAddressesForDP(t.Context(), dataplane)
			require.NoError(t, err)
			require.Equal(t, tc.expectedResult, addresses)
		})
	}
}
