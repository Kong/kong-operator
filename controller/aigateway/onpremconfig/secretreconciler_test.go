package onpremconfig

import (
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	"github.com/kong/kong-operator/v2/pkg/multiinstance/aigateway/changenotifier"
)

func onPremAuthStrategyWithSecretRef(name, secretName string) *aiconfigurationv1alpha1.AIGatewayAuthStrategy {
	return &aiconfigurationv1alpha1.AIGatewayAuthStrategy{
		Namespace: "default", Name: name,
		Spec: aiconfigurationv1alpha1.AIGatewayAuthStrategySpec{
			AIGatewayRef: aiconfigurationv1alpha1.AIGatewayRef{
				Group:         aiconfigurationv1alpha1.AIGatewayRefGroupOnPrem,
				Kind:          aiconfigurationv1alpha1.AIGatewayRefKindOnPrem,
				NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "gw"},
			},
			APISpec: aiconfigurationv1alpha1.AIGatewayAuthStrategyAPISpec{
				AIGatewayAuthStrategyConfig: &aiconfigurationv1alpha1.AIGatewayAuthStrategyConfig{
					Type: aiconfigurationv1alpha1.AIGatewayAuthStrategyConfigTypeOpenIDConnect,
					OpenIDConnect: &aiconfigurationv1alpha1.AIGatewayAuthStrategyOpenIDConnect{
						Config: aiconfigurationv1alpha1.AIGatewayAuthStrategyOpenIDConnectConfig{
							ClientSecret: []aiconfigurationv1alpha1.SensitiveDataSource{
								{
									Type: aiconfigurationv1alpha1.SensitiveDataSourceTypeSecretRef,
									SecretRef: &aiconfigurationv1alpha1.SensitiveDataSecretRef{
										Name: secretName,
										Key:  "client-secret",
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

// waitForChange reads one change from the notifier, failing the test when none
// arrives within a short deadline.
func waitForChange(t *testing.T, ch <-chan changenotifier.Change) changenotifier.Change {
	t.Helper()
	select {
	case change := <-ch:
		return change
	case <-time.After(time.Second):
		t.Fatal("expected a change notification")
		return changenotifier.Change{}
	}
}

func requireNoChange(t *testing.T, ch <-chan changenotifier.Change) {
	t.Helper()
	select {
	case change := <-ch:
		t.Fatalf("unexpected change notification for %s/%s", change.Object.GetNamespace(), change.Object.GetName())
	case <-time.After(50 * time.Millisecond):
	}
}

func TestSecretReconcilerNotifiesReferencingEntities(t *testing.T) {
	const secretName = "client-secret"
	authStrategy := onPremAuthStrategyWithSecretRef("strat", secretName)
	// An entity whose secretRef points at a Secret of the same name in another namespace
	// must not match the Secret in the entity's namespace.
	crossNamespaceRefStrategy := onPremAuthStrategyWithSecretRef("cross-ns-ref-strat", secretName)
	crossNamespaceRefStrategy.Spec.APISpec.AIGatewayAuthStrategyConfig.OpenIDConnect.Config.ClientSecret[0].
		SecretRef.Namespace = new("other")
	// An unrelated Konnect-targeted entity referencing the same Secret must be skipped.
	konnectStrategy := onPremAuthStrategyWithSecretRef("konnect-strat", secretName)
	konnectStrategy.Spec.AIGatewayRef = aiconfigurationv1alpha1.AIGatewayRef{
		Kind:          aiconfigurationv1alpha1.AIGatewayRefKindKonnect,
		NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "konnect-gw"},
	}

	cn := changenotifier.New()
	defer cn.Close()
	cl := newReconcilerClient(t,
		authStrategy,
		crossNamespaceRefStrategy,
		konnectStrategy,
		&corev1.Secret{
			Namespace: "default", Name: secretName,
		},
		// A Secret no entity references: reconciling it must not notify.
		&corev1.Secret{
			Namespace: "default", Name: "unrelated",
		},
	)
	r := &SecretReconciler{Client: cl, Log: logr.Discard(), ChangeNotifier: cn}
	ch := cn.NotifyChannel()

	// Reconciling the referenced Secret notifies the on-prem entity only, addressed
	// to its parent OnPremAIGateway.
	_, err := r.Reconcile(t.Context(), ctrl.Request{Namespace: "default", Name: secretName})
	require.NoError(t, err)
	change := waitForChange(t, ch)
	strategy, ok := change.Object.(*aiconfigurationv1alpha1.AIGatewayAuthStrategy)
	require.True(t, ok)
	require.Equal(t, "default/strat", client.ObjectKeyFromObject(strategy).String())
	require.NotNil(t, change.ParentNN)
	require.Equal(t, types.NamespacedName{Namespace: "default", Name: "gw"}, *change.ParentNN)

	requireNoChange(t, ch)

	// Reconciling an unreferenced Secret notifies nothing.
	_, err = r.Reconcile(t.Context(), ctrl.Request{Namespace: "default", Name: "unrelated"})
	require.NoError(t, err)
	requireNoChange(t, ch)
}

func TestSecretReconcilerNotifiesOnSecretDeletion(t *testing.T) {
	cn := changenotifier.New()
	defer cn.Close()
	// The Secret is already gone from the cache: the reconcile still runs for the
	// delete event and must notify the referencing entity so it re-renders and
	// reports the dangling reference.
	cl := newReconcilerClient(t, onPremAuthStrategyWithSecretRef("strat", "deleted-secret"))
	r := &SecretReconciler{Client: cl, Log: logr.Discard(), ChangeNotifier: cn}

	_, err := r.Reconcile(t.Context(), ctrl.Request{Namespace: "default", Name: "deleted-secret"})
	require.NoError(t, err)

	change := waitForChange(t, cn.NotifyChannel())
	require.Equal(t, "strat", change.Object.GetName())
	require.Equal(t, types.NamespacedName{Namespace: "default", Name: "gw"}, *change.ParentNN)
}

func TestSecretReconcilerWithoutChangeNotifierIsNoop(t *testing.T) {
	cl := newReconcilerClient(t, onPremAuthStrategyWithSecretRef("strat", "secret"))
	r := &SecretReconciler{Client: cl, Log: logr.Discard()}

	_, err := r.Reconcile(t.Context(), ctrl.Request{Namespace: "default", Name: "secret"})
	require.NoError(t, err)
}
