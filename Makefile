.DEFAULT_GOAL := help

GO ?= go
PACKAGE := ./...
BIN_DIR := bin
BINARY ?= typeconv
COVER_PROFILE := coverage.out
COVER_HTML := coverage.html
GOLANGCI_LINT_VERSION ?= v1.62.0
GOBIN_PATH := $(shell $(GO) env GOBIN)
GOPATH_BIN := $(shell $(GO) env GOPATH)/bin
GOLANGCI_BIN := $(if $(GOBIN_PATH),$(GOBIN_PATH)/golangci-lint,$(GOPATH_BIN)/golangci-lint)

all: fmt vet test ## Format, vet and run tests

build: ## Compile packages to verify build
	$(GO) build $(PACKAGE)

test: ## Run tests
	$(GO) test $(PACKAGE)

test-verbose: ## Run tests with verbose output
	$(GO) test -v $(PACKAGE)

bench: ## Run benchmarks
	$(GO) test -bench=. -benchmem $(PACKAGE)

cover: ## Generate test coverage profile
	$(GO) test -coverprofile=$(COVER_PROFILE) $(PACKAGE)
	@echo "Coverage profile written to $(COVER_PROFILE)"

cover-html: cover ## Generate HTML coverage report
	$(GO) tool cover -html=$(COVER_PROFILE) -o $(COVER_HTML)
	@echo "Coverage report written to $(COVER_HTML)"

fmt: ## Format source code
	$(GO) fmt $(PACKAGE)

vet: ## Report suspicious constructs
	$(GO) vet $(PACKAGE)

install-golangci-lint: ## Install golangci-lint
	@echo "Installing golangci-lint $(GOLANGCI_LINT_VERSION)..."
	@$(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

lint: ## Run linters (go vet + golangci-lint; ensure desired version)
	$(GO) vet $(PACKAGE)
	@need_install=0; \
	if ! command -v golangci-lint >/dev/null 2>&1 && [ ! -x "$(GOLANGCI_BIN)" ]; then \
	  need_install=1; \
	else \
	  ver=$$(golangci-lint version --format short 2>/dev/null || echo unknown); \
	  if [ "$$ver" != "$(GOLANGCI_LINT_VERSION)" ]; then need_install=1; fi; \
	fi; \
	if [ $$need_install -eq 1 ]; then $(MAKE) install-golangci-lint; fi
	@if command -v golangci-lint >/dev/null 2>&1; then golangci-lint run; else "$(GOLANGCI_BIN)" run; fi

tidy: ## Add/remove module dependencies
	$(GO) mod tidy

deps: ## Download module dependencies
	$(GO) mod download

vendor: ## Vendor dependencies
	$(GO) mod vendor

clean: ## Clean build artifacts and caches
	$(GO) clean -testcache -cache
	@rm -rf $(BIN_DIR) $(COVER_PROFILE) $(COVER_HTML)

ci: tidy fmt vet test cover ## Run checks suitable for CI

help: ## Show this help
	@awk 'BEGIN {FS = ":.*?## "}; /^[a-zA-Z0-9][a-zA-Z0-9\-_]*:.*?## / {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

.PHONY: all build test test-verbose bench cover cover-html fmt vet lint tidy deps vendor clean ci help install-golangci-lint


