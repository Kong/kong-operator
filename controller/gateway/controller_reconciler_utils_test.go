package gateway

import (
	"context"
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

	operatorv1beta1 "github.com/kong/gateway-operator/api/v1beta1"
	gwtypes "github.com/kong/gateway-operator/internal/types"
	"github.com/kong/gateway-operator/modules/manager/scheme"
	"github.com/kong/gateway-operator/pkg/consts"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"
)

// func init() {
// 	if err := gatewayv1.Install(scheme.Scheme); err != nil {
// 		fmt.Println("error while adding gatewayv1 scheme")
// 		os.Exit(1)
// 	}
// 	if err := gatewayv1beta1.Install(scheme.Scheme); err != nil {
// 		fmt.Println("error while adding gatewayv1 scheme")
// 		os.Exit(1)
// 	}
// }

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

func TestSetDataPlaneIngressServicePorts(t *testing.T) {
	testCases := []struct {
		name          string
		listeners     []gwtypes.Listener
		expectedPorts []operatorv1beta1.DataPlaneServicePort
		expectedError error
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
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tc := tc
			err := setDataPlaneIngressServicePorts(&operatorv1beta1.DataPlaneOptions{}, tc.listeners)
			if tc.expectedError == nil {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, tc.expectedError.Error())
			}
		})
	}
}

func TestIsSecretCrossReferenceGranted(t *testing.T) {
	customizeReferenceGrant := func(rg gatewayv1beta1.ReferenceGrant, opts ...func(rg *gatewayv1beta1.ReferenceGrant)) gatewayv1beta1.ReferenceGrant {
		rg = *rg.DeepCopy()
		for _, opt := range opts {
			opt(&rg)
		}
		return rg
	}

	badSecretName := gwtypes.ObjectName("wrong-secret")
	emptySecretName := gwtypes.ObjectName("")
	goodSecretName := gwtypes.ObjectName("good-secret")
	referenceGrant := gatewayv1beta1.ReferenceGrant{
		Spec: gatewayv1beta1.ReferenceGrantSpec{
			From: []gatewayv1beta1.ReferenceGrantFrom{
				{
					Group:     gatewayv1.GroupName,
					Kind:      "Gateway",
					Namespace: "goodNamespace",
				},
			},
			To: []gatewayv1beta1.ReferenceGrantTo{
				{
					Group: "",
					Kind:  "Secret",
					Name:  &goodSecretName,
				},
			},
		},
	}

	testCases := []struct {
		name            string
		referenceGrants []gatewayv1beta1.ReferenceGrant
		isGranted       bool
	}{
		{
			name:      "no referenceGrants",
			isGranted: false,
		},
		{
			name: "granted",
			referenceGrants: []gatewayv1beta1.ReferenceGrant{
				referenceGrant,
			},
			isGranted: true,
		},
		{
			name: "not granted, bad 'from' group",
			referenceGrants: []gatewayv1beta1.ReferenceGrant{
				customizeReferenceGrant(referenceGrant, func(rg *gatewayv1beta1.ReferenceGrant) {
					rg.Spec.From[0].Group = "wrong-group"
				}),
			},
			isGranted: false,
		},
		{
			name: "not granted, bad 'to' group",
			referenceGrants: []gatewayv1beta1.ReferenceGrant{
				customizeReferenceGrant(referenceGrant, func(rg *gatewayv1beta1.ReferenceGrant) {
					rg.Spec.To[0].Group = "wrong-group"
				}),
			},
			isGranted: false,
		},
		{
			name: "not granted, bad 'from' kind",
			referenceGrants: []gatewayv1beta1.ReferenceGrant{
				customizeReferenceGrant(referenceGrant, func(rg *gatewayv1beta1.ReferenceGrant) {
					rg.Spec.From[0].Kind = "wrong-kind"
				}),
			},
			isGranted: false,
		},
		{
			name: "not granted, bad 'to' kind",
			referenceGrants: []gatewayv1beta1.ReferenceGrant{
				customizeReferenceGrant(referenceGrant, func(rg *gatewayv1beta1.ReferenceGrant) {
					rg.Spec.To[0].Kind = "wrong-kind"
				}),
			},
			isGranted: false,
		},
		{
			name: "not granted, bad 'from' namespace",
			referenceGrants: []gatewayv1beta1.ReferenceGrant{
				customizeReferenceGrant(referenceGrant, func(rg *gatewayv1beta1.ReferenceGrant) {
					rg.Spec.From[0].Namespace = "bad-namespace"
				}),
			},
			isGranted: false,
		},
		{
			name: "not granted, empty 'to' secret name",
			referenceGrants: []gatewayv1beta1.ReferenceGrant{
				customizeReferenceGrant(referenceGrant, func(rg *gatewayv1beta1.ReferenceGrant) {
					rg.Spec.To[0].Name = &emptySecretName
				}),
			},
			isGranted: false,
		},
		{
			name: "not granted, bad 'to' secret name",
			referenceGrants: []gatewayv1beta1.ReferenceGrant{
				customizeReferenceGrant(referenceGrant, func(rg *gatewayv1beta1.ReferenceGrant) {
					rg.Spec.To[0].Name = &badSecretName
				}),
			},
			isGranted: false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.isGranted, isSecretCrossReferenceGranted("goodNamespace", goodSecretName, tc.referenceGrants))
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
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.needsUpdate, gatewayStatusNeedsUpdate(tc.oldGateway, tc.newGateway))
		})
	}
}

func TestGetSupportedKindsWithResolvedRefsCondition(t *testing.T) {
	var generation int64 = 1

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
				Reason:             string(ListenerReasonTooManyTLSSecrets),
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
								Name:  (*gwtypes.ObjectName)(lo.ToPtr("test-secret")),
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
			name:             "tls well-formed, with unallowed cross-namespace reference",
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
				Message:            "Secret other-namespace/test-secret reference not allowed by any referenceGrant.",
				ObservedGeneration: generation,
			},
		},
	}

	for _, tc := range testCases {
		tc := tc
		ctx := context.Background()
		client := fakectrlruntimeclient.
			NewClientBuilder().
			WithScheme(scheme.Get()).
			WithObjects(tc.referenceGrants...).
			WithObjects(tc.secrets...).
			Build()

		t.Run(tc.name, func(t *testing.T) {
			supportedKinds, resolvedRefsCondition, err := getSupportedKindsWithResolvedRefsCondition(ctx,
				client,
				tc.gatewayNamespace,
				generation,
				tc.listener)

			assert.NoError(t, err)
			assert.Equal(t, supportedKinds, tc.expectedSupportedKinds)
			// force the transitionTimes to be equal to properly assert the conditions are equal
			resolvedRefsCondition.LastTransitionTime = tc.expectedResolvedRefsCondition.LastTransitionTime
			assert.Equal(t, tc.expectedResolvedRefsCondition, resolvedRefsCondition)
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

			ctx := context.Background()
			for i, listener := range tc.Gateway.Spec.Listeners {
				routes, err := countAttachedRoutesForGatewayListener(ctx, &tc.Gateway, listener, client)
				assert.Equal(t, tc.ExpectedRoutes[i], routes, "#%d", i)
				assert.Equal(t, tc.ExpectedError[i], err, "#%d", i)
			}
		})
	}
}
