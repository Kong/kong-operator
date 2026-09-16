package dataplane

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
)

func TestReconcile_ControlPlaneNotReady(t *testing.T) {
	notProgrammedCP := func() *konnectv1alpha1.KonnectAIGateway {
		return &konnectv1alpha1.KonnectAIGateway{
			Namespace: reconcileTestNS, Name: reconcileTestAIGWCPName,
			Status: konnectv1alpha1.KonnectAIGatewayStatus{
				Conditions: []metav1.Condition{
					{
						Type:               konnectv1alpha1.KonnectEntityProgrammedConditionType,
						Status:             metav1.ConditionFalse,
						Reason:             "Pending",
						LastTransitionTime: metav1.NewTime(time.Now()),
					},
				},
			},
		}
	}

	tests := []struct {
		name       string
		extraObjs  []client.Object
		wantReason string
	}{
		{
			name:       "control plane not found: no error retry, resolution condition set",
			wantReason: string(aigatewayv1alpha1.ControlPlaneNotFoundReason),
		},
		{
			name:       "control plane not yet Programmed: no error retry, resolution condition set",
			extraObjs:  []client.Object{notProgrammedCP()},
			wantReason: string(aigatewayv1alpha1.KonnectAIGatewayNotProgrammedReason),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			aigwdp := newReconcileAIGWDP()
			objs := append([]client.Object{aigwdp}, tc.extraObjs...)
			cl := fake.NewClientBuilder().
				WithScheme(managerscheme.Get()).
				WithStatusSubresource(&aigatewayv1alpha1.AIGatewayDataPlane{}).
				WithObjects(objs...).
				Build()
			r := newTestReconciler(cl, events.NewFakeRecorder(10))

			res, err := r.Reconcile(t.Context(), aigwdp)
			require.NoError(t, err, "expected transient control plane states must not retry with error backoff")
			assert.False(t, res.Requeue)
			assert.Zero(t, res.RequeueAfter)

			cond := apimeta.FindStatusCondition(aigwdp.Status.Conditions, string(aigatewayv1alpha1.KonnectAIGatewayResolvedType))
			require.NotNil(t, cond)
			assert.Equal(t, metav1.ConditionFalse, cond.Status)
			assert.Equal(t, tc.wantReason, cond.Reason)
		})
	}
}

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
		require.NoError(t, r.ensureServiceReadyCondition(aigwdp, testConfig.Services[0], svc))

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
		require.NoError(t, r.ensureServiceReadyCondition(aigwdp, testConfig.Services[0], svc))

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
		require.NoError(t, r.ensureServiceReadyCondition(aigwdp, testConfig.Services[0], svc))

		cond := apimeta.FindStatusCondition(aigwdp.Status.Conditions, string(aigatewayv1alpha1.ServiceReadyType))
		require.NotNil(t, cond)
		assert.Equal(t, metav1.ConditionTrue, cond.Status)
		require.Len(t, aigwdp.Status.Addresses, 1)
		assert.Equal(t, "203.0.113.5", aigwdp.Status.Addresses[0].Value)
		assert.Equal(t, aigatewayv1alpha1.PublicLoadBalancerAddressSourceType, aigwdp.Status.Addresses[0].SourceType)
	})
}
