# ------------------------------------------------------------------------------
# Configuration - Repository
# ------------------------------------------------------------------------------

REPO ?= github.com/kong/kong-operator
REPO_URL ?= https://$(REPO)
REPO_NAME ?= $(shell echo $(REPO) | cut -d / -f 3)
REPO_INFO ?= $(shell git config --get remote.origin.url)
TAG ?= $(shell git describe --tags)
VERSION ?= $(shell cat VERSION)

ifndef COMMIT
  COMMIT := $(shell git rev-parse --short HEAD)
endif

# ------------------------------------------------------------------------------
# Configuration - Build
# ------------------------------------------------------------------------------

SHELL = bash
.SHELLFLAGS = -ec -o pipefail

IMG ?= docker.io/kong/kong-operator
KUSTOMIZE_IMG_NAME = docker.io/kong/kong-operator

ifeq (Darwin,$(shell uname -s))
LDFLAGS_COMMON ?= -extldflags=-Wl,-ld_classic
endif

LDFLAGS_METADATA ?= \
	-X $(REPO)/modules/manager/metadata.projectName=$(REPO_NAME) \
	-X $(REPO)/modules/manager/metadata.release=$(TAG) \
	-X $(REPO)/modules/manager/metadata.commit=$(COMMIT) \
	-X $(REPO)/modules/manager/metadata.repo=$(REPO_INFO) \
	-X $(REPO)/modules/manager/metadata.repoURL=$(REPO_URL)

# ------------------------------------------------------------------------------
# Configuration - Tooling
# ------------------------------------------------------------------------------

ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))

TOOLS_VERSIONS_FILE = $(PROJECT_DIR)/.tools_versions.yaml

.PHONY: tools
tools: controller-gen kustomize client-gen golangci-lint gotestsum skaffold yq crd-ref-docs

MISE := $(shell which mise)
.PHONY: mise
mise:
	@mise -V >/dev/null 2>/dev/null || (echo "mise - https://github.com/jdx/mise - not found. Please install it." && exit 1)

.PHONY: mise-plugin-install
mise-plugin-install: mise
	@$(MISE) plugin install --yes -q $(DEP) $(URL)

.PHONY: mise-install
mise-install: mise
	@$(MISE) install -q $(DEP_VER)

KIC_WEBHOOKCONFIG_GENERATOR = $(PROJECT_DIR)/bin/kic-webhook-config-generator
.PHONY: kic-webhook-config-generator
kic-webhook-config-generator:
	( cd ./hack/generators/kic/webhook-config-generator && go build -o $(KIC_WEBHOOKCONFIG_GENERATOR) . )

export MISE_DATA_DIR = $(PROJECT_DIR)/bin/

# Do not store yq's version in .tools_versions.yaml as it is used to get tool versions.
# renovate: datasource=github-releases depName=mikefarah/yq
YQ_VERSION = 4.47.1
YQ = $(PROJECT_DIR)/bin/installs/yq/$(YQ_VERSION)/bin/yq
.PHONY: yq
yq: mise # Download yq locally if necessary.
	@$(MISE) plugin install --yes -q yq
	@$(MISE) install -q yq@$(YQ_VERSION)

CONTROLLER_GEN_VERSION = $(shell $(YQ) -r '.controller-tools' < $(TOOLS_VERSIONS_FILE))
CONTROLLER_GEN = $(PROJECT_DIR)/bin/installs/kube-controller-tools/$(CONTROLLER_GEN_VERSION)/bin/controller-gen
.PHONY: controller-gen
controller-gen: mise yq ## Download controller-gen locally if necessary.
	@$(MISE) plugin install --yes -q kube-controller-tools
	@$(MISE) install -q kube-controller-tools@$(CONTROLLER_GEN_VERSION)

KUSTOMIZE_VERSION = $(shell $(YQ) -r '.kustomize' < $(TOOLS_VERSIONS_FILE))
KUSTOMIZE = $(PROJECT_DIR)/bin/installs/kustomize/$(KUSTOMIZE_VERSION)/bin/kustomize
.PHONY: kustomize
kustomize: mise yq ## Download kustomize locally if necessary.
	@$(MISE) plugin install --yes -q kustomize
	@$(MISE) install -q kustomize@$(KUSTOMIZE_VERSION)

CLIENT_GEN_VERSION = $(shell $(YQ) -r '.code-generator' < $(TOOLS_VERSIONS_FILE))
CLIENT_GEN = $(PROJECT_DIR)/bin/installs/kube-code-generator/$(CLIENT_GEN_VERSION)/bin/client-gen
.PHONY: client-gen
client-gen: mise yq ## Download client-gen locally if necessary.
	@$(MISE) plugin install --yes -q kube-code-generator
	@$(MISE) install -q kube-code-generator@$(CLIENT_GEN_VERSION)

GOLANGCI_LINT_VERSION = $(shell $(YQ) -r '.golangci-lint' < $(TOOLS_VERSIONS_FILE))
GOLANGCI_LINT = $(PROJECT_DIR)/bin/installs/golangci-lint/$(GOLANGCI_LINT_VERSION)/bin/golangci-lint
.PHONY: golangci-lint
golangci-lint: mise yq ## Download golangci-lint locally if necessary.
	@$(MISE) plugin install --yes -q golangci-lint
	@$(MISE) install -q golangci-lint@$(GOLANGCI_LINT_VERSION)

MODERNIZE_VERSION = $(shell $(YQ) -r '.modernize' < $(TOOLS_VERSIONS_FILE))
MODERNIZE = $(PROJECT_DIR)/bin/modernize
.PHONY: modernize
modernize: yq
	GOBIN=$(PROJECT_DIR)/bin go install -v \
		golang.org/x/tools/gopls/internal/analysis/modernize/cmd/modernize@$(MODERNIZE_VERSION)

GOTESTSUM_VERSION = $(shell $(YQ) -r '.gotestsum' < $(TOOLS_VERSIONS_FILE))
GOTESTSUM = $(PROJECT_DIR)/bin/installs/gotestsum/$(GOTESTSUM_VERSION)/bin/gotestsum
.PHONY: gotestsum
gotestsum: mise yq ## Download gotestsum locally if necessary.
	@$(MISE) plugin install --yes -q gotestsum https://github.com/pmalek/mise-gotestsum.git
	@$(MISE) install -q gotestsum@$(GOTESTSUM_VERSION)

CRD_REF_DOCS_VERSION = $(shell $(YQ) -r '.crd-ref-docs' < $(TOOLS_VERSIONS_FILE))
CRD_REF_DOCS = $(PROJECT_DIR)/bin/crd-ref-docs
.PHONY: crd-ref-docs
crd-ref-docs: ## Download crd-ref-docs locally if necessary.
	GOBIN=$(PROJECT_DIR)/bin go install -v \
		github.com/elastic/crd-ref-docs@v$(CRD_REF_DOCS_VERSION)

SKAFFOLD_VERSION = $(shell $(YQ) -r '.skaffold' < $(TOOLS_VERSIONS_FILE))
SKAFFOLD = $(PROJECT_DIR)/bin/installs/skaffold/$(SKAFFOLD_VERSION)/bin/skaffold
.PHONY: skaffold
skaffold: mise yq ## Download skaffold locally if necessary.
	@$(MISE) plugin install --yes -q skaffold
	@$(MISE) install -q skaffold@$(SKAFFOLD_VERSION)

MOCKERY_VERSION = $(shell $(YQ) -r '.mockery' < $(TOOLS_VERSIONS_FILE))
MOCKERY = $(PROJECT_DIR)/bin/installs/mockery/$(MOCKERY_VERSION)/bin/mockery
.PHONY: mockery
mockery: mise yq ## Download mockery locally if necessary.
	@$(MISE) plugin install --yes -q mockery https://github.com/cabify/asdf-mockery.git
	@$(MISE) install -q mockery@$(MOCKERY_VERSION)

SETUP_ENVTEST_VERSION = $(shell $(YQ) -r '.setup-envtest' < $(TOOLS_VERSIONS_FILE))
SETUP_ENVTEST = $(PROJECT_DIR)/bin/installs/setup-envtest/$(SETUP_ENVTEST_VERSION)/bin/setup-envtest
.PHONY: setup-envtest
setup-envtest: mise ## Download setup-envtest locally if necessary.
	@$(MAKE) mise-plugin-install DEP=setup-envtest URL=https://github.com/pmalek/mise-setup-envtest.git
	@$(MAKE) mise-install DEP_VER=setup-envtest@$(SETUP_ENVTEST_VERSION)

ACTIONLINT_VERSION = $(shell $(YQ) -r '.actionlint' < $(TOOLS_VERSIONS_FILE))
ACTIONLINT = $(PROJECT_DIR)/bin/installs/actionlint/$(ACTIONLINT_VERSION)/bin/actionlint
.PHONY: download.actionlint
download.actionlint: mise yq ## Download actionlint locally if necessary.
	@$(MISE) plugin install --yes -q actionlint
	@$(MISE) install -q actionlint@$(ACTIONLINT_VERSION)

SHELLCHECK_VERSION = $(shell $(YQ) -r '.shellcheck' < $(TOOLS_VERSIONS_FILE))
SHELLCHECK = $(PROJECT_DIR)/bin/installs/shellcheck/$(SHELLCHECK_VERSION)/bin/shellcheck
.PHONY: download.shellcheck
download.shellcheck: mise yq ## Download shellcheck locally if necessary.
	@$(MISE) plugin install --yes -q shellcheck
	@$(MISE) install -q shellcheck@$(SHELLCHECK_VERSION)

GOVULNCHECK_VERSION = $(shell $(YQ) -r '.govulncheck' < $(TOOLS_VERSIONS_FILE))
GOVULNCHECK = $(PROJECT_DIR)/bin/installs/govulncheck/$(GOVULNCHECK_VERSION)/bin/govulncheck
.PHONY: download.govulncheck
download.govulncheck: mise yq ## Download govulncheck locally if necessary.
	@$(MISE) plugin install --yes -q govulncheck https://github.com/wizzardich/asdf-govulncheck.git
	@$(MISE) install -q govulncheck@$(GOVULNCHECK_VERSION)

CHARTSNAP_VERSION = $(shell yq -ojson -r '.chartsnap' < $(TOOLS_VERSIONS_FILE))
.PHONY: download.chartsnap
download.chartsnap:
	CHARTSNAP_VERSION=${CHARTSNAP_VERSION} ./scripts/install-chartsnap.sh

KUBE_LINTER_VERSION = $(shell yq -ojson -r '.kube-linter' < $(TOOLS_VERSIONS_FILE))
KUBE_LINTER = $(PROJECT_DIR)/bin/installs/kube-linter/v$(KUBE_LINTER_VERSION)/bin/kube-linter
.PHONY: kube-linter
download.kube-linter:
	@$(MAKE) mise-plugin-install DEP=kube-linter
	@$(MAKE) mise-install DEP_VER=kube-linter@v$(KUBE_LINTER_VERSION)

TELEPRESENCE_VERSION = $(shell $(YQ) -r '.telepresence' < $(TOOLS_VERSIONS_FILE))
TELEPRESENCE= $(PROJECT_DIR)/bin/installs/telepresence/$(TELEPRESENCE_VERSION)/bin/telepresence
.PHONY: download.telepresence
download.telepresence: mise yq ## Download telepresence locally if necessary.
	@$(MISE) plugin install --yes -q telepresence
	@$(MISE) install -q telepresence@$(TELEPRESENCE_VERSION)

MARKDOWNLINT_VERSION = $(shell $(YQ) -r '.markdownlint-cli2' < $(TOOLS_VERSIONS_FILE))
MARKDOWNLINT = $(PROJECT_DIR)/bin/installs/markdownlint-cli2/$(MARKDOWNLINT_VERSION)/bin/markdownlint-cli2
.PHONY: download.markdownlint-cli2
download.markdownlint-cli2: mise yq ## Download markdownlint-cli2 locally if necessary.
	@$(MISE) plugin install --yes -q markdownlint-cli2
	@$(MISE) install -q markdownlint-cli2@$(MARKDOWNLINT_VERSION)

.PHONY: use-setup-envtest
use-setup-envtest:
	$(SETUP_ENVTEST) use

# ------------------------------------------------------------------------------
# Build
# ------------------------------------------------------------------------------

.PHONY: all
all: build

.PHONY: clean
clean:
	@rm -rf build/
	@rm -rf bin/*
	@rm -f coverage*.out

.PHONY: tidy
tidy:
	go mod tidy
	go mod verify

.PHONY: build.operator
build.operator:
	$(MAKE) _build.operator LDFLAGS="-s -w"

.PHONY: build.operator.debug
build.operator.debug:
	$(MAKE) _build.operator GCFLAGS="-gcflags='all=-N -l'"

.PHONY: _build.operator
_build.operator:
	go build -o bin/manager $(GCFLAGS) \
		-ldflags "$(LDFLAGS_COMMON) $(LDFLAGS) $(LDFLAGS_METADATA)" \
		cmd/main.go

.PHONY: build
build: generate
	$(MAKE) build.operator

.PHONY: _govulncheck
_govulncheck:
	GOVULNCHECK=$(GOVULNCHECK) SCAN=$(SCAN) ./hack/govulncheck-with-excludes.sh ./...

.PHONY: govulncheck
govulncheck: download.govulncheck
	$(MAKE) _govulncheck SCAN=symbol
	$(MAKE) _govulncheck SCAN=package

GOLANGCI_LINT_CONFIG ?= $(PROJECT_DIR)/.golangci.yaml
.PHONY: lint
lint: lint.golangci-lint lint.modernize

.PHONY: lint.golangci-lint
lint.golangci-lint: golangci-lint
	$(GOLANGCI_LINT) run -v --config $(GOLANGCI_LINT_CONFIG) $(GOLANGCI_LINT_FLAGS)

.PHONY: lint.modernize
lint.modernize: modernize
	$(MODERNIZE) ./...

.PHONY: lint.charts
lint.charts: download.kube-linter
	$(KUBE_LINTER) lint charts/

.PHONY: lint.actions
lint.actions: download.actionlint download.shellcheck
# TODO: add more files to be checked
	SHELLCHECK_OPTS='--exclude=SC2086,SC2155,SC2046' \
	$(ACTIONLINT) -shellcheck $(SHELLCHECK) \
		./.github/workflows/*

.PHONY: lint.markdownlint
lint.markdownlint: download.markdownlint-cli2
	$(MARKDOWNLINT) \
		CHANGELOG.md \
		README.md \
		FEATURES.md \
		charts/kong-operator/README.md \
		charts/kong-operator/CHANGELOG.md

.PHONY: lint.all
lint.all: lint lint.charts lint.actions lint.markdownlint

.PHONY: verify
verify: verify.manifests verify.generators

.PHONY: verify.diff
verify.diff:
	@$(PROJECT_DIR)/scripts/verify-diff.sh $(PROJECT_DIR)

.PHONY: verify.repo
verify.repo:
	@$(PROJECT_DIR)/scripts/verify-repo.sh

.PHONY: verify.manifests
verify.manifests: verify.repo manifests verify.diff

.PHONY: verify.generators
verify.generators: verify.repo generate verify.diff

# ------------------------------------------------------------------------------
# Build - Generators
# ------------------------------------------------------------------------------

API_DIR ?= api

.PHONY: generate
generate: generate.gateway-api-urls generate.crd-kustomize generate.k8sio-gomod-replace generate.kic-webhook-config generate.mocks generate.cli-arguments-docs

.PHONY: generate.crd-kustomize
generate.crd-kustomize:
	./scripts/generate-crd-kustomize.sh

.PHONY: generate.api
generate.api: controller-gen
	$(CONTROLLER_GEN) object:headerFile="hack/generators/boilerplate.go.txt" paths="./$(API_DIR)/..."

# this will generate the custom typed clients needed for end-users implementing logic in Go to use our API types.
.PHONY: generate.clientsets
generate.clientsets: client-gen
	# We create a symlink to the apis/ directory as a hack because currently
	# client-gen does not properly support the use of api/ as your API
	# directory.
	#
	# See: https://github.com/kubernetes/code-generator/issues/167
	ln -sf api apis
	$(CLIENT_GEN) \
		--go-header-file ./hack/generators/boilerplate.go.txt \
		--clientset-name clientset \
		--input-base '' \
		--input $(REPO)/apis/v1alpha1 \
		--input $(REPO)/apis/v1beta1 \
		--output-dir pkg/ \
		--output-pkg $(REPO)/pkg/
	rm apis
	find ./pkg/clientset/ -type f -name '*.go' -exec sed -i '' -e 's/github.com\/kong\/gateway-operator\/apis/github.com\/kong\/gateway-operator\/api/gI' {} \; &> /dev/null

.PHONY: generate.k8sio-gomod-replace
generate.k8sio-gomod-replace:
	./hack/update-k8sio-gomod-replace.sh

.PHONY: generate.kic-webhook-config
generate.kic-webhook-config: kustomize kic-webhook-config-generator
	KUSTOMIZE=$(KUSTOMIZE) $(KIC_WEBHOOKCONFIG_GENERATOR)
	go fmt ./pkg/utils/kubernetes/resources/...

.PHONY: generate.cli-arguments-docs
generate.cli-arguments-docs:
	go run ./scripts/cli-arguments-docs-gen/main.go > ./docs/cli-arguments.md
	$(PROJECT_DIR)/scripts/cli-arguments-docs-gen/post-process-for-konghq.sh \
		$(PROJECT_DIR)/docs/cli-arguments-for-developer-konghq-com.md \
		$(PROJECT_DIR)/docs/cli-arguments.md

# ------------------------------------------------------------------------------
# Build - Manifests
# ------------------------------------------------------------------------------

CONTROLLER_GEN_CRD_OPTIONS ?= "+crd:generateEmbeddedObjectMeta=true"
CONTROLLER_GEN_PATHS_RAW := ./pkg/utils/kubernetes/reduce/ ./controller/... ./ingress-controller/internal/controllers/... ./ingress-controller/internal/konnect/ ./modules/manager/
CONTROLLER_GEN_PATHS := $(patsubst %,%;,$(strip $(CONTROLLER_GEN_PATHS_RAW)))
CONFIG_DIR = $(PROJECT_DIR)/config
CONFIG_CRD_DIR = $(CONFIG_DIR)/crd
CONFIG_CRD_BASE_PATH = $(CONFIG_CRD_DIR)/bases
CONFIG_RBAC_ROLE_DIR = $(CONFIG_DIR)/rbac/role

KUBERNETES_CONFIGURATION_PACKAGE ?= github.com/kong/kubernetes-configuration/v2
KUBERNETES_CONFIGURATION_VERSION ?= $(shell go list -m -f '{{ .Version }}' $(KUBERNETES_CONFIGURATION_PACKAGE))
KUBERNETES_CONFIGURATION_PACKAGE_PATH = $(shell go env GOPATH)/pkg/mod/$(KUBERNETES_CONFIGURATION_PACKAGE)@$(KUBERNETES_CONFIGURATION_VERSION)

.PHONY: manifests
manifests: manifests.conversion-webhook manifests.versions manifests.crds manifests.role manifests.charts ## Generate ClusterRole and CustomResourceDefinition objects.

.PHONY: manifests.conversion-webhook
manifests.conversion-webhook: kustomize
	KUSTOMIZE_BIN=$(KUSTOMIZE) go run hack/generators/conversion-webhook/main.go

.PHONY: manifests.crds
manifests.crds: controller-gen ## Generate CustomResourceDefinition objects.
	$(CONTROLLER_GEN) paths="$(CONTROLLER_GEN_PATHS)" $(CONTROLLER_GEN_CRD_OPTIONS) +output:crd:artifacts:config=$(CONFIG_CRD_BASE_PATH)

.PHONY: manifests.role
manifests.role: controller-gen
	$(CONTROLLER_GEN) paths="$(CONTROLLER_GEN_PATHS)" \
		rbac:roleName=manager-role \
		output:rbac:dir=$(CONFIG_RBAC_ROLE_DIR)

# manifests.versions ensures that image versions are set in the manifests according to the current version.
.PHONY: manifests.versions
manifests.versions: kustomize yq
	$(YQ) eval '.appVersion = "$(VERSION)"' -i charts/kong-operator/Chart.yaml
	$(YQ) eval '.image.tag = "$(VERSION)"' -i charts/kong-operator/values.yaml
	cd config/components/manager-image/ && $(KUSTOMIZE) edit set image $(KUSTOMIZE_IMG_NAME)=$(IMG):$(VERSION)

.PHONY: manifests.charts
manifests.charts:
	@$(MAKE) manifests.charts.kong-operator.crds.operator
	@$(MAKE) manifests.charts.kong-operator.crds.kic
	@$(MAKE) manifests.charts.kong-operator.crds.gwapi-standard
	@$(MAKE) manifests.charts.kong-operator.crds.gwapi-experimental
	@$(MAKE) manifests.charts.kong-operator.chart.yaml
	@$(MAKE) manifests.charts.kong-operator.role

KONG_OPERATOR_CHART_DIR = $(PROJECT_DIR)/charts/kong-operator

.PHONY: ensure.go.pkg.downloaded.kubernetes-configuration
ensure.go.pkg.downloaded.kubernetes-configuration:
	@go mod download $(KUBERNETES_CONFIGURATION_PACKAGE)@$(KUBERNETES_CONFIGURATION_VERSION)

.PHONY: ensure.go.pkg.downloaded.gateway-api
ensure.go.pkg.downloaded.gateway-api:
	@go mod download $(GATEWAY_API_PACKAGE)@$(GATEWAY_API_VERSION)

# NOTE: Target below makes sure that the generated role is always up-to-date
# between the manifests (generated via kubebuilder markers) and the chart.
.PHONY: manifests.charts.kong-operator.role
manifests.charts.kong-operator.role: manifests.role
	cp $(CONFIG_RBAC_ROLE_DIR)/role.yaml $(KONG_OPERATOR_CHART_DIR)/templates/cluster-role.yaml
	$(YQ) eval '.metadata.name = "{{ template \"kong.fullname\" . }}-manager-role"' -i $(KONG_OPERATOR_CHART_DIR)/templates/cluster-role.yaml

.PHONY: manifests.charts.kong-operator.crds.operator
manifests.charts.kong-operator.crds.operator: kustomize ensure.go.pkg.downloaded.kubernetes-configuration
	$(MAKE) manifests.conversion-webhook

.PHONY: manifests.charts.kong-operator.crds.kic
manifests.charts.kong-operator.crds.kic: kustomize ensure.go.pkg.downloaded.kubernetes-configuration
	$(KUSTOMIZE) build $(KUBERNETES_CONFIGURATION_PACKAGE_PATH)/config/crd/ingress-controller > \
		$(KONG_OPERATOR_CHART_DIR)/charts/kic-crds/crds/kic-crds.yaml

GATEWAY_API_STANDARD_CRDS_SUBCHART_CHART_YAML_PATH = $(KONG_OPERATOR_CHART_DIR)/charts/gwapi-standard-crds/Chart.yaml
GATEWAY_API_STANDARD_CRDS_SUBCHART_MANIFEST_PATH = $(KONG_OPERATOR_CHART_DIR)/charts/gwapi-standard-crds/crds/gwapi-crds.yaml

.PHONY: manifests.charts.kong-operator.crds.gwapi-standard
manifests.charts.kong-operator.crds.gwapi-standard: kustomize ensure.go.pkg.downloaded.gateway-api
	@$(MAKE) manifests.charts.print.chart.yaml \
		NAME=gwapi-standard-crds \
		DESCRIPTION="A Helm chart for Kubernetes Gateway API standard channel CRDs" \
		VERSION=$(GATEWAY_API_VERSION:v%=%) \
		CHART_YAML_PATH=$(GATEWAY_API_STANDARD_CRDS_SUBCHART_CHART_YAML_PATH)
	$(KUSTOMIZE) build $(GATEWAY_API_CRDS_KUSTOMIZE_STANDARD_LOCAL_PATH) > $(GATEWAY_API_STANDARD_CRDS_SUBCHART_MANIFEST_PATH)

GATEWAY_API_EXPERIMENTAL_CRDS_SUBCHART_CHART_YAML_PATH = $(KONG_OPERATOR_CHART_DIR)/charts/gwapi-experimental-crds/Chart.yaml
GATEWAY_API_EXPERIMENTAL_CRDS_SUBCHART_MANIFEST_PATH = $(KONG_OPERATOR_CHART_DIR)/charts/gwapi-experimental-crds/crds/gwapi-crds.yaml

.PHONY: manifests.charts.kong-operator.crds.gwapi-experimental
manifests.charts.kong-operator.crds.gwapi-experimental: kustomize ensure.go.pkg.downloaded.gateway-api
	@$(MAKE) manifests.charts.print.chart.yaml \
		NAME=gwapi-experimental-crds \
		DESCRIPTION="A Helm chart for Kubernetes Gateway API experimental channel CRDs" \
		VERSION=$(GATEWAY_API_VERSION:v%=%) \
		CHART_YAML_PATH=$(GATEWAY_API_EXPERIMENTAL_CRDS_SUBCHART_CHART_YAML_PATH)
	$(KUSTOMIZE) build $(GATEWAY_API_CRDS_KUSTOMIZE_EXPERIMENTAL_LOCAL_PATH) > $(GATEWAY_API_EXPERIMENTAL_CRDS_SUBCHART_MANIFEST_PATH)

.PHONY: manifests.charts.print.chart.yaml
manifests.charts.print.chart.yaml: yq
	@echo "Generating $(CHART_YAML_PATH)"
	@touch $(CHART_YAML_PATH)
	@$(YQ) eval '.apiVersion = "v2"' -i $(CHART_YAML_PATH)
	@$(YQ) eval '.name = "$(NAME)"' -i $(CHART_YAML_PATH)
	@$(YQ) eval '.version = "$(VERSION)"' -i $(CHART_YAML_PATH)
	@$(YQ) eval 'with(.appVersion ; . = "$(VERSION)" | . style="double")' -i $(CHART_YAML_PATH)
	@$(YQ) eval '.description = "$(DESCRIPTION)"' -i $(CHART_YAML_PATH)

KONG_OPERATOR_CHART_YAML_PATH = $(KONG_OPERATOR_CHART_DIR)/Chart.yaml

# NOTE: Below yq invocations are split into multiple lines to make it slightly more readable.
# yq command lines splitting in Makefiles proves to be rather hard.
.PHONY: manifests.charts.kong-operator.chart.yaml
manifests.charts.kong-operator.chart.yaml: yq
	@echo "Generating $(KONG_OPERATOR_CHART_YAML_PATH)"
	@$(YQ) eval \
		'.dependencies = [ {"name":"ko-crds","version":"1.0.0"}]' \
		-i $(KONG_OPERATOR_CHART_YAML_PATH)
	@$(YQ) eval \
		'.dependencies += [ {"name":"kic-crds","version":"1.2.0","condition":"kic-crds.enabled"}]' \
		-i $(KONG_OPERATOR_CHART_YAML_PATH)
	@$(YQ) eval \
		'.dependencies += [ {"name":"gwapi-standard-crds","version":"$(GATEWAY_API_VERSION:v%=%)","condition":"gwapi-standard-crds.enabled"}]' \
		-i $(KONG_OPERATOR_CHART_YAML_PATH)
	@$(YQ) eval \
		'.dependencies += [ {"name":"gwapi-experimental-crds","version":"$(GATEWAY_API_VERSION:v%=%)","condition":"gwapi-experimental-crds.enabled"}]' \
		-i $(KONG_OPERATOR_CHART_YAML_PATH)

# ------------------------------------------------------------------------------
# Build - Container Images
# ------------------------------------------------------------------------------

.PHONY: _docker.build
_docker.build:
	docker build -t $(IMG):$(TAG) \
		--target $(TARGET) \
		--build-arg GOPATH=$(shell go env GOPATH) \
		--build-arg GOCACHE=$(shell go env GOCACHE) \
		--build-arg TAG=$(TAG) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg REPO_INFO=$(REPO_INFO) \
		$(DOCKERBUILDFLAGS) \
		.

.PHONY: docker.build
docker.build:
	TAG=$(TAG) TARGET=distroless $(MAKE) _docker.build

.PHONY: docker.push
docker.push:
	docker push $(IMG):$(TAG)

# NOTE: Token-Permissions check has been disabled as it has been flaky on the CI.
# TODO: https://github.com/Kong/kong-operator/issues/2089
.PHONY: docker.run.openssf
docker.run.openssf:
	docker run --rm --env GITHUB_TOKEN=$(GITHUB_TOKEN) \
		gcr.io/openssf/scorecard:stable \
		--repo=$(REPO) \
		--commit=$(COMMIT) \
		--show-details \
		--checks=Pinned-Dependencies,License,Dangerous-Workflow,Binary-Artifacts,Code-Review

# ------------------------------------------------------------------------------
# Testing
# ------------------------------------------------------------------------------

GOTESTSUM_FORMAT ?= standard-verbose
INTEGRATION_TEST_TIMEOUT ?= "30m"
CONFORMANCE_TEST_TIMEOUT ?= "20m"
E2E_TEST_TIMEOUT ?= "20m"

.PHONY: test
test: test.unit

UNIT_TEST_PATHS := ./controller/... ./internal/... ./pkg/... ./modules/... ./ingress-controller/internal/... ./ingress-controller/pkg/...

.PHONY: _test.unit
_test.unit: gotestsum
	GOTESTSUM_FORMAT=$(GOTESTSUM_FORMAT) \
		$(GOTESTSUM) -- $(GOTESTFLAGS) \
		-race \
		-coverprofile=coverage.unit.out \
		-ldflags "$(LDFLAGS_COMMON) $(LDFLAGS)" \
		$(UNIT_TEST_PATHS)

.PHONY: test.unit
test.unit:
	@$(MAKE) _test.unit GOTESTFLAGS="$(GOTESTFLAGS)"

.PHONY: test.unit.pretty
test.unit.pretty:
	@$(MAKE) _test.unit GOTESTSUM_FORMAT=pkgname GOTESTFLAGS="$(GOTESTFLAGS)" UNIT_TEST_PATHS="$(UNIT_TEST_PATHS)"

ENVTEST_TEST_PATHS := ./test/envtest/...
ENVTEST_TIMEOUT ?= 5m
PKG_LIST=./controller/...,./internal/...,./pkg/...,./modules/...

.PHONY: _test.envtest
_test.envtest: gotestsum setup-envtest use-setup-envtest
	KUBECONFIG=$(KUBECONFIG) \
	KUBEBUILDER_ASSETS="$(shell $(SETUP_ENVTEST) use -p path)" \
	GOTESTSUM_FORMAT=$(GOTESTSUM_FORMAT) \
		$(GOTESTSUM) -- $(GOTESTFLAGS) \
		-race \
		-timeout $(ENVTEST_TIMEOUT) \
		-covermode=atomic \
		-coverpkg=$(PKG_LIST) \
		-coverprofile=coverage.envtest.out \
		-ldflags "$(LDFLAGS_COMMON) $(LDFLAGS)" \
		$(ENVTEST_TEST_PATHS)

.PHONY: test.envtest
test.envtest:
	$(MAKE) _test.envtest GOTESTSUM_FORMAT=standard-verbose

.PHONY: test.envtest.pretty
test.envtest.pretty:
	$(MAKE) _test.envtest GOTESTSUM_FORMAT=testname

.PHONY: test.crds-validation
test.crds-validation:
	$(MAKE) _test.envtest GOTESTSUM_FORMAT=standard-verbose ENVTEST_TEST_PATHS=./test/crdsvalidation/...

.PHONY: test.crds-validation.pretty
test.crds-validation.pretty:
	$(MAKE) _test.envtest GOTESTSUM_FORMAT=testname ENVTEST_TEST_PATHS=./test/crdsvalidation/...

.PHONY: _test.integration
_test.integration: gotestsum download.telepresence
	KUBECONFIG=$(KUBECONFIG) \
	TELEPRESENCE_BIN=$(TELEPRESENCE) \
	GOFLAGS=$(GOFLAGS) \
	GOTESTSUM_FORMAT=$(GOTESTSUM_FORMAT) \
	$(GOTESTSUM) -- $(GOTESTFLAGS) \
	-timeout $(INTEGRATION_TEST_TIMEOUT) \
	-ldflags "$(LDFLAGS_COMMON) $(LDFLAGS) $(LDFLAGS_METADATA)" \
	-race \
	-coverprofile=$(COVERPROFILE) \
	./test/integration/...

.PHONY: test.integration
test.integration:
	@$(MAKE) _test.integration \
		GOTESTFLAGS="-skip='BlueGreen' $(GOTESTFLAGS)" \
		COVERPROFILE="coverage.integration.out"

.PHONY: test.integration_bluegreen
test.integration_bluegreen:
	@$(MAKE) _test.integration \
		KONG_OPERATOR_BLUEGREEN_CONTROLLER="true" \
		GOTESTFLAGS="-run='BlueGreen|TestDataPlane' $(GOTESTFLAGS)" \
		COVERPROFILE="coverage.integration-bluegreen.out" \

.PHONY: _test.e2e
_test.e2e: gotestsum
		GOTESTSUM_FORMAT=$(GOTESTSUM_FORMAT) \
		$(GOTESTSUM) -- $(GOTESTFLAGS) \
		-timeout $(E2E_TEST_TIMEOUT) \
		-ldflags "$(LDFLAGS_COMMON) $(LDFLAGS) $(LDFLAGS_METADATA)" \
		-race \
		./test/e2e/...

.PHONY: test.e2e
test.e2e:
	@$(MAKE) _test.e2e \
		GOTESTFLAGS="$(GOTESTFLAGS)"

NCPU := $(shell getconf _NPROCESSORS_ONLN)
PARALLEL := $(if $(PARALLEL),$(PARALLEL),$(NCPU))

.PHONY: _test.conformance
_test.conformance: gotestsum download.telepresence
		KUBECONFIG=$(KUBECONFIG) \
		TELEPRESENCE_BIN=$(TELEPRESENCE) \
		GOTESTSUM_FORMAT=$(GOTESTSUM_FORMAT) \
		$(GOTESTSUM) -- $(GOTESTFLAGS) \
		-timeout $(CONFORMANCE_TEST_TIMEOUT) \
		-ldflags "$(LDFLAGS_COMMON) $(LDFLAGS) $(LDFLAGS_METADATA)" \
		-race \
		-parallel $(PARALLEL) \
		$(TEST_SUITE_PATH)

.PHONY: test.conformance
test.conformance:
	@$(MAKE) _test.conformance \
		GOTESTFLAGS="$(GOTESTFLAGS)" \
		TEST_SUITE_PATH='./test/conformance/...'
	@echo 'Conformance tests from the ingress-controller subdirectory'
	@$(MAKE) _test.conformance \
		GOTESTFLAGS="$(GOTESTFLAGS)-tags=conformance_tests" \
		TEST_SUITE_PATH='./ingress-controller/test/conformance/...'


.PHONY: test.kongintegration
test.kongintegration:
	@$(MAKE) _test.kongintegration GOTESTSUM_FORMAT=standard-verbose

.PHONY: test.kongintegration.pretty
test.kongintegration.pretty:
	@$(MAKE) _test.kongintegration GOTESTSUM_FORMAT=testname

.PHONY: _test.kongintegration
_test.kongintegration: gotestsum
	# Disable testcontainer's reaper (Ryuk). It's needed because Ryuk requires
	# privileged mode to run, which is not desired and could cause issues in CI.
	TESTCONTAINERS_RYUK_DISABLED="true" \
	GOTESTSUM_FORMAT=$(GOTESTSUM_FORMAT) \
	$(GOTESTSUM) -- $(GOTESTFLAGS) \
		-race \
		-ldflags="$(LDFLAGS_COMMON)" \
		-parallel $(NCPU) \
		-coverpkg=$(PKG_LIST) \
		-coverprofile=coverage.kongintegration.out \
		./ingress-controller/test/kongintegration

.PHONY: test.samples
test.samples:
	@cd config/samples/ && find . -not -name "kustomization.*" -type f | sort | xargs -I{} bash -c "echo;echo {}; kubectl apply -f {} && kubectl delete -f {}" \;

.PHONY: test.charts.golden
test.charts.golden:
	@ \
		$(MAKE) _chartsnap CHART=kong-operator || \
	(echo "$$GOLDEN_TEST_FAILURE_MSG" && exit 1)

.PHONY: test.charts.golden.update
test.charts.golden.update:
	@ $(MAKE) _chartsnap CHART=kong-operator CHARTSNAP_ARGS="-u"

.PHONY: test.charts.ct.install
test.charts.ct.install:
# NOTE: We add ko-crds.keep=false below because allowing the chart to manage CRDs
# and keep them around after helm uninstall would yield ownership issues.
# Error: INSTALLATION FAILED: Unable to continue with install: CustomResourceDefinition "aigateways.gateway-operator.konghq.com" in namespace ""
# exists and cannot be imported into the current release: invalid ownership metadata; annotation validation error: key "meta.helm.sh/release-name"
# must equal "kong-operator-h39gau9scc": current value is "kong-operator-tiptva339m"
#
# NOTE: We add the --wait below to ensure that we wait for all objects to get removed
# for each release. Without this, some objects like CRDs can still be around
# when another test helm release is being installed and the above mentioned
# ownership error will be returned.
	ct install --target-branch main \
		--debug \
		--helm-extra-set-args "--set=ko-crds.keep=false" \
		--helm-extra-args "--wait" \
		--helm-extra-args "--timeout=1m" \
		--charts charts/$(CHART_NAME) \
		--namespace kong-test

# Defining multi-line strings to echo: https://stackoverflow.com/a/649462/7958339
define GOLDEN_TEST_FAILURE_MSG
>> Golden tests have failed.
>> Please run 'make test.golden.update' to update golden files and commit the changes if they're expected.
endef
export GOLDEN_TEST_FAILURE_MSG

.PHONY: _chartsnap
_chartsnap: download.chartsnap
	helm chartsnap -c ./charts/$(CHART) -f ./charts/$(CHART)/ci/ $(CHARTSNAP_ARGS) \
		-- \
		--api-versions gateway.networking.k8s.io/v1 \
		--api-versions admissionregistration.k8s.io/v1/ValidatingAdmissionPolicy \
		--api-versions admissionregistration.k8s.io/v1/ValidatingAdmissionPolicyBinding

# https://github.com/vektra/mockery/issues/803#issuecomment-2287198024
.PHONY: generate.mocks
generate.mocks: mockery
	GODEBUG=gotypesalias=0 $(MOCKERY)

# ------------------------------------------------------------------------------
# Gateway API
# ------------------------------------------------------------------------------

# GATEWAY_API_VERSION will be processed by kustomize and therefore accepts
# only branch names, tags, or full commit hashes, i.e. short hashes or go
# pseudo versions are not supported [1].
# Please also note that kustomize fails silently when provided with an
# unsupported ref and downloads the manifests from the main branch.
#
# [1]: https://github.com/kubernetes-sigs/kustomize/blob/master/examples/remoteBuild.md#remote-directories
GATEWAY_API_PACKAGE ?= sigs.k8s.io/gateway-api
GATEWAY_API_RELEASE_CHANNEL ?= experimental
GATEWAY_API_VERSION ?= $(shell go list -m -f '{{ .Version }}' $(GATEWAY_API_PACKAGE))

GATEWAY_API_CRDS_LOCAL_PATH = $(shell go env GOPATH)/pkg/mod/$(GATEWAY_API_PACKAGE)@$(GATEWAY_API_VERSION)/config/crd
GATEWAY_API_CRDS_KUSTOMIZE_STANDARD_LOCAL_PATH = $(GATEWAY_API_CRDS_LOCAL_PATH)/
GATEWAY_API_CRDS_KUSTOMIZE_EXPERIMENTAL_LOCAL_PATH = $(GATEWAY_API_CRDS_LOCAL_PATH)/experimental
GATEWAY_API_REPO ?= kubernetes-sigs/gateway-api
GATEWAY_API_RAW_REPO ?= https://raw.githubusercontent.com/$(GATEWAY_API_REPO)
GATEWAY_API_CRDS_STANDARD_URL = github.com/$(GATEWAY_API_REPO)/config/crd?ref=$(GATEWAY_API_VERSION)
GATEWAY_API_CRDS_EXPERIMENTAL_URL = github.com/$(GATEWAY_API_REPO)/config/crd/experimental?ref=$(GATEWAY_API_VERSION)
GATEWAY_API_RAW_REPO_URL = $(GATEWAY_API_RAW_REPO)/$(GATEWAY_API_VERSION)

.PHONY: generate.gateway-api-urls
generate.gateway-api-urls:
	CRDS_STANDARD_URL="$(GATEWAY_API_CRDS_STANDARD_URL)" \
		CRDS_EXPERIMENTAL_URL="$(GATEWAY_API_CRDS_EXPERIMENTAL_URL)" \
		RAW_REPO_URL="$(GATEWAY_API_RAW_REPO_URL)" \
		INPUT=$(shell pwd)/internal/utils/cmd/generate-gateway-api-urls/gateway_consts.tmpl \
		OUTPUT=$(shell pwd)/pkg/utils/test/zz_generated.gateway_api.go \
		go generate -tags=generate_gateway_api_urls ./internal/utils/cmd/generate-gateway-api-urls

.PHONY: install.gateway-api-crds
install.gateway-api-crds: kustomize ensure.go.pkg.downloaded.gateway-api
	$(KUSTOMIZE) build $(GATEWAY_API_CRDS_KUSTOMIZE_EXPERIMENTAL_LOCAL_PATH) | kubectl apply -f -

.PHONY: uninstall.gateway-api-crds
uninstall.gateway-api-crds: kustomize ensure.go.pkg.downloaded.gateway-api
	$(KUSTOMIZE) build $(GATEWAY_API_CRDS_KUSTOMIZE_EXPERIMENTAL_LOCAL_PATH) | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

# ------------------------------------------------------------------------------
# Debug
# ------------------------------------------------------------------------------

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: _ensure-kong-system-namespace
_ensure-kong-system-namespace:
	@kubectl create ns kong-system 2>/dev/null || true

# Run a controller from your host.
# TODO: https://github.com/Kong/kong-operator/issues/1989
.PHONY: run
run: download.telepresence manifests generate install.all _ensure-kong-system-namespace install.rbacs
	@$(MAKE) _run

# Run a controller from your host and make it impersonate the controller-manager service account from kong-system namespace.
.PHONY: run.with_impersonate
run.with_impersonate: download.telepresence manifests generate install.all _ensure-kong-system-namespace install.rbacs
	@$(MAKE) _run.with-impersonate

KUBECONFIG ?= $(HOME)/.kube/config

# Run the operator without checking any preconditions, installing CRDs etc.
# This is mostly useful when 'run' was run at least once on a server and CRDs, RBACs
# etc didn't change in between the runs.
.PHONY: _run
_run:
	@$(TELEPRESENCE) helm install
	@$(TELEPRESENCE) connect
	bash -c "trap \
		'$(TELEPRESENCE) quit -s; $(TELEPRESENCE) helm uninstall; rm -rf $(TMP_KUBECONFIG) || 1' EXIT; \
		KONG_OPERATOR_KUBECONFIG=$(or $(TMP_KUBECONFIG),$(KUBECONFIG)) \
		KONG_OPERATOR_ANONYMOUS_REPORTS=false \
		KONG_OPERATOR_LOGGING_MODE=development \
		go run ./cmd/main.go \
		--no-leader-election \
		-cluster-ca-secret-namespace kong-system \
		-enable-controller-kongplugininstallation \
		-enable-controller-aigateway \
		-enable-controller-konnect \
		-enable-controller-controlplaneextensions \
		-enable-conversion-webhook=false \
		-zap-time-encoding iso8601 \
		-zap-log-level 2 \
		-zap-devel true \
	"

# Run the operator locally with impersonation of controller-manager service account from kong-system namespace.
# The operator will use a temporary kubeconfig file and impersonate the real RBACs.
.PHONY: _run.with-impersonate
_run.with-impersonate:
	@$(eval TMP := $(shell mktemp -d))
	@$(eval TMP_KUBECONFIG := $(TMP)/kubeconfig)
	[ ! -z "$(KUBECONFIG)" ] || exit 1
	cp $(KUBECONFIG) $(TMP_KUBECONFIG)
	@$(eval TMP_TOKEN := $(shell kubectl create token --namespace=kong-system controller-manager))
	@$(eval CLUSTER := $(shell kubectl config get-contexts | grep '^\*' | tr -s ' ' | cut -d ' ' -f 3))
	KUBECONFIG=$(TMP_KUBECONFIG) kubectl config set-credentials ko --token=$(TMP_TOKEN)
	KUBECONFIG=$(TMP_KUBECONFIG) kubectl config set-context ko --cluster=$(CLUSTER) --user=kgo --namespace=kong-system
	KUBECONFIG=$(TMP_KUBECONFIG) kubectl config use-context ko
	$(MAKE) _run TMP_KUBECONFIG=$(TMP_KUBECONFIG)

SKAFFOLD_RUN_PROFILE ?= dev

.PHONY: _skaffold
_skaffold: skaffold
	GOCACHE=$(shell go env GOCACHE) \
		$(SKAFFOLD) $(CMD) --port-forward=pods --profile=$(SKAFFOLD_PROFILE) $(SKAFFOLD_FLAGS)

.PHONY: run.skaffold
run.skaffold:
	TAG=$(TAG) REPO_INFO=$(REPO_INFO) COMMIT=$(COMMIT) \
		CMD=dev \
		SKAFFOLD_PROFILE=$(SKAFFOLD_RUN_PROFILE) \
		$(MAKE) _skaffold

.PHONY: debug
debug: manifests generate install.all _ensure-kong-system-namespace
	KONG_OPERATOR_ANONYMOUS_REPORTS=false \
	KONG_OPERATOR_LOGGING_MODE=development \
		dlv debug ./cmd/main.go -- \
		--no-leader-election \
		-cluster-ca-secret-namespace kong-system \
		--enable-controller-aigateway \
		--enable-controller-konnect \
		-zap-time-encoding iso8601

.PHONY: debug.skaffold
debug.skaffold: _ensure-kong-system-namespace
	GOCACHE=$(shell go env GOCACHE) \
	TAG=$(TAG)-debug REPO_INFO=$(REPO_INFO) COMMIT=$(COMMIT) \
		$(SKAFFOLD) debug --port-forward=pods --profile=debug

.PHONY: debug.skaffold.continuous
debug.skaffold.continuous: _ensure-kong-system-namespace
	GOCACHE=$(shell go env GOCACHE) \
	TAG=$(TAG)-debug REPO_INFO=$(REPO_INFO) COMMIT=$(COMMIT) \
		$(SKAFFOLD) debug --port-forward=pods --profile=debug --auto-build --auto-deploy --auto-sync

CERT_MANAGER_VERSION = $(shell $(YQ) -r '.cert-manager' < $(TOOLS_VERSIONS_FILE))
.PHONY: install.helm.cert-manager
install.helm.cert-manager: yq
	helm repo add jetstack https://charts.jetstack.io && \
	helm repo update && \
	helm upgrade --install \
	  cert-manager jetstack/cert-manager \
	  --namespace cert-manager \
	  --create-namespace \
	  --version $(CERT_MANAGER_VERSION) \
	  --set crds.enabled=true

.PHONY: uninstall.helm.cert-manager
uninstall.helm.cert-manager:
	helm uninstall cert-manager --namespace cert-manager

# Install CRDs into the K8s cluster specified in ~/.kube/config.
.PHONY: install
install: install.helm.cert-manager manifests kustomize install.gateway-api-crds
	$(KUSTOMIZE) build config/crd | kubectl apply --server-side -f -

KUBERNETES_CONFIGURATION_PACKAGE_PATH = $(shell go env GOPATH)/pkg/mod/$(KUBERNETES_CONFIGURATION_PACKAGE)@$(KUBERNETES_CONFIGURATION_VERSION)
KUBERNETES_CONFIGURATION_CRDS_CRDS_LOCAL_PATH = $(KUBERNETES_CONFIGURATION_PACKAGE_PATH)/config/crd/gateway-operator
KUBERNETES_CONFIGURATION_CRDS_CRDS_INGRESS_CONTROLLER_LOCAL_PATH = $(KUBERNETES_CONFIGURATION_PACKAGE_PATH)/config/crd/ingress-controller

# Install kubernetes-configuration CRDs into the K8s cluster specified in ~/.kube/config.
.PHONY: install.kubernetes-configuration-crds-operator
install.kubernetes-configuration-crds-operator: kustomize ensure.go.pkg.downloaded.kubernetes-configuration
	$(KUSTOMIZE) build $(KUBERNETES_CONFIGURATION_CRDS_CRDS_LOCAL_PATH) | kubectl apply --server-side -f -

# Install kubernetes-configuration ingress controller CRDs into the K8s cluster specified in ~/.kube/config.
.PHONY: install.kubernetes-configuration-crds-ingress-controller
install.kubernetes-configuration-crds-ingress-controller: kustomize ensure.go.pkg.downloaded.kubernetes-configuration
	$(KUSTOMIZE) build $(KUBERNETES_CONFIGURATION_CRDS_CRDS_INGRESS_CONTROLLER_LOCAL_PATH) | kubectl apply --server-side -f -

# Install RBACs from config/rbac into the K8s cluster specified in ~/.kube/config.
.PHONY: install.rbacs
install.rbacs: kustomize
	$(KUSTOMIZE) build config/rbac | kubectl apply -f -

# Install standard and experimental CRDs into the K8s cluster specified in ~/.kube/config.
.PHONY: install.all
install.all: install.helm.cert-manager manifests kustomize install.gateway-api-crds install.kubernetes-configuration-crds-operator install.kubernetes-configuration-crds-ingress-controller
	kubectl get crd -ojsonpath='{.items[*].metadata.name}' | xargs -n1 kubectl wait --for condition=established crd

# Uninstall CRDs from the K8s cluster specified in ~/.kube/config.
# Call with ignore-not-found=true to ignore resource not found errors during deletion.
.PHONY: uninstall
uninstall: manifests kustomize uninstall.gateway-api-crds uninstall.helm.cert-manager
	$(KUSTOMIZE) build config/crd | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: uninstall.kubernetes-configuration-crds
uninstall.kubernetes-configuration-crds: kustomize
	$(KUSTOMIZE) build $(KUBERNETES_CONFIGURATION_CRDS_CRDS_LOCAL_PATH) | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

# Uninstall standard and experimental CRDs from the K8s cluster specified in ~/.kube/config.
# Call with ignore-not-found=true to ignore resource not found errors during deletion.
.PHONY: uninstall.all
uninstall.all: manifests kustomize uninstall.gateway-api-crds uninstall.kubernetes-configuration-crds uninstall.helm.cert-manager

# Deploy controller to the K8s cluster specified in ~/.kube/config.
# This will wait for operator's Deployment to get Available.
# This uses a temporary directory becuase "kustomize edit set image" would introduce
# a change in current work tree which we do not want.
.PHONY: deploy
deploy: manifests kustomize
	$(eval TMP := $(shell mktemp -d))
	cp -R config $(TMP)
	cd $(TMP)/config/components/manager-image/ && $(KUSTOMIZE) edit set image $(KUSTOMIZE_IMG_NAME)=$(IMG):$(VERSION)
	cd $(TMP)/config/default && $(KUSTOMIZE) build . | kubectl apply --server-side -f -
	kubectl wait --timeout=1m deploy -n kong-system gateway-operator-controller-manager --for=condition=Available=true

# Undeploy controller from the K8s cluster specified in ~/.kube/config.
# Call with ignore-not-found=true to ignore resource not found errors during deletion.
.PHONY: undeploy
undeploy:
	$(KUSTOMIZE) build config/default | kubectl delete --wait=false --ignore-not-found=$(ignore-not-found) -f -

# Install and connect telepresence to the cluster.
# This target is essential for debugging the operator from an IDE as it establishes
# connectivity between the local development environment and the Kubernetes cluster.
# It allows the locally running operator to interact with the cluster resources
# as if it were running inside the cluster itself.
.PHONY: install.telepresence
install.telepresence: download.telepresence
	@$(PROJECT_DIR)/scripts/telepresence-manager.sh install "$(TELEPRESENCE)"

# Disconnect and uninstall telepresence from the cluster.
# This target cleans up the telepresence resources created by the install.telepresence target.
# It should be used when you're done debugging the operator locally to ensure proper
# cleanup of network connections and cluster resources.
.PHONY: uninstall.telepresence
uninstall.telepresence: download.telepresence
	@$(PROJECT_DIR)/scripts/telepresence-manager.sh uninstall "$(TELEPRESENCE)"
