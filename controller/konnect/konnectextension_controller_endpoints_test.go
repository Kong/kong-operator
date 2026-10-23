package konnect

import (
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// endpointVerdictTestReconciler returns the dpCert test reconciler on top of a control plane
// publishing endpoints (nil when it has none) and an API auth configuration serving serverURL.
func endpointVerdictTestReconciler(
	t *testing.T,
	endpoints *konnectv1alpha2.KonnectEndpoints,
	clusterType sdkkonnectcomp.ControlPlaneClusterType,
	serverURL string,
) (*KonnectExtensionReconciler, *konnectv1alpha2.KonnectExtension) {
	t.Helper()
	r, ext := dpCertTestReconciler(t, "certificate", dpCertTestObject(dpCertTestExtName, "certificate"))

	cp := &konnectv1alpha2.KonnectGatewayControlPlane{}
	require.NoError(t, r.Get(t.Context(), client.ObjectKey{Namespace: dpCertTestNamespace, Name: "control-plane"}, cp))
	cp.Status.Endpoints = endpoints
	cp.Status.ClusterType = clusterType
	require.NoError(t, r.Update(t.Context(), cp))

	auth := &konnectv1alpha1.KonnectAPIAuthConfiguration{}
	require.NoError(t, r.Get(t.Context(), client.ObjectKey{Namespace: dpCertTestNamespace, Name: "auth"}, auth))
	auth.Status.ServerURL = serverURL
	require.NoError(t, r.Update(t.Context(), auth))

	return r, ext
}

// reconcileRepeatedly reconciles ext, re-reading it each time so that every step sees the writes
// of the previous one.
func reconcileRepeatedly(t *testing.T, r *KonnectExtensionReconciler, ext *konnectv1alpha2.KonnectExtension) {
	t.Helper()
	for range 15 {
		require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(ext), ext))
		_, err := r.Reconcile(t.Context(), ext)
		require.NoError(t, err)
	}
	require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(ext), ext))
}

func TestKonnectExtensionEndpointVerdict(t *testing.T) {
	t.Run("KIC control plane endpoint with a path", func(t *testing.T) {
		r, ext := endpointVerdictTestReconciler(t,
			&konnectv1alpha2.KonnectEndpoints{
				ControlPlaneEndpoint: "https://us.svc.konghq.tech/cp/2cf352e764",
				TelemetryEndpoint:    "https://2cf352e764.us.tp.konghq.tech:443",
			},
			sdkkonnectcomp.ControlPlaneClusterTypeClusterTypeK8SIngressController,
			"https://us.api.konghq.tech",
		)

		reconcileRepeatedly(t, r, ext)

		cond, found := k8sutils.GetCondition(konnectv1alpha2.KonnectExtensionReadyConditionType, ext)
		require.True(t, found)
		assert.Equal(t, metav1.ConditionFalse, cond.Status)
		assert.Equal(t, "InvalidEndpoint", cond.Reason)
		assert.Contains(t, cond.Message, "https://us.svc.konghq.tech/cp/2cf352e764")
		assert.True(t, k8sutils.HasConditionTrue(konnectv1alpha1.DataPlaneCertificateProvisionedConditionType, ext))
	})

	t.Run("private server URL with public endpoints", func(t *testing.T) {
		r, ext := endpointVerdictTestReconciler(t,
			&konnectv1alpha2.KonnectEndpoints{
				ControlPlaneEndpoint: "https://2cf352e764.us.cp.konghq.tech",
				TelemetryEndpoint:    "https://2cf352e764.us.tp.konghq.tech:443",
			},
			sdkkonnectcomp.ControlPlaneClusterTypeClusterTypeControlPlane,
			"https://us.svc.konghq.tech/api",
		)

		reconcileRepeatedly(t, r, ext)

		cond, found := k8sutils.GetCondition(konnectv1alpha2.KonnectExtensionReadyConditionType, ext)
		require.True(t, found)
		assert.Equal(t, metav1.ConditionFalse, cond.Status)
		assert.Equal(t, "EndpointMismatch", cond.Reason)
		assert.Contains(t, cond.Message, "https://2cf352e764.us.cp.konghq.tech")
		assert.Contains(t, cond.Message, "https://us.svc.konghq.tech/api")
	})

	t.Run("control plane without endpoints", func(t *testing.T) {
		r, ext := endpointVerdictTestReconciler(t, nil, "", "https://us.svc.konghq.tech/api")

		reconcileRepeatedly(t, r, ext)

		assert.Nil(t, ext.Status.Konnect)
		for _, cond := range ext.Status.Conditions {
			assert.NotEqual(t, "InvalidEndpoint", cond.Reason)
			assert.NotEqual(t, "EndpointMismatch", cond.Reason)
		}
		assert.True(t, k8sutils.HasConditionTrue(konnectv1alpha2.KonnectExtensionReadyConditionType, ext))
	})
}
