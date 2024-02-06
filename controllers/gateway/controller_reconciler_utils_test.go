package gateway

import (
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	gwtypes "github.com/kong/gateway-operator/internal/types"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
)

func TestParseKongProxyListenEnv(t *testing.T) {
	testcases := []struct {
		Name            string
		KongProxyListen string
		Expected        kongListenConfig
	}{
		{
			Name:            "basic http",
			KongProxyListen: "0.0.0.0:8001 reuseport backlog=16384",
			Expected: kongListenConfig{
				Endpoint: &proxyListenEndpoint{
					Address: "0.0.0.0",
					Port:    8001,
				},
			},
		},
		{
			Name:            "basic https",
			KongProxyListen: "0.0.0.0:8443 http2 ssl reuseport backlog=16384",
			Expected: kongListenConfig{
				SSLEndpoint: &proxyListenEndpoint{
					Address: "0.0.0.0",
					Port:    8443,
				},
			},
		},
		{
			Name:            "basic http + https",
			KongProxyListen: "0.0.0.0:8001 reuseport backlog=16384, 0.0.0.0:8443 http2 ssl reuseport backlog=16384",
			Expected: kongListenConfig{
				Endpoint: &proxyListenEndpoint{
					Address: "0.0.0.0",
					Port:    8001,
				},
				SSLEndpoint: &proxyListenEndpoint{
					Address: "0.0.0.0",
					Port:    8443,
				},
			},
		},
	}

	for _, tc := range testcases {
		tc := tc

		t.Run(tc.Name, func(t *testing.T) {
			actual, err := parseKongListenEnv(tc.KongProxyListen)
			require.NoError(t, err)
			require.Equal(t, tc.Expected, actual)
		})
	}
}

func TestGatewayAddressesFromService(t *testing.T) {
	testCases := []struct {
		name      string
		svc       corev1.Service
		addresses []gwtypes.GatewayStatusAddress
		wantErr   bool
	}{
		{
			name: "ClusterIP Service",
			svc: corev1.Service{
				Spec: corev1.ServiceSpec{
					Type:      "ClusterIP",
					ClusterIP: "198.51.100.1",
				},
			},
			addresses: []gwtypes.GatewayStatusAddress{
				{
					Value: "198.51.100.1",
					Type:  lo.ToPtr(gatewayv1.IPAddressType),
				},
			},
			wantErr: false,
		},
		{
			name: "ClusterIP Service without ClusterIP",
			svc: corev1.Service{
				Spec: corev1.ServiceSpec{
					Type: "ClusterIP",
				},
			},
			addresses: []gwtypes.GatewayStatusAddress{},
			wantErr:   true,
		},
		{
			name: "LoadBalancer with IP addresses",
			svc: corev1.Service{
				Spec: corev1.ServiceSpec{
					Type:      "LoadBalancer",
					ClusterIP: "198.51.100.1",
				},
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{
							{
								IP: "203.0.113.1",
							},
							{
								IP: "203.0.113.2",
							},
						},
					},
				},
			},
			addresses: []gwtypes.GatewayStatusAddress{
				{
					Value: "203.0.113.1",
					Type:  lo.ToPtr(gatewayv1.IPAddressType),
				},
				{
					Value: "203.0.113.2",
					Type:  lo.ToPtr(gatewayv1.IPAddressType),
				},
			},
			wantErr: false,
		},
		{
			name: "LoadBalancer with hostnames",
			svc: corev1.Service{
				Spec: corev1.ServiceSpec{
					Type:      "LoadBalancer",
					ClusterIP: "198.51.100.1",
				},
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{
							{
								Hostname: "one.example.net",
							},
							{
								Hostname: "two.example.net",
							},
						},
					},
				},
			},
			addresses: []gwtypes.GatewayStatusAddress{
				{
					Value: "one.example.net",
					Type:  lo.ToPtr(gatewayv1.HostnameAddressType),
				},
				{
					Value: "two.example.net",
					Type:  lo.ToPtr(gatewayv1.HostnameAddressType),
				},
			},
			wantErr: false,
		},
		{
			name: "LoadBalancer with both IP and hostname in one status entry",
			svc: corev1.Service{
				Spec: corev1.ServiceSpec{
					Type:      "LoadBalancer",
					ClusterIP: "198.51.100.1",
				},
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{
							{
								IP:       "203.0.113.1",
								Hostname: "one.example.net",
							},
							{
								Hostname: "two.example.net",
							},
						},
					},
				},
			},
			addresses: []gwtypes.GatewayStatusAddress{
				{
					Value: "203.0.113.1",
					Type:  lo.ToPtr(gatewayv1.IPAddressType),
				},
				{
					Value: "one.example.net",
					Type:  lo.ToPtr(gatewayv1.HostnameAddressType),
				},
				{
					Value: "two.example.net",
					Type:  lo.ToPtr(gatewayv1.HostnameAddressType),
				},
			},
			wantErr: false,
		},
		{
			name: "LoadBalancer has status entries with neither hostname nor IP",
			svc: corev1.Service{
				Spec: corev1.ServiceSpec{
					Type:      "LoadBalancer",
					ClusterIP: "198.51.100.1",
				},
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{
							{},
						},
					},
				},
			},
			addresses: []gwtypes.GatewayStatusAddress{},
			wantErr:   false,
		},
		{
			name: "LoadBalancer has no status entries",
			svc: corev1.Service{
				Spec: corev1.ServiceSpec{
					Type:      "LoadBalancer",
					ClusterIP: "198.51.100.1",
				},
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{},
				},
			},
			addresses: []gwtypes.GatewayStatusAddress{},
			wantErr:   false,
		},
	}
	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			addresses, err := gatewayAddressesFromService(tc.svc)
			assert.Equal(t, tc.wantErr, err != nil)
			require.Equal(t, addresses, tc.addresses)
		})
	}
}

func TestSetAcceptedOnGateway(t *testing.T) {
	testCases := []struct {
		name                      string
		listeners                 []gatewayv1.ListenerStatus
		expectedAcceptedCondition metav1.Condition
	}{
		{
			name: "single listener accepted",
			listeners: []gatewayv1.ListenerStatus{
				{
					Name: "accepted",
					Conditions: []metav1.Condition{
						{
							Type:               string(gatewayv1.ListenerConditionAccepted),
							Status:             metav1.ConditionTrue,
							Reason:             string(gatewayv1.ListenerReasonAccepted),
							ObservedGeneration: 1,
						},
						{
							Type:               string(gatewayv1.ListenerConditionConflicted),
							Status:             metav1.ConditionFalse,
							Reason:             string(gatewayv1.ListenerReasonNoConflicts),
							ObservedGeneration: 1,
						},
					},
				},
			},
			expectedAcceptedCondition: metav1.Condition{
				Type:               string(gatewayv1.GatewayConditionAccepted),
				Status:             metav1.ConditionTrue,
				Reason:             string(gatewayv1.GatewayReasonAccepted),
				ObservedGeneration: 1,
				Message:            "All listeners are accepted.",
			},
		},
		{
			name: "multiple listeners accepted",
			listeners: []gatewayv1.ListenerStatus{
				{
					Name: "accepted",
					Conditions: []metav1.Condition{
						{
							Type:               string(gatewayv1.ListenerConditionAccepted),
							Status:             metav1.ConditionTrue,
							Reason:             string(gatewayv1.ListenerReasonAccepted),
							ObservedGeneration: 1,
						},
						{
							Type:               string(gatewayv1.ListenerConditionConflicted),
							Status:             metav1.ConditionFalse,
							Reason:             string(gatewayv1.ListenerReasonNoConflicts),
							ObservedGeneration: 1,
						},
					},
				},
				{
					Name: "accepted",
					Conditions: []metav1.Condition{
						{
							Type:               string(gatewayv1.ListenerConditionAccepted),
							Status:             metav1.ConditionTrue,
							Reason:             string(gatewayv1.ListenerReasonAccepted),
							ObservedGeneration: 1,
						},
						{
							Type:               string(gatewayv1.ListenerConditionConflicted),
							Status:             metav1.ConditionFalse,
							Reason:             string(gatewayv1.ListenerReasonNoConflicts),
							ObservedGeneration: 1,
						},
					},
				},
			},
			expectedAcceptedCondition: metav1.Condition{
				Type:               string(gatewayv1.GatewayConditionAccepted),
				Status:             metav1.ConditionTrue,
				Reason:             string(gatewayv1.GatewayReasonAccepted),
				ObservedGeneration: 1,
				Message:            "All listeners are accepted.",
			},
		},
		{
			name: "single listener, not accepted for unsupported protocol",
			listeners: []gatewayv1.ListenerStatus{
				{
					Name: "not accepted, unsupported protocol",
					Conditions: []metav1.Condition{
						{
							Type:               string(gatewayv1.ListenerConditionAccepted),
							Status:             metav1.ConditionFalse,
							Reason:             string(gatewayv1.ListenerReasonUnsupportedProtocol),
							ObservedGeneration: 1,
						},
						{
							Type:               string(gatewayv1.ListenerConditionConflicted),
							Status:             metav1.ConditionFalse,
							Reason:             string(gatewayv1.ListenerReasonNoConflicts),
							ObservedGeneration: 1,
						},
					},
				},
			},
			expectedAcceptedCondition: metav1.Condition{
				Type:               string(gatewayv1.GatewayConditionAccepted),
				Status:             metav1.ConditionFalse,
				Reason:             string(gatewayv1.GatewayReasonListenersNotValid),
				Message:            "Listener 0 is not accepted.",
				ObservedGeneration: 1,
			},
		},
		{
			name: "single listener, hostname conflict",
			listeners: []gatewayv1.ListenerStatus{
				{
					Name: "conflict, unsupported protocol",
					Conditions: []metav1.Condition{
						{
							Type:               string(gatewayv1.ListenerConditionAccepted),
							Status:             metav1.ConditionTrue,
							Reason:             string(gatewayv1.ListenerReasonAccepted),
							ObservedGeneration: 1,
						},
						{
							Type:               string(gatewayv1.ListenerConditionConflicted),
							Status:             metav1.ConditionTrue,
							Reason:             string(gatewayv1.ListenerReasonHostnameConflict),
							ObservedGeneration: 1,
						},
					},
				},
			},
			expectedAcceptedCondition: metav1.Condition{
				Type:               string(gatewayv1.GatewayConditionAccepted),
				Status:             metav1.ConditionFalse,
				Reason:             string(gatewayv1.GatewayReasonListenersNotValid),
				Message:            "Listener 0 is conflicted.",
				ObservedGeneration: 1,
			},
		},
		{
			name: "single listener, protocol conflict",
			listeners: []gatewayv1.ListenerStatus{
				{
					Name: "protocol conflict",
					Conditions: []metav1.Condition{
						{
							Type:               string(gatewayv1.ListenerConditionAccepted),
							Status:             metav1.ConditionTrue,
							Reason:             string(gatewayv1.ListenerReasonAccepted),
							ObservedGeneration: 1,
						},
						{
							Type:               string(gatewayv1.ListenerConditionConflicted),
							Status:             metav1.ConditionTrue,
							Reason:             string(gatewayv1.ListenerReasonProtocolConflict),
							ObservedGeneration: 1,
						},
					},
				},
			},
			expectedAcceptedCondition: metav1.Condition{
				Type:               string(gatewayv1.GatewayConditionAccepted),
				Status:             metav1.ConditionFalse,
				Reason:             string(gatewayv1.GatewayReasonListenersNotValid),
				Message:            "Listener 0 is conflicted.",
				ObservedGeneration: 1,
			},
		},
		{
			name: "multiple listeners, accepted, not accepted and conflicted",
			listeners: []gatewayv1.ListenerStatus{
				{
					Name: "accepted",
					Conditions: []metav1.Condition{
						{
							Type:               string(gatewayv1.ListenerConditionAccepted),
							Status:             metav1.ConditionTrue,
							Reason:             string(gatewayv1.ListenerReasonAccepted),
							ObservedGeneration: 1,
						},
						{
							Type:               string(gatewayv1.ListenerConditionConflicted),
							Status:             metav1.ConditionFalse,
							Reason:             string(gatewayv1.ListenerReasonNoConflicts),
							ObservedGeneration: 1,
						},
					},
				},
				{
					Name: "conflict, unsupported protocol",
					Conditions: []metav1.Condition{
						{
							Type:               string(gatewayv1.ListenerConditionAccepted),
							Status:             metav1.ConditionFalse,
							Reason:             string(gatewayv1.ListenerReasonUnsupportedProtocol),
							ObservedGeneration: 1,
						},
						{
							Type:               string(gatewayv1.ListenerConditionConflicted),
							Status:             metav1.ConditionFalse,
							Reason:             string(gatewayv1.ListenerReasonNoConflicts),
							ObservedGeneration: 1,
						},
					},
				},
				{
					Name: "protocol conflict",
					Conditions: []metav1.Condition{
						{
							Type:               string(gatewayv1.ListenerConditionAccepted),
							Status:             metav1.ConditionTrue,
							Reason:             string(gatewayv1.ListenerReasonAccepted),
							ObservedGeneration: 1,
						},
						{
							Type:               string(gatewayv1.ListenerConditionConflicted),
							Status:             metav1.ConditionTrue,
							Reason:             string(gatewayv1.ListenerReasonProtocolConflict),
							ObservedGeneration: 1,
						},
					},
				},
			},
			expectedAcceptedCondition: metav1.Condition{
				Type:               string(gatewayv1.GatewayConditionAccepted),
				Status:             metav1.ConditionFalse,
				Reason:             string(gatewayv1.GatewayReasonListenersNotValid),
				ObservedGeneration: 1,
				Message:            "Listener 1 is not accepted. Listener 2 is conflicted.",
			},
		},
	}
	for _, tc := range testCases {
		tc := tc

		t.Run(tc.name, func(subt *testing.T) {
			gateway := gatewayConditionsAndListenersAwareT{
				Gateway: &gatewayv1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "test",
						Namespace:  "default",
						Generation: 1,
					},
					Status: gatewayv1.GatewayStatus{
						Listeners: tc.listeners,
					},
				},
			}

			k8sutils.SetAcceptedConditionOnGateway(gateway)
			acceptedCondition, found := k8sutils.GetCondition(k8sutils.ConditionType(gatewayv1.GatewayConditionAccepted), gateway)
			require.True(t, found)
			// force the lastTransitionTime to be equal to properly compare the two conditions
			tc.expectedAcceptedCondition.LastTransitionTime = acceptedCondition.LastTransitionTime
			require.Equal(subt, tc.expectedAcceptedCondition, acceptedCondition)
		})
	}
}
