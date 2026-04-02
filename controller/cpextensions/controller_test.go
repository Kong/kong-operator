package cpextensions

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
)

type countingUpdateClient struct {
	client.Client
	updateCalls int
}

func (c *countingUpdateClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	c.updateCalls++
	return c.Client.Update(ctx, obj, opts...)
}

func TestEnsurePrometheusPlugin_DoesNotUpdateWhenPluginIsAlreadyUpToDate(t *testing.T) {
	ctx := t.Context()
	testScheme := scheme.Get()

	cp := &gwtypes.ControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cp",
			Namespace: "default",
			UID:       types.UID("cp-uid"),
		},
	}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc",
			Namespace: "default",
		},
	}
	ext := &operatorv1alpha1.DataPlaneMetricsExtension{
		Spec: operatorv1alpha1.DataPlaneMetricsExtensionSpec{
			Config: operatorv1alpha1.MetricsConfig{
				Latency:        true,
				Bandwidth:      true,
				UpstreamHealth: false,
				StatusCode:     true,
			},
		},
	}

	generated, err := prometheusPluginForSvc(svc, cp, ext)
	require.NoError(t, err)
	require.NoError(t, controllerutil.SetControllerReference(cp, generated, testScheme))

	baseClient := fakectrlruntimeclient.NewClientBuilder().
		WithScheme(testScheme).
		WithObjects(cp, svc, generated).
		Build()
	wrappedClient := &countingUpdateClient{Client: baseClient}

	r := &Reconciler{
		Client: wrappedClient,
	}

	_, err = r.ensurePrometheusPlugin(ctx, svc, cp, ext)
	require.NoError(t, err)
	require.Equal(t, 0, wrappedClient.updateCalls, "expected no Update() when plugin is already up to date")
}

func TestEnsurePrometheusPlugin_UpdatesWhenPluginDiffers(t *testing.T) {
	ctx := t.Context()
	testScheme := scheme.Get()

	cp := &gwtypes.ControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cp",
			Namespace: "default",
			UID:       types.UID("cp-uid"),
		},
	}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc",
			Namespace: "default",
		},
	}
	oldExt := &operatorv1alpha1.DataPlaneMetricsExtension{
		Spec: operatorv1alpha1.DataPlaneMetricsExtensionSpec{
			Config: operatorv1alpha1.MetricsConfig{
				Latency:        false,
				Bandwidth:      false,
				UpstreamHealth: false,
				StatusCode:     false,
			},
		},
	}
	newExt := &operatorv1alpha1.DataPlaneMetricsExtension{
		Spec: operatorv1alpha1.DataPlaneMetricsExtensionSpec{
			Config: operatorv1alpha1.MetricsConfig{
				Latency:        true,
				Bandwidth:      true,
				UpstreamHealth: true,
				StatusCode:     true,
			},
		},
	}

	oldPlugin, err := prometheusPluginForSvc(svc, cp, oldExt)
	require.NoError(t, err)
	require.NoError(t, controllerutil.SetControllerReference(cp, oldPlugin, testScheme))

	baseClient := fakectrlruntimeclient.NewClientBuilder().
		WithScheme(testScheme).
		WithObjects(cp, svc, oldPlugin).
		Build()
	wrappedClient := &countingUpdateClient{Client: baseClient}

	r := &Reconciler{
		Client: wrappedClient,
	}

	updatedPlugin, err := r.ensurePrometheusPlugin(ctx, svc, cp, newExt)
	require.NoError(t, err)
	require.Equal(t, 1, wrappedClient.updateCalls, "expected Update() when plugin differs")

	expectedPlugin, err := prometheusPluginForSvc(svc, cp, newExt)
	require.NoError(t, err)
	require.Equal(t, string(expectedPlugin.Config.Raw), string(updatedPlugin.Config.Raw))
}

