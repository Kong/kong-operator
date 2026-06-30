# Chainsaw E2E Tests

This directory contains end-to-end tests for the Kong Operator using [Chainsaw](https://kyverno.github.io/chainsaw/) (Kyverno's declarative Kubernetes testing framework).

## Running Tests

```bash
make test.e2e.chainsaw                                                        # Run all hybridgateway tests (default)
make test.e2e.chainsaw CHAINSAW_TEST_DIR=./test/e2e/chainsaw/konnect          # Run specific test suite
```

## Global Configuration

The [.chainsaw.yaml](.chainsaw.yaml) file centralizes default timeouts and settings for all tests. Individual tests inherit these defaults unless they override them.

```yaml
spec:
  timeouts:
    apply: 120s     # Time to apply resources
    assert: 300s    # Time for assertions to succeed (retries until timeout)
    cleanup: 120s   # Time for cleanup operations
    delete: 120s    # Time for deletion operations
    error: 120s     # Time before error timeout
    exec: 300s      # Time for script execution
  parallel: 4       # Run 4 tests in parallel
  failFast: false   # Continue running tests even if one fails
```

When adjusting timeouts, prefer updating the global configuration over setting per-test or per-step overrides. This keeps behavior consistent and easy to reason about.

## Best Practices

### 1. Use Step Templates for Reusable Operations

Step templates in `common/_step_templates/` encapsulate common operations that are shared across tests. Always prefer using an existing template over duplicating YAML.

**Using a template:**
```yaml
- name: Create-echo-service
  use:
    template: ../../common/_step_templates/apply-assert-echoService.yaml
```

**Overriding bindings when needed:**

```yaml
- name: Assert-kong-route
  use:
    template: ../../common/_step_templates/assert-kongRoute.yaml
  bindings:
    - name: route_paths
      value:
        - "~/echo$"
        - "/echo/"
    - name: strip_path
      value: true
    - name: resource_type
      value: "kongservices.configuration.konghq.com"
```

Use inline `try:` blocks with `apply: file:` only for test-specific resources that are not shared across tests.

### 2. Script Best Practices

Scripts in `common/scripts/` are designed to be **standalone**, **debuggable**, and **modular**. They can be run independently from Chainsaw for manual debugging and testing.

#### Standalone

Every script is self-contained and runnable directly from the command line. All inputs come from environment variables, so you can reproduce any script execution by setting the same variables:

```bash
# Run a script standalone for debugging:
NAMESPACE=default \
RESOURCE_TYPE=kongservices.configuration.konghq.com \
GATEWAY_NAME=my-gateway \
GATEWAY_NAMESPACE=default \
HTTP_ROUTE_NAME=my-route \
HTTP_ROUTE_NAMESPACE=default \
bash common/scripts/kongResource_name.sh
```

All environment variables are documented in a header comment at the top of each script:
```bash
# Variables (from environment):
#   NAMESPACE: The namespace to search for the resource.
#   RESOURCE_TYPE: The full resource type, e.g. kongservices.configuration.konghq.com.
#   RETRY_COUNT: (Optional) Number of retries. Default: 180.
#   RETRY_DELAY: (Optional) Delay between retries in seconds. Default: 1.
```

#### Debuggable

Scripts output structured JSON that includes the **exact commands executed** and their **outputs on failure**, so you can reproduce issues by copy-pasting the command from the JSON output.

On success, the JSON includes the command that was run:
```json
{
  "proxy_ip_address": "10.0.0.1",
  "kubectl_command": "kubectl get gateway my-gw -n default -o json"
}
```

On failure, the JSON includes the command, its output, and diagnostic context:
```json
{
  "error": "Request failed with status 503 after 180 attempts",
  "curl_command": "curl -s -o /dev/null -w '%{http_code}' -X GET 'http://10.0.0.1:80/echo'",
  "curl_output": "Connection refused",
  "retry_attempt": 180,
  "max_retries": 180
}
```

For Kubernetes resource lookups, failures include the list of available resources to help diagnose mismatches:
```json
{
  "error": "No matching resource found after 180 attempts",
  "resource_type": "kongservices.configuration.konghq.com",
  "kubectl_command": "kubectl get kongservices.configuration.konghq.com -n default -o json",
  "available_resources": [{"name": "svc-abc123", "annotations": {...}}]
}
```

#### Modular

Scripts compose with each other. For example, `get_kong_resources.sh` calls `kongResource_name.sh` internally for each resource type. Scripts locate each other via `SCRIPT_DIR`:

```bash
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
KONG_RESOURCE_NAME_SCRIPT="${SCRIPT_DIR}/kongResource_name.sh"
```

Each script does one thing well:
- `kongResource_name.sh` - finds a single resource name by annotation
- `get_kong_resources.sh` - finds multiple resource names (composes `kongResource_name.sh`)
- `wait_for_kong_resources_deletion.sh` - waits for resources to be deleted (consumes output from `get_kong_resources.sh`)
- `proxy_ip_address.sh` - gets the gateway proxy IP (used by connectivity test scripts)

#### Other Conventions

**Safety flags** - Every script starts with:
```bash
set -o errexit   # Abort on nonzero exit status
set -o nounset   # Abort on unbound variable
set -o pipefail  # Abort on pipe failure
```

**JSON output** - All scripts output structured JSON to stdout. This enables Chainsaw to parse results using `json_parse($stdout)` and propagate values between steps via `outputs`.

**Standardized retry logic** - Scripts use a consistent retry pattern: 180 retries with 1-second delays (3 minutes total). This accounts for eventual consistency in the control plane and transient failures in connectivity checks.

### 3. Wait-for-Cleanup Pattern (HTTPRoute Updates)

When updating an HTTPRoute, the Kong Operator deletes old Kong configuration resources (KongRoute, KongService, KongUpstream, KongTarget) and creates new ones. During this transition, old and new resources coexist temporarily. This causes a race condition: scripts like `kongResource_name.sh` match resources by annotation and may return the old resource name while the assert checks the new resource.

**Solution:** Capture old resource names before the update, apply the update, then wait for old resources to be deleted before asserting new state.

```yaml
- name: Update-httproute-and-verify-cleanup
  try:
    # 1. Capture current Kong resource names.
    - script:
        env:
          - name: NAMESPACE
            value: ($namespace)
          - name: GATEWAY_NAME
            value: ($gateway_name)
          - name: GATEWAY_NAMESPACE
            value: ($gateway_namespace)
          - name: HTTP_ROUTE_NAME
            value: ($http_route_name)
          - name: HTTP_ROUTE_NAMESPACE
            value: ($http_route_namespace)
          - name: RESOURCE_TYPES
            value: "kongservices.configuration.konghq.com,kongroutes.configuration.konghq.com,kongupstreams.configuration.konghq.com,kongtargets.configuration.konghq.com"
        content: |
          bash ../../common/scripts/get_kong_resources.sh
        outputs:
          - name: old_resources
            value: (json_parse($stdout))

    # 2. Apply the HTTPRoute update.
    - apply:
        file: updated-httproute.yaml

    # 3. Wait for old resources to be deleted.
    - script:
        env:
          - name: RESOURCES_JSON
            value: (to_string($old_resources))
        content: |
          bash ../../common/scripts/wait_for_kong_resources_deletion.sh

    # 4. Assert new state (now safe from race conditions).
    - assert:
        file: assert-updated-httproute.yaml
```

Only include resource types in `RESOURCE_TYPES` that will actually be recreated (i.e., deleted and replaced) by the update. If you include a resource type that won't be deleted, the wait will timeout.

#### When NOT to Use Wait-for-Cleanup

The pattern relies on ALL captured resources being deleted. It does **not** work when:

- **Partial backend removal** - e.g., removing one of two backends. The remaining backend's KongService/KongUpstream/KongTarget keep the same names and won't be deleted, causing a timeout.
- **Filter-only changes** - Filters (like ExtensionRef) don't affect Kong entity hashes. The entities are updated in place, not recreated. No cleanup needed.

### 4. Hybrid Gateway Test Structure

Hybrid gateway tests (`hybridgateway/`) typically follow this flow:

1. **Setup** - Create prerequisites (TLS secrets, KonnectAPIAuthConfiguration, GatewayConfiguration, GatewayClass, Gateway)
2. **Backend services** - Deploy echo/httpbin services
3. **Route configuration** - Create HTTPRoute(s)
4. **Kong resource verification** - Assert KongRoute, KongService, KongUpstream, KongTarget are created correctly
5. **Traffic verification** - Test actual HTTP/HTTPS routing from host and cluster
6. **Update testing** - Modify HTTPRoute, wait for cleanup, assert new state, verify traffic

### 6. Bindings

Define test-wide bindings at the top of the test spec. Use Chainsaw expressions for dynamic values:

```yaml
bindings:
  - name: test_name
    value: ($namespace)                                              # Auto-generated namespace
  - name: gateway_name
    value: (join('-', [($test_name), 'gateway']))                    # Derived names
  - name: konnect_token
    value: (env('KONNECT_TOKEN'))                                    # Environment variables
  - name: fqdn
    value: (join('.', ['echo', ($test_name), 'example.com']))        # Computed values
```

Step-level bindings override test-level bindings when templates need different values.

### 7. Kong Resource Assertions via Scripts

Kong resources (KongRoute, KongService, KongUpstream, KongTarget) are matched by annotations rather than by name, since names include computed hashes. The `kongResource_name.sh` script finds resources matching specific gateway and HTTPRoute annotations:

- `gateway-operator.konghq.com/hybrid-gateways` - Comma-separated list of `namespace/name` gateway references
- `gateway-operator.konghq.com/hybrid-routes` - Comma-separated list of `namespace/name` HTTPRoute references

The assert templates (e.g., `assert-kongRoute.yaml`) first run the script to discover the resource name, then use that name in subsequent assertions. The script retries for up to 180 seconds, but it runs only once and its result is used by the following `assert:` block which retries independently. This is why the wait-for-cleanup pattern exists - without it, the script could discover the old resource name during the brief coexistence window.

### 8. KongTarget Count Verification

KongTargets are verified differently from other Kong resources because an HTTPRoute with multiple backends produces multiple KongTargets referencing the same KongUpstream. Use the `verify-kongTarget-count.yaml` template:

```yaml
- name: Verify-kong-target-count
  use:
    template: ../../common/_step_templates/verify-kongTarget-count.yaml
  bindings:
    - name: expected_kong_target_count
      value: 2
    - name: target_weight
      value: 50
```

The underlying `kongTarget_count.sh` script retries until the expected count is reached or the retry limit is hit.

### 9. Non-Blocking Resource Deletion

Use the `delete-resource-non-blocking.yaml` template when you need to delete a resource and continue with the test without waiting for finalizers:

```yaml
- name: Delete-httproute
  use:
    template: ../../common/_step_templates/delete-resource-non-blocking.yaml
  bindings:
    - name: resource_type
      value: httproute
    - name: resource_name
      value: ($http_route_name)
    - name: resource_namespace
      value: ($namespace)
```

### 10. Cross-Namespace Testing

Tests involving cross-namespace references (e.g., `httproute-status-resolved-refs`) require:

1. A separate namespace for the cross-namespace service
2. A `ReferenceGrant` in the target namespace to permit the reference
3. Testing both granted and denied scenarios to verify proper access control

## Chainsaw Gotchas

### Unbound Variables Have No Default Value Syntax

Chainsaw expressions like `($myvar)` fail if `myvar` is not bound in scope. Unlike JMESPath's `||` operator which works for null values, there is no way to provide a default for a completely unbound variable. Notations like `($myvar || 'default')` will still error if `myvar` does not exist at all.

```yaml
# WRONG: Chainsaw errors because `myvar` is unbound — the || never evaluates
value: ($myvar || 'default')

# CORRECT: Always ensure variables are bound, either at test level or step level
bindings:
  - name: myvar
    value: "default"
```

**Workaround:** Always explicitly bind every variable used in expressions. If a variable is optional, give it a default value in the test-level or step-level bindings. Alternatively, omit the variable from the Chainsaw expression entirely and let the script handle the default via shell syntax (`${VAR:-default}`).

### Script Runs Once, Assert Retries

In a step with a `script:` followed by an `assert:`, the script executes **once** and its output is captured. The `assert:` block retries independently until the assert timeout. If the script captures a stale or wrong value, the assert will keep retrying against that wrong value until it times out.

This is the root cause of the race condition in HTTPRoute update tests: `kongResource_name.sh` runs once and might capture the old resource name, then the assert retries forever trying to match a KongRoute that references that old (soon-to-be-deleted) KongService.

### Type Coercion in Assertions

Chainsaw compares values as strings in assertions. Numeric values from JSON (like `http_status: 200`) need `to_string()` to match string expectations:

```yaml
# WRONG: comparing number 200 to string "200" will fail
(json_parse($stdout).http_status == '200'): true

# CORRECT: convert to string first
(to_string(json_parse($stdout).http_status) == '200'): true
```

Similarly, Kubernetes labels are always strings. If a binding holds a number (e.g., `listener_https_port: 443`), use `to_string()` when matching against labels:

```yaml
(labels."gateway-operator.konghq.com/listener-port"): (to_string($listener_https_port))
```

### JSON Serialization Between Steps

Script outputs parsed with `json_parse($stdout)` become structured objects. To pass them as a JSON string to another script's env var, use `to_string()`:

```yaml
# Step 1: Capture as structured object
outputs:
  - name: old_resources
    value: (json_parse($stdout))

# Step 2: Serialize back to JSON string for the next script
env:
  - name: RESOURCES_JSON
    value: (to_string($old_resources))
```

Without `to_string()`, Chainsaw would pass the object representation instead of a valid JSON string.

### Step Output Scope

Outputs from a `script:` block are scoped to the step (`try:` block) they are defined in. They are available to subsequent operations within the same step but **not** to other steps. If you need a value across steps, structure your operations within the same `try:` block.

### Relative Paths in Templates

Script paths in templates use paths relative to the **test directory**, not the template directory. This means templates in `common/_step_templates/` reference scripts as `../../common/scripts/foo.sh`, which resolves correctly when the template is used from a test in `hybridgateway/<test-name>/`.
