package konnect

import (
	"context"
	"testing"

	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	operatorv1beta1 "github.com/kong/kong-operator/api/gateway-operator/v1beta1"
	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/modules/manager/logging"
	"github.com/kong/kong-operator/modules/manager/scheme"
	"github.com/kong/kong-operator/pkg/consts"
)

func TestSkipKonnectCleanupRemovesFinalizers(t *testing.T) {
	testScheme := scheme.Get()
	utilruntime.Must(corev1.AddToScheme(testScheme))
	utilruntime.Must(operatorv1beta1.AddToScheme(testScheme))
	utilruntime.Must(konnectv1alpha2.AddToScheme(testScheme))

	ext := &konnectv1alpha2.KonnectExtension{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "test-ext",
			Namespace:       "default",
			Finalizers:      []string{KonnectCleanupFinalizer},
			ResourceVersion: "1",
		},
		Status: konnectv1alpha2.KonnectExtensionStatus{
			DataPlaneClientAuth: &konnectv1alpha2.DataPlaneClientAuthStatus{
				CertificateSecretRef: &konnectv1alpha2.SecretRef{Name: "konnect-cert"},
			},
		},
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "konnect-cert",
			Namespace:       "default",
			Finalizers:      []string{KonnectCleanupFinalizer, consts.KonnectExtensionSecretInUseFinalizer},
			ResourceVersion: "1",
		},
	}

	client := fakeclient.NewClientBuilder().
		WithScheme(testScheme).
		WithObjects(ext, secret).
		Build()

	logger := testr.New(t)
	ctx := ctrllog.IntoContext(context.Background(), logger)

	reconciler := &KonnectExtensionReconciler{
		Client:      client,
		LoggingMode: logging.DevelopmentMode,
	}

	res, err := reconciler.skipKonnectCleanup(ctx, logger, ext)
	require.NoError(t, err)
	require.Equal(t, ctrl.Result{}, res)

	updatedSecret := &corev1.Secret{}
	require.NoError(t, client.Get(ctx, types.NamespacedName{Namespace: secret.Namespace, Name: secret.Name}, updatedSecret))
	require.NotContains(t, updatedSecret.Finalizers, KonnectCleanupFinalizer)
	require.NotContains(t, updatedSecret.Finalizers, consts.KonnectExtensionSecretInUseFinalizer)

	updatedExt := &konnectv1alpha2.KonnectExtension{}
	require.NoError(t, client.Get(ctx, types.NamespacedName{Namespace: ext.Namespace, Name: ext.Name}, updatedExt))
	require.NotContains(t, updatedExt.Finalizers, KonnectCleanupFinalizer)
}
