package envtest

import (
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	kogateway "github.com/kong/kong-operator/controller/gateway"
	certhelper "github.com/kong/kong-operator/ingress-controller/test/helpers/certificate"
	"github.com/kong/kong-operator/modules/manager/logging"
	managerscheme "github.com/kong/kong-operator/modules/manager/scheme"
	testutils "github.com/kong/kong-operator/pkg/utils/test"
	"github.com/kong/kong-operator/pkg/vars"
)

func TestGatewaySecretWatch_UpdatesResolvedRefsOnSecretRotation(t *testing.T) {
	t.Parallel()

	// Prepare scheme, envtest, manager and KO Gateway reconciler.
	scheme := managerscheme.Get()
	ctx := t.Context()

	cfg, ns := Setup(t, ctx, scheme)
	mgr, logs := NewManager(t, ctx, cfg, scheme)

	r := &kogateway.Reconciler{
		Client:                mgr.GetClient(),
		Scheme:                scheme,
		Namespace:             ns.Name,
		DefaultDataPlaneImage: "kong:latest",
		LoggingMode:           logging.DevelopmentMode,
	}
	StartReconcilers(ctx, t, mgr, logs, r)

	c := mgr.GetClient()

	// Create a GatewayClass accepted by the controller.
	gc := &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{Name: "gc-ko"},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: gatewayv1.GatewayController(vars.ControllerName()),
		},
	}
	require.NoError(t, c.Create(ctx, gc))

	t.Log("patching GatewayClass status to Accepted=True")
	require.Eventually(t, testutils.GatewayClassAcceptedStatusUpdate(t, ctx, gc.Name, c), waitTime, tickTime)

	// Create an initial INVALID TLS Secret referenced by the Gateway listener.
	secretName := "test-cert"
	bad := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns.Name, Name: secretName},
		Type:       corev1.SecretTypeTLS,
		Data: map[string][]byte{
			corev1.TLSCertKey:       []byte("not-a-cert"),
			corev1.TLSPrivateKeyKey: []byte("not-a-key"),
		},
	}
	require.NoError(t, c.Create(ctx, bad))

	// Create a Gateway with a TLS listener referencing the Secret.
	gw := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns.Name, Name: "gw"},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: gatewayv1.ObjectName(gc.Name),
			Listeners: []gatewayv1.Listener{
				{
					Name:     "https",
					Port:     443,
					Protocol: gatewayv1.HTTPSProtocolType,
					TLS: &gatewayv1.ListenerTLSConfig{
						Mode: lo.ToPtr(gatewayv1.TLSModeTerminate),
						CertificateRefs: []gatewayv1.SecretObjectReference{{
							Name: gatewayv1.ObjectName(secretName),
						}},
					},
				},
			},
		},
	}
	require.NoError(t, c.Create(ctx, gw))

	t.Log("verifying that the invalid Secret results in ResolvedRefs=False (InvalidCertificateRef)")
	gwNN := types.NamespacedName{Namespace: ns.Name, Name: gw.Name}
	require.Eventually(t, testutils.GatewayListenerResolvedRefsCondition(t, ctx, gwNN, c, metav1.ConditionFalse), waitTime, tickTime)

	t.Log("rotating the Secret with a valid TLS certificate and key")
	certPEM, keyPEM := certhelper.MustGenerateCertPEMFormat()
	require.NoError(t, c.Patch(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns.Name, Name: secretName},
		Type:       corev1.SecretTypeTLS,
		Data: map[string][]byte{
			corev1.TLSCertKey:       certPEM,
			corev1.TLSPrivateKeyKey: keyPEM,
		},
	}, client.MergeFrom(bad)))

	t.Log("verifying that after rotation the Secret watch enqueues the Gateway and ResolvedRefs becomes True")
	require.Eventually(t, testutils.GatewayListenerResolvedRefsCondition(t, ctx, gwNN, c, metav1.ConditionTrue), waitTime, tickTime)
}
