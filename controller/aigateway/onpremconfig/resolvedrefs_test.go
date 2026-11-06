package onpremconfig

import (
	"context"
	"errors"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/ingress-controller/pkg/status"
	"github.com/kong/kong-operator/v2/pkg/multiinstance/aigateway/changenotifier"
)

// referenceGrant returns a KongReferenceGrant in namespace that allows fromKind
// entities in fromNamespace to reference every object of the to kind there.
func referenceGrant(namespace, fromNamespace, fromKind string, to configurationv1alpha1.ReferenceGrantTo) *configurationv1alpha1.KongReferenceGrant {
	return &configurationv1alpha1.KongReferenceGrant{
		Name: "allow-" + fromNamespace + "-" + fromKind + "-" + string(to.Kind), Namespace: namespace,
		Spec: configurationv1alpha1.KongReferenceGrantSpec{
			From: []configurationv1alpha1.ReferenceGrantFrom{{
				Group:     configurationv1alpha1.Group(aiconfigurationv1alpha1.GroupVersion.Group),
				Kind:      configurationv1alpha1.Kind(fromKind),
				Namespace: configurationv1alpha1.Namespace(fromNamespace),
			}},
			To: []configurationv1alpha1.ReferenceGrantTo{to},
		},
	}
}

var (
	toSecret          = configurationv1alpha1.ReferenceGrantTo{Group: "core", Kind: "Secret"}
	toOnPremAIGateway = configurationv1alpha1.ReferenceGrantTo{Group: "aigateway.konghq.com", Kind: "OnPremAIGateway"}
)

// secretReferenceGrant returns a KongReferenceGrant in secretNamespace that allows
// fromKind entities in the default namespace to reference every Secret there.
func secretReferenceGrant(secretNamespace, fromKind string) *configurationv1alpha1.KongReferenceGrant {
	return referenceGrant(secretNamespace, "default", fromKind, toSecret)
}

// inOtherNamespace moves an on-prem entity to the "other" namespace, keeping its
// aiGatewayRef on the gateway "gw" of the default namespace.
func inOtherNamespace[T interface {
	client.Object
	aiGatewayRefGetter
}](obj T, ref *aiconfigurationv1alpha1.AIGatewayRef) T {
	obj.SetNamespace("other")
	ref.NamespacedRef = &commonv1alpha1.NamespacedRef{Name: "gw", Namespace: new("default")}
	return obj
}

// otherNamespaceStrategy returns an auth strategy of the "other" namespace that
// references the gateway of the default namespace, with its secretRef in ns.
func otherNamespaceStrategy(name string, ns *string) *aiconfigurationv1alpha1.AIGatewayAuthStrategy {
	strategy := authStrategyWithSecretRefNamespace(name, ns)
	return inOtherNamespace(strategy, &strategy.Spec.AIGatewayRef)
}

// otherNamespaceModel returns a model, a kind without secretRefs, of the "other"
// namespace that references the gateway of the default namespace.
func otherNamespaceModel(name string) *aiconfigurationv1alpha1.AIGatewayModel {
	model := onPremModelFixture(name, 1)
	return inOtherNamespace(model, &model.Spec.AIGatewayRef)
}

// authStrategyWithSecretRefNamespace returns an on-prem auth strategy whose secretRef
// points at the namespace ns (nil for the entity's namespace).
func authStrategyWithSecretRefNamespace(name string, ns *string) *aiconfigurationv1alpha1.AIGatewayAuthStrategy {
	strategy := onPremAuthStrategyWithSecretRef(name, "client-secret")
	strategy.Spec.APISpec.AIGatewayAuthStrategyConfig.OpenIDConnect.Config.ClientSecret[0].SecretRef.Namespace = ns
	return strategy
}

func resolvedRefsCondition(conditions []metav1.Condition) *metav1.Condition {
	return apimeta.FindStatusCondition(conditions, configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs)
}

func TestEnsureResolvedRefsCondition(t *testing.T) {
	t.Parallel()

	staleCondition := metav1.Condition{
		Type:   configurationv1alpha1.KongReferenceGrantConditionTypeResolvedRefs,
		Status: metav1.ConditionTrue,
		Reason: configurationv1alpha1.KongReferenceGrantReasonResolvedRefs,
	}
	tests := []struct {
		name        string
		obj         client.Object
		grants      []client.Object
		conditions  []metav1.Condition
		wantChanged bool
		wantStatus  metav1.ConditionStatus
		wantReason  string
		wantMessage string
	}{
		{
			name: "no secretRef namespace sets no condition",
			obj:  authStrategyWithSecretRefNamespace("strat", nil),
		},
		{
			name:        "same-namespace secretRef removes a stale condition",
			obj:         authStrategyWithSecretRefNamespace("strat", new("default")),
			conditions:  []metav1.Condition{staleCondition},
			wantChanged: true,
		},
		{
			name:        "cross-namespace secretRef without grant is not permitted",
			obj:         authStrategyWithSecretRefNamespace("strat", new("secrets")),
			wantChanged: true,
			wantStatus:  metav1.ConditionFalse,
			wantReason:  configurationv1alpha1.KongReferenceGrantReasonRefNotPermitted,
			wantMessage: "is not granted by any KongReferenceGrant",
		},
		{
			name:        "cross-namespace secretRef allowed by a grant is resolved",
			obj:         authStrategyWithSecretRefNamespace("strat", new("secrets")),
			grants:      []client.Object{secretReferenceGrant("secrets", "AIGatewayAuthStrategy")},
			wantChanged: true,
			wantStatus:  metav1.ConditionTrue,
			wantReason:  configurationv1alpha1.KongReferenceGrantReasonResolvedRefs,
			wantMessage: "KongReferenceGrants allow the cross-namespace references",
		},
		{
			name:        "grant for another kind is not permitted",
			obj:         authStrategyWithSecretRefNamespace("strat", new("secrets")),
			grants:      []client.Object{secretReferenceGrant("secrets", "AIGatewayPolicy")},
			conditions:  []metav1.Condition{staleCondition},
			wantChanged: true,
			wantStatus:  metav1.ConditionFalse,
			wantReason:  configurationv1alpha1.KongReferenceGrantReasonRefNotPermitted,
		},
		{
			name:        "cross-namespace aiGatewayRef without grant is not permitted",
			obj:         otherNamespaceModel("model"),
			wantChanged: true,
			wantStatus:  metav1.ConditionFalse,
			wantReason:  configurationv1alpha1.KongReferenceGrantReasonRefNotPermitted,
			wantMessage: "cross-namespace reference to OnPremAIGateway gw in namespace default",
		},
		{
			name:        "cross-namespace aiGatewayRef allowed by a grant is resolved",
			obj:         otherNamespaceModel("model"),
			grants:      []client.Object{referenceGrant("default", "other", "AIGatewayModel", toOnPremAIGateway)},
			wantChanged: true,
			wantStatus:  metav1.ConditionTrue,
			wantReason:  configurationv1alpha1.KongReferenceGrantReasonResolvedRefs,
			wantMessage: "KongReferenceGrants allow the cross-namespace references",
		},
		{
			name: "cross-namespace aiGatewayRef denied is reported before an allowed secretRef",
			obj:  otherNamespaceStrategy("strat", new("secrets")),
			grants: []client.Object{
				referenceGrant("secrets", "other", "AIGatewayAuthStrategy", toSecret),
			},
			wantChanged: true,
			wantStatus:  metav1.ConditionFalse,
			wantReason:  configurationv1alpha1.KongReferenceGrantReasonRefNotPermitted,
			wantMessage: "OnPremAIGateway",
		},
		{
			name: "cross-namespace aiGatewayRef allowed, secretRef denied is not permitted",
			obj:  otherNamespaceStrategy("strat", new("secrets")),
			grants: []client.Object{
				referenceGrant("default", "other", "AIGatewayAuthStrategy", toOnPremAIGateway),
			},
			wantChanged: true,
			wantStatus:  metav1.ConditionFalse,
			wantReason:  configurationv1alpha1.KongReferenceGrantReasonRefNotPermitted,
			wantMessage: "cross-namespace reference to Secret client-secret in namespace secrets",
		},
		{
			name: "cross-namespace aiGatewayRef and secretRef both allowed are resolved",
			obj:  otherNamespaceStrategy("strat", new("secrets")),
			grants: []client.Object{
				referenceGrant("default", "other", "AIGatewayAuthStrategy", toOnPremAIGateway),
				referenceGrant("secrets", "other", "AIGatewayAuthStrategy", toSecret),
			},
			wantChanged: true,
			wantStatus:  metav1.ConditionTrue,
			wantReason:  configurationv1alpha1.KongReferenceGrantReasonResolvedRefs,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cl := newReconcilerClient(t, tt.grants...)
			conditions := tt.conditions
			changed, err := ensureResolvedRefsCondition(t.Context(), cl, tt.obj, 4, &conditions)
			require.NoError(t, err)
			require.Equal(t, tt.wantChanged, changed)

			cond := resolvedRefsCondition(conditions)
			if tt.wantStatus == "" {
				require.Nil(t, cond)
				return
			}
			require.NotNil(t, cond)
			require.Equal(t, tt.wantStatus, cond.Status)
			require.Equal(t, tt.wantReason, cond.Reason)
			require.Contains(t, cond.Message, tt.wantMessage)
			require.Equal(t, int64(4), cond.ObservedGeneration)

			// The same input again changes nothing.
			changed, err = ensureResolvedRefsCondition(t.Context(), cl, tt.obj, 4, &conditions)
			require.NoError(t, err)
			require.False(t, changed)
		})
	}
}

func TestMapKongReferenceGrantToEntities(t *testing.T) {
	t.Parallel()

	konnectStrategy := authStrategyWithSecretRefNamespace("konnect-strat", new("secrets"))
	konnectStrategy.Spec.AIGatewayRef = aiconfigurationv1alpha1.AIGatewayRef{
		Kind:          aiconfigurationv1alpha1.AIGatewayRefKindKonnect,
		NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "konnect-gw"},
	}
	cred := consumerCredential("cred", "consumer", "api-key")
	cred.Spec.APISpec.APIKey.SecretRef.Namespace = new("secrets")
	cl := newReconcilerClient(t,
		authStrategyWithSecretRefNamespace("cross-ns-strat", new("secrets")),
		authStrategyWithSecretRefNamespace("same-ns-strat", nil),
		authStrategyWithSecretRefNamespace("other-ns-strat", new("elsewhere")),
		konnectStrategy,
		cred,
		otherNamespaceModel("cross-ns-model"),
		onPremModelFixture("same-ns-model", 1),
	)
	grant := secretReferenceGrant("secrets", "AIGatewayAuthStrategy")

	strategies := mapKongReferenceGrantToEntities[aiconfigurationv1alpha1.AIGatewayAuthStrategyList](cl)(t.Context(), grant)
	require.Equal(t, []reconcile.Request{{Namespace: "default", Name: "cross-ns-strat"}}, strategies)

	// A credential has no aiGatewayRef: its reconciler checks the ownership itself.
	creds := mapKongReferenceGrantToEntities[aiconfigurationv1alpha1.AIGatewayConsumerCredentialList](cl)(t.Context(), grant)
	require.Equal(t, []reconcile.Request{{Namespace: "default", Name: "cred"}}, creds)

	// A grant in the gateway's namespace governs the entities of another namespace
	// that reference the gateway, not the ones of the gateway's namespace.
	gatewayGrant := referenceGrant("default", "other", "AIGatewayModel", toOnPremAIGateway)
	models := mapKongReferenceGrantToEntities[aiconfigurationv1alpha1.AIGatewayModelList](cl)(t.Context(), gatewayGrant)
	require.Equal(t, []reconcile.Request{{Namespace: "other", Name: "cross-ns-model"}}, models)
	require.Empty(t, mapKongReferenceGrantToEntities[aiconfigurationv1alpha1.AIGatewayModelList](cl)(t.Context(), grant))

	// Another object kind enqueues nothing.
	require.Empty(t, mapKongReferenceGrantToEntities[aiconfigurationv1alpha1.AIGatewayAuthStrategyList](cl)(t.Context(), cred))
}

func TestReconcilerWritesResolvedRefsCondition(t *testing.T) {
	t.Parallel()

	strategy := authStrategyWithSecretRefNamespace("strat", new("secrets"))
	nn := client.ObjectKeyFromObject(strategy)
	cl := newReconcilerClient(t, strategy)
	rec := &AIGatewayAuthStrategyReconciler{}
	rec.SetCommonFields(
		cl, nil, logr.Discard(), 0, changenotifier.New(),
		&stubStatusClient{configStatus: status.ConfigurationStatusFailed, message: "not granted", enableState: true},
		nil,
	)
	reconcileAndGet := func() *metav1.Condition {
		t.Helper()
		_, err := rec.Reconcile(t.Context(), ctrl.Request{NamespacedName: nn})
		require.NoError(t, err)
		stored := &aiconfigurationv1alpha1.AIGatewayAuthStrategy{}
		require.NoError(t, cl.Get(t.Context(), nn, stored))
		return resolvedRefsCondition(stored.Status.Conditions)
	}

	cond := reconcileAndGet()
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, configurationv1alpha1.KongReferenceGrantReasonRefNotPermitted, cond.Reason)

	require.NoError(t, cl.Create(t.Context(), secretReferenceGrant("secrets", "AIGatewayAuthStrategy")))
	cond = reconcileAndGet()
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionTrue, cond.Status)

	// Back to a same-namespace Secret: the condition is removed.
	stored := &aiconfigurationv1alpha1.AIGatewayAuthStrategy{}
	require.NoError(t, cl.Get(t.Context(), nn, stored))
	stored.Spec.APISpec.AIGatewayAuthStrategyConfig.OpenIDConnect.Config.ClientSecret[0].SecretRef.Namespace = nil
	require.NoError(t, cl.Update(t.Context(), stored))
	require.Nil(t, reconcileAndGet())
}

func TestCredentialReconcilerWritesResolvedRefsCondition(t *testing.T) {
	t.Parallel()

	cred := consumerCredential("cred", "consumer", "api-key")
	cred.Spec.APISpec.APIKey.SecretRef.Namespace = new("secrets")
	cl := newCredentialReconcilerClient(t, cred, onPremConsumer("consumer", "gw"))
	rec := &AIGatewayConsumerCredentialReconciler{
		Client:       cl,
		Log:          logr.Discard(),
		StatusClient: &stubStatusClient{configStatus: status.ConfigurationStatusFailed, message: "not granted", enableState: true},
	}

	_, err := rec.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cred)})
	require.NoError(t, err)
	stored := &aiconfigurationv1alpha1.AIGatewayConsumerCredential{}
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(cred), stored))
	cond := resolvedRefsCondition(stored.Status.Conditions)
	require.NotNil(t, cond)
	require.Equal(t, metav1.ConditionFalse, cond.Status)
	require.Equal(t, configurationv1alpha1.KongReferenceGrantReasonRefNotPermitted, cond.Reason)
}

// errGrantListOutsideCache is what the instance's cache returns when it lists the
// KongReferenceGrants of a namespace the gateway does not watch.
var errGrantListOutsideCache = errors.New("unable to list: unwatched because of unknown namespace for the cache")

// failingGrantListClient fails every KongReferenceGrant list with errGrantListOutsideCache.
func failingGrantListClient(base client.Client) client.Client {
	return interceptor.NewClient(base.(client.WithWatch), interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if _, ok := list.(*configurationv1alpha1.KongReferenceGrantList); ok {
				return errGrantListOutsideCache
			}
			return c.List(ctx, list, opts...)
		},
	})
}

// A failed KongReferenceGrant check must not hold back the Programmed condition, which
// reports the translation's failure; the error is still returned, so the check retries.
func TestReconcilerWritesProgrammedWhenGrantCheckFails(t *testing.T) {
	t.Parallel()

	strategy := authStrategyWithSecretRefNamespace("strat", new("unwatched"))
	cl := failingGrantListClient(newReconcilerClient(t, strategy))
	rec := &AIGatewayAuthStrategyReconciler{}
	rec.SetCommonFields(
		cl, nil, logr.Discard(), 0, changenotifier.New(),
		&stubStatusClient{configStatus: status.ConfigurationStatusFailed, message: "grant check failed", enableState: true},
		nil,
	)

	_, err := rec.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(strategy)})
	require.ErrorIs(t, err, errGrantListOutsideCache)

	stored := &aiconfigurationv1alpha1.AIGatewayAuthStrategy{}
	require.NoError(t, cl.Get(t.Context(), client.ObjectKeyFromObject(strategy), stored))
	programmed := apimeta.FindStatusCondition(stored.Status.Conditions, string(configurationv1.ConditionProgrammed))
	require.NotNil(t, programmed)
	require.Equal(t, metav1.ConditionFalse, programmed.Status)
	require.Equal(t, "grant check failed", programmed.Message)
	// The check did not conclude, so no ResolvedRefs condition is invented.
	require.Nil(t, resolvedRefsCondition(stored.Status.Conditions))
}

func TestCredentialReconcilerWritesProgrammedWhenGrantCheckFails(t *testing.T) {
	t.Parallel()

	cred := consumerCredential("cred", "consumer", "api-key")
	cred.Spec.APISpec.APIKey.SecretRef.Namespace = new("unwatched")
	cl := failingGrantListClient(newCredentialReconcilerClient(t, cred, onPremConsumer("consumer", "gw")))
	rec := &AIGatewayConsumerCredentialReconciler{
		Client:       cl,
		Log:          logr.Discard(),
		StatusClient: &stubStatusClient{configStatus: status.ConfigurationStatusFailed, message: "grant check failed", enableState: true},
	}

	_, err := rec.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cred)})
	require.ErrorIs(t, err, errGrantListOutsideCache)

	programmed := credentialProgrammedCondition(t, cl, client.ObjectKeyFromObject(cred))
	require.NotNil(t, programmed)
	require.Equal(t, "grant check failed", programmed.Message)
}
