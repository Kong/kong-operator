package envtest

import (
	"testing"
	"time"

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
		CacheSyncTimeout:      30 * time.Second,
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

	// Patch status Accepted=True for GatewayClass so KO Gateway controller processes Gateways.
	require.Eventually(t, func() bool {
		var cur gatewayv1.GatewayClass
		if err := c.Get(ctx, types.NamespacedName{Name: gc.Name}, &cur); err != nil {
			return false
		}
		cond := metav1.Condition{
			Type:               string(gatewayv1.GatewayClassConditionStatusAccepted),
			Status:             metav1.ConditionTrue,
			Reason:             string(gatewayv1.GatewayClassReasonAccepted),
			ObservedGeneration: cur.Generation,
			LastTransitionTime: metav1.Now(),
		}
		// Replace existing condition if present; otherwise append.
		updated := false
		for i := range cur.Status.Conditions {
			if cur.Status.Conditions[i].Type == cond.Type {
				cur.Status.Conditions[i] = cond
				updated = true
				break
			}
		}
		if !updated {
			cur.Status.Conditions = append(cur.Status.Conditions, cond)
		}
		if err := c.Status().Update(ctx, &cur); err != nil {
			return false
		}
		return true
	}, 10*time.Second, 200*time.Millisecond)

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
					TLS: &gatewayv1.GatewayTLSConfig{
						Mode: gatewayTLSModePtr(gatewayv1.TLSModeTerminate),
						CertificateRefs: []gatewayv1.SecretObjectReference{{
							Name: gatewayv1.ObjectName(secretName),
						}},
					},
				},
			},
		},
	}
	require.NoError(t, c.Create(ctx, gw))

	// Initially, the invalid Secret should result in ResolvedRefs=False (InvalidCertificateRef).
	require.Eventually(t, func() bool {
		var cur gatewayv1.Gateway
		if err := c.Get(ctx, types.NamespacedName{Namespace: ns.Name, Name: gw.Name}, &cur); err != nil {
			return false
		}
		if len(cur.Status.Listeners) == 0 {
			return false
		}
		for _, ls := range cur.Status.Listeners {
			for _, cond := range ls.Conditions {
				if cond.Type == string(gatewayv1.ListenerConditionResolvedRefs) && cond.Status == metav1.ConditionFalse {
					return true
				}
			}
		}
		return false
	}, 30*time.Second, 300*time.Millisecond)

	// Now rotate the Secret with a valid TLS certificate and key.
	certPEM, keyPEM := certhelper.MustGenerateCertPEMFormat()
	require.NoError(t, c.Patch(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns.Name, Name: secretName},
		Type:       corev1.SecretTypeTLS,
		Data: map[string][]byte{
			corev1.TLSCertKey:       certPEM,
			corev1.TLSPrivateKeyKey: keyPEM,
		},
	}, client.MergeFrom(bad)))

	// After rotation, the Secret watch should enqueue the Gateway and ResolvedRefs should become True.
	require.Eventually(t, func() bool {
		var cur gatewayv1.Gateway
		if err := c.Get(ctx, types.NamespacedName{Namespace: ns.Name, Name: gw.Name}, &cur); err != nil {
			return false
		}
		if len(cur.Status.Listeners) == 0 {
			return false
		}
		for _, ls := range cur.Status.Listeners {
			for _, cond := range ls.Conditions {
				if cond.Type == string(gatewayv1.ListenerConditionResolvedRefs) && cond.Status == metav1.ConditionTrue {
					return true
				}
			}
		}
		return false
	}, 60*time.Second, 500*time.Millisecond)
}

func gatewayTLSModePtr(m gatewayv1.TLSModeType) *gatewayv1.TLSModeType { return &m }
