package gateway

import (
	"errors"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	gwtypes "github.com/kong/gateway-operator/internal/types"
	"github.com/kong/gateway-operator/modules/manager/scheme"
	"github.com/kong/gateway-operator/pkg/consts"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"
	"github.com/kong/gateway-operator/test/helpers"

	kcfgconsts "github.com/kong/kubernetes-configuration/api/common/consts"
	kcfggateway "github.com/kong/kubernetes-configuration/api/gateway-operator/gateway"
	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
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
			acceptedCondition, found := k8sutils.GetCondition(kcfgconsts.ConditionType(gatewayv1.GatewayConditionAccepted), gateway)
			require.True(t, found)
			// force the lastTransitionTime to be equal to properly compare the two conditions
			tc.expectedAcceptedCondition.LastTransitionTime = acceptedCondition.LastTransitionTime
			require.Equal(subt, tc.expectedAcceptedCondition, acceptedCondition)
		})
	}
}

func TestSetDataPlaneIngressServicePorts(t *testing.T) {
	testCases := []struct {
		name             string
		listeners        []gwtypes.Listener
		listenersOptions []operatorv1beta1.GatewayConfigurationListenerOptions
		expectedPorts    []operatorv1beta1.DataPlaneServicePort
		expectedError    error
	}{
		{
			name: "no listeners",
		},
		{
			name: "only valid listeners",
			listeners: []gwtypes.Listener{
				{
					Name:     "http",
					Protocol: gwtypes.HTTPProtocolType,
					Port:     gatewayv1.PortNumber(80),
				},
				{
					Name:     "https",
					Protocol: gatewayv1.HTTPSProtocolType,
					Port:     gatewayv1.PortNumber(443),
				},
			},
			expectedPorts: []operatorv1beta1.DataPlaneServicePort{
				{
					Name:       "http",
					Port:       80,
					TargetPort: intstr.FromInt(consts.DataPlaneProxyPort),
				},
				{
					Name:       "https",
					Port:       443,
					TargetPort: intstr.FromInt(consts.DataPlaneProxySSLPort),
				},
			},
		},
		{
			name: "some invalid listeners",
			listeners: []gwtypes.Listener{
				{
					Name:     "http",
					Protocol: gwtypes.HTTPProtocolType,
					Port:     gatewayv1.PortNumber(80),
				},
				{
					Name:     "udp",
					Protocol: gatewayv1.UDPProtocolType,
					Port:     gatewayv1.PortNumber(8899),
				},
			},
			expectedPorts: []operatorv1beta1.DataPlaneServicePort{
				{
					Name:       "http",
					Port:       80,
					TargetPort: intstr.FromInt(consts.DataPlaneProxyPort),
				},
			},
			expectedError: errors.New("listener 1 uses unsupported protocol UDP"),
		},
		{
			name: "listener options sets nodeport",
			listeners: []gwtypes.Listener{
				{
					Name:     "http",
					Protocol: gwtypes.HTTPProtocolType,
					Port:     gatewayv1.PortNumber(80),
				},
				{
					Name:     "https",
					Protocol: gatewayv1.HTTPSProtocolType,
					Port:     gatewayv1.PortNumber(443),
				},
			},
			listenersOptions: []operatorv1beta1.GatewayConfigurationListenerOptions{
				{
					Name:     "http",
					NodePort: int32(30080),
				},
			},
			expectedPorts: []operatorv1beta1.DataPlaneServicePort{
				{
					Name:       "http",
					Port:       80,
					TargetPort: intstr.FromInt(consts.DataPlaneProxyPort),
					NodePort:   int32(30080),
				},
				{
					Name:       "https",
					Port:       443,
					TargetPort: intstr.FromInt(consts.DataPlaneProxySSLPort),
				},
			},
		},
		{
			name: "listener options' name does not match listener",
			listeners: []gwtypes.Listener{
				{
					Name:     "http",
					Protocol: gwtypes.HTTPProtocolType,
					Port:     gatewayv1.PortNumber(80),
				},
				{
					Name:     "https",
					Protocol: gatewayv1.HTTPSProtocolType,
					Port:     gatewayv1.PortNumber(443),
				},
			},
			listenersOptions: []operatorv1beta1.GatewayConfigurationListenerOptions{
				{
					Name:     "http-1",
					NodePort: int32(30080),
				},
			},
			expectedPorts: nil,
			expectedError: errors.New("GatewayConfiguration.spec.listenersOptions[0]: name 'http-1' not in gateway's listeners"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := setDataPlaneIngressServicePorts(&operatorv1beta1.DataPlaneOptions{}, tc.listeners, tc.listenersOptions)
			if tc.expectedError == nil {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, tc.expectedError.Error())
			}
		})
	}
}

func TestGatewayStatusNeedsUpdate(t *testing.T) {
	customizeGateway := func(gateway gatewayv1.Gateway, opts ...func(*gatewayv1.Gateway)) *gatewayv1.Gateway {
		newGateway := gateway.DeepCopy()
		for _, opt := range opts {
			opt(newGateway)
		}
		return newGateway
	}

	listenerStatus := gatewayv1.ListenerStatus{
		SupportedKinds: []gwtypes.RouteGroupKind{
			{
				Kind: "HTTPRoute",
			},
		},
		Conditions: []metav1.Condition{
			{
				Type:   string(gatewayv1.GatewayConditionAccepted),
				Status: metav1.ConditionTrue,
				Reason: string(gatewayv1.GatewayReasonAccepted),
			},
			{
				Type:   string(gatewayv1.GatewayConditionProgrammed),
				Status: metav1.ConditionTrue,
				Reason: string(gatewayv1.GatewayReasonProgrammed),
			},
			{
				Type:   string(gatewayv1.ListenerConditionResolvedRefs),
				Status: metav1.ConditionTrue,
				Reason: string(gatewayv1.ListenerReasonResolvedRefs),
			},
		},
	}
	gateway := gatewayv1.Gateway{
		Status: gatewayv1.GatewayStatus{
			Conditions: []metav1.Condition{
				{
					Type:   string(gatewayv1.GatewayConditionAccepted),
					Status: metav1.ConditionTrue,
					Reason: string(gatewayv1.GatewayReasonAccepted),
				},
				{
					Type:   string(gatewayv1.GatewayConditionProgrammed),
					Status: metav1.ConditionTrue,
					Reason: string(gatewayv1.GatewayReasonProgrammed),
				},
			},
			Listeners: []gatewayv1.ListenerStatus{
				listenerStatus,
			},
		},
	}

	testCases := []struct {
		name        string
		needsUpdate bool
		oldGateway  gatewayConditionsAndListenersAwareT
		newGateway  gatewayConditionsAndListenersAwareT
	}{
		{
			name:        "no update needed",
			needsUpdate: false,
			oldGateway:  gatewayConditionsAndListenersAware(&gateway),
			newGateway:  gatewayConditionsAndListenersAware(&gateway),
		},
		{
			name:        "update needed, old is not accepted",
			needsUpdate: true,
			oldGateway: gatewayConditionsAndListenersAware(customizeGateway(gateway, func(g *gatewayv1.Gateway) {
				g.Status.Conditions[0].Status = metav1.ConditionFalse
				g.Status.Conditions[0].Reason = string(gatewayv1.GatewayReasonInvalid)
			})),
			newGateway: gatewayConditionsAndListenersAware(&gateway),
		},
		{
			name:        "update needed, different amount of listeners",
			needsUpdate: true,
			oldGateway:  gatewayConditionsAndListenersAware(&gateway),
			newGateway: gatewayConditionsAndListenersAware(customizeGateway(gateway, func(g *gatewayv1.Gateway) {
				g.Status.Listeners = append(g.Status.Listeners, listenerStatus)
			})),
		},
		{
			name:        "update needed, different amount of listeners' condition",
			needsUpdate: true,
			oldGateway:  gatewayConditionsAndListenersAware(&gateway),
			newGateway: gatewayConditionsAndListenersAware(customizeGateway(gateway, func(g *gatewayv1.Gateway) {
				g.Status.Listeners[0].Conditions = append(g.Status.Listeners[0].Conditions,
					metav1.Condition{
						Type:   string(gatewayv1.ListenerConditionConflicted),
						Status: metav1.ConditionFalse,
						Reason: string(gatewayv1.ListenerReasonHostnameConflict),
					},
				)
			})),
		},
		{
			name:        "update needed, different supportedkinds",
			needsUpdate: true,
			oldGateway: gatewayConditionsAndListenersAware(customizeGateway(gateway, func(g *gatewayv1.Gateway) {
				g.Status.Listeners[0].SupportedKinds = []gwtypes.RouteGroupKind{}
			})),
			newGateway: gatewayConditionsAndListenersAware(&gateway),
		},
		{
			name:        "update needed, different listener conditions",
			needsUpdate: true,
			oldGateway: gatewayConditionsAndListenersAware(customizeGateway(gateway, func(g *gatewayv1.Gateway) {
				g.Status.Listeners[0].Conditions[0].Status = metav1.ConditionFalse
				g.Status.Listeners[0].Conditions[0].Reason = string(gatewayv1.ListenerReasonInvalid)
			})),
			newGateway: gatewayConditionsAndListenersAware(&gateway),
		},
		{
			name:        "update needed, unsorted listener conditions",
			needsUpdate: true,
			oldGateway: gatewayConditionsAndListenersAware(customizeGateway(gateway, func(g *gatewayv1.Gateway) {
				g.Status.Listeners[0].Conditions = []metav1.Condition{
					{
						Type:   string(gatewayv1.GatewayConditionAccepted),
						Status: metav1.ConditionTrue,
						Reason: string(gatewayv1.GatewayReasonAccepted),
					},
					{
						Type:   string(gatewayv1.ListenerConditionResolvedRefs),
						Status: metav1.ConditionTrue,
						Reason: string(gatewayv1.ListenerReasonResolvedRefs),
					},
					{
						Type:   string(gatewayv1.GatewayConditionProgrammed),
						Status: metav1.ConditionTrue,
						Reason: string(gatewayv1.GatewayReasonProgrammed),
					},
				}
			})),
			newGateway: gatewayConditionsAndListenersAware(&gateway),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.needsUpdate, gatewayStatusNeedsUpdate(tc.oldGateway, tc.newGateway))
		})
	}
}

func TestGetSupportedKindsWithResolvedRefsCondition(t *testing.T) {
	var generation int64 = 1
	ca := helpers.CreateCA(t)

	testCases := []struct {
		name                          string
		gatewayNamespace              string
		listener                      gwtypes.Listener
		referenceGrants               []client.Object
		secrets                       []client.Object
		expectedSupportedKinds        []gwtypes.RouteGroupKind
		expectedResolvedRefsCondition metav1.Condition
	}{
		{
			name: "no tls, HTTP protocol, no allowed routes",
			listener: gwtypes.Listener{
				Protocol: gwtypes.HTTPProtocolType,
			},
			expectedSupportedKinds: []gwtypes.RouteGroupKind{
				{
					Group: (*gwtypes.Group)(&gatewayv1.GroupVersion.Group),
					Kind:  "HTTPRoute",
				},
			},
			expectedResolvedRefsCondition: metav1.Condition{
				Type:               string(gatewayv1.ListenerConditionResolvedRefs),
				Status:             metav1.ConditionTrue,
				Reason:             string(gatewayv1.ListenerReasonResolvedRefs),
				Message:            "Listeners' references are accepted.",
				ObservedGeneration: generation,
			},
		},
		{
			name: "no tls, UDP protocol, no allowed routes",
			listener: gwtypes.Listener{
				Protocol: gatewayv1.UDPProtocolType,
			},
			expectedSupportedKinds: []gwtypes.RouteGroupKind{},
			expectedResolvedRefsCondition: metav1.Condition{
				Type:               string(gatewayv1.ListenerConditionResolvedRefs),
				Status:             metav1.ConditionTrue,
				Reason:             string(gatewayv1.ListenerReasonResolvedRefs),
				Message:            "Listeners' references are accepted.",
				ObservedGeneration: generation,
			},
		},
		{
			name: "no tls, HTTP protocol, HTTP and UDP routes",
			listener: gwtypes.Listener{
				Protocol: gwtypes.HTTPProtocolType,
				AllowedRoutes: &gwtypes.AllowedRoutes{
					Kinds: []gwtypes.RouteGroupKind{
						{
							Group: (*gwtypes.Group)(&gatewayv1.GroupVersion.Group),
							Kind:  "HTTPRoute",
						},
						{
							Group: (*gwtypes.Group)(&gatewayv1.GroupVersion.Group),
							Kind:  "UDPRoute",
						},
					},
				},
			},
			expectedSupportedKinds: []gwtypes.RouteGroupKind{
				{
					Group: (*gwtypes.Group)(&gatewayv1.GroupVersion.Group),
					Kind:  "HTTPRoute",
				},
			},
			expectedResolvedRefsCondition: metav1.Condition{
				Type:               string(gatewayv1.ListenerConditionResolvedRefs),
				Status:             metav1.ConditionFalse,
				Reason:             string(gatewayv1.ListenerReasonInvalidRouteKinds),
				Message:            "Route UDPRoute not supported.",
				ObservedGeneration: generation,
			},
		},
		{
			name:             "tls well-formed, no cross-namespace reference",
			gatewayNamespace: "default",
			listener: gwtypes.Listener{
				Protocol: gatewayv1.HTTPSProtocolType,
				TLS: &gatewayv1.GatewayTLSConfig{
					Mode: lo.ToPtr(gatewayv1.TLSModeTerminate),
					CertificateRefs: []gatewayv1.SecretObjectReference{
						{
							Name: "test-secret",
						},
					},
				},
			},
			secrets: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-secret",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"tls.crt": ca.CertPEM.Bytes(),
						"tls.key": ca.KeyPEM.Bytes(),
					},
				},
			},
			expectedSupportedKinds: []gwtypes.RouteGroupKind{
				{
					Group: (*gwtypes.Group)(&gatewayv1.GroupVersion.Group),
					Kind:  "HTTPRoute",
				},
			},
			expectedResolvedRefsCondition: metav1.Condition{
				Type:               string(gatewayv1.ListenerConditionResolvedRefs),
				Status:             metav1.ConditionTrue,
				Reason:             string(gatewayv1.ListenerReasonResolvedRefs),
				Message:            "Listeners' references are accepted.",
				ObservedGeneration: generation,
			},
		},
		{
			name:             "tls with passthrough, HTTPS protocol, no allowed routes",
			gatewayNamespace: "default",
			listener: gwtypes.Listener{
				Protocol: gatewayv1.HTTPSProtocolType,
				TLS: &gatewayv1.GatewayTLSConfig{
					Mode: lo.ToPtr(gatewayv1.TLSModePassthrough),
					CertificateRefs: []gatewayv1.SecretObjectReference{
						{
							Name: "test-secret",
						},
					},
				},
			},
			secrets: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-secret",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"tls.crt": ca.CertPEM.Bytes(),
						"tls.key": ca.KeyPEM.Bytes(),
					},
				},
			},
			expectedSupportedKinds: []gwtypes.RouteGroupKind{
				{
					Group: (*gwtypes.Group)(&gatewayv1.GroupVersion.Group),
					Kind:  "HTTPRoute",
				},
			},
			expectedResolvedRefsCondition: metav1.Condition{
				Type:               string(gatewayv1.ListenerConditionResolvedRefs),
				Status:             metav1.ConditionFalse,
				Reason:             string(gatewayv1.ListenerReasonInvalidCertificateRef),
				Message:            "Only Terminate mode is supported.",
				ObservedGeneration: generation,
			},
		},
		{
			name:             "tls bad-formed, multiple TLS secrets no cross-namespace reference",
			gatewayNamespace: "default",
			listener: gwtypes.Listener{
				Protocol: gatewayv1.HTTPSProtocolType,
				TLS: &gatewayv1.GatewayTLSConfig{
					Mode: lo.ToPtr(gatewayv1.TLSModeTerminate),
					CertificateRefs: []gatewayv1.SecretObjectReference{
						{
							Name: "test-secret",
						},
						{
							Name: "test-secret-2",
						},
					},
				},
			},
			expectedSupportedKinds: []gwtypes.RouteGroupKind{
				{
					Group: (*gwtypes.Group)(&gatewayv1.GroupVersion.Group),
					Kind:  "HTTPRoute",
				},
			},
			expectedResolvedRefsCondition: metav1.Condition{
				Type:               string(gatewayv1.ListenerConditionResolvedRefs),
				Status:             metav1.ConditionFalse,
				Reason:             string(kcfggateway.ListenerReasonTooManyTLSSecrets),
				Message:            "Only one certificate per listener is supported.",
				ObservedGeneration: generation,
			},
		},
		{
			name:             "tls bad-formed, no tls secret, no cross-namespace reference",
			gatewayNamespace: "default",
			listener: gwtypes.Listener{
				Protocol: gatewayv1.HTTPSProtocolType,
				TLS: &gatewayv1.GatewayTLSConfig{
					Mode: lo.ToPtr(gatewayv1.TLSModeTerminate),
					CertificateRefs: []gatewayv1.SecretObjectReference{
						{
							Name: "test-secret",
						},
					},
				},
			},
			expectedSupportedKinds: []gwtypes.RouteGroupKind{
				{
					Group: (*gwtypes.Group)(&gatewayv1.GroupVersion.Group),
					Kind:  "HTTPRoute",
				},
			},
			expectedResolvedRefsCondition: metav1.Condition{
				Type:               string(gatewayv1.ListenerConditionResolvedRefs),
				Status:             metav1.ConditionFalse,
				Reason:             string(gatewayv1.ListenerReasonInvalidCertificateRef),
				Message:            "Referenced secret default/test-secret does not exist.",
				ObservedGeneration: generation,
			},
		},
		{
			name:             "tls bad-formed, bad group and kind of tls secret, no cross-namespace reference",
			gatewayNamespace: "default",
			listener: gwtypes.Listener{
				Protocol: gatewayv1.HTTPSProtocolType,
				TLS: &gatewayv1.GatewayTLSConfig{
					Mode: lo.ToPtr(gatewayv1.TLSModeTerminate),
					CertificateRefs: []gatewayv1.SecretObjectReference{
						{
							Name:  "test-secret",
							Group: (*gwtypes.Group)(lo.ToPtr("bad-group")),
							Kind:  (*gwtypes.Kind)(lo.ToPtr("bad-kind")),
						},
					},
				},
			},
			expectedSupportedKinds: []gwtypes.RouteGroupKind{
				{
					Group: (*gwtypes.Group)(&gatewayv1.GroupVersion.Group),
					Kind:  "HTTPRoute",
				},
			},
			expectedResolvedRefsCondition: metav1.Condition{
				Type:               string(gatewayv1.ListenerConditionResolvedRefs),
				Status:             metav1.ConditionFalse,
				Reason:             string(gatewayv1.ListenerReasonInvalidCertificateRef),
				Message:            "Group bad-group not supported in CertificateRef. Kind bad-kind not supported in CertificateRef.",
				ObservedGeneration: generation,
			},
		},
		{
			name:             "tls bad-formed, invalid cert and key",
			gatewayNamespace: "default",
			listener: gwtypes.Listener{
				Protocol: gatewayv1.HTTPSProtocolType,
				TLS: &gatewayv1.GatewayTLSConfig{
					Mode: lo.ToPtr(gatewayv1.TLSModeTerminate),
					CertificateRefs: []gatewayv1.SecretObjectReference{
						{
							Name: "test-secret",
						},
					},
				},
			},
			secrets: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-secret",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"tls.crt": []byte("invalid-cert"),
						"tls.key": []byte("invalid-key"),
					},
				},
			},
			expectedSupportedKinds: []gwtypes.RouteGroupKind{
				{
					Group: (*gwtypes.Group)(&gatewayv1.GroupVersion.Group),
					Kind:  "HTTPRoute",
				},
			},
			expectedResolvedRefsCondition: metav1.Condition{
				Type:               string(gatewayv1.ListenerConditionResolvedRefs),
				Status:             metav1.ConditionFalse,
				Reason:             string(gatewayv1.ListenerReasonInvalidCertificateRef),
				Message:            "Referenced secret does not contain a valid TLS certificate.",
				ObservedGeneration: generation,
			},
		},
		{
			name:             "tls well-formed, with allowed cross-namespace reference",
			gatewayNamespace: "default",
			listener: gwtypes.Listener{
				Protocol: gatewayv1.HTTPSProtocolType,
				TLS: &gatewayv1.GatewayTLSConfig{
					Mode: lo.ToPtr(gatewayv1.TLSModeTerminate),
					CertificateRefs: []gatewayv1.SecretObjectReference{
						{
							Name:      "test-secret",
							Namespace: (*gatewayv1.Namespace)(lo.ToPtr("other-namespace")),
						},
					},
				},
			},
			secrets: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-secret",
						Namespace: "other-namespace",
					},
					Data: map[string][]byte{
						"tls.crt": ca.CertPEM.Bytes(),
						"tls.key": ca.KeyPEM.Bytes(),
					},
				},
			},
			referenceGrants: []client.Object{
				&gatewayv1beta1.ReferenceGrant{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "other-namespace",
					},
					Spec: gatewayv1beta1.ReferenceGrantSpec{
						From: []gatewayv1beta1.ReferenceGrantFrom{
							{
								Group:     gatewayv1.GroupName,
								Kind:      "Gateway",
								Namespace: "default",
							},
						},
						To: []gatewayv1beta1.ReferenceGrantTo{
							{
								Group: "",
								Kind:  "Secret",
								Name:  (lo.ToPtr(gwtypes.ObjectName("test-secret"))),
							},
						},
					},
				},
			},
			expectedSupportedKinds: []gwtypes.RouteGroupKind{
				{
					Group: (*gwtypes.Group)(&gatewayv1.GroupVersion.Group),
					Kind:  "HTTPRoute",
				},
			},
			expectedResolvedRefsCondition: metav1.Condition{
				Type:               string(gatewayv1.ListenerConditionResolvedRefs),
				Status:             metav1.ConditionTrue,
				Reason:             string(gatewayv1.ListenerReasonResolvedRefs),
				Message:            "Listeners' references are accepted.",
				ObservedGeneration: generation,
			},
		},
		{
			name:             "tls well-formed, with not allowed cross-namespace reference",
			gatewayNamespace: "default",
			listener: gwtypes.Listener{
				Protocol: gatewayv1.HTTPSProtocolType,
				TLS: &gatewayv1.GatewayTLSConfig{
					Mode: lo.ToPtr(gatewayv1.TLSModeTerminate),
					CertificateRefs: []gatewayv1.SecretObjectReference{
						{
							Name:      "test-secret",
							Namespace: (*gatewayv1.Namespace)(lo.ToPtr("other-namespace")),
						},
					},
				},
			},
			secrets: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-secret",
						Namespace: "other-namespace",
					},
					Data: map[string][]byte{
						"tls.crt": ca.CertPEM.Bytes(),
						"tls.key": ca.KeyPEM.Bytes(),
					},
				},
			},
			expectedSupportedKinds: []gwtypes.RouteGroupKind{
				{
					Group: (*gwtypes.Group)(&gatewayv1.GroupVersion.Group),
					Kind:  "HTTPRoute",
				},
			},
			expectedResolvedRefsCondition: metav1.Condition{
				Type:               string(gatewayv1.ListenerConditionResolvedRefs),
				Status:             metav1.ConditionFalse,
				Reason:             string(gatewayv1.ListenerReasonRefNotPermitted),
				Message:            "Secret other-namespace/test-secret reference not allowed by any ReferenceGrant.",
				ObservedGeneration: generation,
			},
		},
	}

	for _, tc := range testCases {

		ctx := t.Context()
		client := fakectrlruntimeclient.
			NewClientBuilder().
			WithScheme(scheme.Get()).
			WithObjects(tc.referenceGrants...).
			WithObjects(tc.secrets...).
			Build()

		t.Run(tc.name, func(t *testing.T) {
			supportedKinds, resolvedRefsCondition, err := getSupportedKindsWithResolvedRefsCondition(
				ctx,
				client,
				gatewayv1.Gateway{
					TypeMeta: metav1.TypeMeta{
						APIVersion: gatewayv1.GroupVersion.String(),
						Kind:       "Gateway",
					},
					ObjectMeta: metav1.ObjectMeta{
						Namespace: tc.gatewayNamespace,
					},
				},
				generation,
				tc.listener,
			)

			require.NoError(t, err)
			assert.Equal(t, tc.expectedSupportedKinds, supportedKinds)
			// force the transitionTimes to be equal to properly assert the conditions are equal
			resolvedRefsCondition.LastTransitionTime = tc.expectedResolvedRefsCondition.LastTransitionTime
			assert.Equal(t, tc.expectedResolvedRefsCondition, resolvedRefsCondition)
		})
	}
}

func TestGatewayConfigDataPlaneOptionsToDataPlaneOptions(t *testing.T) {
	testCases := []struct {
		name                  string
		gatewayConfigNS       string
		opts                  operatorv1beta1.GatewayConfigDataPlaneOptions
		expectedDataPlaneOpts *operatorv1beta1.DataPlaneOptions
	}{
		{
			name:                  "empty options",
			gatewayConfigNS:       "default",
			opts:                  operatorv1beta1.GatewayConfigDataPlaneOptions{},
			expectedDataPlaneOpts: &operatorv1beta1.DataPlaneOptions{},
		},
		{
			name:            "deployment options",
			gatewayConfigNS: "default",
			opts: operatorv1beta1.GatewayConfigDataPlaneOptions{
				Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
					DeploymentOptions: operatorv1beta1.DeploymentOptions{
						PodTemplateSpec: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name: "test-container",
									},
								},
							},
						},
					},
				},
			},
			expectedDataPlaneOpts: &operatorv1beta1.DataPlaneOptions{
				Deployment: operatorv1beta1.DataPlaneDeploymentOptions{
					DeploymentOptions: operatorv1beta1.DeploymentOptions{
						PodTemplateSpec: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name: "test-container",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name:            "plugins to install (same namespace)",
			gatewayConfigNS: "default",
			opts: operatorv1beta1.GatewayConfigDataPlaneOptions{
				PluginsToInstall: []operatorv1beta1.NamespacedName{
					{
						Name: "plugin1",
					},
					{
						Name:      "plugin2",
						Namespace: "default",
					},
				},
			},
			expectedDataPlaneOpts: &operatorv1beta1.DataPlaneOptions{
				PluginsToInstall: []operatorv1beta1.NamespacedName{
					{
						Name:      "plugin1",
						Namespace: "default",
					},
					{
						Name:      "plugin2",
						Namespace: "default",
					},
				},
			},
		},
		{
			name:            "plugins to install (different namespace)",
			gatewayConfigNS: "default",
			opts: operatorv1beta1.GatewayConfigDataPlaneOptions{
				PluginsToInstall: []operatorv1beta1.NamespacedName{
					{
						Name: "plugin1",
					},
					{
						Name:      "plugin2",
						Namespace: "other",
					},
				},
			},
			expectedDataPlaneOpts: &operatorv1beta1.DataPlaneOptions{
				PluginsToInstall: []operatorv1beta1.NamespacedName{
					{
						Name:      "plugin1",
						Namespace: "default",
					},
					{
						Name:      "plugin2",
						Namespace: "other",
					},
				},
			},
		},
		{
			name:            "network services options with ingress",
			gatewayConfigNS: "default",
			opts: operatorv1beta1.GatewayConfigDataPlaneOptions{
				Network: operatorv1beta1.GatewayConfigDataPlaneNetworkOptions{
					Services: &operatorv1beta1.GatewayConfigDataPlaneServices{
						Ingress: &operatorv1beta1.GatewayConfigServiceOptions{
							ServiceOptions: operatorv1beta1.ServiceOptions{
								Name: lo.ToPtr("custom-ingress"),
								Annotations: map[string]string{
									"service.beta.kubernetes.io/aws-load-balancer-type": "nlb",
								},
							},
						},
					},
				},
			},
			expectedDataPlaneOpts: &operatorv1beta1.DataPlaneOptions{
				Network: operatorv1beta1.DataPlaneNetworkOptions{
					Services: &operatorv1beta1.DataPlaneServices{
						Ingress: &operatorv1beta1.DataPlaneServiceOptions{
							ServiceOptions: operatorv1beta1.ServiceOptions{
								Name: lo.ToPtr("custom-ingress"),
								Annotations: map[string]string{
									"service.beta.kubernetes.io/aws-load-balancer-type": "nlb",
								},
							},
						},
					},
				},
			},
		},
		{
			name:            "PodDisruptionBudget",
			gatewayConfigNS: "default",
			opts: operatorv1beta1.GatewayConfigDataPlaneOptions{
				Resources: &operatorv1beta1.GatewayConfigDataPlaneResources{
					PodDisruptionBudget: &operatorv1beta1.PodDisruptionBudget{
						Spec: operatorv1beta1.PodDisruptionBudgetSpec{
							MinAvailable: lo.ToPtr(intstr.FromInt(1)),
						},
					},
				},
			},
			expectedDataPlaneOpts: &operatorv1beta1.DataPlaneOptions{
				Resources: operatorv1beta1.DataPlaneResources{
					PodDisruptionBudget: &operatorv1beta1.PodDisruptionBudget{
						Spec: operatorv1beta1.PodDisruptionBudgetSpec{
							MinAvailable: lo.ToPtr(intstr.FromInt(1)),
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := gatewayConfigDataPlaneOptionsToDataPlaneOptions(tc.gatewayConfigNS, tc.opts)
			assert.Equal(t, tc.expectedDataPlaneOpts, result)
		})
	}
}

func TestCountAttachedRoutesForGatewayListener(t *testing.T) {
	testCases := []struct {
		Name           string
		Gateway        gwtypes.Gateway
		Objects        []client.Object
		ExpectedRoutes []int32
		ExpectedError  []error
	}{
		{
			Name: "no routes",
			Gateway: gwtypes.Gateway{
				TypeMeta: metav1.TypeMeta{
					APIVersion: gatewayv1.GroupVersion.String(),
					Kind:       "Gateway",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gw",
					Namespace: "test-namespace",
				},
				Spec: gwtypes.GatewaySpec{
					Listeners: []gwtypes.Listener{
						{
							AllowedRoutes: &gwtypes.AllowedRoutes{
								Namespaces: &gwtypes.RouteNamespaces{
									From: lo.ToPtr(gwtypes.NamespacesFromSame),
								},
							},
						},
					},
				},
			},
			ExpectedRoutes: []int32{0},
			ExpectedError:  []error{nil},
		},
		{
			Name: "1 HTTPRoute in the same namespace as the Gateway",
			Gateway: gwtypes.Gateway{
				TypeMeta: metav1.TypeMeta{
					APIVersion: gatewayv1.GroupVersion.String(),
					Kind:       "Gateway",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gw",
					Namespace: "test-namespace",
				},
				Spec: gwtypes.GatewaySpec{
					Listeners: []gwtypes.Listener{
						{
							Name:     gatewayv1.SectionName("http"),
							Protocol: gwtypes.HTTPProtocolType,
							AllowedRoutes: &gwtypes.AllowedRoutes{
								Namespaces: &gwtypes.RouteNamespaces{
									From: lo.ToPtr(gwtypes.NamespacesFromSame),
								},
							},
						},
					},
				},
			},
			Objects: []client.Object{
				&gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "route-1",
						Namespace: "test-namespace",
					},
					Spec: gwtypes.HTTPRouteSpec{
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{
									Name:  gwtypes.ObjectName("test-gw"),
									Group: (*gwtypes.Group)(&gatewayv1.GroupVersion.Group),
									Kind:  lo.ToPtr(gwtypes.Kind("Gateway")),
								},
							},
						},
					},
				},
			},
			ExpectedRoutes: []int32{1},
			ExpectedError:  []error{nil},
		},
		{
			Name: "1 HTTPRoute in a different namespace than the Gateway",
			Gateway: gwtypes.Gateway{
				TypeMeta: metav1.TypeMeta{
					APIVersion: gatewayv1.GroupVersion.String(),
					Kind:       "Gateway",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gw",
					Namespace: "test-namespace",
				},
				Spec: gwtypes.GatewaySpec{
					Listeners: []gwtypes.Listener{
						{
							Name:     gatewayv1.SectionName("http"),
							Protocol: gwtypes.HTTPProtocolType,
							AllowedRoutes: &gwtypes.AllowedRoutes{
								Namespaces: &gwtypes.RouteNamespaces{
									From: lo.ToPtr(gwtypes.NamespacesFromSame),
								},
							},
						},
					},
				},
			},
			Objects: []client.Object{
				&gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "route-1",
						Namespace: "test-namespace-2",
					},
					Spec: gwtypes.HTTPRouteSpec{
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{
									Name:  gwtypes.ObjectName("test-gw"),
									Group: (*gwtypes.Group)(&gatewayv1.GroupVersion.Group),
									Kind:  lo.ToPtr(gwtypes.Kind("Gateway")),
								},
							},
						},
					},
				},
			},
			ExpectedRoutes: []int32{0},
			ExpectedError:  []error{nil},
		},
		{
			Name: "1 HTTPRoute in a different namespace than the Gateway but allowed through 'All' namespace selector",
			Gateway: gwtypes.Gateway{
				TypeMeta: metav1.TypeMeta{
					APIVersion: gatewayv1.GroupVersion.String(),
					Kind:       "Gateway",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gw",
					Namespace: "test-namespace",
				},
				Spec: gwtypes.GatewaySpec{
					Listeners: []gwtypes.Listener{
						{
							Name:     gatewayv1.SectionName("http"),
							Protocol: gwtypes.HTTPProtocolType,
							AllowedRoutes: &gwtypes.AllowedRoutes{
								Namespaces: &gwtypes.RouteNamespaces{
									From: lo.ToPtr(gwtypes.NamespacesFromAll),
								},
							},
						},
					},
				},
			},
			Objects: []client.Object{
				&gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "route-1",
						Namespace: "test-namespace-2",
					},
					Spec: gwtypes.HTTPRouteSpec{
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{
									Name:  gwtypes.ObjectName("test-gw"),
									Group: (*gwtypes.Group)(&gatewayv1.GroupVersion.Group),
									Kind:  lo.ToPtr(gwtypes.Kind("Gateway")),
								},
							},
						},
					},
				},
			},
			ExpectedRoutes: []int32{1},
			ExpectedError:  []error{nil},
		},
		{
			Name: "2 HTTPRoutes, 1 matching the Gateway's namespace and 1 not",
			Gateway: gwtypes.Gateway{
				TypeMeta: metav1.TypeMeta{
					APIVersion: gatewayv1.GroupVersion.String(),
					Kind:       "Gateway",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gw",
					Namespace: "test-namespace",
				},
				Spec: gwtypes.GatewaySpec{
					Listeners: []gwtypes.Listener{
						{
							Name:     gatewayv1.SectionName("http"),
							Protocol: gwtypes.HTTPProtocolType,
							AllowedRoutes: &gwtypes.AllowedRoutes{
								Namespaces: &gwtypes.RouteNamespaces{
									From: lo.ToPtr(gwtypes.NamespacesFromSame),
								},
							},
						},
					},
				},
			},
			Objects: []client.Object{
				&gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "route-1",
						Namespace: "test-namespace-2",
					},
					Spec: gwtypes.HTTPRouteSpec{
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{
									Name:  gwtypes.ObjectName("test-gw"),
									Group: (*gwtypes.Group)(&gatewayv1.GroupVersion.Group),
									Kind:  lo.ToPtr(gwtypes.Kind("Gateway")),
								},
							},
						},
					},
				},
				&gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "route-2",
						Namespace: "test-namespace",
					},
					Spec: gwtypes.HTTPRouteSpec{
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{
									Name:  gwtypes.ObjectName("test-gw"),
									Group: (*gwtypes.Group)(&gatewayv1.GroupVersion.Group),
									Kind:  lo.ToPtr(gwtypes.Kind("Gateway")),
								},
							},
						},
					},
				},
			},
			ExpectedRoutes: []int32{1},
			ExpectedError:  []error{nil},
		},
		{
			Name: "2 HTTPRoutes, both matching due to 'All' selector used",
			Gateway: gwtypes.Gateway{
				TypeMeta: metav1.TypeMeta{
					APIVersion: gatewayv1.GroupVersion.String(),
					Kind:       "Gateway",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gw",
					Namespace: "test-namespace",
				},
				Spec: gwtypes.GatewaySpec{
					Listeners: []gwtypes.Listener{
						{
							Name:     gatewayv1.SectionName("http"),
							Protocol: gwtypes.HTTPProtocolType,
							AllowedRoutes: &gwtypes.AllowedRoutes{
								Namespaces: &gwtypes.RouteNamespaces{
									From: lo.ToPtr(gwtypes.NamespacesFromAll),
								},
							},
						},
					},
				},
			},
			Objects: []client.Object{
				&gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "route-1",
						Namespace: "test-namespace-2",
					},
					Spec: gwtypes.HTTPRouteSpec{
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{
									Name:  gwtypes.ObjectName("test-gw"),
									Group: (*gwtypes.Group)(&gatewayv1.GroupVersion.Group),
									Kind:  lo.ToPtr(gwtypes.Kind("Gateway")),
								},
							},
						},
					},
				},
				&gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "route-2",
						Namespace: "test-namespace",
					},
					Spec: gwtypes.HTTPRouteSpec{
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{
									Name:  gwtypes.ObjectName("test-gw"),
									Group: (*gwtypes.Group)(&gatewayv1.GroupVersion.Group),
									Kind:  lo.ToPtr(gwtypes.Kind("Gateway")),
								},
							},
						},
					},
				},
			},
			ExpectedRoutes: []int32{2},
			ExpectedError:  []error{nil},
		},
		{
			Name: "1 HTTPRoute, not matching due to namespace label selector not matching",
			Gateway: gwtypes.Gateway{
				TypeMeta: metav1.TypeMeta{
					APIVersion: gatewayv1.GroupVersion.String(),
					Kind:       "Gateway",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gw",
					Namespace: "test-namespace",
				},
				Spec: gwtypes.GatewaySpec{
					Listeners: []gwtypes.Listener{
						{
							Name:     gatewayv1.SectionName("http"),
							Protocol: gwtypes.HTTPProtocolType,
							AllowedRoutes: &gwtypes.AllowedRoutes{
								Namespaces: &gwtypes.RouteNamespaces{
									From: lo.ToPtr(gwtypes.NamespacesFromSelector),
									Selector: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											"kubernetes.io/metadata.name": "test-namespace-non-existing",
										},
									},
								},
							},
						},
					},
				},
			},
			Objects: []client.Object{
				&gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "route-1",
						Namespace: "test-namespace",
					},
					Spec: gwtypes.HTTPRouteSpec{
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{
									Name:  gwtypes.ObjectName("test-gw"),
									Group: (*gwtypes.Group)(&gatewayv1.GroupVersion.Group),
									Kind:  lo.ToPtr(gwtypes.Kind("Gateway")),
								},
							},
						},
					},
				},
			},
			ExpectedRoutes: []int32{0},
			ExpectedError:  []error{nil},
		},
		{
			Name: "1 HTTPRoute, matching thanks to namespace label selector matching",
			Gateway: gwtypes.Gateway{
				TypeMeta: metav1.TypeMeta{
					APIVersion: gatewayv1.GroupVersion.String(),
					Kind:       "Gateway",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gw",
					Namespace: "test-namespace",
				},
				Spec: gwtypes.GatewaySpec{
					Listeners: []gwtypes.Listener{
						{
							Name:     gatewayv1.SectionName("http"),
							Protocol: gwtypes.HTTPProtocolType,
							AllowedRoutes: &gwtypes.AllowedRoutes{
								Namespaces: &gwtypes.RouteNamespaces{
									From: lo.ToPtr(gwtypes.NamespacesFromSelector),
									Selector: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											"kubernetes.io/metadata.name": "test-namespace-2",
										},
									},
								},
							},
						},
					},
				},
			},
			Objects: []client.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-namespace-2",
						Labels: map[string]string{
							"kubernetes.io/metadata.name": "test-namespace-2",
						},
					},
				},
				&gwtypes.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "route-1",
						Namespace: "test-namespace-2",
					},
					Spec: gwtypes.HTTPRouteSpec{
						CommonRouteSpec: gwtypes.CommonRouteSpec{
							ParentRefs: []gwtypes.ParentReference{
								{
									Name:  gwtypes.ObjectName("test-gw"),
									Group: (*gwtypes.Group)(&gatewayv1.GroupVersion.Group),
									Kind:  lo.ToPtr(gwtypes.Kind("Gateway")),
								},
							},
						},
					},
				},
			},
			ExpectedRoutes: []int32{1},
			ExpectedError:  []error{nil},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			client := fakectrlruntimeclient.
				NewClientBuilder().
				WithScheme(scheme.Get()).
				WithObjects(&tc.Gateway).
				WithObjects(tc.Objects...).
				Build()

			ctx := t.Context()
			for i, listener := range tc.Gateway.Spec.Listeners {
				routes, err := countAttachedRoutesForGatewayListener(ctx, &tc.Gateway, listener, client)
				assert.Equal(t, tc.ExpectedRoutes[i], routes, "#%d", i)
				assert.Equal(t, tc.ExpectedError[i], err, "#%d", i)
			}
		})
	}
}
