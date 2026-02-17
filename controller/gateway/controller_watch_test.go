package gateway

import (
	"testing"

	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	operatorv2beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v2beta1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/internal/utils/index"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/vars"
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
								TLS: &gatewayv1.ListenerTLSConfig{
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
								TLS: &gatewayv1.ListenerTLSConfig{
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
								TLS: &gatewayv1.ListenerTLSConfig{
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
								TLS: &gatewayv1.ListenerTLSConfig{
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
								TLS: &gatewayv1.ListenerTLSConfig{
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
			for _, opt := range index.OptionsForGateway() {
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

func TestReconciler_listGatewaysForKongReferenceGrant(t *testing.T) {
	gatewayClassWithParamsRef := &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-gatewayclass",
		},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: gatewayv1.GatewayController(vars.ControllerName()),
			ParametersRef: &gatewayv1.ParametersReference{
				Group:     gatewayv1.Group(operatorv1beta1.SchemeGroupVersion.Group),
				Kind:      "GatewayConfiguration",
				Name:      "test-gwconfig",
				Namespace: lo.ToPtr(gatewayv1.Namespace("default")),
			},
		},
	}

	gatewayUsingClass := &gwtypes.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gateway",
			Namespace: "default",
			UID:       types.UID(uuid.NewString()),
		},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: "test-gatewayclass",
			Listeners: []gatewayv1.Listener{
				{
					Name:     "http",
					Port:     80,
					Protocol: gatewayv1.HTTPProtocolType,
				},
			},
		},
	}

	gwConfigWithAuthRef := &operatorv2beta1.GatewayConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gwconfig",
			Namespace: "default",
		},
		Spec: operatorv2beta1.GatewayConfigurationSpec{
			Konnect: &operatorv2beta1.KonnectOptions{
				APIAuthConfigurationRef: &konnectv1alpha2.ControlPlaneKonnectAPIAuthConfigurationRef{
					Name:      "my-auth",
					Namespace: lo.ToPtr("auth-ns"),
				},
			},
		},
	}

	testCases := []struct {
		name             string
		obj              client.Object
		objects          []client.Object
		expectedRequests []reconcile.Request
	}{
		{
			name: "kong reference grant with matching gateway configuration returns gateway requests",
			obj: &configurationv1alpha1.KongReferenceGrant{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-grant",
					Namespace: "auth-ns",
				},
				Spec: configurationv1alpha1.KongReferenceGrantSpec{
					From: []configurationv1alpha1.ReferenceGrantFrom{
						{
							Group:     configurationv1alpha1.Group(operatorv2beta1.SchemeGroupVersion.Group),
							Kind:      "GatewayConfiguration",
							Namespace: "default",
						},
					},
					To: []configurationv1alpha1.ReferenceGrantTo{
						{
							Group: configurationv1alpha1.Group(konnectv1alpha1.GroupVersion.Group),
							Kind:  "KonnectAPIAuthConfiguration",
							Name:  lo.ToPtr(configurationv1alpha1.ObjectName("my-auth")),
						},
					},
				},
			},
			objects: []client.Object{
				gwConfigWithAuthRef,
				gatewayClassWithParamsRef,
				gatewayUsingClass,
			},
			expectedRequests: []reconcile.Request{
				{NamespacedName: types.NamespacedName{Namespace: "default", Name: "test-gateway"}},
			},
		},
		{
			name: "kong reference grant with no matching from group returns empty",
			obj: &configurationv1alpha1.KongReferenceGrant{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-grant",
					Namespace: "auth-ns",
				},
				Spec: configurationv1alpha1.KongReferenceGrantSpec{
					From: []configurationv1alpha1.ReferenceGrantFrom{
						{
							Group:     "wrong.group.io",
							Kind:      "GatewayConfiguration",
							Namespace: "default",
						},
					},
					To: []configurationv1alpha1.ReferenceGrantTo{
						{
							Group: configurationv1alpha1.Group(konnectv1alpha1.GroupVersion.Group),
							Kind:  "KonnectAPIAuthConfiguration",
							Name:  lo.ToPtr(configurationv1alpha1.ObjectName("my-auth")),
						},
					},
				},
			},
			objects: []client.Object{
				gwConfigWithAuthRef,
				gatewayClassWithParamsRef,
				gatewayUsingClass,
			},
			expectedRequests: nil,
		},
		{
			name: "kong reference grant with no matching to kind returns empty",
			obj: &configurationv1alpha1.KongReferenceGrant{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-grant",
					Namespace: "auth-ns",
				},
				Spec: configurationv1alpha1.KongReferenceGrantSpec{
					From: []configurationv1alpha1.ReferenceGrantFrom{
						{
							Group:     configurationv1alpha1.Group(operatorv2beta1.SchemeGroupVersion.Group),
							Kind:      "GatewayConfiguration",
							Namespace: "default",
						},
					},
					To: []configurationv1alpha1.ReferenceGrantTo{
						{
							Group: configurationv1alpha1.Group(konnectv1alpha1.GroupVersion.Group),
							Kind:  "WrongKind",
							Name:  lo.ToPtr(configurationv1alpha1.ObjectName("my-auth")),
						},
					},
				},
			},
			objects: []client.Object{
				gwConfigWithAuthRef,
				gatewayClassWithParamsRef,
				gatewayUsingClass,
			},
			expectedRequests: nil,
		},
		{
			name: "kong reference grant allowing any auth name matches gateway configuration",
			obj: &configurationv1alpha1.KongReferenceGrant{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-grant",
					Namespace: "auth-ns",
				},
				Spec: configurationv1alpha1.KongReferenceGrantSpec{
					From: []configurationv1alpha1.ReferenceGrantFrom{
						{
							Group:     configurationv1alpha1.Group(operatorv2beta1.SchemeGroupVersion.Group),
							Kind:      "GatewayConfiguration",
							Namespace: "default",
						},
					},
					To: []configurationv1alpha1.ReferenceGrantTo{
						{
							Group: configurationv1alpha1.Group(konnectv1alpha1.GroupVersion.Group),
							Kind:  "KonnectAPIAuthConfiguration",
							Name:  nil,
						},
					},
				},
			},
			objects: []client.Object{
				gwConfigWithAuthRef,
				gatewayClassWithParamsRef,
				gatewayUsingClass,
			},
			expectedRequests: []reconcile.Request{
				{NamespacedName: types.NamespacedName{Namespace: "default", Name: "test-gateway"}},
			},
		},
		{
			name: "kong reference grant with specific name not matching returns empty",
			obj: &configurationv1alpha1.KongReferenceGrant{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-grant",
					Namespace: "auth-ns",
				},
				Spec: configurationv1alpha1.KongReferenceGrantSpec{
					From: []configurationv1alpha1.ReferenceGrantFrom{
						{
							Group:     configurationv1alpha1.Group(operatorv2beta1.SchemeGroupVersion.Group),
							Kind:      "GatewayConfiguration",
							Namespace: "default",
						},
					},
					To: []configurationv1alpha1.ReferenceGrantTo{
						{
							Group: configurationv1alpha1.Group(konnectv1alpha1.GroupVersion.Group),
							Kind:  "KonnectAPIAuthConfiguration",
							Name:  lo.ToPtr(configurationv1alpha1.ObjectName("other-auth")),
						},
					},
				},
			},
			objects: []client.Object{
				gwConfigWithAuthRef,
				gatewayClassWithParamsRef,
				gatewayUsingClass,
			},
			expectedRequests: nil,
		},
		{
			name: "wrong type passed returns empty",
			obj: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "some-namespace",
				},
			},
			objects:          nil,
			expectedRequests: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()

			clientBuilder := fakectrlruntimeclient.NewClientBuilder().
				WithScheme(scheme.Get())

			for _, obj := range tc.objects {
				clientBuilder.WithObjects(obj)
			}

			fakeClient := clientBuilder.Build()

			reconciler := &Reconciler{
				Client: fakeClient,
			}

			requests := reconciler.listGatewaysForKongReferenceGrant(ctx, tc.obj)

			require.ElementsMatch(t, tc.expectedRequests, requests)
		})
	}
}
