package aigateway_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Kong/ai-deck-converter/aigw"
	"github.com/Kong/ai-deck-converter/convert"
	"github.com/kong/go-kong/kong"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/yaml"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	aigatewayv1alpha1 "github.com/kong/kong-operator/v2/api/aigateway/v1alpha1"
	"github.com/kong/kong-operator/v2/internal/utils/index"
	"github.com/kong/kong-operator/v2/pkg/multiinstance/aigateway/translator"
	"github.com/kong/kong-operator/v2/test"
	"github.com/kong/kong-operator/v2/test/helpers/asserts"
	rtcontainers "github.com/kong/kong-operator/v2/test/runtimeintegration/aigateway/containers"
)

// goldenPayloadPath holds the golden dbless payload rendered by
// ai-deck-converter for the representative document below.
const goldenPayloadPath = "testdata/golden-dbless.yaml"

// requestTimeout bounds a single Admin API request.
const requestTimeout = 15 * time.Second

// modelEntityName is the name the representative model CR renders as in the
// ai_models entity collection.
const modelEntityName = "mock-model"

// TestConverterDBLessPayloadAcceptedByAIGatewayAdminAPI locks in the
// assumption the on-prem AI Gateway flow is built on: the dbless payload
// produced by ai-deck-converter (ConvertDocumentToDBLessYAML) for a
// representative configuration document is accepted unmodified by the AI
// Gateway Admin API via the same POST /config?check_hash=1&flatten_errors=1
// call the operator's push machinery makes, and the ai_models entries it
// carries become readable through the Admin API.
//
// The golden file under testdata pins the payload so converter-side changes
// that alter what the operator pushes show up as reviewable diffs even before
// they reach a data plane.
func TestConverterDBLessPayloadAcceptedByAIGatewayAdminAPI(t *testing.T) {
	if test.KongLicenseData() == "" {
		t.Skip("KONG_LICENSE_DATA is not set, skipping the AI Gateway contract test")
	}
	t.Parallel()
	ctx := t.Context()

	aigwC := rtcontainers.NewAIGateway(ctx, t)
	adminURL := aigwC.AdminURL(ctx, t)
	aigwClient, err := kong.NewClient(&adminURL, http.DefaultClient)
	require.NoError(t, err)

	payload := representativeDBLessPayload(t)
	assertGoldenPayload(t, payload)

	// The payload is parsed and re-marshaled as JSON once, mirroring how the
	// ingress controller's golden tests feed golden outputs to the Admin API:
	// JSON is the transport format the Admin API handles most predictably,
	// and the parse keeps the body independent of the golden bytes' YAML
	// formatting.
	cfg := map[string]any{}
	require.NoError(t, yaml.Unmarshal(payload, &cfg))
	cfgAsJSON, err := json.Marshal(cfg)
	require.NoError(t, err)

	require.EventuallyWithT(t, func(t *assert.CollectT) {
		reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
		defer cancel()

		err := aigwClient.ReloadDeclarativeRawConfig(reqCtx, bytes.NewReader(cfgAsJSON), true, true)
		if !assert.NoErrorf(t, err, "the AI Gateway Admin API rejected the converter's dbless payload") {
			if apiErr, ok := errors.AsType[*kong.APIError](err); ok {
				t.Errorf("Admin API response: %s", apiErr.Raw())
			}
			return
		}
	}, 60*time.Second, 250*time.Millisecond)

	assertAIModelsApplied(ctx, t, aigwC.AdminURL(ctx, t))
}

// TestEmptyDBLessPayloadKeepsAIGatewayReady pins the reason the translator stubs an
// empty configuration (translator.ensureNonEmptyConfig): the AI Gateway serves 200 on
// GET /status/ready — the endpoint the operator's data plane readiness probe hits —
// only once it has applied a configuration that carries at least one entity. Pushing
// the bare empty payload leaves it not-ready, and pushing the payload the translator
// renders for a gateway with no entities brings it ready.
func TestEmptyDBLessPayloadKeepsAIGatewayReady(t *testing.T) {
	if test.KongLicenseData() == "" {
		t.Skip("KONG_LICENSE_DATA is not set, skipping the AI Gateway contract test")
	}
	t.Parallel()
	ctx := t.Context()

	aigwC := rtcontainers.NewAIGateway(ctx, t)
	adminURL := aigwC.AdminURL(ctx, t)
	statusURL := aigwC.StatusURL(ctx, t)
	aigwClient, err := kong.NewClient(&adminURL, http.DefaultClient)
	require.NoError(t, err)

	push := func(payload string) {
		t.Helper()

		cfg := map[string]any{}
		require.NoError(t, yaml.Unmarshal([]byte(payload), &cfg))
		cfgAsJSON, err := json.Marshal(cfg)
		require.NoError(t, err)

		reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
		defer cancel()
		require.NoError(t, aigwClient.ReloadDeclarativeRawConfig(reqCtx, bytes.NewReader(cfgAsJSON), true, true))
	}

	// The bare db-less payload: exactly what the converter emits for a document
	// with no entities, without the translator's stub.
	push(`{"_format_version": "3.0", "_transform": false}`)
	asserts.Never(t, func(ctx context.Context) bool {
		return statusReady(ctx, t, statusURL)
	}, 5*time.Second, 250*time.Millisecond,
		"an empty configuration must leave the AI Gateway not-ready, or the stub entity is unnecessary")

	// The payload the translator renders for a gateway with no entities.
	payload, issues, err := translator.RenderDBLessYAML(&aigw.Document{}, convert.Options{})
	require.NoError(t, err)
	require.Empty(t, issues)
	push(string(payload))
	require.Eventually(t, func() bool {
		return statusReady(ctx, t, statusURL)
	}, 30*time.Second, 250*time.Millisecond,
		"the stub upstream must make the AI Gateway report ready")
}

// statusReady reports whether GET /status/ready answers 200.
func statusReady(ctx context.Context, t *testing.T, statusURL string) bool {
	t.Helper()

	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, statusURL+"/status/ready", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	return resp.StatusCode == http.StatusOK
}

// assertAIModelsApplied checks the Admin API's view of the applied
// configuration: GET /config returns the configuration the data plane
// operates on, and it must carry the ai_models entry the model CR rendered as
// (the ai_models custom entity has no REST endpoint of its own).
func assertAIModelsApplied(ctx context.Context, t *testing.T, adminURL string) {
	t.Helper()

	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, adminURL+"/config", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	require.Equal(t, http.StatusOK, resp.StatusCode, "GET /config failed: %s", body)
	assert.Contains(t, string(body), "ai_models", "the applied configuration must carry the ai_models collection")
	assert.Contains(t, string(body), modelEntityName,
		"the applied configuration must carry the model's ai_models entry")
}

// representativeDBLessPayload renders the dbless payload for a representative
// on-prem configuration: one entity of every aiconfiguration kind (see
// representativeObjects), built through the same translator the operator's
// push machinery uses.
func representativeDBLessPayload(t *testing.T) []byte {
	t.Helper()

	gw := &aigatewayv1alpha1.OnPremAIGateway{
		Namespace: "default", Name: gwName,
	}

	scheme := runtime.NewScheme()
	require.NoError(t, aigatewayv1alpha1.AddToScheme(scheme))
	require.NoError(t, aiconfigurationv1alpha1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	objs := representativeObjects(t)
	builder := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gw)
	for _, obj := range objs {
		builder = builder.WithObjects(obj.(client.Object))
	}
	// BuildDocument lists every entity kind pointing at the gateway, so every
	// kind's OnOnPremAIGatewayRef index must be registered on the fake client.
	for _, opt := range index.OptionsForAIGatewayModel() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayModelProvider() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayPolicy() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayConsumerGroup() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayConsumerCredential() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayConsumer() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayAuthStrategy() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayCACertificate() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayCertificate() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewaySNI() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayCustomPolicy() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayAgent() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayMCPServer() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	for _, opt := range index.OptionsForAIGatewayVault() {
		builder = builder.WithIndex(opt.Object, opt.Field, opt.ExtractValueFn)
	}
	cl := builder.Build()

	doc, statuses, err := translator.BuildDocument(context.Background(), cl, types.NamespacedName{Namespace: "default", Name: gwName})
	require.NoError(t, err)
	for _, s := range statuses {
		require.NoError(t, s.Err, "entity %s failed to translate", client.ObjectKeyFromObject(s.Obj))
	}
	if docYAML, err := yaml.Marshal(doc); err == nil {
		t.Logf("translated document:\n%s", docYAML)
	}

	// Every entity kind must contribute to the document.
	require.Len(t, doc.Models, 1, "the document must carry the model")
	require.Len(t, doc.ModelProviders, 1, "the document must carry the provider")
	require.Len(t, doc.Policies, 2, "the document must carry both policies")
	require.Len(t, doc.CustomPolicies, 1, "the document must carry the custom policy")
	require.Len(t, doc.ConsumerGroups, 1, "the document must carry the consumer group")
	require.Len(t, doc.Consumers, 1, "the document must carry the consumer")
	require.Len(t, doc.AuthStrategies, 1, "the document must carry the auth strategy")
	require.Len(t, doc.CACertificates, 1, "the document must carry the CA certificate")
	require.Len(t, doc.Certificates, 1, "the document must carry the certificate")
	require.Len(t, doc.SNIs, 1, "the document must carry the SNI")
	require.Len(t, doc.Agents, 1, "the document must carry the agent")
	require.Len(t, doc.MCPServers, 1, "the document must carry the MCP server")
	require.Len(t, doc.Vaults, 1, "the document must carry the vault")

	payload, issues, err := translator.RenderDBLessYAML(doc, convert.Options{})
	require.NoError(t, err)
	for _, issue := range issues {
		if issue.Severity == convert.Error {
			t.Logf("converter error: %s", issue.Reason)
		} else {
			t.Logf("converter warning: %s", issue.Reason)
		}
	}
	errs := make([]convert.ConversionIssue, 0, len(issues))
	for _, issue := range issues {
		if issue.Severity == convert.Error {
			errs = append(errs, issue)
		}
	}
	require.Empty(t, errs, "the representative document must convert without errors")
	return payload
}

// assertGoldenPayload compares the rendered payload against the golden file,
// updating it in place when RUNTIMEINTEGRATION_UPDATE_GOLDEN is set.
func assertGoldenPayload(t *testing.T, payload []byte) {
	t.Helper()

	if os.Getenv("RUNTIMEINTEGRATION_UPDATE_GOLDEN") == "true" {
		require.NoError(t, os.MkdirAll(filepath.Dir(goldenPayloadPath), 0o755))
		require.NoError(t, os.WriteFile(goldenPayloadPath, payload, 0o600))
		t.Logf("golden payload updated: %s", goldenPayloadPath)
		return
	}

	golden, err := os.ReadFile(goldenPayloadPath)
	require.NoError(t, err, "the golden payload must exist; run with RUNTIMEINTEGRATION_UPDATE_GOLDEN=true to generate it")
	assert.Contains(t, string(golden), "ai_models:",
		"the golden payload must carry the ai_models collection")
	assert.Equal(t, string(golden), string(payload),
		"the converter's dbless payload changed: either update the golden (RUNTIMEINTEGRATION_UPDATE_GOLDEN=true) or treat this as a regression against the AI Gateway Admin API contract")
}
