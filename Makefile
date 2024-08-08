# ------------------------------------------------------------------------------
# Configuration - Repository
# ------------------------------------------------------------------------------

REPO ?= github.com/kong/gateway-operator
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

IMG ?= docker.io/kong/gateway-operator-oss
KUSTOMIZE_IMG_NAME = docker.io/kong/gateway-operator-oss

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
tools: kic-role-generator controller-gen kustomize client-gen golangci-lint gotestsum skaffold yq crd-ref-docs

MISE := $(shell which mise)
.PHONY: mise
mise:
	@mise -V >/dev/null || (echo "mise - https://github.com/jdx/mise - not found. Please install it." && exit 1)

KIC_ROLE_GENERATOR = $(PROJECT_DIR)/bin/kic-role-generator
.PHONY: kic-role-generator
kic-role-generator:
	( cd ./hack/generators/kic/role-generator && go build -o $(KIC_ROLE_GENERATOR) . )

KIC_WEBHOOKCONFIG_GENERATOR = $(PROJECT_DIR)/bin/kic-webhook-config-generator
.PHONY: kic-webhook-config-generator
kic-webhook-config-generator:
	( cd ./hack/generators/kic/webhook-config-generator && go build -o $(KIC_WEBHOOKCONFIG_GENERATOR) . )

export MISE_DATA_DIR = $(PROJECT_DIR)/bin/

# Do not store yq's version in .tools_versions.yaml as it is used to get tool versions.
# renovate: datasource=github-releases depName=mikefarah/yq
YQ_VERSION = 4.43.1
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

GOLANGCI_LINT_CONFIG ?= $(PROJECT_DIR)/.golangci.yaml
.PHONY: lint
lint: golangci-lint
	$(GOLANGCI_LINT) run -v --config $(GOLANGCI_LINT_CONFIG) $(GOLANGCI_LINT_FLAGS)

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
generate: generate.api generate.clientsets generate.rbacs generate.gateway-api-urls generate.docs generate.k8sio-gomod-replace generate.testcases-registration generate.kic-webhook-config

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
.PHONY: generate.rbacs
generate.rbacs: kic-role-generator
	$(KIC_ROLE_GENERATOR) --force

.PHONY: generate.docs
generate.docs: crd-ref-docs
	./scripts/apidocs-gen/generate.sh $(CRD_REF_DOCS)

.PHONY: generate.k8sio-gomod-replace
generate.k8sio-gomod-replace:
	./hack/update-k8sio-gomod-replace.sh

.PHONY: generate.testcases-registration
generate.testcases-registration:
	go run ./hack/generators/testcases-registration/main.go

.PHONY: generate.kic-webhook-config
generate.kic-webhook-config: kustomize kic-webhook-config-generator
	KUSTOMIZE=$(KUSTOMIZE) $(KIC_WEBHOOKCONFIG_GENERATOR)

# ------------------------------------------------------------------------------
# Files generation checks
# ------------------------------------------------------------------------------

.PHONY: check.rbacs
check.rbacs: kic-role-generator
	$(KIC_ROLE_GENERATOR) --fail-on-error

# ------------------------------------------------------------------------------
# Build - Manifests
# ------------------------------------------------------------------------------

CONTROLLER_GEN_CRD_OPTIONS ?= "+crd:generateEmbeddedObjectMeta=true"
CONTROLLER_GEN_PATHS_RAW := ./pkg/utils/kubernetes/resources/clusterroles/ ./pkg/utils/kubernetes/reduce/ ./controller/... ./$(API_DIR)/...
CONTROLLER_GEN_PATHS := $(patsubst %,%;,$(strip $(CONTROLLER_GEN_PATHS_RAW)))
CONFIG_CRD_PATH = config/crd
CONFIG_CRD_BASE_PATH = $(CONFIG_CRD_PATH)/bases

.PHONY: manifests
manifests: controller-gen manifests.versions ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) paths="$(CONTROLLER_GEN_PATHS)" rbac:roleName=manager-role output:rbac:dir=config/rbac/role
	$(CONTROLLER_GEN) paths="$(CONTROLLER_GEN_PATHS)" webhook
	$(CONTROLLER_GEN) paths="$(CONTROLLER_GEN_PATHS)" $(CONTROLLER_GEN_CRD_OPTIONS) +output:crd:artifacts:config=$(CONFIG_CRD_BASE_PATH)
	cp $(CONFIG_CRD_BASE_PATH)/gateway-operator.konghq.com_dataplanes.yaml $(CONFIG_CRD_PATH)/dataplane/

# manifests.versions ensures that image versions are set in the manifests according to the current version.
.PHONY: manifests.versions
manifests.versions: kustomize yq
	cd config/components/manager-image/ && $(KUSTOMIZE) edit set image $(KUSTOMIZE_IMG_NAME)=$(IMG):$(VERSION)

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

# ------------------------------------------------------------------------------
# Testing
# ------------------------------------------------------------------------------

GOTESTSUM_FORMAT ?= standard-verbose
INTEGRATION_TEST_TIMEOUT ?= "30m"
CONFORMANCE_TEST_TIMEOUT ?= "20m"
E2E_TEST_TIMEOUT ?= "20m"

.PHONY: test
test: test.unit

UNIT_TEST_PATHS := ./controller/... ./internal/... ./pkg/... ./modules/...

.PHONY: _test.unit
_test.unit: gotestsum
	GOTESTSUM_FORMAT=$(GOTESTSUM_FORMAT) \
		$(GOTESTSUM) -- $(GOTESTFLAGS) \
		-race \
		-coverprofile=coverage.unit.out \
		$(UNIT_TEST_PATHS)

.PHONY: test.unit
test.unit:
	@$(MAKE) _test.unit GOTESTFLAGS="$(GOTESTFLAGS)"

.PHONY: test.unit.pretty
test.unit.pretty:
	@$(MAKE) _test.unit GOTESTSUM_FORMAT=pkgname GOTESTFLAGS="$(GOTESTFLAGS)" UNIT_TEST_PATHS="$(UNIT_TEST_PATHS)"

.PHONY: _test.integration
_test.integration: webhook-certs-dir gotestsum
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
		GOTESTFLAGS="-skip='BlueGreen|TestGatewayProvisionDataPlaneFail' $(GOTESTFLAGS)" \
		COVERPROFILE="coverage.integration.out"

.PHONY: test.integration_bluegreen
test.integration_bluegreen:
	@$(MAKE) _test.integration \
		GATEWAY_OPERATOR_BLUEGREEN_CONTROLLER="true" \
		GOTESTFLAGS="-run='BlueGreen|TestDataPlane' $(GOTESTFLAGS)" \
		COVERPROFILE="coverage.integration-bluegreen.out" \

.PHONY: test.integration_provision_dataplane_fail
test.integration_provision_dataplane_fail:
	@$(MAKE) _test.integration \
		WEBHOOK_ENABLED=true \
		GOTESTFLAGS="-run=TestGatewayProvisionDataPlaneFail $(GOTESTFLAGS)" \
		COVERPROFILE="coverage.integration.out"

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
_test.conformance: gotestsum
		GOTESTSUM_FORMAT=$(GOTESTSUM_FORMAT) \
		$(GOTESTSUM) -- $(GOTESTFLAGS) \
		-timeout $(CONFORMANCE_TEST_TIMEOUT) \
		-ldflags "$(LDFLAGS_COMMON) $(LDFLAGS) $(LDFLAGS_METADATA)" \
		-race \
		-parallel $(PARALLEL) \
		./test/conformance/...

.PHONY: test.conformance
test.conformance:
	@$(MAKE) _test.conformance \
		GOTESTFLAGS="$(GOTESTFLAGS)"

.PHONY: test.samples
test.samples: kustomize
	find ./config/samples -not -name "kustomization.*" -type f | sort | xargs -I{} bash -c "kubectl apply -f {}; kubectl delete -f {}"

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
GATEWAY_API_REPO ?= kubernetes-sigs/gateway-api
GATEWAY_API_RAW_REPO ?= https://raw.githubusercontent.com/$(GATEWAY_API_REPO)
GATEWAY_API_CRDS_STANDARD_URL = github.com/$(GATEWAY_API_REPO)/config/crd?ref=$(GATEWAY_API_VERSION)
GATEWAY_API_CRDS_EXPERIMENTAL_URL = github.com/$(GATEWAY_API_REPO)/config/crd/experimental?ref=$(GATEWAY_API_VERSION)
GATEWAY_API_RAW_REPO_URL = $(GATEWAY_API_RAW_REPO)/$(GATEWAY_API_VERSION)

KIC_REPO ?= kong/kubernetes-ingress-controller
KIC_PACKAGE ?= github.com/$(KIC_REPO)/v3
KIC_VERSION ?= $(shell go list -m -f '{{ .Version }}' $(KIC_PACKAGE))
KIC_CRDS_URL ?= github.com/$(KIC_REPO)/config/crd?ref=$(KIC_VERSION)

.PHONY: generate.gateway-api-urls
generate.gateway-api-urls:
	CRDS_STANDARD_URL="$(GATEWAY_API_CRDS_STANDARD_URL)" \
		CRDS_EXPERIMENTAL_URL="$(GATEWAY_API_CRDS_EXPERIMENTAL_URL)" \
		RAW_REPO_URL="$(GATEWAY_API_RAW_REPO_URL)" \
		INPUT=$(shell pwd)/internal/utils/cmd/generate-gateway-api-urls/gateway_consts.tmpl \
		OUTPUT=$(shell pwd)/pkg/utils/test/zz_generated_gateway_api.go \
		go generate -tags=generate_gateway_api_urls ./internal/utils/cmd/generate-gateway-api-urls

.PHONY: go-mod-download-gateway-api
go-mod-download-gateway-api:
	@go mod download $(GATEWAY_API_PACKAGE)

.PHONY: install-gateway-api-crds
install-gateway-api-crds: go-mod-download-gateway-api kustomize
	$(KUSTOMIZE) build $(GATEWAY_API_CRDS_LOCAL_PATH) | kubectl apply -f -

.PHONY: uninstall-gateway-api-crds
uninstall-gateway-api-crds: go-mod-download-gateway-api kustomize
	$(KUSTOMIZE) build $(GATEWAY_API_CRDS_LOCAL_PATH) | kubectl delete -f -

# ------------------------------------------------------------------------------
# Debug
# ------------------------------------------------------------------------------

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: webhook-certs-dir
webhook-certs-dir:
	@mkdir -p /tmp/k8s-webhook-server/serving-certs/

.PHONY: _ensure-kong-system-namespace
_ensure-kong-system-namespace:
	@kubectl create ns kong-system 2>/dev/null || true

# Run a controller from your host.
# TODO: In order not to rely on 'main' version of Gateway API CRDs address but
# on the tag that is used in code (defined in go.mod) address this by solving
# https://github.com/Kong/gateway-operator/pull/480.
.PHONY: run
run: webhook-certs-dir manifests generate install-gateway-api-crds install _ensure-kong-system-namespace
	@$(MAKE) _run

# Run the operator without checking any preconditions, installing CRDs etc.
# This is mostly useful when 'run' was run at least once on a server and CRDs, RBACs
# etc didn't change in between the runs.
.PHONY: _run
_run:
	GATEWAY_OPERATOR_DEVELOPMENT_MODE=true go run ./cmd/main.go \
		--no-leader-election \
		-cluster-ca-secret-namespace kong-system \
		-enable-controller-controlplane \
		-enable-controller-gateway \
		-enable-controller-aigateway \
		-zap-time-encoding iso8601 \
		-zap-log-level 2 \
		-zap-devel true

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
debug: webhook-certs-dir manifests generate install _ensure-kong-system-namespace
	GATEWAY_OPERATOR_DEVELOPMENT_MODE=true dlv debug ./main.go -- \
		--no-leader-election \
		-cluster-ca-secret-namespace kong-system \
		--enable-controller-aigateway \
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

# Install CRDs into the K8s cluster specified in ~/.kube/config.
.PHONY: install
install: manifests kustomize install-gateway-api-crds
	$(KUSTOMIZE) build $(KIC_CRDS_URL) | kubectl apply -f -
	$(KUSTOMIZE) build config/crd | kubectl apply --server-side -f -

KUBERNETES_CONFIGURATION_CRDS_PACKAGE ?= github.com/kong/kubernetes-configuration
KUBERNETES_CONFIGURATION_CRDS_VERSION ?= $(shell go list -m -f '{{ .Version }}' $(KUBERNETES_CONFIGURATION_CRDS_PACKAGE))
KUBERNETES_CONFIGURATION_CRDS_CRDS_LOCAL_PATH = $(shell go env GOPATH)/pkg/mod/$(KUBERNETES_CONFIGURATION_CRDS_PACKAGE)@$(KUBERNETES_CONFIGURATION_CRDS_VERSION)/config/crd

# Install kubernetes-configuration CRDs into the K8s cluster specified in ~/.kube/config.
.PHONY: install.kubernetes-configuration-crds
install.kubernetes-configuration-crds: kustomize
	$(KUSTOMIZE) build $(KUBERNETES_CONFIGURATION_CRDS_CRDS_LOCAL_PATH) | kubectl apply -f -

# Install standard and experimental CRDs into the K8s cluster specified in ~/.kube/config.
.PHONY: install.all
install.all: manifests kustomize install-gateway-api-crds install.kubernetes-configuration-crds
	$(KUSTOMIZE) build $(KIC_CRDS_URL) | kubectl apply -f -
	kubectl apply --server-side -f $(PROJECT_DIR)/config/crd/bases/
	kubectl get crd -ojsonpath='{.items[*].metadata.name}' | xargs -n1 kubectl wait --for condition=established crd

# Uninstall CRDs from the K8s cluster specified in ~/.kube/config.
# Call with ignore-not-found=true to ignore resource not found errors during deletion.
.PHONY: uninstall
uninstall: manifests kustomize uninstall-gateway-api-crds
	$(KUSTOMIZE) build $(KIC_CRDS_URL) | kubectl delete --ignore-not-found=$(ignore-not-found) -f -
	$(KUSTOMIZE) build config/crd | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: uninstall.kubernetes-configuration-crds
uninstall.kubernetes-configuration-crds: kustomize
	$(KUSTOMIZE) build $(KUBERNETES_CONFIGURATION_CRDS_CRDS_LOCAL_PATH) | kubectl delete -f -

# Uninstall standard and experimental CRDs from the K8s cluster specified in ~/.kube/config.
# Call with ignore-not-found=true to ignore resource not found errors during deletion.
.PHONY: uninstall.all
uninstall.all: manifests kustomize uninstall-gateway-api-crds uninstall.kubernetes-configuration-crds
	$(KUSTOMIZE) build $(KIC_CRDS_URL) | kubectl apply -f -
	kubectl delete --ignore-not-found=$(ignore-not-found) -f $(PROJECT_DIR)/config/crd/bases/

# Deploy controller to the K8s cluster specified in ~/.kube/config.
# This will wait for operator's Deployment to get Available.
# This uses a temporary directory becuase "kustomize edit set image" would introduce
# a change in current work tree which we do not want.
.PHONY: deploy
deploy: manifests kustomize
	$(eval TMP := $(shell mktemp -d))
	cp -R config $(TMP)
	cd $(TMP)/config/components/manager-image/ && $(KUSTOMIZE) edit set image $(KUSTOMIZE_IMG_NAME)=$(IMG):$(VERSION)
	cd $(TMP)/config/default && $(KUSTOMIZE) build . | kubectl apply -f -
	kubectl wait --timeout=1m deploy -n kong-system gateway-operator-controller-manager --for=condition=Available=true

# Undeploy controller from the K8s cluster specified in ~/.kube/config.
# Call with ignore-not-found=true to ignore resource not found errors during deletion.
.PHONY: undeploy
undeploy:
	$(KUSTOMIZE) build config/default | kubectl delete --wait=false --ignore-not-found=$(ignore-not-found) -f -
