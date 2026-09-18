package konnect

import (
	"testing"

	sdkkonnectcomp "github.com/Kong/sdk-konnect-go/models/components"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/controller/pkg/op"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/internal/utils/index"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

func TestGetCertificateSecretDuringCleanup(t *testing.T) {
	testScheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(testScheme))

	const (
		namespace = "default"
		ownerName = "test-extension"
		ownerUID  = types.UID("test-extension-uid")
	)
	automatic := konnectv1alpha2.AutomaticSecretProvisioning
	manual := konnectv1alpha2.ManualSecretProvisioning

	t.Run("finds automatically provisioned Secret pending cleanup by owner", func(t *testing.T) {
		secret := &corev1.Secret{
			Name:      "generated-certificate",
			Namespace: namespace,
			Labels: map[string]string{
				SecretKonnectDataPlaneCertificateLabel: "true",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: konnectv1alpha2.GroupVersion.String(),
					Kind:       konnectv1alpha2.KonnectExtensionKind,
					Name:       ownerName,
					UID:        ownerUID,
				},
			},
			Finalizers: []string{
				consts.KonnectExtensionSecretInUseFinalizer,
				KonnectCleanupFinalizer,
			},
		}
		cleanSecret := secret.DeepCopy()
		cleanSecret.Name = "older-generated-certificate"
		cleanSecret.Finalizers = nil
		reconciler := KonnectExtensionReconciler{
			Client: fake.NewClientBuilder().
				WithScheme(testScheme).
				WithObjects(secret, cleanSecret).
				Build(),
		}
		extension := konnectv1alpha2.KonnectExtension{
			Name:      ownerName,
			Namespace: namespace,
			UID:       ownerUID,
			Spec: konnectv1alpha2.KonnectExtensionSpec{
				ClientAuth: &konnectv1alpha2.KonnectExtensionClientAuth{
					CertificateSecret: konnectv1alpha2.CertificateSecret{
						Provisioning: &automatic,
					},
				},
			},
			Status: konnectv1alpha2.KonnectExtensionStatus{
				DataPlaneClientAuth: &konnectv1alpha2.DataPlaneClientAuthStatus{
					CertificateSecretRef: &konnectv1alpha2.SecretRef{
						Name: "stale-certificate-reference",
					},
				},
			},
		}

		res, got, err := reconciler.getCertificateSecret(t.Context(), extension, true)
		require.NoError(t, err)
		assert.Equal(t, op.Noop, res)
		assert.Equal(t, secret.Name, got.Name)
		assert.ElementsMatch(t, secret.Finalizers, got.Finalizers)
	})

	t.Run("uses the spec reference for a manually provisioned Secret when status is missing", func(t *testing.T) {
		secret := &corev1.Secret{
			Name:      "manual-certificate",
			Namespace: namespace,
		}
		reconciler := KonnectExtensionReconciler{
			Client: fake.NewClientBuilder().
				WithScheme(testScheme).
				WithObjects(secret).
				Build(),
		}
		extension := konnectv1alpha2.KonnectExtension{
			Name:      ownerName,
			Namespace: namespace,
			UID:       ownerUID,
			Spec: konnectv1alpha2.KonnectExtensionSpec{
				ClientAuth: &konnectv1alpha2.KonnectExtensionClientAuth{
					CertificateSecret: konnectv1alpha2.CertificateSecret{
						Provisioning: &manual,
						CertificateSecretRef: &konnectv1alpha2.SecretRef{
							Name: secret.Name,
						},
					},
				},
			},
		}

		res, got, err := reconciler.getCertificateSecret(t.Context(), extension, true)
		require.NoError(t, err)
		assert.Equal(t, op.Noop, res)
		assert.Equal(t, secret.Name, got.Name)
	})
}

func TestKonnectExtensionCleanupWaitsForCertificateSecret(t *testing.T) {
	const (
		namespace = "default"
		ownerName = "test-extension"
		ownerUID  = types.UID("test-extension-uid")
	)
	now := metav1.Now()
	automatic := konnectv1alpha2.AutomaticSecretProvisioning
	extension := &konnectv1alpha2.KonnectExtension{
		Name:              ownerName,
		Namespace:         namespace,
		UID:               ownerUID,
		DeletionTimestamp: &now,
		Finalizers:        []string{KonnectCleanupFinalizer},
		Spec: konnectv1alpha2.KonnectExtensionSpec{
			ClientAuth: &konnectv1alpha2.KonnectExtensionClientAuth{
				CertificateSecret: konnectv1alpha2.CertificateSecret{
					Provisioning: &automatic,
				},
			},
		},
	}
	secret := &corev1.Secret{
		Name:      "generated-certificate",
		Namespace: namespace,
		Labels: map[string]string{
			SecretKonnectDataPlaneCertificateLabel: "true",
		},
		OwnerReferences: []metav1.OwnerReference{
			{
				APIVersion: konnectv1alpha2.GroupVersion.String(),
				Kind:       konnectv1alpha2.KonnectExtensionKind,
				Name:       ownerName,
				UID:        ownerUID,
			},
		},
		Finalizers: []string{
			consts.KonnectExtensionSecretInUseFinalizer,
			KonnectCleanupFinalizer,
		},
	}
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme.Get()).
		WithObjects(extension, secret).
		WithStatusSubresource(extension).
		WithIndex(
			&operatorv1beta1.DataPlane{},
			index.KonnectExtensionIndex,
			func(client.Object) []string { return nil },
		).
		WithIndex(
			&gwtypes.ControlPlane{},
			index.KonnectExtensionIndex,
			func(client.Object) []string { return nil },
		).
		Build()
	reconciler := KonnectExtensionReconciler{Client: fakeClient}

	_, err := reconciler.Reconcile(t.Context(), extension.DeepCopy())
	require.NoError(t, err)

	var got konnectv1alpha2.KonnectExtension
	require.NoError(t, fakeClient.Get(t.Context(), client.ObjectKeyFromObject(extension), &got))
	assert.Contains(t, got.Finalizers, KonnectCleanupFinalizer)
}

func TestEnforceKonnectExtensionStatus(t *testing.T) {
	cp := &konnectv1alpha2.KonnectGatewayControlPlane{
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
	apiAuthRef := konnectv1alpha2.ControlPlaneKonnectAPIAuthConfigurationRef{
		Name: "my-auth-config",
	}
	certificateSecret := corev1.Secret{
		Name: "my-secret",
	}

	t.Run("updates both Konnect and DataPlaneClientAuth when both are different", func(t *testing.T) {
		ext := &konnectv1alpha2.KonnectExtension{
			Status: konnectv1alpha2.KonnectExtensionStatus{
				Konnect:             nil,
				DataPlaneClientAuth: nil,
			},
		}
		updated := enforceKonnectExtensionStatus(cp, apiAuthRef, certificateSecret, ext)
		assert.True(t, updated)
		require.NotNil(t, ext.Status.Konnect)
		require.NotNil(t, ext.Status.DataPlaneClientAuth)
		assert.Equal(t, "cp-id", ext.Status.Konnect.ControlPlaneID)
		assert.Equal(t, konnectv1alpha2.ClusterTypeControlPlane, ext.Status.Konnect.ClusterType)
		assert.Equal(t, "cp-endpoint", ext.Status.Konnect.Endpoints.ControlPlaneEndpoint)
		assert.Equal(t, "telemetry-endpoint", ext.Status.Konnect.Endpoints.TelemetryEndpoint)
		require.NotNil(t, ext.Status.Konnect.AuthRef)
		assert.Equal(t, "my-auth-config", ext.Status.Konnect.AuthRef.Name)
		assert.Equal(t, "my-secret", ext.Status.DataPlaneClientAuth.CertificateSecretRef.Name)
	})

	t.Run("does not update if already up-to-date", func(t *testing.T) {
		konnectStatus := &konnectv1alpha2.KonnectExtensionControlPlaneStatus{
			ControlPlaneID: "cp-id",
			ClusterType:    konnectv1alpha2.ClusterTypeControlPlane,
			AuthRef:        &apiAuthRef,
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
		updated := enforceKonnectExtensionStatus(cp, apiAuthRef, certificateSecret, ext)
		assert.False(t, updated)
	})

	t.Run("updates only DataPlaneClientAuth if only that is different", func(t *testing.T) {
		konnectStatus := &konnectv1alpha2.KonnectExtensionControlPlaneStatus{
			ControlPlaneID: "cp-id",
			ClusterType:    konnectv1alpha2.ClusterTypeControlPlane,
			AuthRef:        &apiAuthRef,
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
		updated := enforceKonnectExtensionStatus(cp, apiAuthRef, certificateSecret, ext)
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
		updated := enforceKonnectExtensionStatus(cp, apiAuthRef, certificateSecret, ext)
		assert.True(t, updated)
		require.NotNil(t, ext.Status.Konnect)
		require.NotNil(t, ext.Status.DataPlaneClientAuth)
		assert.Equal(t, "cp-id", ext.Status.Konnect.ControlPlaneID)
		assert.Equal(t, konnectv1alpha2.ClusterTypeControlPlane, ext.Status.Konnect.ClusterType)
		assert.Equal(t, "cp-endpoint", ext.Status.Konnect.Endpoints.ControlPlaneEndpoint)
		assert.Equal(t, "telemetry-endpoint", ext.Status.Konnect.Endpoints.TelemetryEndpoint)
		require.NotNil(t, ext.Status.Konnect.AuthRef)
		assert.Equal(t, "my-auth-config", ext.Status.Konnect.AuthRef.Name)
		assert.Equal(t, "my-secret", ext.Status.DataPlaneClientAuth.CertificateSecretRef.Name)
	})

	t.Run("updates when apiAuthRef changes", func(t *testing.T) {
		oldAuthRef := konnectv1alpha2.ControlPlaneKonnectAPIAuthConfigurationRef{
			Name: "old-auth-config",
		}
		konnectStatus := &konnectv1alpha2.KonnectExtensionControlPlaneStatus{
			ControlPlaneID: "cp-id",
			ClusterType:    konnectv1alpha2.ClusterTypeControlPlane,
			AuthRef:        &oldAuthRef,
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
		updated := enforceKonnectExtensionStatus(cp, apiAuthRef, certificateSecret, ext)
		assert.True(t, updated)
		require.NotNil(t, ext.Status.Konnect)
		require.NotNil(t, ext.Status.Konnect.AuthRef)
		assert.Equal(t, "my-auth-config", ext.Status.Konnect.AuthRef.Name)
		assert.Equal(t, "cp-id", ext.Status.Konnect.ControlPlaneID)
		assert.Equal(t, konnectv1alpha2.ClusterTypeControlPlane, ext.Status.Konnect.ClusterType)
	})

	t.Run("clears Konnect status when cp is nil", func(t *testing.T) {
		konnectStatus := &konnectv1alpha2.KonnectExtensionControlPlaneStatus{
			ControlPlaneID: "cp-id",
			ClusterType:    konnectv1alpha2.ClusterTypeControlPlane,
			AuthRef:        &apiAuthRef,
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
		updated := enforceKonnectExtensionStatus(nil, apiAuthRef, certificateSecret, ext)
		assert.True(t, updated)
		assert.Nil(t, ext.Status.Konnect)
		require.NotNil(t, ext.Status.DataPlaneClientAuth)
		assert.Equal(t, "my-secret", ext.Status.DataPlaneClientAuth.CertificateSecretRef.Name)
	})

	t.Run("does not update when cp is nil and Konnect status is already nil", func(t *testing.T) {
		dataPlaneClientAuth := &konnectv1alpha2.DataPlaneClientAuthStatus{
			CertificateSecretRef: &konnectv1alpha2.SecretRef{Name: "my-secret"},
		}
		ext := &konnectv1alpha2.KonnectExtension{
			Status: konnectv1alpha2.KonnectExtensionStatus{
				Konnect:             nil,
				DataPlaneClientAuth: dataPlaneClientAuth,
			},
		}
		updated := enforceKonnectExtensionStatus(nil, apiAuthRef, certificateSecret, ext)
		assert.False(t, updated)
		assert.Nil(t, ext.Status.Konnect)
	})

	t.Run("clears Konnect status when cp.Status.Endpoints is nil", func(t *testing.T) {
		cpWithNilEndpoints := &konnectv1alpha2.KonnectGatewayControlPlane{
			Status: konnectv1alpha2.KonnectGatewayControlPlaneStatus{
				KonnectEntityStatus: konnectv1alpha2.KonnectEntityStatus{
					ID: "cp-id",
				},
				Endpoints: nil,
			},
		}
		konnectStatus := &konnectv1alpha2.KonnectExtensionControlPlaneStatus{
			ControlPlaneID: "cp-id",
			ClusterType:    konnectv1alpha2.ClusterTypeControlPlane,
			AuthRef:        &apiAuthRef,
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
		updated := enforceKonnectExtensionStatus(cpWithNilEndpoints, apiAuthRef, certificateSecret, ext)
		assert.True(t, updated)
		assert.Nil(t, ext.Status.Konnect)
		require.NotNil(t, ext.Status.DataPlaneClientAuth)
		assert.Equal(t, "my-secret", ext.Status.DataPlaneClientAuth.CertificateSecretRef.Name)
	})
}
