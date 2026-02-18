package resources

import (
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
)

func TestGetSelectorOverrides(t *testing.T) {
	testCases := []struct {
		name             string
		annotationValue  string
		expectedSelector map[string]string
		needsErr         bool
	}{
		{
			name:     "no annotation",
			needsErr: true,
		},
		{
			name:            "malformed annotation value",
			annotationValue: "malformedSelector",
			needsErr:        true,
		},
		{
			name:            "valid selector + incomplete selector 1",
			annotationValue: "app=test,app2",
			needsErr:        true,
		},
		{
			name:            "valid selector + incomplete selector 2",
			annotationValue: "app=test,app2=",
			needsErr:        true,
		},
		{
			name:            "valid selector + incomplete selector 3",
			annotationValue: "app=test,",
			needsErr:        true,
		},
		{
			name:            "single selector",
			annotationValue: "app=test",
			expectedSelector: map[string]string{
				"app": "test",
			},
			needsErr: false,
		},
		{
			name:            "multiple selectors",
			annotationValue: "app=test,app2=test2",
			expectedSelector: map[string]string{
				"app":  "test",
				"app2": "test2",
			},
			needsErr: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			newSelector, err := getSelectorOverrides(tc.annotationValue)
			if tc.needsErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tc.expectedSelector, newSelector)
		})
	}
}

func TestGenerateNewIngressServiceForDataPlane(t *testing.T) {
	testCases := []struct {
		name        string
		dataplane   *operatorv1beta1.DataPlane
		expectedSvc *corev1.Service
		expectedErr error
	}{
		{
			name: "base",
			dataplane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dp-1",
					Namespace: "default",
					UID:       types.UID("1234"),
				},
				TypeMeta: metav1.TypeMeta{
					APIVersion: "gateway.konghq.com/v1beta1",
					Kind:       "DataPlane",
				},
			},
			expectedSvc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "dataplane-ingress-dp-1-",
					Namespace:    "default",
					Labels: map[string]string{
						"app": "dp-1",
						"gateway-operator.konghq.com/dataplane-service-type": "ingress",
						"gateway-operator.konghq.com/managed-by":             "dataplane",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "gateway.konghq.com/v1beta1",
							Kind:       "DataPlane",
							Name:       "dp-1",
							UID:        "1234",
							Controller: lo.ToPtr(true),
						},
					},
					Finalizers: []string{
						"gateway-operator.konghq.com/wait-for-owner",
					},
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeLoadBalancer,
					Ports: []corev1.ServicePort{
						{
							Name:       "http",
							Protocol:   corev1.ProtocolTCP,
							Port:       80,
							TargetPort: intstr.FromInt(8000),
						},
						{
							Name:       "https",
							Protocol:   corev1.ProtocolTCP,
							Port:       443,
							TargetPort: intstr.FromInt(8443),
						},
					},
					Selector: map[string]string{
						"app": "dp-1",
					},
				},
			},
			expectedErr: nil,
		},
		{
			name: "setting ExternalTrafficPolicy to Local",
			dataplane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dp-1",
					Namespace: "default",
					UID:       types.UID("1234"),
				},
				TypeMeta: metav1.TypeMeta{
					APIVersion: "gateway.konghq.com/v1beta1",
					Kind:       "DataPlane",
				},
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Network: operatorv1beta1.DataPlaneNetworkOptions{
							Services: &operatorv1beta1.DataPlaneServices{
								Ingress: &operatorv1beta1.DataPlaneServiceOptions{
									ServiceOptions: operatorv1beta1.ServiceOptions{
										ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyTypeLocal,
										Type:                  corev1.ServiceTypeLoadBalancer,
									},
								},
							},
						},
					},
				},
			},
			expectedSvc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "dataplane-ingress-dp-1-",
					Namespace:    "default",
					Labels: map[string]string{
						"app": "dp-1",
						"gateway-operator.konghq.com/dataplane-service-type": "ingress",
						"gateway-operator.konghq.com/managed-by":             "dataplane",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "gateway.konghq.com/v1beta1",
							Kind:       "DataPlane",
							Name:       "dp-1",
							UID:        "1234",
							Controller: lo.ToPtr(true),
						},
					},
					Finalizers: []string{
						"gateway-operator.konghq.com/wait-for-owner",
					},
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeLoadBalancer,
					Ports: []corev1.ServicePort{
						{
							Name:       "http",
							Protocol:   corev1.ProtocolTCP,
							Port:       80,
							TargetPort: intstr.FromInt(8000),
						},
						{
							Name:       "https",
							Protocol:   corev1.ProtocolTCP,
							Port:       443,
							TargetPort: intstr.FromInt(8443),
						},
					},
					Selector: map[string]string{
						"app": "dp-1",
					},
					ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyTypeLocal,
				},
			},
			expectedErr: nil,
		},
		{
			name: "setting ExternalTrafficPolicy to Cluster",
			dataplane: &operatorv1beta1.DataPlane{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dp-1",
					Namespace: "default",
					UID:       types.UID("1234"),
				},
				TypeMeta: metav1.TypeMeta{
					APIVersion: "gateway.konghq.com/v1beta1",
					Kind:       "DataPlane",
				},
				Spec: operatorv1beta1.DataPlaneSpec{
					DataPlaneOptions: operatorv1beta1.DataPlaneOptions{
						Network: operatorv1beta1.DataPlaneNetworkOptions{
							Services: &operatorv1beta1.DataPlaneServices{
								Ingress: &operatorv1beta1.DataPlaneServiceOptions{
									ServiceOptions: operatorv1beta1.ServiceOptions{
										ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyTypeCluster,
										Type:                  corev1.ServiceTypeLoadBalancer,
									},
								},
							},
						},
					},
				},
			},
			expectedSvc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "dataplane-ingress-dp-1-",
					Namespace:    "default",
					Labels: map[string]string{
						"app": "dp-1",
						"gateway-operator.konghq.com/dataplane-service-type": "ingress",
						"gateway-operator.konghq.com/managed-by":             "dataplane",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "gateway.konghq.com/v1beta1",
							Kind:       "DataPlane",
							Name:       "dp-1",
							UID:        "1234",
							Controller: lo.ToPtr(true),
						},
					},
					Finalizers: []string{
						"gateway-operator.konghq.com/wait-for-owner",
					},
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeLoadBalancer,
					Ports: []corev1.ServicePort{
						{
							Name:       "http",
							Protocol:   corev1.ProtocolTCP,
							Port:       80,
							TargetPort: intstr.FromInt(8000),
						},
						{
							Name:       "https",
							Protocol:   corev1.ProtocolTCP,
							Port:       443,
							TargetPort: intstr.FromInt(8443),
						},
					},
					Selector: map[string]string{
						"app": "dp-1",
					},
					ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyTypeCluster,
				},
			},
			expectedErr: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			svc, err := GenerateNewIngressServiceForDataPlane(tc.dataplane)
			require.Equal(t, tc.expectedErr, err)
			require.Equal(t, tc.expectedSvc, svc)
		})
	}
}
