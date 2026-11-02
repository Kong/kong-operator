package onpremconfig

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
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
						Config: aiconfigurationv1alpha1.AIGWOpenIDConnectGeneratedConfig{
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

func onPremConsumer(name, gatewayName string) *aiconfigurationv1alpha1.AIGatewayConsumer {
	return &aiconfigurationv1alpha1.AIGatewayConsumer{
		Namespace: "default", Name: name,
		Spec: aiconfigurationv1alpha1.AIGatewayConsumerSpec{
			AIGatewayRef: aiconfigurationv1alpha1.AIGatewayRef{
				Group:         aiconfigurationv1alpha1.AIGatewayRefGroupOnPrem,
				Kind:          aiconfigurationv1alpha1.AIGatewayRefKindOnPrem,
				NamespacedRef: &commonv1alpha1.NamespacedRef{Name: gatewayName},
			},
		},
	}
}

func consumerCredential(name, consumerName, secretName string) *aiconfigurationv1alpha1.AIGatewayConsumerCredential {
	return &aiconfigurationv1alpha1.AIGatewayConsumerCredential{
		Namespace: "default", Name: name,
		Spec: aiconfigurationv1alpha1.AIGatewayConsumerCredentialSpec{
			AIGatewayConsumerRef: commonv1alpha1.ObjectRef{
				Type:          commonv1alpha1.ObjectRefTypeNamespacedRef,
				NamespacedRef: &commonv1alpha1.NamespacedRef{Name: consumerName},
			},
			APISpec: aiconfigurationv1alpha1.AIGatewayConsumerCredentialAPISpec{
				APIKey: aiconfigurationv1alpha1.SensitiveDataSource{
					Type: aiconfigurationv1alpha1.SensitiveDataSourceTypeSecretRef,
					SecretRef: &aiconfigurationv1alpha1.SensitiveDataSecretRef{
						Name: secretName,
						Key:  "key",
					},
				},
			},
		},
	}
}

// A credential's Secret is rendered inside its parent consumer's document, so a change
// to it must notify the consumer, addressed to the consumer's OnPremAIGateway.
func TestSecretReconcilerNotifiesConsumerForCredentialSecret(t *testing.T) {
	const secretName = "consumer-api-key"
	consumer := onPremConsumer("consumer", "gw")
	// A credential whose consumer targets Konnect must be skipped.
	konnectConsumer := onPremConsumer("konnect-consumer", "konnect-gw")
	konnectConsumer.Spec.AIGatewayRef = aiconfigurationv1alpha1.AIGatewayRef{
		Kind:          aiconfigurationv1alpha1.AIGatewayRefKindKonnect,
		NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "konnect-gw"},
	}

	cn := changenotifier.New()
	defer cn.Close()
	cl := newReconcilerClient(t,
		consumer,
		konnectConsumer,
		consumerCredential("cred", "consumer", secretName),
		consumerCredential("konnect-cred", "konnect-consumer", secretName),
	)
	r := &SecretReconciler{Client: cl, Log: logr.Discard(), ChangeNotifier: cn}
	ch := cn.NotifyChannel()

	_, err := r.Reconcile(t.Context(), ctrl.Request{Namespace: "default", Name: secretName})
	require.NoError(t, err)
	change := waitForChange(t, ch)
	consumerObj, ok := change.Object.(*aiconfigurationv1alpha1.AIGatewayConsumer)
	require.True(t, ok)
	require.Equal(t, "default/consumer", client.ObjectKeyFromObject(consumerObj).String())
	require.NotNil(t, change.ParentNN)
	require.Equal(t, types.NamespacedName{Namespace: "default", Name: "gw"}, *change.ParentNN)

	requireNoChange(t, ch)

	// Reconciling an unreferenced Secret notifies nothing.
	_, err = r.Reconcile(t.Context(), ctrl.Request{Namespace: "default", Name: "unrelated"})
	require.NoError(t, err)
	requireNoChange(t, ch)
}

func TestSecretReconcilerNotifiesConsumerOnCredentialSecretDeletion(t *testing.T) {
	cn := changenotifier.New()
	defer cn.Close()
	cl := newReconcilerClient(t,
		onPremConsumer("consumer", "gw"),
		consumerCredential("cred", "consumer", "deleted-secret"),
	)
	r := &SecretReconciler{Client: cl, Log: logr.Discard(), ChangeNotifier: cn}

	_, err := r.Reconcile(t.Context(), ctrl.Request{Namespace: "default", Name: "deleted-secret"})
	require.NoError(t, err)

	change := waitForChange(t, cn.NotifyChannel())
	require.Equal(t, "consumer", change.Object.GetName())
	require.Equal(t, types.NamespacedName{Namespace: "default", Name: "gw"}, *change.ParentNN)
}

// Credentials that can never render into a consumer's document must be skipped without
// error and without a notification: a konnectID-type consumerRef has no NamespacedRef
// to resolve, and a consumerRef to a consumer that no longer exists has nothing to
// notify (the credential dangles until the consumer reappears or is removed).
func TestSecretReconcilerSkipsUnnotifiableCredentialRefs(t *testing.T) {
	const secretName = "consumer-api-key"
	konnectIDRefCred := consumerCredential("konnect-id-cred", "irrelevant", secretName)
	konnectIDRefCred.Spec.AIGatewayConsumerRef = commonv1alpha1.ObjectRef{
		Type:      commonv1alpha1.ObjectRefTypeKonnectID,
		KonnectID: new("d9d95f9f-eb20-43be-93ce-60117d41d75d"),
	}

	cn := changenotifier.New()
	defer cn.Close()
	cl := newReconcilerClient(t,
		konnectIDRefCred,
		consumerCredential("dangling-cred", "missing-consumer", secretName),
	)
	r := &SecretReconciler{Client: cl, Log: logr.Discard(), ChangeNotifier: cn}

	_, err := r.Reconcile(t.Context(), ctrl.Request{Namespace: "default", Name: secretName})
	require.NoError(t, err)
	requireNoChange(t, cn.NotifyChannel())
}

// A cross-namespace secretRef points at a Secret outside the entity's namespace, so a
// change to that Secret must notify the entity too.
func TestSecretReconcilerNotifiesCrossNamespaceReferrers(t *testing.T) {
	const secretName = "client-secret"
	crossNamespaceRefStrategy := onPremAuthStrategyWithSecretRef("strat", secretName)
	crossNamespaceRefStrategy.Spec.APISpec.AIGatewayAuthStrategyConfig.OpenIDConnect.Config.ClientSecret[0].
		SecretRef.Namespace = new("secrets")
	cred := consumerCredential("cred", "consumer", secretName)
	cred.Spec.APISpec.APIKey.SecretRef.Namespace = new("secrets")

	cn := changenotifier.New()
	defer cn.Close()
	cl := newReconcilerClient(t, crossNamespaceRefStrategy, onPremConsumer("consumer", "gw"), cred)
	r := &SecretReconciler{Client: cl, Log: logr.Discard(), ChangeNotifier: cn}
	ch := cn.NotifyChannel()

	_, err := r.Reconcile(t.Context(), ctrl.Request{Namespace: "secrets", Name: secretName})
	require.NoError(t, err)
	notified := []string{
		client.ObjectKeyFromObject(waitForChange(t, ch).Object).String(),
		client.ObjectKeyFromObject(waitForChange(t, ch).Object).String(),
	}
	require.ElementsMatch(t, []string{"default/strat", "default/consumer"}, notified)
	requireNoChange(t, ch)

	// The same Secret name in the entity's namespace is not referenced.
	_, err = r.Reconcile(t.Context(), ctrl.Request{Namespace: "default", Name: secretName})
	require.NoError(t, err)
	requireNoChange(t, ch)
}

func onPremConjurVault(name, secretName string, secretNamespace *string) *aiconfigurationv1alpha1.AIGatewayVault {
	return &aiconfigurationv1alpha1.AIGatewayVault{
		Namespace: "default", Name: name,
		Spec: aiconfigurationv1alpha1.AIGatewayVaultSpec{
			AIGatewayRef: aiconfigurationv1alpha1.AIGatewayRef{
				Group:         aiconfigurationv1alpha1.AIGatewayRefGroupOnPrem,
				Kind:          aiconfigurationv1alpha1.AIGatewayRefKindOnPrem,
				NamespacedRef: &commonv1alpha1.NamespacedRef{Name: "gw"},
			},
			APISpec: aiconfigurationv1alpha1.AIGatewayVaultAPISpec{
				AIGatewayVaultConfig: &aiconfigurationv1alpha1.AIGatewayVaultConfig{
					Type: aiconfigurationv1alpha1.AIGatewayVaultConfigTypeConjur,
					Conjur: &aiconfigurationv1alpha1.ConjurVault{
						Name: "conjur",
						Config: aiconfigurationv1alpha1.ConjurVaultConfig{
							APIKey: aiconfigurationv1alpha1.SensitiveDataSource{
								Type: aiconfigurationv1alpha1.SensitiveDataSourceTypeSecretRef,
								SecretRef: &aiconfigurationv1alpha1.SensitiveDataSecretRef{
									Name: secretName, Key: "apiKey", Namespace: secretNamespace,
								},
							},
						},
					},
				},
			},
		},
	}
}

// A vault renders its Secrets into the pushed configuration, so a Secret change or a
// KongReferenceGrant change must re-render it like any other entity with secretRefs.
func TestSecretAndGrantReconcilersNotifyVault(t *testing.T) {
	cn := changenotifier.New()
	defer cn.Close()
	cl := newReconcilerClient(t, onPremConjurVault("vault", "conjur-apikey", new("secrets")))
	ch := cn.NotifyChannel()

	secretReconciler := &SecretReconciler{Client: cl, Log: logr.Discard(), ChangeNotifier: cn}
	_, err := secretReconciler.Reconcile(t.Context(), ctrl.Request{Namespace: "secrets", Name: "conjur-apikey"})
	require.NoError(t, err)
	change := waitForChange(t, ch)
	require.Equal(t, "default/vault", client.ObjectKeyFromObject(change.Object).String())
	require.Equal(t, types.NamespacedName{Namespace: "default", Name: "gw"}, *change.ParentNN)
	requireNoChange(t, ch)

	grantReconciler := &KongReferenceGrantReconciler{Client: cl, Log: logr.Discard(), ChangeNotifier: cn}
	_, err = grantReconciler.Reconcile(t.Context(), ctrl.Request{Namespace: "secrets", Name: "allow-vaults"})
	require.NoError(t, err)
	require.Equal(t, "default/vault", client.ObjectKeyFromObject(waitForChange(t, ch).Object).String())
	requireNoChange(t, ch)
}

// listRecorder records the types of the lists read through it.
type listRecorder struct {
	client.Client

	listed map[string]struct{}
}

func (r *listRecorder) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	r.listed[fmt.Sprintf("%T", list)] = struct{}{}
	return r.Client.List(ctx, list, opts...)
}

// Every kind with secretRefs must be in secretRefNotifierFuncs: a missing kind keeps
// pushing a stale Secret value, or a revoked cross-namespace one, until an unrelated
// event re-renders its gateway. The expected kinds come from the scheme, so a new kind
// with secretRefs fails here until it is added.
func TestSecretRefNotifierFuncsCoverEveryKindWithSecretRefs(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, aiconfigurationv1alpha1.AddToScheme(scheme))

	var want []string
	for gvk := range scheme.AllKnownTypes() {
		if gvk.GroupVersion() != aiconfigurationv1alpha1.GroupVersion || strings.HasSuffix(gvk.Kind, "List") {
			continue
		}
		obj, err := scheme.New(gvk)
		require.NoError(t, err)
		if _, ok := obj.(sensitiveSecretRefGetter); !ok {
			continue
		}
		list, err := scheme.New(gvk.GroupVersion().WithKind(gvk.Kind + "List"))
		require.NoError(t, err)
		want = append(want, fmt.Sprintf("%T", list))
	}
	require.NotEmpty(t, want)

	recorder := &listRecorder{Client: newReconcilerClient(t), listed: map[string]struct{}{}}
	cn := changenotifier.New()
	defer cn.Close()
	require.NoError(t, notifySecretRefEntities(t.Context(), recorder, referencesSecret(types.NamespacedName{Namespace: "default", Name: "s"}), cn, logr.Discard()))

	for _, w := range want {
		require.Contains(t, recorder.listed, w, "secretRefNotifierFuncs does not list %s", w)
	}
}
