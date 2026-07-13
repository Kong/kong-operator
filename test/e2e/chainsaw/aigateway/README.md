# AIGateway chainsaw e2e

Reproduces `ai-gateway-test-harness` criteria against the Kong Operator's Konnect AIGateway
CRDs and `AIGatewayDataPlane`. Each test stands up a Konnect AIGateway control plane, an
OpenAI-compatible provider + model, and an in-cluster data plane, then drives a real
chat-completions request and asserts `HTTP 200` + the `X-Kong-LLM-Model` header.

## Mock LLM upstream

The providers point at a shared in-cluster mock (no external egress, no real tokens): an Ollama
server behind a DB-less Kong proxy that enforces api-key auth. The manifests live in
[`../fixtures/aigateway`](../fixtures/aigateway) (kept outside this directory so chainsaw does
not run them as a test) and must be applied before the tests.

The mock provides:

- **Two models** served by Ollama — `qwen2.5:0.5b` and `gemma3:270m` — so multi-model /
  load-balancing criteria have distinct backends to route across.
- **Two independent providers** on the Kong proxy, exposed on the same route pattern, each with
  its own api key isolated by an ACL group (a key only works on its own provider's route):

  | Provider | Route | API key | ACL group |
  |----------|-------|---------|-----------|
  | A | `/provider-a` | `test-ai-gateway-key`   | `provider-a` |
  | B | `/provider-b` | `test-ai-gateway-key-b` | `provider-b` |

  Both routes forward to Ollama's `/v1/chat/completions`; the model is chosen by the request
  body, so either provider can serve either model.

In CI the fixtures are applied by the `aigateway` matrix job in
`.github/workflows/__e2e_chainsaw_tests.yaml`.

## Provider credentials

Provider api keys are supplied via a Kubernetes `Secret` referenced with `secretRef` (labelled
`konghq.com/secret: "true"`), never inline in the `AIGatewayModelProvider`. New tests must follow
this pattern — create a `Secret` for the credential and reference it from the provider's
`config.auth.headers[].value.secretRef`.

## Running locally

Requires a cluster with the operator deployed and the `konnect` + `aigatewaydataplane`
controllers enabled (see the Helm `env.enable_controller_*` values), plus a Konnect token.

```bash
# 1. Apply the shared mock stack (runs test/e2e/chainsaw/fixtures/aigateway/prereq.sh).
make test.e2e.chainsaw.prereq DIRNAME=aigateway

# 2. Run the suite.
KONNECT_TOKEN=<pat> KONNECT_SERVER_URL=<region>.api.konghq.tech \
  CHAINSAW_TEST_DIR=test/e2e/chainsaw/aigateway \
  make test.e2e.chainsaw

# 3. Tear down the mock stack when done.
kubectl delete -f test/e2e/chainsaw/fixtures/aigateway --ignore-not-found
```

## Notes

- The `AIGatewayDataPlane` image is overridden (binding `aigw_dp_image`, env `AIGW_DP_IMAGE`)
  to a `kong-ai-gateway-dev` build. The operator default (`kong-ai-gateway:2.0.0`) cannot load
  the config the `.tech` org emits today.
- On clusters that pull images slowly (e.g. colima), pre-load the mock and data plane images
  with `kind load docker-image ... --name <cluster>`.
