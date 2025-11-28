package gateway

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/internal/utils/index"
	"github.com/kong/kong-operator/modules/manager/scheme"
	"github.com/kong/kong-operator/pkg/vars"
)

func TestReconciler_listGatewayReconcileRequestsForSecret(t *testing.T) {
	testCases := []struct {
		name             string
		secret           *corev1.Secret
		gatewayClass     *gatewayv1.GatewayClass
		gateways         []*gwtypes.Gateway
		expectedRequests []reconcile.Request
	}{
		{
			name: "secret referenced by single gateway returns one request",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cert",
					Namespace: "default",
				},
			},
			gatewayClass: &gatewayv1.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-gatewayclass",
				},
				Spec: gatewayv1.GatewayClassSpec{
					ControllerName: gatewayv1.GatewayController(vars.ControllerName()),
				},
				Status: gatewayv1.GatewayClassStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(gatewayv1.GatewayClassConditionStatusAccepted),
							Status:             metav1.ConditionTrue,
							Reason:             string(gatewayv1.GatewayClassReasonAccepted),
							LastTransitionTime: metav1.Now(),
						},
					},
				},
			},
			gateways: []*gwtypes.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gw-1",
						Namespace: "default",
						UID:       types.UID(uuid.NewString()),
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: "test-gatewayclass",
						Listeners: []gatewayv1.Listener{
							{
								Name:     "https",
								Port:     443,
								Protocol: gatewayv1.HTTPSProtocolType,
								TLS: &gatewayv1.GatewayTLSConfig{
									CertificateRefs: []gatewayv1.SecretObjectReference{
										{Name: "test-cert"},
									},
								},
							},
						},
					},
				},
			},
			expectedRequests: []reconcile.Request{
				{NamespacedName: types.NamespacedName{Namespace: "default", Name: "gw-1"}},
			},
		},
		{
			name: "secret referenced by multiple gateways returns multiple requests",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "shared-cert",
					Namespace: "default",
				},
			},
			gatewayClass: &gatewayv1.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-gatewayclass",
				},
				Spec: gatewayv1.GatewayClassSpec{
					ControllerName: gatewayv1.GatewayController(vars.ControllerName()),
				},
				Status: gatewayv1.GatewayClassStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(gatewayv1.GatewayClassConditionStatusAccepted),
							Status:             metav1.ConditionTrue,
							Reason:             string(gatewayv1.GatewayClassReasonAccepted),
							LastTransitionTime: metav1.Now(),
						},
					},
				},
			},
			gateways: []*gwtypes.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gw-1",
						Namespace: "default",
						UID:       types.UID(uuid.NewString()),
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: "test-gatewayclass",
						Listeners: []gatewayv1.Listener{
							{
								Name:     "https",
								Port:     443,
								Protocol: gatewayv1.HTTPSProtocolType,
								TLS: &gatewayv1.GatewayTLSConfig{
									CertificateRefs: []gatewayv1.SecretObjectReference{
										{Name: "shared-cert"},
									},
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gw-2",
						Namespace: "default",
						UID:       types.UID(uuid.NewString()),
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: "test-gatewayclass",
						Listeners: []gatewayv1.Listener{
							{
								Name:     "https",
								Port:     443,
								Protocol: gatewayv1.HTTPSProtocolType,
								TLS: &gatewayv1.GatewayTLSConfig{
									CertificateRefs: []gatewayv1.SecretObjectReference{
										{Name: "shared-cert"},
									},
								},
							},
						},
					},
				},
			},
			expectedRequests: []reconcile.Request{
				{NamespacedName: types.NamespacedName{Namespace: "default", Name: "gw-1"}},
				{NamespacedName: types.NamespacedName{Namespace: "default", Name: "gw-2"}},
			},
		},
		{
			name: "secret not referenced by any gateway returns empty",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "unused-cert",
					Namespace: "default",
				},
			},
			gatewayClass: &gatewayv1.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-gatewayclass",
				},
				Spec: gatewayv1.GatewayClassSpec{
					ControllerName: gatewayv1.GatewayController(vars.ControllerName()),
				},
				Status: gatewayv1.GatewayClassStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(gatewayv1.GatewayClassConditionStatusAccepted),
							Status:             metav1.ConditionTrue,
							Reason:             string(gatewayv1.GatewayClassReasonAccepted),
							LastTransitionTime: metav1.Now(),
						},
					},
				},
			},
			gateways: []*gwtypes.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gw-1",
						Namespace: "default",
						UID:       types.UID(uuid.NewString()),
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: "test-gatewayclass",
						Listeners: []gatewayv1.Listener{
							{
								Name:     "https",
								Port:     443,
								Protocol: gatewayv1.HTTPSProtocolType,
								TLS: &gatewayv1.GatewayTLSConfig{
									CertificateRefs: []gatewayv1.SecretObjectReference{
										{Name: "other-cert"},
									},
								},
							},
						},
					},
				},
			},
			expectedRequests: nil,
		},
		{
			name: "gateway with non-matching gatewayclass is filtered out",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cert",
					Namespace: "default",
				},
			},
			gatewayClass: &gatewayv1.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "other-gatewayclass",
				},
				Spec: gatewayv1.GatewayClassSpec{
					ControllerName: gatewayv1.GatewayController("other-controller"),
				},
				Status: gatewayv1.GatewayClassStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(gatewayv1.GatewayClassConditionStatusAccepted),
							Status:             metav1.ConditionTrue,
							Reason:             string(gatewayv1.GatewayClassReasonAccepted),
							LastTransitionTime: metav1.Now(),
						},
					},
				},
			},
			gateways: []*gwtypes.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gw-1",
						Namespace: "default",
						UID:       types.UID(uuid.NewString()),
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: "other-gatewayclass",
						Listeners: []gatewayv1.Listener{
							{
								Name:     "https",
								Port:     443,
								Protocol: gatewayv1.HTTPSProtocolType,
								TLS: &gatewayv1.GatewayTLSConfig{
									CertificateRefs: []gatewayv1.SecretObjectReference{
										{Name: "test-cert"},
									},
								},
							},
						},
					},
				},
			},
			expectedRequests: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()

			// Build client with index and objects.
			clientBuilder := fakectrlruntimeclient.NewClientBuilder().
				WithScheme(scheme.Get()).
				WithObjects(tc.secret, tc.gatewayClass)

			for _, gw := range tc.gateways {
				clientBuilder.WithObjects(gw)
			}

			// Register the TLS certificate index.
			for _, opt := range index.OptionsForGatewayTLSSecret() {
				clientBuilder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
			}

			fakeClient := clientBuilder.Build()

			reconciler := &Reconciler{
				Client: fakeClient,
			}

			requests := reconciler.listGatewayReconcileRequestsForSecret(ctx, tc.secret)

			require.ElementsMatch(t, tc.expectedRequests, requests)
		})
	}
}
