# ------------------------------------------------------------------------------
# Configuration - Make
# ------------------------------------------------------------------------------

# Some sensible Make defaults: https://tech.davis-hansson.com/p/make/
SHELL := bash
.SHELLFLAGS := -eu -o pipefail -c

# ------------------------------------------------------------------------------
# Configuration - Golang
# ------------------------------------------------------------------------------

ifeq (Darwin,$(shell uname -s))
LDFLAGS_COMMON ?= -extldflags=-Wl,-ld_classic
endif

# ------------------------------------------------------------------------------
# Configuration - Tooling
# ------------------------------------------------------------------------------

PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))

TOOLS_VERSIONS_FILE = ../.tools_versions.yaml

MISE := $(shell which mise)
.PHONY: mise
mise:
	@mise -V >/dev/null || (echo "mise - https://github.com/jdx/mise - not found. Please install it." && exit 1)

export MISE_DATA_DIR = $(PROJECT_DIR)/bin/

# NOTE: mise targets use -q to silence the output.
# Users can use MISE_VERBOSE=1 MISE_DEBUG=1 to get more verbose output.

.PHONY: mise-plugin-install
mise-plugin-install: mise
	@$(MISE) plugin install --yes -q $(DEP) $(URL)

.PHONY: mise-install
mise-install: mise
	@$(MISE) install -q $(DEP_VER)

# Do not store yq's version in .tools_versions.yaml as it is used to get tool versions.
# renovate: datasource=github-releases depName=mikefarah/yq
YQ_VERSION = 4.49.2
YQ = $(PROJECT_DIR)/bin/installs/yq/$(YQ_VERSION)/bin/yq
.PHONY: yq
yq: mise # Download yq locally if necessary.
	@$(MAKE) mise-plugin-install DEP=yq
	@$(MAKE) mise-install DEP_VER=yq@$(YQ_VERSION)

# ------------------------------------------------------------------------------
# Testing
# ------------------------------------------------------------------------------

NCPU ?= $(shell getconf _NPROCESSORS_ONLN)
PKG_LIST = ./pkg/...,./internal/...
INTEGRATION_TEST_TIMEOUT ?= "45m"
KONG_CONTROLLER_FEATURE_GATES ?= GatewayAlpha=true
JUNIT_REPORT ?= /dev/null

TEST_KONG_HELM_CHART_VERSION ?= $(shell $(YQ) -ojson -r '.integration.helm.kong' < ./test/test_dependencies.yaml)

# Integration tests don't use gotestsum because there's a data race issue
# when go toolchain is writing to os.Stderr which is being read in go-kong
# https://github.com/Kong/go-kong/blob/c71247b5c8aae2/kong/client.go#L182
# which in turn produces a data race becuase gotestsum needs go test invoked with
# -json which enables the problematic branch in go toolchain (that writes to os.Stderr).
#
# Related issue: https://github.com/Kong/kubernetes-ingress-controller/issues/3754
.PHONY: _test.integration
_test.integration:
	TEST_KONG_HELM_CHART_VERSION="$(TEST_KONG_HELM_CHART_VERSION)" \
	TEST_DATABASE_MODE="$(DBMODE)" \
	GOFLAGS="-tags=$(GOTAGS)" \
	KONG_CONTROLLER_FEATURE_GATES="$(KONG_CONTROLLER_FEATURE_GATES)" \
	go test $(GOTESTFLAGS) \
	-v \
	-timeout $(INTEGRATION_TEST_TIMEOUT) \
	-parallel $(NCPU) \
	-race \
	-ldflags="$(LDFLAGS_COMMON)" \
	-covermode=atomic \
	-coverpkg=$(PKG_LIST) \
	-coverprofile=$(COVERAGE_OUT) \
	./test/integration

.PHONY: test.integration.dbless
test.integration.dbless:
	@$(MAKE) _test.integration \
		GOTAGS="integration_tests" \
		DBMODE=off \
		COVERAGE_OUT=coverage.dbless.out

.PHONY: test.integration.postgres
test.integration.postgres:
	@$(MAKE) _test.integration \
		GOTAGS="integration_tests" \
		DBMODE=postgres \
		COVERAGE_OUT=coverage.postgres.out

.PHONY: test.integration
test.integration: test.integration.dbless test.integration.postgres
