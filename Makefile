# ------------------------------------------------------------------------------
# Configuration - Repository
# ------------------------------------------------------------------------------

REPO ?= github.com/kong/gateway-operator
REPO_INFO ?= $(shell git config --get remote.origin.url)
TAG ?= $(shell git describe --tags)

ifndef COMMIT
  COMMIT := $(shell git rev-parse --short HEAD)
endif

# ------------------------------------------------------------------------------
# Configuration - Build
# ------------------------------------------------------------------------------

SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

IMG ?= docker.io/kong/gateway-operator
KUSTOMIZE_IMG_NAME = docker.io/kong/gateway-operator

# ------------------------------------------------------------------------------
# Configuration - OperatorHub
# ------------------------------------------------------------------------------


# Read the version from the VERSION file
VERSION ?= $(shell cat VERSION)

CHANNELS ?= alpha,beta
BUNDLE_CHANNELS := --channels=$(CHANNELS)

DEFAULT_CHANNEL ?= alpha
BUNDLE_DEFAULT_CHANNEL := --default-channel=$(DEFAULT_CHANNEL)

BUNDLE_METADATA_OPTS ?= $(BUNDLE_CHANNELS) $(BUNDLE_DEFAULT_CHANNEL)
IMAGE_TAG_BASE ?= docker.io/kong/gateway-operator
BUNDLE_IMG ?= $(IMAGE_TAG_BASE)-bundle:$(VERSION)
BUNDLE_GEN_FLAGS ?= --overwrite --version $(VERSION) $(BUNDLE_METADATA_OPTS)
USE_IMAGE_DIGESTS ?= false
ifeq ($(USE_IMAGE_DIGESTS), true)
	BUNDLE_GEN_FLAGS += --use-image-digests
endif

BUNDLE_DEFAULT_KUSTOMIZE_MANIFESTS ?= config/manifests
BUNDLE_DEFAULT_DIR ?= bundle/regular

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
	(cd third_party && \
		ls ./$(TOOL).go > /dev/null && \
		GOBIN=$(PROJECT_DIR)/bin go generate -tags=third_party ./$(TOOL).go )

.PHONY: _download_tool_own
_download_tool_own:
	(cd third_party/$(TOOL) && \
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
	( cd ./hack/generators/kic-role-generator && go build -o $(KIC_ROLE_GENERATOR) . )

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

# It seems that there's problem with operator-sdk dependencies when imported from a different project.
# After spending some time on it, decided to just use a 'thing that works' which is to download
# its repo and build the binary separately, not via third_party/go.mod as it's done for other tools.
#
# github.com/kong/gateway-operator/third_party imports
#         github.com/operator-framework/operator-registry/cmd/opm imports
#         github.com/operator-framework/operator-registry/pkg/registry tested by
#         github.com/operator-framework/operator-registry/pkg/registry.test imports
#         github.com/operator-framework/operator-registry/pkg/lib/bundle imports
#         github.com/operator-framework/api/pkg/validation imports
#         github.com/operator-framework/api/pkg/validation/internal imports
#         k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/validation imports
#         k8s.io/apiserver/pkg/util/webhook imports
#         k8s.io/component-base/traces imports
#         go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp imports
#         go.opentelemetry.io/otel/semconv: module go.opentelemetry.io/otel@latest found (v1.9.0), but does not contain package go.opentelemetry.io/otel/semconv
# github.com/kong/gateway-operator/third_party imports
#         github.com/operator-framework/operator-registry/cmd/opm imports
#         github.com/operator-framework/operator-registry/pkg/registry tested by
#         github.com/operator-framework/operator-registry/pkg/registry.test imports
#         github.com/operator-framework/operator-registry/pkg/lib/bundle imports
#         github.com/operator-framework/api/pkg/validation imports
#         github.com/operator-framework/api/pkg/validation/internal imports
#         k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/validation imports
#         k8s.io/apiserver/pkg/util/webhook imports
#         k8s.io/component-base/traces imports
#         go.opentelemetry.io/otel/exporters/otlp imports
#         go.opentelemetry.io/otel/sdk/metric/controller/basic imports
#         go.opentelemetry.io/otel/metric/registry: module go.opentelemetry.io/otel/metric@latest found (v0.31.0), but does not contain package go.opentelemetry.io/otel/metric/registry
OPERATOR_SDK = $(PROJECT_DIR)/bin/operator-sdk
OPERATOR_SDK_VERSION ?= v1.23.0
.PHONY: operator-sdk
operator-sdk:
	@[ -f $(OPERATOR_SDK) ] || { \
	set -e ;\
	TMP_DIR=$$(mktemp -d) ;\
	cd $$TMP_DIR ;\
	git clone https://github.com/operator-framework/operator-sdk ;\
	cd operator-sdk ;\
	git checkout -q $(OPERATOR_SDK_VERSION) ;\
	echo "Checked out operator-sdk at $(OPERATOR_SDK_VERSION)" ;\
	make build/operator-sdk BUILD_DIR=$(PROJECT_DIR)/bin ;\
	rm -rf $$TMP_DIR ;\
	}

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
verify: verify.manifests verify.generators verify.bundle

.PHONY: verify.bundle
verify.bundle: verify.repo bundle verify.diff

.PHONY: verify.diff
verify.diff:
	@./scripts/verify-diff.sh

.PHONY: verify.repo
verify.repo:
	@./scripts/verify-repo.sh

.PHONY: verify.manifests
verify.manifests: verify.repo manifests verify.diff

.PHONY: verify.generators
verify.generators: verify.repo generate verify.diff

# ------------------------------------------------------------------------------
# Build - Generators
# ------------------------------------------------------------------------------

APIS_DIR ?= apis

.PHONY: generate
generate: controller-gen generate.apis generate.clientsets generate.rbacs generate.gateway-api-urls generate.docs

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
CONTROLLER_GEN_PATHS ?= "./internal/utils/kubernetes/resources/clusterroles/;./internal/utils/kubernetes/reduce/;./controllers/...;./$(APIS_DIR)/..."

.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) paths=$(CONTROLLER_GEN_PATHS) rbac:roleName=manager-role
	$(CONTROLLER_GEN) paths=$(CONTROLLER_GEN_PATHS) webhook
	$(CONTROLLER_GEN) paths=$(CONTROLLER_GEN_PATHS) $(CONTROLLER_GEN_CRD_OPTIONS) +output:crd:artifacts:config=config/crd/bases

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
		.

.PHONY: docker.build
docker.build:
	TAG=$(TAG) TARGET=distroless $(MAKE) _docker.build

.PHONY: docker.push
docker.push:
	docker push $(IMG):$(TAG)

# ------------------------------------------------------------------------------
# Build - OperatorHub Bundles
# ------------------------------------------------------------------------------
.PHONY: _bundle
_bundle: manifests kustomize operator-sdk yq
	$(OPERATOR_SDK) generate kustomize manifests --apis-dir=$(APIS_DIR)/
	cd config/manager && $(KUSTOMIZE) edit set image $(KUSTOMIZE_IMG_NAME)=$(IMG):$(VERSION)
	$(YQ) -i e '.metadata.annotations.containerImage |= "$(IMG):$(VERSION)"' \
		 config/manifests/bases/kong-gateway-operator.clusterserviceversion.yaml
	$(KUSTOMIZE) build $(KUSTOMIZE_DIR) | $(OPERATOR_SDK) generate bundle --output-dir=$(BUNDLE_DIR) $(BUNDLE_GEN_FLAGS)
	$(OPERATOR_SDK) bundle validate $(BUNDLE_DIR)
	mv bundle.Dockerfile $(BUNDLE_DIR)

.PHONY: bundle
bundle:
	KUSTOMIZE_DIR=$(BUNDLE_DEFAULT_KUSTOMIZE_MANIFESTS) \
	BUNDLE_DIR=$(BUNDLE_DEFAULT_DIR) \
		$(MAKE) _bundle

.PHONY: bundle.build
bundle.build: ## Build the bundle image.
	docker build -f $(BUNDLE_DEFAULT_DIR)/bundle.Dockerfile -t $(BUNDLE_IMG) .

.PHONY: bundle-push
bundle-push: ## Push the bundle image.
	$(MAKE) docker-push IMG=$(BUNDLE_IMG)

# A comma-separated list of bundle images (e.g. make catalog-build BUNDLE_IMGS=example.com/operator-bundle:v0.1.0,example.com/operator-bundle:v0.2.0).
# These images MUST exist in a registry and be pull-able.
BUNDLE_IMGS ?= $(BUNDLE_IMG)

# The image tag given to the resulting catalog image (e.g. make catalog-build CATALOG_IMG=example.com/operator-catalog:v0.2.0).
CATALOG_IMG ?= $(IMAGE_TAG_BASE)-catalog:v$(VERSION)

# Set CATALOG_BASE_IMG to an existing catalog image tag to add $BUNDLE_IMGS to that image.
ifneq ($(origin CATALOG_BASE_IMG), undefined)
FROM_INDEX_OPT := --from-index $(CATALOG_BASE_IMG)
endif

# Build a catalog image by adding bundle images to an empty catalog using the operator package manager tool, 'opm'.
# This recipe invokes 'opm' in 'semver' bundle add mode. For more information on add modes, see:
# https://github.com/operator-framework/community-operators/blob/7f1438c/docs/packaging-operator.md#updating-your-existing-operator
.PHONY: catalog-build
catalog-build: opm ## Build a catalog image.
	$(OPM) index add --container-tool docker --mode semver --tag $(CATALOG_IMG) --bundles $(BUNDLE_IMGS) $(FROM_INDEX_OPT)

# Push the catalog image.
.PHONY: catalog-push
catalog-push: ## Push a catalog image.
	$(MAKE) docker-push IMG=$(CATALOG_IMG)

# ------------------------------------------------------------------------------
# Testing
# ------------------------------------------------------------------------------

GOTESTSUM_FORMAT ?= standard-verbose
INTEGRATION_TEST_TIMEOUT ?= "30m"
CONFORMANCE_TEST_TIMEOUT ?= "20m"

.PHONY: test
test: test.unit

.PHONY: _test.unit
_test.unit: gotestsum
	GOTESTSUM_FORMAT=$(GOTESTSUM_FORMAT) \
		$(GOTESTSUM) -- $(GOTESTFLAGS) \
		-race \
		-coverprofile=coverage.unit.out \
		./controllers/... \
		./internal/... \
		./pkg/...

.PHONY: test.unit
test.unit:
	@$(MAKE) _test.unit GOTESTFLAGS="$(GOTESTFLAGS)"

.PHONY: test.unit.pretty
test.unit.pretty:
	@$(MAKE) _test.unit GOTESTSUM_FORMAT=pkgname GOTESTFLAGS="$(GOTESTFLAGS)"

.PHONY: _test.integration
_test.integration: gotestsum
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
		GOTESTFLAGS="$(GOTESTFLAGS)" \
		COVERPROFILE="coverage.integration.out" \
		GOFLAGS="-tags=integration_tests"

.PHONY: test.integration_bluegreen
test.integration_bluegreen:
	@$(MAKE) _test.integration \
		GATEWAY_OPERATOR_BLUEGREEN_CONTROLLER="true" \
		GOTESTFLAGS="$(GOTESTFLAGS)" \
		COVERPROFILE="coverage.integration-bluegreen.out" \
		GOFLAGS="-tags=integration_tests_bluegreen"

.PHONY: _test.e2e
_test.e2e: gotestsum
	GOFLAGS="-tags=e2e_tests" \
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
	GOFLAGS="-tags=conformance_tests" \
		GOTESTSUM_FORMAT=$(GOTESTSUM_FORMAT) \
		$(GOTESTSUM) -- $(GOTESTFLAGS) \
		-timeout $(CONFORMANCE_TEST_TIMEOUT) \
		-race \
		-parallel $(PARALLEL) \
		./test/conformance/...

.PHONY: test.conformance
test.conformance:
	@$(MAKE) _test.conformance \
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
		OUTPUT=$(shell pwd)/internal/utils/test/zz_generated_gateway_api.go \
		go generate -tags=generate_gateway_api_urls ./internal/utils/cmd/generate-gateway-api-urls

.PHONY: go-mod-download-gateway-api
go-mod-download-gateway-api:
	@go mod download $(GATEWAY_API_PACKAGE)

.PHONY: install-gateway-api-crds
install-gateway-api-crds: go-mod-download-gateway-api kustomize
	$(KUSTOMIZE) build $(GATEWAY_API_CRDS_LOCAL_PATH) | kubectl apply -f -
	$(KUSTOMIZE) build $(GATEWAY_API_CRDS_LOCAL_PATH)/experimental | kubectl apply -f -

.PHONY: uninstall-gateway-api-crds
uninstall-gateway-api-crds: go-mod-download-gateway-api kustomize
	$(KUSTOMIZE) build $(GATEWAY_API_CRDS_LOCAL_PATH) | kubectl delete -f -
	$(KUSTOMIZE) build $(GATEWAY_API_CRDS_LOCAL_PATH)/experimental | kubectl delete -f -

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
		-zap-log-level 2

SKAFFOLD_RUN_PROFILE ?= dev

.PHONY: _skaffold
_skaffold: skaffold
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
install: manifests kustomize
	$(KUSTOMIZE) build config/crd | kubectl apply --server-side -f -

# Uninstall CRDs from the K8s cluster specified in ~/.kube/config.
# Call with ignore-not-found=true to ignore resource not found errors during deletion.
.PHONY: uninstall
uninstall: manifests kustomize
	$(KUSTOMIZE) build config/crd | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

# Deploy controller to the K8s cluster specified in ~/.kube/config.
# This will wait for operator's Deployment to get Available.
# This uses a temporary directory becuase "kustomize edit set image" would introduce
# a change in current work tree which we do not want.
.PHONY: deploy
deploy: manifests kustomize
	$(eval TMP := $(shell mktemp -d))
	cp -R config $(TMP)
	cd $(TMP)/config/manager && $(KUSTOMIZE) edit set image $(KUSTOMIZE_IMG_NAME)=$(IMG):$(VERSION)
	cd $(TMP)/config/default && $(KUSTOMIZE) build . | kubectl apply -f -
	kubectl wait --timeout=1m deploy -n kong-system gateway-operator-controller-manager --for=condition=Available=true

# Undeploy controller from the K8s cluster specified in ~/.kube/config.
# Call with ignore-not-found=true to ignore resource not found errors during deletion.
.PHONY: undeploy
undeploy:
	$(KUSTOMIZE) build config/default | kubectl delete --wait=false --ignore-not-found=$(ignore-not-found) -f -
