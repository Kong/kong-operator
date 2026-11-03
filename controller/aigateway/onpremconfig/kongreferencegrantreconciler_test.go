package onpremconfig

import (
	"context"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
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

	// A grant in the entities' own namespace governs none of their secretRefs, and the
	// namespace holds no gateway to re-render.
	_, err = r.Reconcile(t.Context(), ctrl.Request{Namespace: "default", Name: "allow-other"})
	require.NoError(t, err)
	requireNoChange(t, ch)
}

// onPremGatewayFixture returns the OnPremAIGateway object the grant reconciler lists when
// a grant of that namespace changes.
func onPremGatewayFixture(name, namespace string) *aigatewayv1alpha1.OnPremAIGateway {
	return &aigatewayv1alpha1.OnPremAIGateway{Namespace: namespace, Name: name}
}

// TestKongReferenceGrantReconcilerNotifiesGatewaysOfItsNamespace covers the re-render
// trigger of a grant on the entity -> gateway reference: every gateway of the grant's
// namespace re-renders, because the grant may allow, or no longer allow, entities of
// another namespace to reference it. Gateways of other namespaces do not depend on it.
func TestKongReferenceGrantReconcilerNotifiesGatewaysOfItsNamespace(t *testing.T) {
	cn := changenotifier.New()
	defer cn.Close()
	cl := newReconcilerClient(t,
		onPremGatewayFixture("gw", "default"),
		onPremGatewayFixture("other-gw", "other"),
	)
	r := &KongReferenceGrantReconciler{Client: cl, Log: logr.Discard(), ChangeNotifier: cn}
	ch := cn.NotifyChannel()

	_, err := r.Reconcile(t.Context(), ctrl.Request{Namespace: "default", Name: "grant"})
	require.NoError(t, err)

	change := waitForChange(t, ch)
	require.Equal(t, types.NamespacedName{Namespace: "default", Name: "gw"}, client.ObjectKeyFromObject(change.Object))
	require.NotNil(t, change.ParentNN)
	require.Equal(t, types.NamespacedName{Namespace: "default", Name: "gw"}, *change.ParentNN)
	requireNoChange(t, ch)

	// A namespace without gateways notifies nothing.
	_, err = r.Reconcile(t.Context(), ctrl.Request{Namespace: "empty", Name: "grant"})
	require.NoError(t, err)
	requireNoChange(t, ch)
}

// gatewayOnlyCacheClient mimics the instance's cache, where the OnPremAIGateway cache
// holds the gateway's namespace only: a list scoped to any other namespace fails like
// controller-runtime's namespace-scoped cache does.
type gatewayOnlyCacheClient struct {
	client.Client
	namespace string
}

func (c gatewayOnlyCacheClient) List(
	ctx context.Context,
	list client.ObjectList,
	opts ...client.ListOption,
) error {
	var o client.ListOptions
	o.ApplyOptions(opts)
	if o.Namespace != "" && o.Namespace != c.namespace {
		return fmt.Errorf("unable to list: %s because of unknown namespace for the cache", o.Namespace)
	}
	return c.Client.List(ctx, list, opts...)
}

// TestKongReferenceGrantReconcilerWithGrantOutsideGatewayNamespace covers a grant in the
// namespace of a cross-namespace Secret: the stub fails a namespace-scoped gateway list, so
// the reconciler must list without InNamespace and must still notify the entities whose
// secretRefs the grant governs.
func TestKongReferenceGrantReconcilerWithGrantOutsideGatewayNamespace(t *testing.T) {
	strategy := onPremAuthStrategyWithSecretRef("cross-ns-strat", "client-secret")
	strategy.Spec.APISpec.AIGatewayAuthStrategyConfig.OpenIDConnect.Config.ClientSecret[0].
		SecretRef.Namespace = new("secrets")

	cn := changenotifier.New()
	defer cn.Close()
	cl := gatewayOnlyCacheClient{
		Client:    newReconcilerClient(t, onPremGatewayFixture("gw", "default"), strategy),
		namespace: "default",
	}
	r := &KongReferenceGrantReconciler{Client: cl, Log: logr.Discard(), ChangeNotifier: cn}
	ch := cn.NotifyChannel()

	_, err := r.Reconcile(t.Context(), ctrl.Request{Namespace: "secrets", Name: "allow-aigateway-secrets"})
	require.NoError(t, err)

	change := waitForChange(t, ch)
	require.Equal(t, types.NamespacedName{Namespace: "default", Name: "cross-ns-strat"}, client.ObjectKeyFromObject(change.Object))
	require.Equal(t, types.NamespacedName{Namespace: "default", Name: "gw"}, *change.ParentNN)
}

func TestKongReferenceGrantReconcilerNotifiesSecretRefEntitiesWhenGatewayListFails(t *testing.T) {
	strategy := onPremAuthStrategyWithSecretRef("cross-ns-strat", "client-secret")
	strategy.Spec.APISpec.AIGatewayAuthStrategyConfig.OpenIDConnect.Config.ClientSecret[0].
		SecretRef.Namespace = new("secrets")

	cn := changenotifier.New()
	defer cn.Close()
	cl := failingGatewayListClient{Client: newReconcilerClient(t, onPremGatewayFixture("gw", "default"), strategy)}
	r := &KongReferenceGrantReconciler{Client: cl, Log: logr.Discard(), ChangeNotifier: cn}
	ch := cn.NotifyChannel()

	_, err := r.Reconcile(t.Context(), ctrl.Request{Namespace: "secrets", Name: "allow-aigateway-secrets"})
	require.Error(t, err)

	change := waitForChange(t, ch)
	require.Equal(t, types.NamespacedName{Namespace: "default", Name: "cross-ns-strat"}, client.ObjectKeyFromObject(change.Object))
}

// failingGatewayListClient fails every OnPremAIGateway list, to prove that a gateway
// list failure does not skip the notification of the secretRef entities.
type failingGatewayListClient struct {
	client.Client
}

func (c failingGatewayListClient) List(
	ctx context.Context,
	list client.ObjectList,
	opts ...client.ListOption,
) error {
	if _, ok := list.(*aigatewayv1alpha1.OnPremAIGatewayList); ok {
		return fmt.Errorf("unable to list: gateway cache unavailable")
	}
	return c.Client.List(ctx, list, opts...)
}

func TestKongReferenceGrantReconcilerWithoutChangeNotifierIsNoop(t *testing.T) {
	cl := newReconcilerClient(t,
		onPremAuthStrategyWithSecretRef("strat", "secret"),
		onPremGatewayFixture("gw", "default"),
	)
	r := &KongReferenceGrantReconciler{Client: cl, Log: logr.Discard()}

	_, err := r.Reconcile(t.Context(), ctrl.Request{Namespace: "secrets", Name: "grant"})
	require.NoError(t, err)

	_, err = r.Reconcile(t.Context(), ctrl.Request{Namespace: "default", Name: "grant"})
	require.NoError(t, err)
}
