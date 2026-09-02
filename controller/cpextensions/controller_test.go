package cpextensions

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakectrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

type countingUpdateClient struct {
	client.Client

	updateCalls int
}

func (c *countingUpdateClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	c.updateCalls++
	return c.Client.Update(ctx, obj, opts...)
}

// failingKongPluginCreateClient fails every Create() call for KongPlugin objects,
// simulating e.g. a validating webhook rejecting the object.
type failingKongPluginCreateClient struct {
	client.Client

	err error
}

func (c *failingKongPluginCreateClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	if _, ok := obj.(*configurationv1.KongPlugin); ok {
		return c.err
	}
	return c.Client.Create(ctx, obj, opts...)
}

// noopScraperNotifier is a no-op ScrapeUpdateNotifier for tests that don't
// exercise the metrics scraper wiring.
type noopScraperNotifier struct{}

func (noopScraperNotifier) NotifyAdd(context.Context, *gwtypes.ControlPlane)   {}
func (noopScraperNotifier) NotifyRemove(context.Context, types.NamespacedName) {}

func TestEnsurePrometheusPlugin_DoesNotUpdateWhenPluginIsAlreadyUpToDate(t *testing.T) {
	ctx := t.Context()
	testScheme := scheme.Get()

	cp := &gwtypes.ControlPlane{
		Name:      "cp",
		Namespace: "default",
		UID:       types.UID("cp-uid"),
	}
	svc := &corev1.Service{
		Name:      "svc",
		Namespace: "default",
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
		Name:      "cp",
		Namespace: "default",
		UID:       types.UID("cp-uid"),
	}
	svc := &corev1.Service{
		Name:      "svc",
		Namespace: "default",
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

// TestReconcile_ReturnsErrorWhenPluginCreateFails guards against a regression
// where a failed KongPlugin creation (e.g. rejected by a validating webhook)
// was silently swallowed: the reconciler logged the error and returned nil,
// so controller-runtime never requeued and the Service was left without its
// plugin annotation indefinitely.
func TestReconcile_ReturnsErrorWhenPluginCreateFails(t *testing.T) {
	ctx := t.Context()
	testScheme := scheme.Get()

	svc := &corev1.Service{
		Name:      "svc",
		Namespace: "default",
	}
	ext := &operatorv1alpha1.DataPlaneMetricsExtension{
		Name:      "metrics-ext",
		Namespace: "default",
		Spec: operatorv1alpha1.DataPlaneMetricsExtensionSpec{
			ServiceSelector: operatorv1alpha1.ServiceSelector{
				MatchNames: []operatorv1alpha1.ServiceSelectorEntry{{Name: svc.Name}},
			},
			Config: operatorv1alpha1.MetricsConfig{Latency: true},
		},
	}
	cp := &gwtypes.ControlPlane{
		Name:      "cp",
		Namespace: "default",
		UID:       types.UID("cp-uid"),
		Spec: gwtypes.ControlPlaneSpec{
			Extensions: []commonv1alpha1.ExtensionRef{
				{
					Group: operatorv1alpha1.SchemeGroupVersion.Group,
					Kind:  operatorv1alpha1.DataPlaneMetricsExtensionKind,
					Name:  ext.Name,
				},
			},
		},
	}

	baseClient := fakectrlruntimeclient.NewClientBuilder().
		WithScheme(testScheme).
		WithObjects(cp, svc, ext).
		Build()
	injectedErr := errors.New("admission webhook denied the request")
	wrappedClient := &failingKongPluginCreateClient{Client: baseClient, err: injectedErr}

	r := &Reconciler{
		Client:                          wrappedClient,
		DataPlaneScraperManagerNotifier: noopScraperNotifier{},
	}

	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)})
	require.Error(t, err)
	require.ErrorIs(t, err, injectedErr)

	var gotSvc corev1.Service
	require.NoError(t, baseClient.Get(ctx, client.ObjectKeyFromObject(svc), &gotSvc))
	_, ok := gotSvc.Annotations[consts.KongIngressControllerPluginsAnnotation]
	require.False(t, ok, "Service should not be annotated when plugin creation failed")
}

// TestReconcile_IgnoresNotFoundOnPluginDelete guards against a regression
// where cleaning up a Service that's no longer selected by any extension
// would fail forever if its Prometheus KongPlugin was already gone (e.g.
// deleted by a previous reconcile or manually): r.Delete returned NotFound,
// which was treated as a real error.
func TestReconcile_IgnoresNotFoundOnPluginDelete(t *testing.T) {
	ctx := t.Context()
	testScheme := scheme.Get()

	cp := &gwtypes.ControlPlane{
		Name:      "cp",
		Namespace: "default",
		UID:       types.UID("cp-uid"),
		// No extensions: this Service is no longer selected by anything.
	}
	svc := &corev1.Service{
		Name:      "svc",
		Namespace: "default",
		Labels: map[string]string{
			GatewayOperatorControlPlaneNameManagingPluginsLabel:      cp.Name,
			GatewayOperatorControlPlaneNamespaceManagingPluginsLabel: cp.Namespace,
		},
		Annotations: map[string]string{
			consts.KongIngressControllerPluginsAnnotation: "svc-metrics-prometheus",
		},
	}
	// Note: no KongPlugin object exists in the fake client, so r.Delete()
	// will return a NotFound error.

	baseClient := fakectrlruntimeclient.NewClientBuilder().
		WithScheme(testScheme).
		WithObjects(cp, svc).
		Build()

	r := &Reconciler{
		Client:                          baseClient,
		DataPlaneScraperManagerNotifier: noopScraperNotifier{},
	}

	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cp)})
	require.NoError(t, err)

	var gotSvc corev1.Service
	require.NoError(t, baseClient.Get(ctx, client.ObjectKeyFromObject(svc), &gotSvc))
	_, hasLabel := gotSvc.Labels[GatewayOperatorControlPlaneNameManagingPluginsLabel]
	require.False(t, hasLabel, "managing-plugins label should be cleared")
	_, hasAnnotation := gotSvc.Annotations[consts.KongIngressControllerPluginsAnnotation]
	require.False(t, hasAnnotation, "plugins annotation should be cleared")
}
