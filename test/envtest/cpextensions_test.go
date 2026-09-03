package envtest

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	operatorv2beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v2beta1"
	"github.com/kong/kong-operator/v2/controller/cpextensions"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
)

// noopScraperNotifier is a no-op cpextensions.ScrapeUpdateNotifier for tests
// that don't exercise the metrics scraper wiring.
type noopScraperNotifier struct{}

func (noopScraperNotifier) NotifyAdd(context.Context, *gwtypes.ControlPlane)   {}
func (noopScraperNotifier) NotifyRemove(context.Context, types.NamespacedName) {}

// TestControlPlaneExtensionsRequeuesOnTransientPluginCreateFailure reproduces
// the sporadic CI failure in TestControlPlaneExtensionsDataPlaneMetrics
// (test/integration/ko/controlplane_extensions_dataplanemetrics_test.go).
//
// In that failure, the KongPlugin admission webhook rejected the Prometheus
// KongPlugin's Create because it happened to validate the plugin against an
// unrelated, unusable Kong Gateway. cpextensions.Reconciler logged the error
// and returned nil, so controller-runtime never requeued and the Service was
// left without its "konghq.com/plugins" annotation forever.
//
// Here the rejection is reproduced deterministically with a dedicated
// ValidatingWebhookConfiguration that always fails to be called, instead of
// depending on a real Kong Gateway's license state. Reconcile must return an
// error while the webhook blocks creation, and once the webhook is removed
// the reconciler must converge on its own (via controller-runtime's requeue),
// without any further trigger.
func TestControlPlaneExtensionsRequeuesOnTransientPluginCreateFailure(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	cfg, ns := Setup(t, ctx, scheme.Get(), WithInstallGatewayCRDs(true))
	mgr, logs := NewManager(t, ctx, cfg, scheme.Get())

	reconciler := &cpextensions.Reconciler{
		Client:                          mgr.GetClient(),
		DataPlaneScraperManagerNotifier: noopScraperNotifier{},
	}
	StartReconcilers(ctx, t, mgr, logs, reconciler)

	// Wait for the manager's cache to finish its initial sync before proceeding.
	// StartReconcilers only launches mgr.Start(ctx) in a goroutine and returns
	// immediately, so without this the controller's workers can start seconds
	// after this point on a loaded machine, and the require.Never/EventuallyWithT
	// assertions below would run - and expire - before the reconciler has run
	// even once.
	require.True(t, mgr.GetCache().WaitForCacheSync(ctx), "manager caches failed to sync")

	cl := mgr.GetClient()

	// Block every KongPlugin Create with a webhook that can never be reached.
	// failurePolicy: Fail turns the connection failure into a rejected Create,
	// the same outcome CI hit when the (unrelated) real admission webhook
	// returned an error.
	webhook := &admissionregistrationv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cpext-test-block-kongplugins",
		},
		Webhooks: []admissionregistrationv1.ValidatingWebhook{
			{
				Name:                    "block.kongplugins.cpext-test.konghq.com",
				AdmissionReviewVersions: []string{"v1"},
				SideEffects:             new(admissionregistrationv1.SideEffectClassNone),
				FailurePolicy:           new(admissionregistrationv1.Fail),
				TimeoutSeconds:          new(int32(1)),
				ClientConfig: admissionregistrationv1.WebhookClientConfig{
					URL: new("https://127.0.0.1:1/validate"),
				},
				Rules: []admissionregistrationv1.RuleWithOperations{
					{
						Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create},
						Rule: admissionregistrationv1.Rule{
							APIGroups:   []string{configurationv1.SchemeGroupVersion.Group},
							APIVersions: []string{configurationv1.SchemeGroupVersion.Version},
							Resources:   []string{"kongplugins"},
							Scope:       new(admissionregistrationv1.AllScopes),
						},
					},
				},
			},
		},
	}
	require.NoError(t, cl.Create(ctx, webhook))
	t.Cleanup(func() {
		_ = cl.Delete(ctx, webhook)
	})

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "httpbin",
			Namespace: ns.Name,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{Port: 80}},
		},
	}
	require.NoError(t, cl.Create(ctx, svc))

	ext := &operatorv1alpha1.DataPlaneMetricsExtension{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "metrics-ext-",
			Namespace:    ns.Name,
		},
		Spec: operatorv1alpha1.DataPlaneMetricsExtensionSpec{
			ServiceSelector: operatorv1alpha1.ServiceSelector{
				MatchNames: []operatorv1alpha1.ServiceSelectorEntry{{Name: svc.Name}},
			},
			Config: operatorv1alpha1.MetricsConfig{Latency: true},
		},
	}
	require.NoError(t, cl.Create(ctx, ext))

	cp := &gwtypes.ControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "cp-",
			Namespace:    ns.Name,
		},
		Spec: gwtypes.ControlPlaneSpec{
			DataPlane: operatorv2beta1.ControlPlaneDataPlaneTarget{
				Type: operatorv2beta1.ControlPlaneDataPlaneTargetManagedByType,
			},
			Extensions: []commonv1alpha1.ExtensionRef{
				{
					Group: operatorv1alpha1.SchemeGroupVersion.Group,
					Kind:  operatorv1alpha1.DataPlaneMetricsExtensionKind,
					NamespacedRef: commonv1alpha1.NamespacedRef{
						Name: ext.Name,
					},
				},
			},
		},
	}
	require.NoError(t, cl.Create(ctx, cp))

	// Sanity check: while the webhook blocks Create, no KongPlugin ever appears.
	// Kept short (well under the retry budget below): every failed Reconcile is
	// requeued with controller-runtime's default exponential backoff (5ms base,
	// doubling), so a longer blocking window banks more failures and pushes the
	// next retry - and thus how long the EventuallyWithT below must wait - further out.
	require.Never(t, func() bool {
		var plugins configurationv1.KongPluginList
		_ = cl.List(ctx, &plugins, client.InNamespace(ns.Name))
		return len(plugins.Items) > 0
	}, 500*time.Millisecond, tickTime)

	require.NoError(t, cl.Delete(ctx, webhook))

	// This must converge quickly via controller-runtime's error-triggered
	// requeue, not via the manager's periodic cache resync (NewManager sets a
	// 10s cache SyncPeriod, see test/envtest/controller.go): the window below
	// is kept well under that so that a missing requeue (the bug) times out
	// here instead of being masked by the next background resync.
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		var plugins configurationv1.KongPluginList
		require.NoError(ct, cl.List(ctx, &plugins, client.InNamespace(ns.Name)))
		require.Len(ct, plugins.Items, 1)

		var gotSvc corev1.Service
		require.NoError(ct, cl.Get(ctx, client.ObjectKeyFromObject(svc), &gotSvc))
		ann, ok := gotSvc.Annotations[consts.KongIngressControllerPluginsAnnotation]
		require.True(ct, ok)
		require.Equal(ct, plugins.Items[0].Name, ann)
	}, 5*time.Second, 100*time.Millisecond)
}
