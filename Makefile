# ------------------------------------------------------------------------------
# Configuration - Repository
# ------------------------------------------------------------------------------

REPO ?= github.com/kong/gateway-operator
REPO_INFO ?= $(shell git config --get remote.origin.url)
TAG ?= $(shell git describe --tags)

ifndef COMMIT
  COMMIT := $(shell git rev-parse --short HEAD)
endif

.PHONY: submodule
submodule: ## Initialize and update git submodules if needed
	@-if ! scripts/verify-submodules-status.sh; then \
		make submodule.init; \
	fi

.PHONY: submodule.init
submodule.init: ## Initialize and update git submodules
	git submodule sync --recursive
	git submodule update --init --recursive

.PHONY: submodule.init-force
submodule.init-force: ## Reinitialize and update git submodules
	git submodule sync --recursive
	git submodule update --init --recursive --force

# ------------------------------------------------------------------------------
# Configuration - Build
# ------------------------------------------------------------------------------

SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

IMG ?= ghcr.io/kong/gateway-operator
RHTAG ?= latest-redhat

# ------------------------------------------------------------------------------
# Configuration - OperatorHub
# ------------------------------------------------------------------------------


# Read the version from the VERSION file
VERSION ?= $(shell cat VERSION)

CHANNELS ?= alpha
BUNDLE_CHANNELS := --channels=$(CHANNELS)

DEFAULT_CHANNEL ?= alpha
BUNDLE_DEFAULT_CHANNEL := --default-channel=$(DEFAULT_CHANNEL)

BUNDLE_METADATA_OPTS ?= $(BUNDLE_CHANNELS) $(BUNDLE_DEFAULT_CHANNEL)
IMAGE_TAG_BASE ?= ghcr.io/kong/gateway-operator
BUNDLE_IMG ?= $(IMAGE_TAG_BASE)-bundle:$(VERSION)
BUNDLE_GEN_FLAGS ?= --overwrite --version $(VERSION) $(BUNDLE_METADATA_OPTS)
USE_IMAGE_DIGESTS ?= false
ifeq ($(USE_IMAGE_DIGESTS), true)
	BUNDLE_GEN_FLAGS += --use-image-digests
endif

BUNDLE_DEFAULT_KUSTOMIZE_MANIFESTS ?= config/manifests
BUNDLE_DEFAULT_DIR ?= bundle/regular
BUNDLE_DEFAULT_DOCKERFILE ?= bundle_regular.Dockerfile

BUNDLE_RED_HAT_KUSTOMIZE_MANIFESTS ?= config/redhat-certified
BUNDLE_RED_HAT_DIR ?= bundle/redhat-certified
BUNDLE_RED_HAT_DOCKERFILE ?= bundle_redhat_certified.Dockerfile

OPENSHIFT_SUPPORTED_VERSIONS ?= v4.10-v4.11

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
	(cd third_party && GOBIN=$(PROJECT_DIR)/bin go generate -tags=third_party ./$(TOOL).go )

.PHONY: tools
tools: envtest kic-role-generator controller-gen kustomize client-gen golangci-lint gotestsum

ENVTEST = $(PROJECT_DIR)/bin/setup-envtest
.PHONY: envtest
envtest: ## Download envtest-setup locally if necessary.
	@$(MAKE) _download_tool TOOL=setup-envtest

KIC_ROLE_GENERATOR = $(PROJECT_DIR)/bin/kic-role-generator
.PHONY: kic-role-generator
kic-role-generator: submodule
	go build -o $(KIC_ROLE_GENERATOR) ./hack/generators/kic-role-generator

CONTROLLER_GEN = $(PROJECT_DIR)/bin/controller-gen
.PHONY: controller-gen
controller-gen: ## Download controller-gen locally if necessary.
	@$(MAKE) _download_tool TOOL=controller-gen

KUSTOMIZE = $(PROJECT_DIR)/bin/kustomize
.PHONY: kustomize
kustomize: ## Download kustomize locally if necessary.
	@$(MAKE) _download_tool TOOL=kustomize

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
	go build -o bin/manager -ldflags "-s -w \
		-X $(REPO)/internal/manager/metadata.Release=$(TAG) \
		-X $(REPO)/internal/manager/metadata.Commit=$(COMMIT) \
		-X $(REPO)/internal/manager/metadata.Repo=$(REPO_INFO)" \
		main.go

.PHONY: build
build: generate fmt vet lint submodule
	$(MAKE) build.operator

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: lint
lint: golangci-lint
	$(GOLANGCI_LINT) run -v

.PHONY: verify.bundle
verify.bundle: verify.repo bundle.regular bundle.redhat-certified verify.diff

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
generate: controller-gen generate.apis generate.clientsets generate.rbacs

.PHONY: generate.apis
generate.apis:
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./$(APIS_DIR)/..."

# this will generate the custom typed clients needed for end-users implementing logic in Go to use our API types.
.PHONY: generate.clientsets
generate.clientsets: client-gen
	$(CLIENT_GEN) \
		--go-header-file ./hack/boilerplate.go.txt \
		--clientset-name clientset \
		--input-base '' \
		--input $(REPO)/$(APIS_DIR)/v1alpha1 \
		--output-base pkg/ \
		--output-package $(REPO)/pkg/ \
		--trim-path-prefix pkg/$(REPO)/

.PHONY: generate.rbacs
generate.rbacs: kic-role-generator
	$(KIC_ROLE_GENERATOR) --force

# ------------------------------------------------------------------------------
# Files generation checks
# ------------------------------------------------------------------------------

.PHONY: check.rbacs
check.rbacs: kic-role-generator
	$(KIC_ROLE_GENERATOR) --fail-on-error

# ------------------------------------------------------------------------------
# Build - Manifests
# ------------------------------------------------------------------------------

.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./controllers/...;./$(APIS_DIR)/..." output:crd:artifacts:config=config/crd/bases

# ------------------------------------------------------------------------------
# Build - Container Images
# ------------------------------------------------------------------------------

.PHONY: _docker.build
_docker.build:
	docker build -t $(IMG):$(TAG) \
		--target $(TARGET) \
		--build-arg TAG=$(TAG) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg REPO_INFO=$(REPO_INFO) \
		.

.PHONY: docker.build
docker.build:
	TAG=$(TAG) TARGET=distroless $(MAKE) _docker.build

.PHONY: docker.build.redhat
docker.build.redhat:
	TAG=$(RHTAG) TARGET=redhat $(MAKE) _docker.build

.PHONY: docker.push
docker.push:
	docker push $(IMG):$(TAG)

# ------------------------------------------------------------------------------
# Build - OperatorHub Bundles
# ------------------------------------------------------------------------------
.PHONY: _bundle
_bundle: manifests kustomize operator-sdk
	$(OPERATOR_SDK) generate kustomize manifests --apis-dir=$(APIS_DIR)/
	cd config/manager && $(KUSTOMIZE) edit set image $(IMG)=$(IMG):$(VERSION)
	yq -i e '.metadata.annotations.containerImage |= "$(IMG):$(VERSION)"' \
		 config/manifests/bases/kong-gateway-operator.clusterserviceversion.yaml
	$(KUSTOMIZE) build $(KUSTOMIZE_DIR) | $(OPERATOR_SDK) generate bundle --output-dir=$(BUNDLE_DIR) $(BUNDLE_GEN_FLAGS)
	$(OPERATOR_SDK) bundle validate $(BUNDLE_DIR)
	mv bundle.Dockerfile $(BUNDLE_DIR)

.PHONY: bundle.regular
bundle.regular:
	KUSTOMIZE_DIR=$(BUNDLE_DEFAULT_KUSTOMIZE_MANIFESTS) \
	BUNDLE_DIR=$(BUNDLE_DEFAULT_DIR) \
		$(MAKE) _bundle

.PHONY: bundle.redhat-certified
bundle.redhat-certified:
	KUSTOMIZE_DIR=$(BUNDLE_RED_HAT_KUSTOMIZE_MANIFESTS) \
	BUNDLE_DIR=$(BUNDLE_RED_HAT_DIR) \
		$(MAKE) _bundle
	yq -i e '.annotations."com.redhat.openshift.versions" = "$(OPENSHIFT_SUPPORTED_VERSIONS)"' \
		$(BUNDLE_RED_HAT_DIR)/metadata/annotations.yaml
	echo "# Annotations for OpenShift." >> $(BUNDLE_RED_HAT_DIR)/bundle.Dockerfile
	echo "# https://redhat-connect.gitbook.io/certified-operator-guide/ocp-deployment/operator-metadata/bundle-directory" >> $(BUNDLE_RED_HAT_DIR)/bundle.Dockerfile
	echo "LABEL com.redhat.openshift.versions=\"$(OPENSHIFT_SUPPORTED_VERSIONS)\"" >> $(BUNDLE_RED_HAT_DIR)/bundle.Dockerfile
	echo "LABEL com.redhat.delivery.operator.bundle=true" >> $(BUNDLE_RED_HAT_DIR)/bundle.Dockerfile

.PHONY: bundle.regular.build
bundle.regular.build: ## Build the bundle image.
	docker build -f $(BUNDLE_DEFAULT_DIR)/bundle.Dockerfile -t $(BUNDLE_IMG) .

.PHONY: bundle.redhat-certified.build
bundle.redhat-certified.build: ## Build the bundle image.
	docker build -f $(BUNDLE_RED_HAT_DIR)/bundle.Dockerfile -t $(BUNDLE_IMG) .

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
	GOFLAGS="-tags=integration_tests" \
		GOTESTSUM_FORMAT=$(GOTESTSUM_FORMAT) \
		$(GOTESTSUM) -- $(GOTESTFLAGS) \
		-race \
		-coverprofile=coverage.integration.out \
		./test/integration/...

.PHONY: test.integration
test.integration:
	@$(MAKE) _test.integration \
		GOTESTFLAGS="$(GOTESTFLAGS)"

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

.PHONY: _test.conformance
_test.conformance: gotestsum
	GOFLAGS="-tags=conformance_tests" \
		GOTESTSUM_FORMAT=$(GOTESTSUM_FORMAT) \
		$(GOTESTSUM) -- $(GOTESTFLAGS) \
		-race \
		./test/conformance/...

.PHONY: test.conformance
test.conformance:
	@$(MAKE) _test.conformance \
		GOTESTFLAGS="$(GOTESTFLAGS)"

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
run: webhook-certs-dir manifests generate fmt vet install _ensure-kong-system-namespace
	kubectl kustomize https://github.com/kubernetes-sigs/gateway-api.git/config/crd?ref=main | kubectl apply -f -
	@$(MAKE) _run

# Run the operator without checking any preconditions, installing CRDs etc.
# This is mostly useful when 'run' was run at least once on a server and CRDs, RBACs
# etc didn't change in between the runs.
.PHONY: _run
_run:
	CONTROLLER_DEVELOPMENT_MODE=true go run ./main.go \
		--no-leader-election \
		-cluster-ca-secret-namespace kong-system \
		-zap-time-encoding iso8601

.PHONY: debug
debug: webhook-certs-dir manifests generate fmt vet install _ensure-kong-system-namespace
	CONTROLLER_DEVELOPMENT_MODE=true dlv debug ./main.go -- \
		--no-leader-election \
		-cluster-ca-secret-namespace kong-system \
		-zap-time-encoding iso8601

# Install CRDs into the K8s cluster specified in ~/.kube/config.
.PHONY: install
install: manifests kustomize
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

# Uninstall CRDs from the K8s cluster specified in ~/.kube/config.
# Call with ignore-not-found=true to ignore resource not found errors during deletion.
.PHONY: uninstall
uninstall: manifests kustomize
	$(KUSTOMIZE) build config/crd | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

# Deploy controller to the K8s cluster specified in ~/.kube/config.
.PHONY: deploy
deploy: manifests kustomize
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/default | kubectl apply -f -

# Undeploy controller from the K8s cluster specified in ~/.kube/config.
# Call with ignore-not-found=true to ignore resource not found errors during deletion.
.PHONY: undeploy
undeploy:
	$(KUSTOMIZE) build config/default | kubectl delete --ignore-not-found=$(ignore-not-found) -f -
