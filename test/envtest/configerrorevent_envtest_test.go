package envtest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/kong/kubernetes-testing-framework/pkg/utils/kubernetes/generators"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	configurationv1beta1 "github.com/kong/kong-operator/v2/api/configuration/v1beta1"
	"github.com/kong/kong-operator/v2/ingress-controller/test"
	"github.com/kong/kong-operator/v2/ingress-controller/test/annotations"
	"github.com/kong/kong-operator/v2/ingress-controller/test/dataplane"
	"github.com/kong/kong-operator/v2/ingress-controller/test/manager/consts"
	"github.com/kong/kong-operator/v2/ingress-controller/test/mocks"
)

func TestConfigErrorEventGenerationInMemoryMode(t *testing.T) {
	// Can't be run in parallel because we're using t.Setenv() below which doesn't allow it.

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	scheme := Scheme(t, WithKong, WithGatewayAPI)

	restConfig, _ := Setup(t, ctx, scheme)
	ctrlClient := NewControllerClient(t, scheme, restConfig)

	ns := CreateNamespace(ctx, t, ctrlClient)
	ingressClassName := "kongenvtest"
	deployIngressClass(ctx, t, ingressClassName, ctrlClient)

	const podName = "kong-ingress-controller-tyjh1"
	t.Setenv("POD_NAMESPACE", ns.Name)
	t.Setenv("POD_NAME", podName)

	t.Log("deploying a minimal HTTP container deployment to test Ingress routes")
	container := generators.NewContainer("httpbin", test.HTTPBinImage, test.HTTPBinPort)
	deployment := generators.NewDeploymentForContainer(container)
	deployment.Namespace = ns.Name
	require.NoError(t, ctrlClient.Create(ctx, deployment))

	t.Log("creating a KongUpstreamPolicy with sticky sessions configuration")
	upstreamPolicy := &configurationv1beta1.KongUpstreamPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "echo-drain-policy",
			Namespace: ns.Name,
			Annotations: map[string]string{
				annotations.IngressClassKey: ingressClassName,
			},
		},
		Spec: configurationv1beta1.KongUpstreamPolicySpec{
			Algorithm: new("sticky-sessions"),
			Slots:     new(100),
			HashOn: &configurationv1beta1.KongUpstreamHash{
				Input: new(configurationv1beta1.HashInput("none")),
			},
			StickySessions: &configurationv1beta1.KongUpstreamStickySessions{
				Cookie:     "session-id",
				CookiePath: new("/"),
			},
		},
	}
	require.NoError(t, ctrlClient.Create(ctx, upstreamPolicy))

	t.Logf("exposing deployment %s via service", deployment.Name)
	service := generators.NewServiceForDeployment(deployment, corev1.ServiceTypeLoadBalancer)
	service.Annotations = map[string]string{
		// TCP services cannot have paths, and we don't catch this as a translation error
		"konghq.com/protocol":        "tcp",
		"konghq.com/path":            "/aitmatov",
		"konghq.com/upstream-policy": upstreamPolicy.Name,
		// Referencing non-existent KongPlugins.
		"konghq.com/plugins": "foo,bar,n1:p1",
	}
	service.Namespace = ns.Name
	require.NoError(t, ctrlClient.Create(ctx, service))

	t.Logf("creating an ingress for service %s with invalid configuration", service.Name)
	// GRPC routes cannot have methods, only HTTP, and we don't catch this as a translation error
	ingress := generators.NewIngressForService("/bar", map[string]string{
		"konghq.com/strip-path": "true",
		"konghq.com/protocols":  "grpcs",
		"konghq.com/methods":    "GET",
		"konghq.com/plugins":    "baz",
	}, service)
	ingress.Spec.IngressClassName = new(ingressClassName)
	ingress.Namespace = ns.Name
	t.Logf("deploying ingress %s", ingress.Name)
	require.NoError(t, ctrlClient.Create(ctx, ingress))

	RunManager(ctx, t, restConfig,
		AdminAPIOptFns(
			mocks.WithConfigPostError(formatErrBody(t, ns.Name, ingress, service)),
			mocks.WithVersion("3.14.0"),
		),
		WithPublishService(ns.Name),
		WithIngressClass(ingressClassName),
		WithKongUpstreamPolicyEnabled(),
		WithProxySyncInterval(100*time.Millisecond),
		// Add the init cache sync duration to prevent:
		// Warning | KongConfigurationTranslationFailed | Service | httpbin | failed fetching KongUpstreamPolicy: KongUpstreamPolicy 800363bc-a654-497a-8467-061e56e22a8f/echo-drain-policy not found
		WithInitCacheSyncDuration(2*time.Second),
	)

	predicatesToCheck := []func(e corev1.Event) bool{
		// `ingress-controller/internal/dataplane/kong_client_test.go` covers every individual
		// recorder emission. Here we only assert the subset of Events that has been stable
		// through the API-server-backed envtest Event stream.
		warningPredicate(dataplane.KongConfigurationApplyFailedEventReason, "Ingress", ingress.Name, `^invalid methods: cannot set 'methods' when 'protocols' is 'grpc' or 'grpcs'$`),
		warningPredicate(dataplane.KongConfigurationApplyFailedEventReason, "Service", service.Name, `^invalid path: value must be null$`),
		warningPredicate(dataplane.FallbackKongConfigurationApplyFailedEventReason, "Ingress", ingress.Name, `^invalid methods: cannot set 'methods' when 'protocols' is 'grpc' or 'grpcs'$`),
		warningPredicate(dataplane.FallbackKongConfigurationApplyFailedEventReason, "Service", service.Name, `^invalid path: value must be null$`),
		warningPredicate(dataplane.KongConfigurationApplyFailedEventReason, "Service", service.Name, `^invalid service:httpbin\.httpbin\.80: failed conditional validation given value of field 'protocol'$`),
		warningPredicate(dataplane.KongConfigurationApplyFailedEventReason, "Pod", podName, `failed to apply Kong configuration to http://[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+:[0-9]+: HTTP status 400 \(message: "failed posting new config to /config"\)`),
		warningPredicate(dataplane.KongConfigurationTranslationFailedEventReason, "Ingress", ingress.Name, `^referenced KongPlugin or KongClusterPlugin "baz" does not exist$`),
		warningPredicate(dataplane.FallbackKongConfigurationApplyFailedEventReason, "Service", service.Name, `^invalid service:httpbin\.httpbin\.80: failed conditional validation given value of field 'protocol'$`),
		warningPredicate(dataplane.FallbackKongConfigurationApplyFailedEventReason, "Pod", podName, `failed to apply fallback Kong configuration to http://[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+:[0-9]+: HTTP status 400 \(message: "failed posting new config to /config"\)`),
	}
	optionalPredicates := []func(e corev1.Event) bool{
		// These Service translation failures are not special at the recorder level;
		// they were observed to be unstable in the API-server-backed envtest Event
		// stream during the startup burst. If they are observed here they must still
		// have the expected shape.
		warningPredicate(dataplane.KongConfigurationTranslationFailedEventReason, "Service", service.Name, `^referenced KongPlugin or KongClusterPlugin "foo" does not exist$`),
		warningPredicate(dataplane.KongConfigurationTranslationFailedEventReason, "Service", service.Name, `^referenced KongPlugin or KongClusterPlugin "bar" does not exist$`),
		warningPredicate(dataplane.KongConfigurationTranslationFailedEventReason, "Service", service.Name, `^no grant found to referenced "n1:p1" plugin in the requested remote KongPlugin bind$`),
	}

	assertExpectedEvents(ctx, t, ctrlClient, ns, t.Name(), predicatesToCheck, optionalPredicates)
}

func TestConfigErrorEventGenerationDBMode(t *testing.T) {
	// Can't be run in parallel because we're using t.Setenv() below which doesn't allow it.

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	scheme := Scheme(t, WithKong)
	restConfig, _ := Setup(t, ctx, scheme)
	ctrlClientGlobal := NewControllerClient(t, scheme, restConfig)
	ns := CreateNamespace(ctx, t, ctrlClientGlobal)
	ctrlClient := client.NewNamespacedClient(ctrlClientGlobal, ns.Name)

	ingressClassName := "kongenvtest"
	deployIngressClass(ctx, t, ingressClassName, ctrlClient)

	const podName = "kong-ingress-controller-tyjh1"
	t.Setenv("POD_NAMESPACE", ns.Name)
	t.Setenv("POD_NAME", podName)

	t.Logf("creating a static consumer in %s namespace which will be used to test global validation", ns.Name)
	consumer := &configurationv1.KongConsumer{
		ObjectMeta: metav1.ObjectMeta{
			Name: "donenbai",
			Annotations: map[string]string{
				annotations.IngressClassKey: ingressClassName,
				// Referencing non-existent KongPlugin.
				"konghq.com/plugins": "foo, n1:p1",
			},
		},
		Username: "donenbai",
	}
	require.NoError(t, ctrlClient.Create(ctx, consumer))
	t.Cleanup(func() {
		if err := ctrlClient.Delete(ctx, consumer); err != nil && !apierrors.IsNotFound(err) && !errors.Is(err, context.Canceled) {
			assert.NoError(t, err)
		}
	})

	RunManager(ctx, t, restConfig,
		AdminAPIOptFns(
			mocks.WithRoot(formatDBRootResponse("999.999.999")),
		),
		WithPublishService(ns.Name),
		WithIngressClass(ingressClassName),
		WithProxySyncInterval(100*time.Millisecond),
	)

	predicatesToCheck := []func(e corev1.Event) bool{
		// Per-event recorder coverage lives in `kong_client_test.go`; envtest asserts the
		// stable end-to-end Events persisted by the API server.
		warningPredicate(dataplane.KongConfigurationApplyFailedEventReason, "KongConsumer", consumer.Name, fmt.Sprintf(`^invalid consumer:%s: HTTP status 400 \(message: "2 schema violations \(at least one of these fields must be non-empty: 'custom_id', 'username'; fake: unknown field\)"\)$`, consumer.Name)),
		warningPredicate(dataplane.KongConfigurationTranslationFailedEventReason, "KongConsumer", consumer.Name, `^referenced KongPlugin or KongClusterPlugin "foo" does not exist$`),
		warningPredicate(dataplane.KongConfigurationTranslationFailedEventReason, "KongConsumer", consumer.Name, `^no grant found to referenced "n1:p1" plugin in the requested remote KongPlugin bind$`),
		func(e corev1.Event) bool {
			return normalApplySucceededPredicate(podName)(e) || normalFallbackApplySucceededPredicate(podName)(e)
		},
		warningPredicate(dataplane.KongConfigurationApplyFailedEventReason, "Pod", podName, `failed to apply Kong configuration to http://[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+:[0-9]+: 1 errors occurred:\s+while processing event: Create consumer donenbai failed: HTTP status 400 \(message: "2 schema violations \(at least one of these fields must be non-empty: 'custom_id', 'username'; fake: unknown field\)\"\)`),
	}
	optionalPredicates := []func(e corev1.Event) bool{
		normalApplySucceededPredicate(podName),
		normalFallbackApplySucceededPredicate(podName),
	}

	assertExpectedEvents(ctx, t, ctrlClient, ns, t.Name(), predicatesToCheck, optionalPredicates)
}

func TestStickySessionsNotSupportedEventGeneration(t *testing.T) {
	t.Skip("skipping flaky test, TODO: https://github.com/Kong/kong-operator/issues/2082")

	// Can't be run in parallel because we're using t.Setenv() below which doesn't allow it.

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	scheme := Scheme(t, WithKong)
	restConfig, _ := Setup(t, ctx, scheme)
	ctrlClientGlobal := NewControllerClient(t, scheme, restConfig)
	ns := CreateNamespace(ctx, t, ctrlClientGlobal)
	ctrlClient := client.NewNamespacedClient(ctrlClientGlobal, ns.Name)

	ingressClassName := "kongenvtest"
	deployIngressClass(ctx, t, ingressClassName, ctrlClient)

	const podName = "kong-ingress-controller-tyjh1"
	t.Setenv("POD_NAMESPACE", ns.Name)
	t.Setenv("POD_NAME", podName)

	t.Log("deploying a minimal HTTP container deployment to test Ingress routes")
	container := generators.NewContainer("httpbin", test.HTTPBinImage, test.HTTPBinPort)
	deployment := generators.NewDeploymentForContainer(container)
	deployment.Namespace = ns.Name
	require.NoError(t, ctrlClient.Create(ctx, deployment))

	t.Log("creating a KongUpstreamPolicy with sticky sessions configuration")
	upstreamPolicy := &configurationv1beta1.KongUpstreamPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "echo-drain-policy",
			Namespace: ns.Name,
			Annotations: map[string]string{
				annotations.IngressClassKey: ingressClassName,
			},
		},
		Spec: configurationv1beta1.KongUpstreamPolicySpec{
			Algorithm: new("sticky-sessions"),
			Slots:     new(100),
			HashOn: &configurationv1beta1.KongUpstreamHash{
				Input: new(configurationv1beta1.HashInput("none")),
			},
			StickySessions: &configurationv1beta1.KongUpstreamStickySessions{
				Cookie:     "session-id",
				CookiePath: new("/"),
			},
		},
	}
	require.NoError(t, ctrlClient.Create(ctx, upstreamPolicy))

	t.Logf("exposing deployment %s via service", deployment.Name)
	service := generators.NewServiceForDeployment(deployment, corev1.ServiceTypeLoadBalancer)
	service.Annotations = map[string]string{
		"konghq.com/upstream-policy": upstreamPolicy.Name,
	}
	service.Namespace = ns.Name
	require.NoError(t, ctrlClient.Create(ctx, service))

	t.Logf("creating an ingress for service %s with invalid configuration", service.Name)
	ingress := generators.NewIngressForService("/bar", nil, service)
	ingress.Spec.IngressClassName = new(ingressClassName)
	ingress.Namespace = ns.Name
	t.Logf("deploying ingress %s", ingress.Name)
	require.NoError(t, ctrlClient.Create(ctx, ingress))

	kongContainer := runKongGatewayWithoutStickySessionsSupport(ctx, t)
	RunManager(ctx, t, restConfig,
		AdminAPIOptFns(),
		WithPublishService(ns.Name),
		WithIngressClass(ingressClassName),
		WithKongServiceFacadeFeatureEnabled(),
		WithInitCacheSyncDuration(2*time.Second),
		WithProxySyncInterval(100*time.Millisecond),
		WithKongAdminURLs(kongContainer.AdminURL(ctx, t)),
	)

	predicatesToCheck := []func(e corev1.Event) bool{
		func(e corev1.Event) bool {
			ok, err := regexp.MatchString(`successfully applied Kong configuration to http://([0-9]+\.[0-9]+\.[0-9]+\.[0-9]+:[0-9]+|localhost:[0-9]+)`, e.Message)
			return e.Type == corev1.EventTypeNormal &&
				e.Reason == dataplane.KongConfigurationApplySucceededEventReason &&
				e.InvolvedObject.Kind == "Pod" &&
				e.InvolvedObject.Name == podName &&
				ok && err == nil
		},
		warningPredicate(dataplane.KongConfigurationTranslationFailedEventReason, "Service", service.Name, `^sticky sessions algorithm specified in KongUpstreamPolicy 'echo-drain-policy' is not supported with Kong Gateway versions < 3\.11\.0$`),
	}

	assertExpectedEvents(ctx, t, ctrlClient, ns, t.Name(), predicatesToCheck)
}

func warningPredicate(eventReason, invObjKind, invObjName, msgToMatch string) func(e corev1.Event) bool {
	return predicate(corev1.EventTypeWarning, eventReason, invObjKind, invObjName, msgToMatch)
}

func normalApplySucceededPredicate(podName string) func(e corev1.Event) bool {
	return predicate(corev1.EventTypeNormal, dataplane.KongConfigurationApplySucceededEventReason, "Pod", podName, `^successfully applied Kong configuration to http://[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+:[0-9]+$`)
}

func normalFallbackApplySucceededPredicate(podName string) func(e corev1.Event) bool {
	return predicate(corev1.EventTypeNormal, dataplane.FallbackKongConfigurationApplySucceededEventReason, "Pod", podName, `^successfully applied fallback Kong configuration to http://[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+:[0-9]+$`)
}

func predicate(eventType, eventReason, invObjKind, invObjName, msgToMatch string) func(e corev1.Event) bool {
	return func(e corev1.Event) bool {
		ok, err := regexp.MatchString(msgToMatch, e.Message)
		return e.Type == eventType &&
			e.Reason == eventReason &&
			e.InvolvedObject.Kind == invObjKind &&
			e.InvolvedObject.Name == invObjName &&
			ok && err == nil
	}
}

func assertExpectedEvents(
	ctx context.Context, t *testing.T, ctrlClient client.Client, ns corev1.Namespace, expectedInstanceID string,
	requiredPredicates []func(e corev1.Event) bool, optionalPredicates ...[]func(e corev1.Event) bool,
) {
	t.Helper()
	t.Log("checking for events generated by the controller")
	const (
		waitTime = time.Minute
		tickTime = 100 * time.Millisecond
	)
	observedEvents := make(map[string]corev1.Event, len(requiredPredicates))
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		var events corev1.EventList
		// Filter out events that are not related to the current test instance.
		require.NoError(c, ctrlClient.List(ctx, &events, client.InNamespace(ns.Name)))
		currentEvents := lo.Filter(events.Items, func(e corev1.Event, _ int) bool {
			return e.Annotations[consts.InstanceIDAnnotationKey] == expectedInstanceID
		})

		for _, event := range currentEvents {
			observedEvents[eventKey(event)] = event
		}

		collectedEvents := make([]corev1.Event, 0, len(observedEvents))
		for _, event := range observedEvents {
			collectedEvents = append(collectedEvents, event)
		}
		allowedPredicates := append([]func(e corev1.Event) bool{}, requiredPredicates...)
		for _, predicates := range optionalPredicates {
			allowedPredicates = append(allowedPredicates, predicates...)
		}

		require.Emptyf(
			c,
			missingEventPredicateIndexes(requiredPredicates, collectedEvents),
			"missing expected events while observing:\n%s",
			formatObservedEvents(collectedEvents),
		)
		require.Emptyf(
			c,
			unexpectedEventIndexes(allowedPredicates, collectedEvents),
			"observed unexpected events:\n%s",
			formatObservedEvents(collectedEvents),
		)
	}, waitTime, tickTime)
}

func missingEventPredicateIndexes(predicatesToCheck []func(e corev1.Event) bool, collectedEvents []corev1.Event) []int {
	missing := make([]int, 0)
	for pi, predicate := range predicatesToCheck {
		if !lo.SomeBy(collectedEvents, func(e corev1.Event) bool {
			return predicate(e)
		}) {
			missing = append(missing, pi)
		}
	}
	return missing
}

func unexpectedEventIndexes(allowedPredicates []func(e corev1.Event) bool, collectedEvents []corev1.Event) []int {
	unexpected := make([]int, 0)
	for ei, event := range collectedEvents {
		if !lo.SomeBy(allowedPredicates, func(predicate func(e corev1.Event) bool) bool {
			return predicate(event)
		}) {
			unexpected = append(unexpected, ei)
		}
	}
	return unexpected
}

func formatObservedEvents(events []corev1.Event) string {
	if len(events) == 0 {
		return "<none>"
	}

	var b strings.Builder
	for _, e := range events {
		fmt.Fprintf(&b, "- %s | %s | %s | %s | %s\n", e.Type, e.Reason, e.InvolvedObject.Kind, e.InvolvedObject.Name, e.Message)
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func eventKey(e corev1.Event) string {
	if e.UID != "" {
		return string(e.UID)
	}
	return fmt.Sprintf("%s/%s|%s|%s|%s|%s", e.Namespace, e.Name, e.Type, e.Reason, e.InvolvedObject.Kind, e.Message)
}

func formatErrBody(t *testing.T, namespace string, ingress *netv1.Ingress, service *corev1.Service) []byte {
	t.Helper()

	const errBody = `{
	"code": 14,
	"name": "invalid declarative configuration",
	"flattened_errors": [
		{
			"entity_name": "{{ .Ingress.Name }}.httpbin.httpbin..80",
			"entity_tags": [
				"k8s-name:httpbin",
				"k8s-namespace:{{ .Namespace }}",
				"k8s-kind:Ingress",
				"k8s-uid:{{ .Ingress.UID }}",
				"k8s-group:networking.k8s.io",
				"k8s-version:v1"
			],
			"errors": [
				{
					"field": "methods",
					"type": "field",
					"message": "cannot set 'methods' when 'protocols' is 'grpc' or 'grpcs'"
				}
			],
			"entity": {
				"regex_priority": 0,
				"preserve_host": true,
				"name": "{{ .Ingress.Name }}.httpbin.httpbin..80",
				"protocols": [
					"grpcs"
				],
				"https_redirect_status_code": 426,
				"request_buffering": true,
				"tags": [
					"k8s-name:httpbin",
					"k8s-namespace:{{ .Namespace }}",
					"k8s-kind:Ingress",
					"k8s-uid:{{ .Ingress.UID }}",
					"k8s-group:networking.k8s.io",
					"k8s-version:v1"
				],
				"path_handling": "v0",
				"response_buffering": true,
				"methods": [
					"GET"
				],
				"paths": [
					"/bar/",
					"~/bar$"
				]
			},
			"entity_type": "route"
		},
		{
			"entity_name": "{{ .Ingress.Name }}.httpbin.80",
			"entity_tags": [
				"k8s-name:httpbin",
				"k8s-namespace:{{ .Namespace }}",
				"k8s-kind:Service",
				"k8s-uid:{{ .Service.UID }}",
				"k8s-version:v1"
			],
			"errors": [
				{
					"field": "path",
					"type": "field",
					"message": "value must be null"
				},
				{
					"type": "entity",
					"message": "failed conditional validation given value of field 'protocol'"
				}
			],
			"entity": {
				"read_timeout": 60000,
				"path": "/aitmatov",
				"write_timeout": 60000,
				"protocol": "tcp",
				"tags": [
					"k8s-name:httpbin",
					"k8s-namespace:{{ .Namespace }}",
					"k8s-kind:Service",
					"k8s-uid:{{ .Service.UID }}",
					"k8s-version:v1"
				],
				"retries": 5,
				"port": 80,
				"name": "{{ .Ingress.Name }}.httpbin.80",
				"host": "httpbin.{{ .Ingress.Name }}.80.svc",
				"connect_timeout": 60000
			},
			"entity_type": "service"
		}
	],
	"message": "declarative config is invalid: {}",
	"fields": {}
}`
	tmpl, err := template.New("body").Parse(errBody)
	require.NoError(t, err)

	type ErrBody struct {
		Namespace string
		Ingress   *netv1.Ingress
		Service   *corev1.Service
	}

	var b bytes.Buffer
	require.NoError(t, tmpl.Execute(&b, ErrBody{
		Namespace: namespace,
		Ingress:   ingress,
		Service:   service,
	}))

	return b.Bytes()
}

func formatDBRootResponse(version string) []byte {
	const defaultDBLessRootResponse = `{
		"version": "%s",
		"configuration": {
			"database": "postgres",
			"router_flavor": "traditional",
			"role": "traditional",
			"proxy_listeners": [
				{
					"ipv6only=on": false,
					"ipv6only=off": false,
					"ssl": false,
					"so_keepalive=off": false,
					"listener": "0.0.0.0:8000",
					"bind": false,
					"port": 8000,
					"deferred": false,
					"so_keepalive=on": false,
					"http2": false,
					"proxy_protocol": false,
					"ip": "0.0.0.0",
					"reuseport": false
				}
			]
		}
	}`
	return fmt.Appendf(nil, defaultDBLessRootResponse, version)
}
