# ------------------------------------------------------------------------------
# Configuration - Repository
# ------------------------------------------------------------------------------

REPO ?= github.com/kong/gateway-operator
REPO_NAME ?= $(echo ${REPO} | cut -d / -f 3)
REPO_INFO ?= $(shell git config --get remote.origin.url)
TAG ?= $(shell git describe --tags)
VERSION ?= $(shell cat VERSION)

ifndef COMMIT
  COMMIT := $(shell git rev-parse --short HEAD)
endif

# ------------------------------------------------------------------------------
# Configuration - Build
# ------------------------------------------------------------------------------

SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

IMG ?= docker.io/kong/gateway-operator-oss
KUSTOMIZE_IMG_NAME = docker.io/kong/gateway-operator-oss

# ------------------------------------------------------------------------------
# Configuration - Tooling
# ------------------------------------------------------------------------------

ENVTEST_K8S_VERSION = 1.23
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))

.PHONY: _download_tool
_download_tool:
	(cd $(PROJECT_DIR)/third_party && \
		ls ./$(TOOL).go > /dev/null && \
		GOBIN=$(PROJECT_DIR)/bin go generate -tags=third_party ./$(TOOL).go )

.PHONY: _download_tool_own
_download_tool_own:
	(cd $(PROJECT_DIR)/third_party/$(TOOL) && \
		ls ./$(TOOL).go > /dev/null && \
		GOBIN=$(PROJECT_DIR)/bin go generate -tags=third_party ./$(TOOL).go )

.PHONY: tools
tools: envtest kic-role-generator controller-gen kustomize client-gen golangci-lint gotestsum dlv skaffold yq crd-ref-docs

ENVTEST = $(PROJECT_DIR)/bin/setup-envtest
.PHONY: envtest
envtest: ## Download envtest-setup locally if necessary.
	@$(MAKE) _download_tool TOOL=setup-envtest

KIC_ROLE_GENERATOR = $(PROJECT_DIR)/bin/kic-role-generator
.PHONY: kic-role-generator
kic-role-generator:
	( cd ./hack/generators/kic/role-generator && go build -o $(KIC_ROLE_GENERATOR) . )

KIC_WEBHOOKCONFIG_GENERATOR = $(PROJECT_DIR)/bin/kic-webhook-config-generator
.PHONY: kic-webhook-config-generator
kic-webhook-config-generator:
	( cd ./hack/generators/kic/webhook-config-generator && go build -o $(KIC_WEBHOOKCONFIG_GENERATOR) . )

CONTROLLER_GEN = $(PROJECT_DIR)/bin/controller-gen
.PHONY: controller-gen
controller-gen: ## Download controller-gen locally if necessary.
	@$(MAKE) _download_tool TOOL=controller-gen

KUSTOMIZE = $(PROJECT_DIR)/bin/kustomize
.PHONY: kustomize
kustomize: ## Download kustomize locally if necessary.
	@$(MAKE) _download_tool_own TOOL=kustomize

CLIENT_GEN = $(PROJECT_DIR)/bin/client-gen
.PHONY: client-gen
client-gen: ## Download client-gen locally if necessary.
	@$(MAKE) _download_tool TOOL=client-gen

GOLANGCI_LINT = $(PROJECT_DIR)/bin/golangci-lint
.PHONY: golangci-lint
golangci-lint: ## Download golangci-lint locally if necessary.
	@$(MAKE) _download_tool TOOL=golangci-lint

OPM = $(PROJECT_DIR)/bin/opm
.PHONY: opm
opm:
	@$(MAKE) _download_tool TOOL=opm

GOTESTSUM = $(PROJECT_DIR)/bin/gotestsum
.PHONY: gotestsum
gotestsum: ## Download gotestsum locally if necessary.
	@$(MAKE) _download_tool TOOL=gotestsum

CRD_REF_DOCS = $(PROJECT_DIR)/bin/crd-ref-docs
.PHONY: crd-ref-docs
crd-ref-docs: ## Download crd-ref-docs locally if necessary.
	@$(MAKE) _download_tool TOOL=crd-ref-docs

DLV = $(PROJECT_DIR)/bin/dlv
.PHONY: dlv
dlv: ## Download dlv locally if necessary.
	@$(MAKE) _download_tool TOOL=dlv

SKAFFOLD = $(PROJECT_DIR)/bin/skaffold
.PHONY: skaffold
skaffold: ## Download skaffold locally if necessary.
	@$(MAKE) _download_tool_own TOOL=skaffold

YQ = $(PROJECT_DIR)/bin/yq
.PHONY: yq
yq: ## Download yq locally if necessary.
	@$(MAKE) _download_tool_own TOOL=yq

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
	go build -o bin/manager $(GCFLAGS) -ldflags "$(LDFLAGS) \
		-X $(REPO)/modules/manager/metadata.Release=$(TAG) \
		-X $(REPO)/modules/manager/metadata.Commit=$(COMMIT) \
		-X $(REPO)/modules/manager/metadata.Repo=$(REPO_INFO)" \
		main.go

.PHONY: build
build: generate
	$(MAKE) build.operator

.PHONY: lint
lint: golangci-lint
	$(GOLANGCI_LINT) run -v --config .golangci.yaml $(GOLANGCI_LINT_FLAGS)

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

APIS_DIR ?= apis

.PHONY: generate
generate: controller-gen generate.apis generate.clientsets generate.rbacs generate.gateway-api-urls generate.docs generate.k8sio-gomod-replace generate.testcases-registration generate.kic-webhook-config

.PHONY: generate.apis
generate.apis:
	$(CONTROLLER_GEN) object:headerFile="hack/generators/boilerplate.go.txt" paths="./$(APIS_DIR)/..."

# this will generate the custom typed clients needed for end-users implementing logic in Go to use our API types.
.PHONY: generate.clientsets
generate.clientsets: client-gen
	$(CLIENT_GEN) \
		--go-header-file ./hack/generators/boilerplate.go.txt \
		--clientset-name clientset \
		--input-base '' \
		--input $(REPO)/$(APIS_DIR)/v1alpha1 \
		--input $(REPO)/$(APIS_DIR)/v1beta1 \
		--output-base pkg/ \
		--output-package $(REPO)/pkg/ \
		--trim-path-prefix pkg/$(REPO)/

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
generate.kic-webhook-config: kic-webhook-config-generator
	$(KIC_WEBHOOKCONFIG_GENERATOR)

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
CONTROLLER_GEN_PATHS_RAW := ./pkg/utils/kubernetes/resources/clusterroles/ ./pkg/utils/kubernetes/reduce/ ./controllers/... ./$(APIS_DIR)/...
CONTROLLER_GEN_PATHS := $(patsubst %,%;,$(strip $(CONTROLLER_GEN_PATHS_RAW)))

.PHONY: manifests
manifests: controller-gen manifests.versions ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) paths="$(CONTROLLER_GEN_PATHS)" rbac:roleName=manager-role output:rbac:dir=config/rbac/role
	$(CONTROLLER_GEN) paths="$(CONTROLLER_GEN_PATHS)" webhook
	$(CONTROLLER_GEN) paths="$(CONTROLLER_GEN_PATHS)" $(CONTROLLER_GEN_CRD_OPTIONS) +output:crd:artifacts:config=config/crd/bases

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

.PHONY: test
test: test.unit

UNIT_TEST_PATHS := ./controllers/... ./internal/... ./pkg/... ./modules/...

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
		GOTESTFLAGS="-run=BlueGreen $(GOTESTFLAGS)" \
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
		-race \
		-parallel $(PARALLEL) \
		./test/conformance/...

.PHONY: test.conformance
test.conformance:
	@$(MAKE) _test.conformance \
		KGO_PROJECT_URL=$(REPO) \
		KGO_PROJECT_NAME=$(REPO_NAME) \
		KGO_RELEASE=$(TAG)
		GOTESTFLAGS="$(GOTESTFLAGS)"

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
GATEWAY_API_CRDS_STANDARD_URL = github.com/$(GATEWAY_API_REPO)/config/crd/standard?ref=$(GATEWAY_API_VERSION)
GATEWAY_API_CRDS_EXPERIMENTAL_URL = github.com/$(GATEWAY_API_REPO)/config/crd/experimental?ref=$(GATEWAY_API_VERSION)
GATEWAY_API_RAW_REPO_URL = $(GATEWAY_API_RAW_REPO)/$(GATEWAY_API_VERSION)

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
	CONTROLLER_DEVELOPMENT_MODE=true go run ./main.go \
		--no-leader-election \
		-cluster-ca-secret-namespace kong-system \
		-zap-time-encoding iso8601 \
		-enable-controller-controlplane \
		-enable-controller-gateway \
		-enable-controller-aigateway \
		-zap-log-level 2

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
	CONTROLLER_DEVELOPMENT_MODE=true dlv debug ./main.go -- \
		--no-leader-election \
		-cluster-ca-secret-namespace kong-system \
		--enable-controller-aigateway \
		-zap-time-encoding iso8601

.PHONY: debug.skaffold
debug.skaffold: _ensure-kong-system-namespace
	TAG=$(TAG)-debug REPO_INFO=$(REPO_INFO) COMMIT=$(COMMIT) \
		$(SKAFFOLD) debug --port-forward=pods --profile=debug

.PHONY: debug.skaffold.continuous
debug.skaffold.continuous: _ensure-kong-system-namespace
	TAG=$(TAG)-debug REPO_INFO=$(REPO_INFO) COMMIT=$(COMMIT) \
		$(SKAFFOLD) debug --port-forward=pods --profile=debug --auto-build --auto-deploy --auto-sync

# Install CRDs into the K8s cluster specified in ~/.kube/config.
.PHONY: install
install: manifests kustomize install-gateway-api-crds
	$(KUSTOMIZE) build config/crd | kubectl apply --server-side -f -

# Uninstall CRDs from the K8s cluster specified in ~/.kube/config.
# Call with ignore-not-found=true to ignore resource not found errors during deletion.
.PHONY: uninstall
uninstall: manifests kustomize uninstall-gateway-api-crds
	$(KUSTOMIZE) build config/crd | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

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
