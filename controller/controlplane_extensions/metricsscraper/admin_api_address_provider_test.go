package metricsscraper

import (
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/v2/api/gateway-operator/v1beta1"

	"github.com/kong/kong-operator/pkg/consts"
)

func TestAdminAPIAddressProvider_AdminAddressesFromDP(t *testing.T) {
	testCases := []struct {
		name           string
		dataplane      *operatorv1beta1.DataPlane
		endpointSlices *discoveryv1.EndpointSliceList
		expectedResult []string
		expectedError  error
	}{
		{
			name: "Single EndpointSlice",
			dataplane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dataplane-1",
					Namespace: "default",
				},
			},
			endpointSlices: &discoveryv1.EndpointSliceList{
				Items: []discoveryv1.EndpointSlice{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "endpoint-slice-1",
							Namespace: "default",
							Labels: map[string]string{
								"gateway-operator.konghq.com/dataplane-service-state": "live",
								"gateway-operator.konghq.com/dataplane-service-type":  "admin",
								"gateway-operator.konghq.com/managed-by":              "dataplane",
								"app":                                                 "dataplane-1",
							},
							OwnerReferences: []metav1.OwnerReference{
								{
									APIVersion: "v1",
									Kind:       "Service",
									Name:       "servicename",
								},
							},
						},
						AddressType: discoveryv1.AddressTypeIPv4,
						Ports: []discoveryv1.EndpointPort{
							{
								Name:     lo.ToPtr("http"),
								Protocol: lo.ToPtr(corev1.ProtocolTCP),
								Port:     lo.ToPtr(int32(8001)),
							},
						},
						Endpoints: []discoveryv1.Endpoint{
							{
								Addresses: []string{"192.168.0.1"},
							},
							{
								Addresses: []string{"192.168.0.2"},
							},
						},
					},
				},
			},
			expectedResult: []string{
				"https://192-168-0-1.servicename.default.svc:8001",
				"https://192-168-0-2.servicename.default.svc:8001",
			},
			expectedError: nil,
		},
		{
			name: "Single EndpointSlice, IPv6 not supported",
			dataplane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dataplane-1",
					Namespace: "default",
				},
			},
			endpointSlices: &discoveryv1.EndpointSliceList{
				Items: []discoveryv1.EndpointSlice{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "endpoint-slice-1",
							Namespace: "default",
							Labels: map[string]string{
								"gateway-operator.konghq.com/dataplane-service-state": "live",
								"gateway-operator.konghq.com/dataplane-service-type":  "admin",
								"gateway-operator.konghq.com/managed-by":              "dataplane",
								"app":                                                 "dataplane-1",
							},
							OwnerReferences: []metav1.OwnerReference{
								{
									APIVersion: "v1",
									Kind:       "Service",
									Name:       "servicename",
								},
							},
						},
						AddressType: discoveryv1.AddressTypeIPv6,
						Ports: []discoveryv1.EndpointPort{
							{
								Name:     lo.ToPtr("http"),
								Protocol: lo.ToPtr(corev1.ProtocolTCP),
								Port:     lo.ToPtr(int32(8001)),
							},
						},
						Endpoints: []discoveryv1.Endpoint{
							{
								Addresses: []string{"fd69:2f34:87f7:8411:48e:92f4:f3ec:dd01"},
							},
						},
					},
				},
			},
		},
		{
			name: "Single EndpointSlice, IPv4 and IPv6, only the former is returned",
			dataplane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dataplane-1",
					Namespace: "default",
				},
			},
			endpointSlices: &discoveryv1.EndpointSliceList{
				Items: []discoveryv1.EndpointSlice{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "endpoint-slice-1",
							Namespace: "default",
							Labels: map[string]string{
								"gateway-operator.konghq.com/dataplane-service-state": "live",
								"gateway-operator.konghq.com/dataplane-service-type":  "admin",
								"gateway-operator.konghq.com/managed-by":              "dataplane",
								"app":                                                 "dataplane-1",
							},
							OwnerReferences: []metav1.OwnerReference{
								{
									APIVersion: "v1",
									Kind:       "Service",
									Name:       "servicename",
								},
							},
						},
						AddressType: discoveryv1.AddressTypeIPv6,
						Ports: []discoveryv1.EndpointPort{
							{
								Name:     lo.ToPtr("http"),
								Protocol: lo.ToPtr(corev1.ProtocolTCP),
								Port:     lo.ToPtr(int32(8001)),
							},
						},
						Endpoints: []discoveryv1.Endpoint{
							{
								Addresses: []string{"fd69:2f34:87f7:8411:48e:92f4:f3ec:dd01"},
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "endpoint-slice-2",
							Namespace: "default",
							Labels: map[string]string{
								"gateway-operator.konghq.com/dataplane-service-state": "live",
								"gateway-operator.konghq.com/dataplane-service-type":  "admin",
								"gateway-operator.konghq.com/managed-by":              "dataplane",
								"app":                                                 "dataplane-1",
							},
							OwnerReferences: []metav1.OwnerReference{
								{
									APIVersion: "v1",
									Kind:       "Service",
									Name:       "servicename",
								},
							},
						},
						AddressType: discoveryv1.AddressTypeIPv4,
						Ports: []discoveryv1.EndpointPort{
							{
								Name:     lo.ToPtr("http"),
								Protocol: lo.ToPtr(corev1.ProtocolTCP),
								Port:     lo.ToPtr(int32(8001)),
							},
						},
						Endpoints: []discoveryv1.Endpoint{
							{
								Addresses: []string{"192.168.0.2"},
							},
						},
					},
				},
			},
			expectedResult: []string{
				"https://192-168-0-2.servicename.default.svc:8001",
			},
		},
		{
			name: "2 EndpointSlices, 1 belongs to another DataPlane",
			dataplane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dataplane-1",
					Namespace: "default",
				},
			},
			endpointSlices: &discoveryv1.EndpointSliceList{
				Items: []discoveryv1.EndpointSlice{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "endpoint-slice-1",
							Namespace: "default",
							Labels: map[string]string{
								consts.DataPlaneServiceStateLabel:    consts.DataPlaneStateLabelValueLive,
								consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneAdminServiceLabelValue),
								consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
								"app":                                "dataplane-1",
							},
							OwnerReferences: []metav1.OwnerReference{
								{
									APIVersion: "v1",
									Kind:       "Service",
									Name:       "servicename",
								},
							},
						},
						AddressType: discoveryv1.AddressTypeIPv4,
						Ports: []discoveryv1.EndpointPort{
							{
								Name:     lo.ToPtr("http"),
								Protocol: lo.ToPtr(corev1.ProtocolTCP),
								Port:     lo.ToPtr(int32(8001)),
							},
						},
						Endpoints: []discoveryv1.Endpoint{
							{
								Addresses: []string{"192.168.0.1"},
							},
							{
								Addresses: []string{"192.168.0.2"},
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "endpoint-slice-2",
							Namespace: "default",
							Labels: map[string]string{
								consts.DataPlaneServiceStateLabel:    consts.DataPlaneStateLabelValueLive,
								consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneAdminServiceLabelValue),
								consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
								"app":                                "dataplane-2",
							},
						},
						AddressType: discoveryv1.AddressTypeIPv4,
						Ports: []discoveryv1.EndpointPort{
							{
								Name:     lo.ToPtr("http"),
								Protocol: lo.ToPtr(corev1.ProtocolTCP),
								Port:     lo.ToPtr(int32(8001)),
							},
						},
						Endpoints: []discoveryv1.Endpoint{
							{
								Addresses: []string{"192.168.100.1"},
							},
						},
					},
				},
			},
			expectedResult: []string{
				"https://192-168-0-1.servicename.default.svc:8001",
				"https://192-168-0-2.servicename.default.svc:8001",
			},
			expectedError: nil,
		},
		{
			name: "multiple addresses in a single endpoint result in only the first address being used",
			dataplane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dataplane-3",
					Namespace: "default",
				},
			},
			endpointSlices: &discoveryv1.EndpointSliceList{
				Items: []discoveryv1.EndpointSlice{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "endpoint-slice-3",
							Namespace: "default",
							Labels: map[string]string{
								consts.DataPlaneServiceStateLabel:    consts.DataPlaneStateLabelValueLive,
								consts.DataPlaneServiceTypeLabel:     string(consts.DataPlaneAdminServiceLabelValue),
								consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
								"app":                                "dataplane-3",
							},
							OwnerReferences: []metav1.OwnerReference{
								{
									APIVersion: "v1",
									Kind:       "Service",
									Name:       "servicename",
								},
							},
						},
						AddressType: discoveryv1.AddressTypeIPv4,
						Ports: []discoveryv1.EndpointPort{
							{
								Name:     lo.ToPtr("http"),
								Protocol: lo.ToPtr(corev1.ProtocolTCP),
								Port:     lo.ToPtr(int32(8001)),
							},
						},
						Endpoints: []discoveryv1.Endpoint{
							{
								Addresses: []string{"192.168.0.1", "192.168.0.2", "192.168.0.3"},
							},
							{
								Addresses: []string{"192.168.100.1"},
							},
						},
					},
				},
			},
			expectedResult: []string{
				"https://192-168-0-1.servicename.default.svc:8001",
				"https://192-168-100-1.servicename.default.svc:8001",
			},
			expectedError: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().WithLists(tc.endpointSlices).Build()
			provider := NewAdminAPIAddressProvider(fakeClient)

			addresses, err := provider.AdminAddressesForDP(t.Context(), tc.dataplane)

			assert.Equal(t, tc.expectedError, err)
			assert.ElementsMatch(t, tc.expectedResult, addresses)
		})
	}
}
