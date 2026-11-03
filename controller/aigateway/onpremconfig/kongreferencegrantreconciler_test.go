package onpremconfig

import (
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/v2/pkg/multiinstance/aigateway/changenotifier"
)

func TestKongReferenceGrantReconcilerNotifiesCrossNamespaceReferrers(t *testing.T) {
	crossNamespaceRefStrategy := onPremAuthStrategyWithSecretRef("cross-ns-strat", "client-secret")
	crossNamespaceRefStrategy.Spec.APISpec.AIGatewayAuthStrategyConfig.OpenIDConnect.Config.ClientSecret[0].
		SecretRef.Namespace = new("secrets")
	// A same-namespace secretRef needs no grant, so a grant change must not notify it.
	sameNamespaceRefStrategy := onPremAuthStrategyWithSecretRef("same-ns-strat", "client-secret")
	// A cross-namespace secretRef into another namespace is not governed by the grant.
	otherNamespaceRefStrategy := onPremAuthStrategyWithSecretRef("other-ns-strat", "client-secret")
	otherNamespaceRefStrategy.Spec.APISpec.AIGatewayAuthStrategyConfig.OpenIDConnect.Config.ClientSecret[0].
		SecretRef.Namespace = new("elsewhere")
	cred := consumerCredential("cred", "consumer", "api-key")
	cred.Spec.APISpec.APIKey.SecretRef.Namespace = new("secrets")

	cn := changenotifier.New()
	defer cn.Close()
	cl := newReconcilerClient(t,
		crossNamespaceRefStrategy,
		sameNamespaceRefStrategy,
		otherNamespaceRefStrategy,
		onPremConsumer("consumer", "gw"),
		cred,
	)
	r := &KongReferenceGrantReconciler{Client: cl, Log: logr.Discard(), ChangeNotifier: cn}
	ch := cn.NotifyChannel()

	// The grant is not in the cache: a deleted grant must still re-render its referrers.
	_, err := r.Reconcile(t.Context(), ctrl.Request{Namespace: "secrets", Name: "allow-default"})
	require.NoError(t, err)
	var notified []string
	for range 2 {
		change := waitForChange(t, ch)
		require.Equal(t, types.NamespacedName{Namespace: "default", Name: "gw"}, *change.ParentNN)
		notified = append(notified, client.ObjectKeyFromObject(change.Object).String())
	}
	require.ElementsMatch(t, []string{"default/cross-ns-strat", "default/consumer"}, notified)
	requireNoChange(t, ch)

	// A grant in the entities' own namespace governs none of their secretRefs.
	_, err = r.Reconcile(t.Context(), ctrl.Request{Namespace: "default", Name: "allow-other"})
	require.NoError(t, err)
	requireNoChange(t, ch)
}

func TestKongReferenceGrantReconcilerWithoutChangeNotifierIsNoop(t *testing.T) {
	cl := newReconcilerClient(t, onPremAuthStrategyWithSecretRef("strat", "secret"))
	r := &KongReferenceGrantReconciler{Client: cl, Log: logr.Discard()}

	_, err := r.Reconcile(t.Context(), ctrl.Request{Namespace: "secrets", Name: "grant"})
	require.NoError(t, err)
}
