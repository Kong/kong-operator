package dataplane

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
)

func TestServiceIsReady(t *testing.T) {
	tests := []struct {
		name string
		svc  *corev1.Service
		want bool
	}{
		{
			name: "ClusterIP service is always ready",
			svc:  &corev1.Service{Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP}},
			want: true,
		},
		{
			name: "NodePort service is always ready",
			svc:  &corev1.Service{Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeNodePort}},
			want: true,
		},
		{
			name: "LoadBalancer with no ingress is not ready",
			svc:  &corev1.Service{Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer}},
			want: false,
		},
		{
			name: "LoadBalancer with IP is ready",
			svc: &corev1.Service{
				Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{{IP: "1.2.3.4"}},
					},
				},
			},
			want: true,
		},
		{
			name: "LoadBalancer with hostname is ready",
			svc: &corev1.Service{
				Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: []corev1.LoadBalancerIngress{{Hostname: "lb.example.com"}},
					},
				},
			},
			want: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, serviceIsReady(tc.svc))
		})
	}
}

func TestEnsureServiceReadyCondition(t *testing.T) {
	r := &testReconciler{Config: testConfig}

	t.Run("ClusterIP sets ServiceReady=True and reports the ClusterIP", func(t *testing.T) {
		aigwdp := newReconcileAIGWDP()
		svc := &corev1.Service{
			Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP, ClusterIPs: []string{"10.0.0.1"}},
		}
		require.NoError(t, r.ensureServiceReadyCondition(aigwdp, svc))

		cond := apimeta.FindStatusCondition(aigwdp.Status.Conditions, string(aigatewayv1alpha1.ServiceReadyType))
		require.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionTrue, cond.Status)
		assert.Equal(t, string(aigatewayv1alpha1.ServiceReadyReason), cond.Reason)
		assert.Len(t, aigwdp.Status.Addresses, 1)
		assert.Equal(t, aigatewayv1alpha1.PrivateIPAddressSourceType, aigwdp.Status.Addresses[0].SourceType)
	})

	t.Run("LoadBalancer with no ingress sets ServiceReady=False", func(t *testing.T) {
		aigwdp := newReconcileAIGWDP()
		svc := &corev1.Service{Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer}}
		require.NoError(t, r.ensureServiceReadyCondition(aigwdp, svc))

		cond := apimeta.FindStatusCondition(aigwdp.Status.Conditions, string(aigatewayv1alpha1.ServiceReadyType))
		require.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionFalse, cond.Status)
		assert.Equal(t, string(aigatewayv1alpha1.WaitingForAddressReason), cond.Reason)
		assert.Empty(t, aigwdp.Status.Addresses)
	})

	t.Run("LoadBalancer with IP sets ServiceReady=True and populates addresses", func(t *testing.T) {
		aigwdp := newReconcileAIGWDP()
		svc := &corev1.Service{
			Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
			Status: corev1.ServiceStatus{
				LoadBalancer: corev1.LoadBalancerStatus{
					Ingress: []corev1.LoadBalancerIngress{{IP: "203.0.113.5"}},
				},
			},
		}
		require.NoError(t, r.ensureServiceReadyCondition(aigwdp, svc))

		cond := apimeta.FindStatusCondition(aigwdp.Status.Conditions, string(aigatewayv1alpha1.ServiceReadyType))
		require.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionTrue, cond.Status)
		require.Len(t, aigwdp.Status.Addresses, 1)
		assert.Equal(t, "203.0.113.5", aigwdp.Status.Addresses[0].Value)
		assert.Equal(t, aigatewayv1alpha1.PublicLoadBalancerAddressSourceType, aigwdp.Status.Addresses[0].SourceType)
	})
}
