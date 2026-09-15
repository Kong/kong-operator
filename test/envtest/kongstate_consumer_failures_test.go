package envtest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	"github.com/kong/kong-operator/v2/ingress-controller/test/annotations"
	"github.com/kong/kong-operator/v2/ingress-controller/test/labels"
)

func TestKongStateFillConsumersAndCredentialsFailure(t *testing.T) {
	t.Parallel()

	const (
		// Waiting for translation-failure events means waiting for the manager to
		// start and its informer caches to sync. Under `-parallel 4` envtest with
		// -race + coverage that alone has been observed to take ~9s in CI, so keep
		// this generous - same as assertExpectedEvents in
		// configerrorevent_envtest_test.go.
		waitTime = time.Minute
		tickTime = 100 * time.Millisecond
	)

	// We use a deferred cancel to stop the manager and not wait for its timeout.
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	scheme := Scheme(t, WithKong)
	cfg, _ := Setup(t, ctx, scheme)
	client := NewControllerClient(t, scheme, cfg)

	ns := CreateNamespace(ctx, t, client)

	secrets := []*corev1.Secret{
		{
			Name:      "key-auth-cred",
			Namespace: ns.Name,
			Labels: map[string]string{
				labels.CredentialTypeLabel: "key-auth",
			},
			Data: map[string][]byte{
				"key": []byte("whatever"),
				"ttl": []byte("1024"),
			},
		},
		{
			Name:      "empty-cred",
			Namespace: ns.Name,
			Labels: map[string]string{
				labels.CredentialTypeLabel: "key-auth",
			},
			Data: map[string][]byte{},
		},
	}
	for _, secret := range secrets {
		require.NoError(t, client.Create(ctx, secret))
	}

	kongConsumers := []*configurationv1.KongConsumer{
		{
			Name:        "consumer-key-auth-cred",
			Namespace:   ns.Name,
			Annotations: map[string]string{annotations.IngressClassKey: annotations.DefaultIngressClass},
			Username:    "foo",
			Credentials: []string{
				"key-auth-cred",
			},
		},
		{
			Name:        "consumer-empty-cred",
			Namespace:   ns.Name,
			Annotations: map[string]string{annotations.IngressClassKey: annotations.DefaultIngressClass},
			CustomID:    "bar",
			Credentials: []string{
				"empty-cred",
			},
		},
	}
	for _, kongConsumer := range kongConsumers {
		require.NoError(t, client.Create(ctx, kongConsumer))
	}

	// These KongConsumers should fail admission via the CRD Validation Expressions.
	brokenKongConsumers := []*configurationv1.KongConsumer{
		{
			Name:        "consumer-no-username-and-no-custom-id",
			Namespace:   ns.Name,
			Annotations: map[string]string{annotations.IngressClassKey: annotations.DefaultIngressClass},
			Credentials: []string{
				"key-auth-cred",
			},
		},
	}
	for _, brokenKongConsumer := range brokenKongConsumers {
		require.Error(t, client.Create(ctx, brokenKongConsumer))
	}

	// KongConsumer name -> event message
	kongConsumerTranslationFailureMessages := map[string]string{
		"consumer-empty-cred": `credential "empty-cred" failure: failed to provision credential: key-auth is invalid: no key`,
	}

	RunManager(ctx, t, cfg, AdminAPIOptFns(), WithProxySyncInterval(500*time.Millisecond))

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		events := &corev1.EventList{}
		if !assert.NoError(c, client.List(ctx, events, &ctrlclient.ListOptions{
			Namespace: ns.Name,
		})) {
			return
		}

		for name, msg := range kongConsumerTranslationFailureMessages {
			// find the translation failure event attached to each expected KongConsumer.
			_, found := lo.Find(events.Items, func(e corev1.Event) bool {
				return e.InvolvedObject.Kind == "KongConsumer" && e.InvolvedObject.Name == name &&
					e.Reason == "KongConfigurationTranslationFailed" &&
					strings.Contains(e.Message, msg)
			})
			assert.Truef(c, found,
				"no KongConfigurationTranslationFailed event for KongConsumer %q containing %q; observed events:\n%s",
				name, msg, formatObservedEvents(events.Items),
			)
		}
	}, waitTime, tickTime)
}
