package envtest

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	dpreconciler "github.com/kong/kong-operator/v2/controller/dataplane"
	kogateway "github.com/kong/kong-operator/v2/controller/gateway"
	"github.com/kong/kong-operator/v2/ingress-controller/test/gatewayapi"
	"github.com/kong/kong-operator/v2/ingress-controller/test/util"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
	"github.com/kong/kong-operator/v2/pkg/consts"
	testutils "github.com/kong/kong-operator/v2/pkg/utils/test"
	"github.com/kong/kong-operator/v2/pkg/vars"
	certhelper "github.com/kong/kong-operator/v2/test/helpers/certificate"
)

func TestGatewayAddressOverride(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	scheme := Scheme(t, WithGatewayAPI, WithKong)
	envcfg, _ := Setup(t, ctx, scheme, WithInstallGatewayCRDs(true))
	ctrlClient := NewControllerClient(t, scheme, envcfg)

	expected := []string{"10.0.0.1", "10.0.0.2"}
	udp := []string{"10.0.0.3", "10.0.0.4"}
	gw, _ := deployGateway(ctx, t, ctrlClient)
	RunManager(ctx, t, envcfg,
		AdminAPIOptFns(),
		WithPublishService(gw.Namespace),
		WithPublishStatusAddress(expected, udp),
		WithGatewayFeatureEnabled,
		WithGatewayAPIControllers(),
	)

	allExpected := slices.Concat(expected, udp)
	require.Eventually(t, func() bool {
		err := ctrlClient.Get(ctx, k8stypes.NamespacedName{Namespace: gw.Namespace, Name: gw.Name}, &gw)
		if err != nil {
			t.Logf("Failed to get gateway %s/%s: %v", gw.Namespace, gw.Name, err)
			return false
		}

		expectedCount := 0
		unexpectedCount := 0
		for _, addr := range gw.Status.Addresses {
			if _, ok := lo.Find(allExpected, func(i string) bool { return i == addr.Value }); ok {
				expectedCount++
			} else {
				unexpectedCount++
			}
		}
		return expectedCount == len(allExpected) && unexpectedCount == 0
	}, time.Minute, time.Second, "did not find override addresses only in status")
}

// TestGatewayReconciliation_MoreThan100Routes verifies that if we create more
// than 100 HTTPRoutes, they all get reconciled and correctly attached to a
// Gateway's listener.
// It reproduces https://github.com/Kong/kubernetes-ingress-controller/issues/4456.
func TestGatewayReconciliation_MoreThan100Routes(t *testing.T) {
	t.Parallel()

	const (
		waitTime = time.Minute
		tickTime = 500 * time.Millisecond
	)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	scheme := Scheme(t, WithGatewayAPI, WithKong)
	envcfg, _ := Setup(t, ctx, scheme, WithInstallGatewayCRDs(true))
	ctrlClient := NewControllerClient(t, scheme, envcfg)

	gw, _ := deployGateway(ctx, t, ctrlClient)
	RunManager(ctx, t, envcfg,
		AdminAPIOptFns(),
		WithPublishService(gw.Namespace),
		WithGatewayFeatureEnabled,
		WithGatewayAPIControllers(),
	)

	const numOfRoutes = 120
	createHTTPRoutes(ctx, t, ctrlClient, gw, numOfRoutes)

	require.Eventually(t, func() bool {
		err := ctrlClient.Get(ctx, k8stypes.NamespacedName{Namespace: gw.Namespace, Name: gw.Name}, &gw)
		if err != nil {
			t.Logf("Failed to get gateway %s/%s: %v", gw.Namespace, gw.Name, err)
			return false
		}
		httpListener, ok := lo.Find(gw.Status.Listeners, func(listener gatewayapi.ListenerStatus) bool {
			return listener.Name == "http"
		})
		if !ok {
			t.Logf("failed to find http listener status in gateway %s/%s", gw.Namespace, gw.Name)
			return false
		}
		if httpListener.AttachedRoutes != numOfRoutes {
			t.Logf("expected %d routes to be attached to the http listener, got %d", numOfRoutes, httpListener.AttachedRoutes)
			return false
		}
		return true
	}, waitTime, tickTime, "failed to reconcile all HTTPRoutes")
}

// createHTTPRoutes creates a number of dummy HTTPRoutes for the given Gateway.
func createHTTPRoutes(
	ctx context.Context,
	t *testing.T,
	ctrlClient ctrlclient.Client,
	gw gatewayapi.Gateway,
	numOfRoutes int,
) []*gatewayapi.HTTPRoute {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "backend-svc",
			Namespace: gw.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:     "http",
					Protocol: corev1.ProtocolTCP,
					Port:     80,
				},
			},
		},
	}
	require.NoError(t, ctrlClient.Create(ctx, svc))
	t.Cleanup(func() { _ = ctrlClient.Delete(ctx, svc) })

	routes := make([]*gatewayapi.HTTPRoute, 0, numOfRoutes)
	for range numOfRoutes {
		httpPort := gatewayapi.PortNumber(80)
		pathMatchPrefix := gatewayapi.PathMatchPathPrefix
		httpRoute := &gatewayapi.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{
				Name:      uuid.NewString(),
				Namespace: gw.Namespace,
			},
			Spec: gatewayapi.HTTPRouteSpec{
				CommonRouteSpec: gatewayapi.CommonRouteSpec{
					ParentRefs: []gatewayapi.ParentReference{{
						Name: gatewayapi.ObjectName(gw.Name),
					}},
				},
				Rules: []gatewayapi.HTTPRouteRule{{
					Matches: []gatewayapi.HTTPRouteMatch{
						{
							Path: &gatewayapi.HTTPPathMatch{
								Type:  &pathMatchPrefix,
								Value: new("/test-http-route"),
							},
						},
					},
					BackendRefs: []gatewayapi.HTTPBackendRef{{
						BackendRef: gatewayapi.BackendRef{
							BackendObjectReference: gatewayapi.BackendObjectReference{
								Name: gatewayapi.ObjectName("backend-svc"),
								Port: &httpPort,
								Kind: util.StringToGatewayAPIKindPtr("Service"),
							},
						},
					}},
				}},
			},
		}

		require.NoError(t, ctrlClient.Create(ctx, httpRoute))
		t.Cleanup(func() { _ = ctrlClient.Delete(ctx, httpRoute) })
		routes = append(routes, httpRoute)
	}
	return routes
}

// TestGatewayInfrastructureLabels verifies that labels and annotations set in
// Gateway.spec.infrastructure are propagated to the DataPlane ingress Service
// labels/annotations and to the DataPlane Deployment pod template
// labels/annotations.
func TestGatewayInfrastructureLabels(t *testing.T) {
	t.Parallel()

	const (
		infraLabel      = "e2e.test/infra-label"
		infraLabelValue = "infra-label-value"
		infraAnnotation = "e2e.test/infra-annotation"
		infraAnnotValue = "infra-annotation-value"

		waitTime = 30 * time.Second
		pollTime = 500 * time.Millisecond
	)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	scheme := managerscheme.Get()
	envcfg, ns := Setup(t, ctx, scheme, WithInstallGatewayCRDs(true))
	mgr, logs := NewManager(t, ctx, envcfg, scheme)
	c := mgr.GetClient()

	// Create the cluster CA secret required by the DataPlane reconciler.
	cert, key := certhelper.MustGenerateCertPEMFormat(
		certhelper.WithCommonName("kong-operator-cluster-ca"),
		certhelper.WithCATrue(),
	)
	caSecretName := "cluster-ca-infra-labels-test"
	caSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns.Name,
			Name:      caSecretName,
			Labels:    map[string]string{"konghq.com/secret": "true"},
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			corev1.TLSCertKey:       cert,
			corev1.TLSPrivateKeyKey: key,
		},
	}
	require.NoError(t, c.Create(ctx, caSecret))
	t.Cleanup(func() { _ = c.Delete(ctx, caSecret) })

	// Start KO Gateway and DataPlane reconcilers.
	StartReconcilers(ctx, t, mgr, logs,
		&kogateway.Reconciler{
			Client:                c,
			Scheme:                scheme,
			Namespace:             ns.Name,
			DefaultDataPlaneImage: consts.DefaultDataPlaneImage,
		},
		&dpreconciler.Reconciler{
			Client:                   c,
			ClusterCASecretName:      caSecretName,
			ClusterCASecretNamespace: ns.Name,
			DefaultImage:             consts.DefaultDataPlaneImage,
		},
	)

	// Create a GatewayClass accepted by the KO controller.
	gc := &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{Name: "gc-infra-labels"},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: gatewayv1.GatewayController(vars.ControllerName()),
		},
	}
	require.NoError(t, c.Create(ctx, gc))
	t.Cleanup(func() { _ = c.Delete(ctx, gc) })

	t.Log("patching GatewayClass status to Accepted=True")
	require.Eventually(t, testutils.GatewayClassAcceptedStatusUpdate(t, ctx, gc.Name, c), waitTime, pollTime)

	// Create a Gateway referencing the GatewayClass.
	gw := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns.Name,
			Name:      "gw-infra-labels",
		},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: gatewayv1.ObjectName(gc.Name),
			Listeners: []gatewayv1.Listener{
				{
					Name:     "http",
					Protocol: gatewayv1.HTTPProtocolType,
					Port:     gatewayv1.PortNumber(80),
				},
			},
		},
	}
	require.NoError(t, c.Create(ctx, gw))
	t.Cleanup(func() { _ = c.Delete(ctx, gw) })

	// Patch the Gateway to add infrastructure labels and annotations.
	gwOld := gw.DeepCopy()
	gw.Spec.Infrastructure = &gatewayv1.GatewayInfrastructure{
		Labels: map[gatewayv1.LabelKey]gatewayv1.LabelValue{
			infraLabel: infraLabelValue,
		},
		Annotations: map[gatewayv1.AnnotationKey]gatewayv1.AnnotationValue{
			infraAnnotation: infraAnnotValue,
		},
	}
	require.NoError(t, c.Patch(ctx, gw, ctrlclient.MergeFrom(gwOld)))

	// Verify labels/annotations appear on the DataPlane ingress Service.
	t.Log("verifying infrastructure labels and annotations appear on the ingress Service")
	require.Eventually(t, func() bool {
		var svcs corev1.ServiceList
		if err := c.List(ctx, &svcs,
			ctrlclient.InNamespace(ns.Name),
			ctrlclient.MatchingLabels{
				consts.DataPlaneServiceTypeLabel: string(consts.DataPlaneIngressServiceLabelValue),
			},
		); err != nil {
			t.Logf("failed listing ingress services: %v", err)
			return false
		}
		if len(svcs.Items) == 0 {
			t.Log("no ingress services found yet")
			return false
		}
		svc := &svcs.Items[0]
		if svc.Labels[infraLabel] != infraLabelValue {
			t.Logf("ingress service label %q = %q, want %q", infraLabel, svc.Labels[infraLabel], infraLabelValue)
			return false
		}
		if svc.Annotations[infraAnnotation] != infraAnnotValue {
			t.Logf("ingress service annotation %q = %q, want %q", infraAnnotation, svc.Annotations[infraAnnotation], infraAnnotValue)
			return false
		}
		return true
	}, waitTime, pollTime, "infrastructure labels/annotations did not appear on ingress Service")

	// Verify labels/annotations appear on the DataPlane Deployment pod template.
	t.Log("verifying infrastructure labels and annotations appear on the Deployment pod template")
	require.Eventually(t, func() bool {
		var deps appsv1.DeploymentList
		if err := c.List(ctx, &deps,
			ctrlclient.InNamespace(ns.Name),
			ctrlclient.MatchingLabels{
				consts.GatewayOperatorManagedByLabel: consts.DataPlaneManagedLabelValue,
			},
		); err != nil {
			t.Logf("failed listing deployments: %v", err)
			return false
		}
		if len(deps.Items) == 0 {
			t.Log("no deployments found yet")
			return false
		}
		dep := &deps.Items[0]
		podLabels := dep.Spec.Template.Labels
		podAnnotations := dep.Spec.Template.Annotations
		if podLabels[infraLabel] != infraLabelValue {
			t.Logf("pod template label %q = %q, want %q", infraLabel, podLabels[infraLabel], infraLabelValue)
			return false
		}
		if podAnnotations[infraAnnotation] != infraAnnotValue {
			t.Logf("pod template annotation %q = %q, want %q", infraAnnotation, podAnnotations[infraAnnotation], infraAnnotValue)
			return false
		}
		return true
	}, waitTime, pollTime, "infrastructure labels/annotations did not appear on Deployment pod template")
}
