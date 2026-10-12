package aigateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	aexbuilder "k8s.io/apiextensions-apiserver/pkg/controller/openapi/builder"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/managedfields"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/events"
	kubespec3 "k8s.io/kube-openapi/pkg/spec3"
	validationspec "k8s.io/kube-openapi/pkg/validation/spec"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	k8syaml "sigs.k8s.io/yaml"

	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	kcfgconsts "github.com/kong/kong-operator/v2/api/common/consts"
	"github.com/kong/kong-operator/v2/ingress-controller/pkg/manager"
	adminapi "github.com/kong/kong-operator/v2/internal/adminapi"
	"github.com/kong/kong-operator/v2/internal/utils/index"
	managerscheme "github.com/kong/kong-operator/v2/modules/manager/scheme"
)

// newTestTypeConverter builds a managedfields.TypeConverter from the OnPremAIGateway
// CRD manifest, mirroring what the production TypeConverterProvider does. Memoized
// via [sync.OnceValue] since it is expensive to build.
var newTestTypeConverter = sync.OnceValue(func() managedfields.TypeConverter {
	path := filepath.Join("..", "..", "..", "config", "crd", "kong-operator", "aigateway.konghq.com_onpremaigateways.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		panic(fmt.Errorf("failed to read CRD manifest %s: %w", path, err))
	}
	crd := &apiextensionsv1.CustomResourceDefinition{}
	if err := k8syaml.Unmarshal(raw, crd); err != nil {
		panic(fmt.Errorf("failed to unmarshal CRD manifest %s: %w", path, err))
	}
	var specs []*kubespec3.OpenAPI
	for _, v := range crd.Spec.Versions {
		spec, err := aexbuilder.BuildOpenAPIV3(crd, v.Name, aexbuilder.Options{})
		if err != nil {
			panic(fmt.Errorf("failed to build OpenAPI v3 for %s/%s: %w", crd.Name, v.Name, err))
		}
		specs = append(specs, spec)
	}
	merged, err := aexbuilder.MergeSpecsV3(specs...)
	if err != nil {
		panic(fmt.Errorf("failed to merge CRD OpenAPI v3 specs: %w", err))
	}
	schemas := map[string]*validationspec.Schema{}
	if merged.Components != nil {
		maps.Copy(schemas, merged.Components.Schemas)
	}
	tc, err := managedfields.NewTypeConverter(schemas, false)
	if err != nil {
		panic(fmt.Errorf("failed to create TypeConverter: %w", err))
	}
	return tc
})

// fakePushClient records the payloads pushed through it and fails every push when
// failErr is set.
type fakePushClient struct {
	payloads []string
	failErr  error
}

func (f *fakePushClient) ReloadDeclarativeRawConfig(_ context.Context, config io.Reader, _, _ bool) error {
	b, err := io.ReadAll(config)
	if err != nil {
		return err
	}
	f.payloads = append(f.payloads, string(b))
	return f.failErr
}

// fakePushClientFactory stubs Instance.newPushClient: it records the endpoints it
// built clients for and serves a per-address push client, failing the pushes to
// the addresses listed in failures.
type fakePushClientFactory struct {
	built    []string
	failures map[string]error
	clients  map[string]*fakePushClient
}

func (f *fakePushClientFactory) newPushClient(address, _ string, _, _, _ []byte) (pusher, error) {
	f.built = append(f.built, address)
	if f.clients == nil {
		f.clients = map[string]*fakePushClient{}
	}
	c, ok := f.clients[address]
	if !ok {
		c = &fakePushClient{failErr: f.failures[address]}
		f.clients[address] = c
	}
	return c, nil
}

func testPushInstance(t *testing.T, objs ...client.Object) *Instance {
	t.Helper()

	i := NewInstance(
		manager.NewRandomID(),
		testr.New(t),
		Config{},
		Env{
			Scheme:                  managerscheme.Get(),
			GatewayNN:               types.NamespacedName{Namespace: testGatewayNamespace, Name: testGatewayName},
			AdminClientCertSecretNN: types.NamespacedName{Namespace: testGatewayNamespace, Name: "gw-admin-client-cert"},
			TypeConverter:           newTestTypeConverter(),
		},
	)

	objs = append(objs, &aigatewayv1alpha1.OnPremAIGateway{
		Namespace: testGatewayNamespace,
		Name:      testGatewayName,
	})
	// The translator lists the configuration entities by the OnOnPremAIGatewayRef
	// field index, so the fake client must register the same indexes the real
	// manager does.
	builder := fake.NewClientBuilder().
		WithScheme(managerscheme.Get()).
		WithReturnManagedFields().
		WithStatusSubresource(&aigatewayv1alpha1.OnPremAIGateway{}).
		WithObjects(objs...)
	for _, opts := range [][]index.Option{
		index.OptionsForAIGatewayModel(),
		index.OptionsForAIGatewayModelProvider(),
		index.OptionsForAIGatewayPolicy(),
		index.OptionsForAIGatewayConsumerGroup(),
		index.OptionsForAIGatewayAuthStrategy(),
		index.OptionsForAIGatewayConsumer(),
		index.OptionsForAIGatewayCertificate(),
		index.OptionsForAIGatewayCACertificate(),
		index.OptionsForAIGatewaySNI(),
		index.OptionsForAIGatewayCustomPolicy(),
		index.OptionsForAIGatewayAgent(),
		index.OptionsForAIGatewayMCPServer(),
	} {
		for _, opt := range opts {
			builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
		}
	}
	cl := builder.Build()
	i.client = cl
	i.eventRecorder = events.NewFakeRecorder(16)
	return i
}

func adminClientCertSecret() *corev1.Secret {
	return &corev1.Secret{
		Namespace: testGatewayNamespace,
		Name:      "gw-admin-client-cert",
		Data: map[string][]byte{
			"tls.crt": []byte("client-cert"),
			"tls.key": []byte("client-key"),
			"ca.crt":  []byte("ca"),
		},
	}
}

func getGatewayCondition(t *testing.T, instance *Instance, conditionType kcfgconsts.ConditionType) metav1.Condition {
	t.Helper()
	gw := &aigatewayv1alpha1.OnPremAIGateway{
		Namespace: testGatewayNamespace,
		Name:      testGatewayName,
	}
	require.NoError(t, instance.client.Get(context.Background(), client.ObjectKeyFromObject(gw), gw))
	for _, c := range gw.Status.Conditions {
		if c.Type == string(conditionType) {
			return c
		}
	}
	require.FailNow(t, "expected condition on the gateway status",
		"expected %s, conditions: %v", conditionType, gw.Status.Conditions)
	return metav1.Condition{}
}

func getGatewayPushCondition(t *testing.T, instance *Instance) metav1.Condition {
	t.Helper()
	return getGatewayCondition(t, instance, aigatewayv1alpha1.OnPremAIGatewayDataPlanesConfiguredType)
}

// requirePushed runs the push and asserts that it ran and succeeded:
// pushed=true and no error.
func requirePushed(t *testing.T, instance *Instance, gwNN types.NamespacedName, yamlPayload []byte) {
	t.Helper()
	pushed, err := instance.sendConfigToDataPlanes(context.Background(), gwNN, yamlPayload)
	require.NoError(t, err)
	require.True(t, pushed)
}

func TestSendConfigToDataPlanes(t *testing.T) {
	ctx := context.Background()
	gwNN := types.NamespacedName{Namespace: testGatewayNamespace, Name: testGatewayName}

	yamlPayload := []byte("_format_version: \"3.0\"\nservices:\n- name: svc\n  host: example.com\n")

	t.Run("pushes to all discovered endpoints", func(t *testing.T) {
		instance := testPushInstance(t, adminClientCertSecret())
		factory := &fakePushClientFactory{}
		instance.newPushClient = factory.newPushClient
		instance.setAdminAPIs(sets.New(adminAPI("https://10.0.0.1:8444"), adminAPI("https://10.0.0.2:8444")))

		requirePushed(t, instance, gwNN, yamlPayload)
		require.Len(t, factory.built, 2)

		// The payload must have been converted to JSON before the push.
		for addr, c := range factory.clients {
			require.Len(t, c.payloads, 1, "expected exactly one push to %s", addr)
			var decoded map[string]any
			require.NoError(t, json.Unmarshal([]byte(c.payloads[0]), &decoded))
			require.Contains(t, decoded, "_format_version")
		}

		condition := getGatewayPushCondition(t, instance)
		require.Equal(t, metav1.ConditionTrue, condition.Status)
		require.Equal(t, string(aigatewayv1alpha1.OnPremAIGatewayConfigurationPushSucceededReason), condition.Reason)
	})

	t.Run("pushes to the remaining endpoints when one fails", func(t *testing.T) {
		instance := testPushInstance(t, adminClientCertSecret())
		factory := &fakePushClientFactory{
			failures: map[string]error{"https://10.0.0.1:8444": fmt.Errorf("connection refused")},
		}
		instance.newPushClient = factory.newPushClient
		instance.setAdminAPIs(sets.New(adminAPI("https://10.0.0.1:8444"), adminAPI("https://10.0.0.2:8444")))

		pushed, err := instance.sendConfigToDataPlanes(ctx, gwNN, yamlPayload)
		require.Error(t, err)
		require.True(t, pushed)
		require.Contains(t, err.Error(), "default/dp: connection refused")

		// The healthy endpoint still got the payload.
		healthy := factory.clients["https://10.0.0.2:8444"]
		require.NotNil(t, healthy)
		require.Len(t, healthy.payloads, 1)

		condition := getGatewayPushCondition(t, instance)
		require.Equal(t, metav1.ConditionFalse, condition.Status)
		require.Equal(t, string(aigatewayv1alpha1.OnPremAIGatewayConfigurationPushFailedReason), condition.Reason)

		// A Warning event listing the failure is emitted when the condition changes.
		recorder := instance.eventRecorder.(*events.FakeRecorder)
		select {
		case event := <-recorder.Events:
			require.Contains(t, event, "connection refused")
		default:
			t.Fatal("expected a Warning event for the failed push")
		}
	})

	t.Run("recurring failures emit a single event and keep the transition time", func(t *testing.T) {
		instance := testPushInstance(t, adminClientCertSecret())
		factory := &fakePushClientFactory{
			failures: map[string]error{"https://10.0.0.1:8444": fmt.Errorf("connection refused")},
		}
		instance.newPushClient = factory.newPushClient
		instance.setAdminAPIs(sets.New(adminAPI("https://10.0.0.1:8444")))

		_, err := instance.sendConfigToDataPlanes(ctx, gwNN, yamlPayload)
		require.Error(t, err)
		first := getGatewayPushCondition(t, instance)

		// The first failed push emits its Warning event; drain it.
		recorder := instance.eventRecorder.(*events.FakeRecorder)
		select {
		case <-recorder.Events:
		default:
			t.Fatal("expected a Warning event for the first failed push")
		}

		// The retry loop re-attempts the push on the next tick: the unchanged
		// failure must not be reported as a change (no event, same timestamp).
		_, err = instance.sendConfigToDataPlanes(ctx, gwNN, yamlPayload)
		require.Error(t, err)
		second := getGatewayPushCondition(t, instance)

		require.Equal(t, first.LastTransitionTime, second.LastTransitionTime)
		select {
		case event := <-recorder.Events:
			t.Fatal("expected no additional Warning event for the unchanged failure", event)
		default:
		}
	})

	t.Run("no discovered endpoints is a no-op", func(t *testing.T) {
		instance := testPushInstance(t, adminClientCertSecret())
		factory := &fakePushClientFactory{}
		instance.newPushClient = factory.newPushClient
		instance.setAdminAPIs(sets.New[adminapi.DiscoveredAdminAPI]())

		pushed, err := instance.sendConfigToDataPlanes(ctx, gwNN, yamlPayload)
		require.NoError(t, err)
		require.False(t, pushed)
		require.Empty(t, factory.built)
	})

	t.Run("missing certificate material fails", func(t *testing.T) {
		instance := testPushInstance(t, &corev1.Secret{
			Namespace: testGatewayNamespace, Name: "gw-admin-client-cert",
			Data: map[string][]byte{"tls.crt": []byte("cert")},
		})
		factory := &fakePushClientFactory{}
		instance.newPushClient = factory.newPushClient
		instance.setAdminAPIs(sets.New(adminAPI("https://10.0.0.1:8444")))

		_, err := instance.sendConfigToDataPlanes(ctx, gwNN, yamlPayload)
		require.Error(t, err)
		require.Contains(t, err.Error(), "missing required certificate material")
		require.Empty(t, factory.built)

		// Even a push that failed before contacting any endpoint is reported on
		// the gateway status.
		condition := getGatewayPushCondition(t, instance)
		require.Equal(t, metav1.ConditionFalse, condition.Status)
		require.Equal(t, string(aigatewayv1alpha1.OnPremAIGatewayConfigurationPushFailedReason), condition.Reason)
	})

	t.Run("caches push clients across syncs and prunes churned endpoints", func(t *testing.T) {
		instance := testPushInstance(t, adminClientCertSecret())
		factory := &fakePushClientFactory{}
		instance.newPushClient = factory.newPushClient
		instance.setAdminAPIs(sets.New(adminAPI("https://10.0.0.1:8444"), adminAPI("https://10.0.0.2:8444")))

		// The first sync builds one client per endpoint; the retry loop re-runs
		// the sync, so the second one must reuse the cached clients.
		requirePushed(t, instance, gwNN, yamlPayload)
		requirePushed(t, instance, gwNN, yamlPayload)
		require.Len(t, factory.built, 2)
		for addr, c := range factory.clients {
			require.Len(t, c.payloads, 2, "expected exactly two pushes to %s", addr)
		}

		// Endpoints no longer discovered get their cached clients pruned, and
		// newly discovered ones get fresh clients.
		instance.setAdminAPIs(sets.New(adminAPI("https://10.0.0.2:8444"), adminAPI("https://10.0.0.3:8444")))
		requirePushed(t, instance, gwNN, yamlPayload)
		require.Len(t, factory.built, 3)
		require.Len(t, instance.pushClients, 2)
	})
}

// TestReportTranslationStatus drives the ConfigurationProgrammed condition and
// the translation events the instance reports on the OnPremAIGateway.
func TestReportTranslationStatus(t *testing.T) {
	ctx := context.Background()
	gwNN := types.NamespacedName{Namespace: testGatewayNamespace, Name: testGatewayName}

	eventsOf := func(t *testing.T, instance *Instance) []string {
		t.Helper()
		var out []string
		recorder := instance.eventRecorder.(*events.FakeRecorder)
		for {
			select {
			case e := <-recorder.Events:
				out = append(out, e)
			default:
				return out
			}
		}
	}

	t.Run("successful translation without warnings", func(t *testing.T) {
		instance := testPushInstance(t)
		instance.reportTranslationStatus(ctx, gwNN, nil, nil, nil)

		condition := getGatewayCondition(t, instance, aigatewayv1alpha1.OnPremAIGatewayConfigurationProgrammedType)
		require.Equal(t, metav1.ConditionTrue, condition.Status)
		require.Equal(t, string(aigatewayv1alpha1.OnPremAIGatewayConfigurationTranslatedReason), condition.Reason)
		require.Equal(t, "Configuration translated successfully", condition.Message)
		require.Empty(t, eventsOf(t, instance))
	})

	t.Run("successful translation with warnings emits an event per warning", func(t *testing.T) {
		instance := testPushInstance(t)
		instance.reportTranslationStatus(ctx, gwNN, nil, nil,
			[]string{`model "m1" references unknown provider "p1"`, `sni "s1" references unknown certificate "c1"`})

		condition := getGatewayCondition(t, instance, aigatewayv1alpha1.OnPremAIGatewayConfigurationProgrammedType)
		require.Equal(t, metav1.ConditionTrue, condition.Status)
		require.Equal(t, string(aigatewayv1alpha1.OnPremAIGatewayConfigurationTranslatedReason), condition.Reason)
		require.Contains(t, condition.Message, "translated with 2 warnings")

		// One Warning event per warning, each carrying the warning text.
		events := eventsOf(t, instance)
		require.Len(t, events, 2)
		for i, w := range []string{"unknown provider", "unknown certificate"} {
			require.Contains(t, events[i], aigatewayv1alpha1.OnPremAIGatewayConfigurationTranslationWarningReason)
			require.Contains(t, events[i], w)
		}
	})

	t.Run("render error fails the condition and emits the failure and warning events", func(t *testing.T) {
		instance := testPushInstance(t)
		instance.reportTranslationStatus(ctx, gwNN, errors.New("rendering failed: broken document"),
			nil, []string{`model "m1" references unknown provider "p1"`})

		condition := getGatewayCondition(t, instance, aigatewayv1alpha1.OnPremAIGatewayConfigurationProgrammedType)
		require.Equal(t, metav1.ConditionFalse, condition.Status)
		require.Equal(t, string(aigatewayv1alpha1.OnPremAIGatewayConfigurationTranslationFailedReason), condition.Reason)
		require.Contains(t, condition.Message, "rendering failed: broken document")

		// The condition change emits the failure event; the warning is emitted
		// too, even on the error path.
		events := eventsOf(t, instance)
		require.Len(t, events, 2)
		require.Contains(t, events[0], aigatewayv1alpha1.OnPremAIGatewayConfigurationTranslationFailedReason)
		require.Contains(t, events[1], aigatewayv1alpha1.OnPremAIGatewayConfigurationTranslationWarningReason)
	})

	t.Run("entity failures are aggregated in the condition message", func(t *testing.T) {
		instance := testPushInstance(t)
		broken := testAIGatewayModel("model-broken")
		instance.reportTranslationStatus(ctx, gwNN, nil, []EntityFailure{
			{Obj: broken, Err: errors.New("spec.apiSpec is required")},
		}, nil)

		condition := getGatewayCondition(t, instance, aigatewayv1alpha1.OnPremAIGatewayConfigurationProgrammedType)
		require.Equal(t, metav1.ConditionFalse, condition.Status)
		require.Equal(t, string(aigatewayv1alpha1.OnPremAIGatewayConfigurationTranslationFailedReason), condition.Reason)
		require.Contains(t, condition.Message, "AIGatewayModel/default/model-broken: spec.apiSpec is required")

		events := eventsOf(t, instance)
		require.Len(t, events, 1)
		require.Contains(t, events[0], aigatewayv1alpha1.OnPremAIGatewayConfigurationTranslationFailedReason)
	})

	t.Run("recurring failure emits a single event and keeps the transition time", func(t *testing.T) {
		instance := testPushInstance(t)
		renderErr := errors.New("rendering failed: broken document")
		instance.reportTranslationStatus(ctx, gwNN, renderErr, nil, nil)
		first := getGatewayCondition(t, instance, aigatewayv1alpha1.OnPremAIGatewayConfigurationProgrammedType)
		require.Len(t, eventsOf(t, instance), 1)

		// The retry loop re-reports the same failure on the next tick: the
		// unchanged failure must not be reported as a change.
		instance.reportTranslationStatus(ctx, gwNN, renderErr, nil, nil)
		second := getGatewayCondition(t, instance, aigatewayv1alpha1.OnPremAIGatewayConfigurationProgrammedType)

		require.Equal(t, first.LastTransitionTime, second.LastTransitionTime)
		require.Empty(t, eventsOf(t, instance))
	})

	t.Run("an overlong failure message is truncated", func(t *testing.T) {
		instance := testPushInstance(t)
		long := strings.Repeat("x", maxConditionMessageLength*2)
		instance.reportTranslationStatus(ctx, gwNN, errors.New(long), nil, nil)

		condition := getGatewayCondition(t, instance, aigatewayv1alpha1.OnPremAIGatewayConfigurationProgrammedType)
		require.Len(t, condition.Message, maxConditionMessageLength)
		require.True(t, strings.HasSuffix(condition.Message, "..."))
	})

	t.Run("a nil event recorder is tolerated", func(t *testing.T) {
		instance := testPushInstance(t)
		instance.eventRecorder = nil
		instance.reportTranslationStatus(ctx, gwNN, errors.New("rendering failed"), nil,
			[]string{"some warning"})

		condition := getGatewayCondition(t, instance, aigatewayv1alpha1.OnPremAIGatewayConfigurationProgrammedType)
		require.Equal(t, metav1.ConditionFalse, condition.Status)
	})

	t.Run("the instance-owned conditions survive each other's applies", func(t *testing.T) {
		instance := testPushInstance(t)

		// Translation reported first, push second.
		instance.reportTranslationStatus(ctx, gwNN, errors.New("rendering failed"), nil, nil)
		require.NoError(t, instance.reportPushStatus(ctx, gwNN, 1, nil))
		require.Equal(t, metav1.ConditionFalse,
			getGatewayCondition(t, instance, aigatewayv1alpha1.OnPremAIGatewayConfigurationProgrammedType).Status)
		require.Equal(t, metav1.ConditionTrue,
			getGatewayCondition(t, instance, aigatewayv1alpha1.OnPremAIGatewayDataPlanesConfiguredType).Status)

		// Push reported first, translation second: the translation condition
		// must not have been pruned by the push apply.
		instance = testPushInstance(t)
		require.NoError(t, instance.reportPushStatus(ctx, gwNN, 1, []string{"endpoint down"}))
		instance.reportTranslationStatus(ctx, gwNN, nil, nil, nil)
		require.Equal(t, metav1.ConditionTrue,
			getGatewayCondition(t, instance, aigatewayv1alpha1.OnPremAIGatewayConfigurationProgrammedType).Status)
		require.Equal(t, metav1.ConditionFalse,
			getGatewayCondition(t, instance, aigatewayv1alpha1.OnPremAIGatewayDataPlanesConfiguredType).Status)
	})
}

func TestTruncateMessage(t *testing.T) {
	require.Equal(t, "short", truncateMessage("short"))

	// ASCII input is cut at the length limit minus the ellipsis.
	long := strings.Repeat("a", maxConditionMessageLength+10)
	truncated := truncateMessage(long)
	require.Len(t, truncated, maxConditionMessageLength)
	require.True(t, strings.HasSuffix(truncated, "..."))

	// Multi-byte input cut at a rune boundary stays valid UTF-8: the boundary
	// lands inside a multi-byte rune, which must not be split.
	multibyte := strings.Repeat("ą", maxConditionMessageLength) // 'ą' is 2 bytes
	truncated = truncateMessage(multibyte)
	require.LessOrEqual(t, len(truncated), maxConditionMessageLength)
	require.True(t, utf8.ValidString(truncated))
}
