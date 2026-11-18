# AGENTS.md

This file gives guidance to AI agents such as Claude Code or Copilot. It applies to all work in this repository.

## Project Overview

Kong Operator is a Kubernetes Operator (Kubebuilder v4, multigroup layout) that manages Kong Gateway deployments, in process ingress controller instances and integrates with Konnect cloud.
It provides a Kubernetes-native way to deploy and manage Kong Gateway and its configuration.
Domain: `gateway-operator.konghq.com`.

## Build Commands

```bash
make build              # Full build: generate code + compile binary to bin/manager
make build.operator     # Build optimized binary only (no code generation)
make generate           # Full code generation (CRDs, deepcopy, mocks, docs, manifests, chart golden files)
make manifests          # Generate CRDs and RBAC manifests only
make tools              # Install all required development tools via mise
make verify.crd-breaking-changes  # Check CRDs for breaking schema changes
```

## Linting

```bash
make lint               # Run Go linters (modules, golangci-lint)
make lint.all           # Full lint: Go + charts + GitHub Actions + markdown
make lint.api           # Lint Kubernetes API types
make lint.golangci-lint # Run golangci-lint linter for Go code
make lint.actions       # Lint github action files 
make go-fix             # Run go-fix on the codebase to ensure you're not using old or deprecated Go constructs.
```

CI runs `make lint` with `GOLANGCI_LINT_FLAGS="--fix=false"` (auto-fix is enabled locally but disabled in CI).

## Testing

### Unit Tests

```bash
make test.unit          # Run unit tests with verbose output
make test.unit.pretty   # Run unit tests with compact output
make test.unit.pretty GOTESTFLAGS="-run TestSpecificGatewayNN -count 1" # Run specific test
```

### Envtest (Controller Tests with Simulated Kubernetes)

```bash
make test.envtest                                                    # Run envtest tests with standard verbose output
make test.envtest.pretty                                             # Run envtest tests with compact output
make test.envtest CLUSTER_VERSION=1.30                               # Run envtest tests with specific Kubernetes (apiserver) version
make test.envtest GOTESTFLAGS="-run TestSpecificGatewayNN -count 1"  # Run specific test from envtest suite
```

### Integration Tests (Require Running Cluster)

```bash
make test.integration-ko                 # Kong Operator integration tests
make test.integration-bluegreen          # DataPlane upgrade/blue-green tests
make test.integration-validatingwebhook  # Webhook tests
```

#### kind: "too many open files" / fsnotify watcher errors

Tests that spin up a kind cluster (integration, conformance, e2e) can fail with
`failed to create fsnotify watcher: too many open files`. kind nodes share the host
kernel, so the `fs.inotify.max_user_instances` / `max_user_watches` limits must be raised
on the **host running Docker** (see https://kind.sigs.k8s.io/docs/user/known-issues/#pod-errors-due-to-too-many-open-files).

- **Linux host**: `make tune.inotify` (one-off). Persist the setting in `/etc/sysctl.d/99-kind-inotify.conf`.
- **macOS** (Docker Desktop / colima): the limit lives in the Linux VM, not macOS.
  - colima: `colima ssh -- sudo sysctl -w fs.inotify.max_user_instances=8192 fs.inotify.max_user_watches=524288`
  - Docker Desktop: `docker run --rm --privileged alpine sysctl -w fs.inotify.max_user_instances=8192 fs.inotify.max_user_watches=524288`
  - Note: not persistent across VM restarts.

CI raises these limits automatically via the `.github/actions/bump-inotify` composite action.

### CRD Validation Tests

```bash
make test.crds-validation        # Run the CRD validation tests against a real API server (envtest)
make test.crds-validation.pretty # Same tests with compact output
```

### Other Tests

```bash
make test.conformance           # Gateway API conformance tests
make test.kongintegration       # Kong integration tests (uses testcontainers)
make test.e2e                   # End-to-end tests
make test.e2e.chainsaw          # Chainsaw-based E2E tests
make test.charts.golden         # Helm chart golden file tests
make test.charts.golden.update  # Update chart golden files
```

### Helm Chart CI Test Fixtures (`charts/kong-operator/ci/*.yaml`)

Two test targets use each `charts/kong-operator/ci/*-values.yaml` file. `make test.charts.golden` renders the chart and compares the output with a snapshot. The `chart-testing (install)` CI job runs `helm install` on a real kind cluster and waits until the Deployment is ready.

- A fixture with `nodeSelector`, `affinity`, or `tolerations` must use a constraint that the kind test node satisfies,
  for example `kubernetes.io/os: linux`. A trivially true rule also works, such as the `NotIn dummy` podAffinity trick in
  `affinity-values.yaml`. A hard constraint that the node does not match makes the pod unschedulable.
  `test.charts.golden` still passes because it does not use a cluster, but the `chart-testing (install)` CI job times out
  after 3 minutes with `Available: 0/1`.
- If you add or change a `ci/*.yaml` fixture, run `make test.charts.golden.update` to regenerate the snapshot. Then run `make test.charts.golden` and `make lint.charts`.

### Test File Locations

- Unit tests: `*_test.go` files next to source code
- Envtest: `test/envtest/`
- Integration: `test/integration/`
- Conformance: `test/conformance/`
- CRD validation: `test/crdsvalidation/`
- Runtime integration (kind cluster): `test/runtimeintegration/`
- E2E: `test/e2e/`
- Mocks: `test/mocks/` (generated by mockery)

## Running Locally

```bash
make run                # Run operator locally via telepresence (installs CRDs, sets up RBAC)
make run.skaffold       # Run with skaffold for iterative development
make debug              # Run with delve debugger
```

Requires a Kubernetes cluster configured in KUBECONFIG and telepresence installed.

## Architecture

### Entry Point

`cmd/main.go` → `modules/cli.Parse()` → `modules/manager.Run()` → `SetupControllers()`

### API Types (`api/`)

Kubernetes API definitions. The generators use them to produce CRDs, deepcopy methods, clientsets, informers, listers, and documentation.

- `api/gateway-operator/v1beta1/` - **DataPlane**
- `api/gateway-operator/v2beta1/` - **ControlPlane** and **GatewayConfiguration** (storage versions, served through conversion webhooks)
- `api/gateway-operator/v1alpha1/` - **AIGateway** (experimental), **DataPlaneMetricsExtension**
- `api/aigateway/v1alpha1/` - **OnPremAIGateway**
- `api/aiconfiguration/v1alpha1/` - AI Gateway entities for Konnect and on-prem layouts
- `api/eventgateway/v1alpha1/` - Event Gateway entities, including **KegDataPlane**
- `api/mcp/v1alpha1/` - **MCPServerDataPlane**
- `api/incubator/v1alpha1/` - **KongServiceFacade**
- `api/konnect/v1alpha1/`, `api/konnect/v1alpha2/` - Konnect integration, including **MCPServer**
- `api/configuration/` - Kong configuration types
- `api/common/v1alpha1/` - Shared types (ControlPlaneRef, ObjectRef, KongTags)

### Controllers (`controller/`)

Each controller implements `SetupWithManager(ctx, mgr)` and `Reconcile(ctx, req)`:

- `dataplane/` - Manages Kong Gateway Deployments, Services, ConfigMaps
- `controlplane/` - Manages Kong Ingress Controller deployments
- `gateway/` - Implements Gateway API, creates DataPlane/ControlPlane
- `konnect/` - Konnect cloud synchronization
- `eventgateway/` - Event Gateway data planes, for Konnect and on-prem installs
- `aigateway/` - AI Gateway data planes, on-prem configuration, and Konnect entity reconciliation
- `gwadmin/` - gw-admin Deployment, configuration, and license handling
- `mcpserver/` - MCP Server data planes and their Kong entities
- `hybridgateway/` - Hybrid mode support
- `specialized/` - AIGateway and other specialized controllers

### Core Modules (`modules/`)

- `manager/` - Controller-runtime setup, controller registration (`controller_setup.go`)
- `cli/` - Command-line flag parsing
- `admission/` - Validating/conversion webhooks

### Documentation (`docs/`)

Most files are generated from the API definitions and CLI arguments. Notes for maintainers live in `docs/internal/`.

### Configuration manifests (`config/`)

The `config/` directory holds the files that deploy the operator: manifests, kustomize bases, and related resources.

- `/config/samples`: Sample YAML manifests for custom resources. CI applies these files to a real Kubernetes cluster.
  Only the files directly in that directory are applied. Subdirectories such as `/config/samples/feature-gated` hold samples that need more setup than a stock install. The extra setup can be an extra CRD channel or a feature gate that is off by default. `make test.samples` skips these files on purpose.

### Embedded Ingress Controller (`ingress-controller/`)

Contains the embedded Kong Kubernetes Ingress Controller code.

### Helm chart (`charts/kong-operator/`)

Helm chart for deploying the operator.

### GitHub CI definitions (`.github/`)

GitHub Actions workflows and composite actions. `.github/instructions/` holds extra instructions for AI agents.

## Code Conventions

### Go Code Conventions

- Follow the standard Go conventions (gofmt, goimports).
- Use `new()` for literal initialization, for example `new("dummy-string")` instead of `ptrTo("dummy-string")`, and `new(true)` instead of `ptrTo(true)`.

### Naming (Enforced by Linter)

- Use `DataPlane` not `Dataplane`.
- Use `ControlPlane` not `Controlplane`.
- For ControlPlane types, use the `github.com/kong/kong-operator/v2/internal/types` package (aliased as `gwtypes`).

### Required Import Aliases

See `.golangci.yaml` for the full list of required import aliases.

### Blocked Packages

See `.golangci.yaml` for the full list of blocked packages and their alternatives.

## Tool Management

The project uses mise for tool versions. The versions are defined in `.tools_versions.yaml` and `.mise.toml`. Each Makefile target downloads the tools that it needs.

```bash
make tools    # Install: controller-gen, kustomize, client-gen, golangci-lint, gotestsum, skaffold, crd-ref-docs, crdify, grpcurl
```

## Adding New CRDs

1. Add the types in `api/<group>/<version>/` and register the kinds in `addKnownTypes` in `groupversion_info.go`.
2. Add the `+kubebuilder:` markers that CRD generation needs.
3. Run `make generate` to regenerate CRDs, deepcopy, docs, and manifests.
4. If chart templates change, run `make test.charts.golden.update`.
5. Add CRD schema tests in `test/crdsvalidation/`.
6. Add a sample YAML file in `config/samples/` and apply it in `test/crdsvalidation/`.

If you change CRDs in `config/crd/kong-operator/`, run `make verify.crd-breaking-changes`. It compares the schemas against the base branch and reports incompatible changes before CI does.

## Development Workflow

### Before Submitting Changes

1. Run `make lint` and make sure that it passes. CI runs it with `--fix=false`.
2. Run `make test.unit` for quick feedback.
3. Run `make test.envtest` to test the controller logic.
4. If you changed API types, CRDs, or another generated input, regenerate the output and make sure that it is current:

   ```bash
   make generate
   make verify.generators
   ```

5. If you changed CRDs, run `make verify.crd-breaking-changes`.

   ```bash
   make verify.crd-breaking-changes
   ```

6. **Update CHANGELOG.md** - For significant changes, add release notes.
   Bugfix entries go under a `### Fixes` heading (not `### Fixed`); this is enforced by `make lint.markdownlint`.

### Verify Before Commit

```bash
make lint                        # Linting passes
make test.unit                   # Unit tests pass
make verify.generators           # Generated code is up to date
make verify.crd-breaking-changes # CRDs remain backward compatible
```

### If Modifying API Types

When changing files in `api/`:

```bash
make generate                   # Regenerate CRDs, deepcopy, docs, manifests
make test.charts.golden.update  # Update Helm chart golden files if CRDs changed
make verify.manifests           # Report manifest drift
```

## Changelog

Add an entry to `CHANGELOG.md` for every change that a user of the operator can see. Put it under the section for the next release, in one of these subsections: Breaking changes, Added, Changed, or Fixes.

- Do not add entries for tests, linters, generator output, refactors, or CI work.
- Keep the entry brief: one or two sentences that say what changed. Most entries in `CHANGELOG.md` show the style.
- End the entry with a link to the pull request or issue, for example `[#1234](https://github.com/Kong/kong-operator/pull/1234)`.
- If an existing deployment must change to keep working, use the Breaking changes subsection and state the migration step.

## Code exploration

### cx

If `cx` is installed, use it for semantic code navigation. It is built for AI agents. If you need the exact text, read the file in full.

## CI Workflow

The main test workflow is `.github/workflows/tests.yaml`.

- It runs on pull requests to all branches, and on pushes to main and release branches.
- The jobs are lint, verify, unit-tests, envtest-tests, CRD schema tests, conformance-tests, and integration tests.
- It uses mise for tool management.
- The tested Kubernetes versions are in `.github/supported_k8s_node_versions.yaml`.
