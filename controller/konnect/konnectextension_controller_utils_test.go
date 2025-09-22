package konnect

import (
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
)

func TestEnforceKonnectExtensionStatus(t *testing.T) {
	cp := konnectv1alpha2.KonnectGatewayControlPlane{
		Status: konnectv1alpha2.KonnectGatewayControlPlaneStatus{
			KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
				ID: "cp-id",
			},
			Endpoints: &konnectv1alpha2.KonnectEndpoints{
				ControlPlaneEndpoint: "cp-endpoint",
				TelemetryEndpoint:    "telemetry-endpoint",
			},
		},
		Spec: konnectv1alpha2.KonnectGatewayControlPlaneSpec{
			CreateControlPlaneRequest: &sdkkonnectcomp.CreateControlPlaneRequest{
				ClusterType: sdkkonnectcomp.CreateControlPlaneRequestClusterTypeClusterTypeControlPlane.ToPointer(),
			},
		},
	}
	certificateSecret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "my-secret",
		},
	}

	t.Run("updates both Konnect and DataPlaneClientAuth when both are different", func(t *testing.T) {
		ext := &konnectv1alpha2.KonnectExtension{
			Status: konnectv1alpha2.KonnectExtensionStatus{
				Konnect:             nil,
				DataPlaneClientAuth: nil,
			},
		}
		updated := enforceKonnectExtensionStatus(cp, certificateSecret, ext)
		assert.True(t, updated)
		require.NotNil(t, ext.Status.Konnect)
		require.NotNil(t, ext.Status.DataPlaneClientAuth)
		assert.Equal(t, "cp-id", ext.Status.Konnect.ControlPlaneID)
		assert.Equal(t, konnectv1alpha2.ClusterTypeControlPlane, ext.Status.Konnect.ClusterType)
		assert.Equal(t, "cp-endpoint", ext.Status.Konnect.Endpoints.ControlPlaneEndpoint)
		assert.Equal(t, "telemetry-endpoint", ext.Status.Konnect.Endpoints.TelemetryEndpoint)
		assert.Equal(t, "my-secret", ext.Status.DataPlaneClientAuth.CertificateSecretRef.Name)
	})

	t.Run("does not update if already up-to-date", func(t *testing.T) {
		konnectStatus := &konnectv1alpha2.KonnectExtensionControlPlaneStatus{
			ControlPlaneID: "cp-id",
			ClusterType:    konnectv1alpha2.ClusterTypeControlPlane,
			Endpoints: konnectv1alpha2.KonnectEndpoints{
				ControlPlaneEndpoint: "cp-endpoint",
				TelemetryEndpoint:    "telemetry-endpoint",
			},
		}
		dataPlaneClientAuth := &konnectv1alpha2.DataPlaneClientAuthStatus{
			CertificateSecretRef: &konnectv1alpha2.SecretRef{Name: "my-secret"},
		}
		ext := &konnectv1alpha2.KonnectExtension{
			Status: konnectv1alpha2.KonnectExtensionStatus{
				Konnect:             konnectStatus,
				DataPlaneClientAuth: dataPlaneClientAuth,
			},
		}
		updated := enforceKonnectExtensionStatus(cp, certificateSecret, ext)
		assert.False(t, updated)
	})

	t.Run("updates only DataPlaneClientAuth if only that is different", func(t *testing.T) {
		konnectStatus := &konnectv1alpha2.KonnectExtensionControlPlaneStatus{
			ControlPlaneID: "cp-id",
			ClusterType:    konnectv1alpha2.ClusterTypeControlPlane,
			Endpoints: konnectv1alpha2.KonnectEndpoints{
				ControlPlaneEndpoint: "cp-endpoint",
				TelemetryEndpoint:    "telemetry-endpoint",
			},
		}
		ext := &konnectv1alpha2.KonnectExtension{
			Status: konnectv1alpha2.KonnectExtensionStatus{
				Konnect:             konnectStatus,
				DataPlaneClientAuth: nil,
			},
		}
		updated := enforceKonnectExtensionStatus(cp, certificateSecret, ext)
		assert.True(t, updated)
		require.NotNil(t, ext.Status.DataPlaneClientAuth)
		assert.Equal(t, "my-secret", ext.Status.DataPlaneClientAuth.CertificateSecretRef.Name)
	})

	t.Run("updates only Konnect if only that is different", func(t *testing.T) {
		dataPlaneClientAuth := &konnectv1alpha2.DataPlaneClientAuthStatus{
			CertificateSecretRef: &konnectv1alpha2.SecretRef{Name: "my-secret"},
		}
		ext := &konnectv1alpha2.KonnectExtension{
			Status: konnectv1alpha2.KonnectExtensionStatus{
				Konnect: &konnectv1alpha2.KonnectExtensionControlPlaneStatus{
					ControlPlaneID: "other-id",
					ClusterType:    konnectv1alpha2.ClusterTypeK8sIngressController,
					Endpoints: konnectv1alpha2.KonnectEndpoints{
						ControlPlaneEndpoint: "other-endpoint",
						TelemetryEndpoint:    "other-telemetry",
					},
				},
				DataPlaneClientAuth: dataPlaneClientAuth,
			},
		}
		updated := enforceKonnectExtensionStatus(cp, certificateSecret, ext)
		assert.True(t, updated)
		require.NotNil(t, ext.Status.Konnect)
		require.NotNil(t, ext.Status.DataPlaneClientAuth)
		assert.Equal(t, "cp-id", ext.Status.Konnect.ControlPlaneID)
		assert.Equal(t, konnectv1alpha2.ClusterTypeControlPlane, ext.Status.Konnect.ClusterType)
		assert.Equal(t, "cp-endpoint", ext.Status.Konnect.Endpoints.ControlPlaneEndpoint)
		assert.Equal(t, "telemetry-endpoint", ext.Status.Konnect.Endpoints.TelemetryEndpoint)
		assert.Equal(t, "my-secret", ext.Status.DataPlaneClientAuth.CertificateSecretRef.Name)
	})
}
