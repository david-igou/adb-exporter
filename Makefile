BINARY    := adb-exporter
PKG       := github.com/david-igou/adb-exporter
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
REVISION  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS   := -s -w -X main.version=$(VERSION) -X main.revision=$(REVISION)
COVERFILE := coverage.out
DISTDIR   := dist

GO ?= go

.DEFAULT_GOAL := help

.PHONY: help all build install test race cover lint fmt fmt-check vet tidy vulncheck crossbuild clean

help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z0-9_-]+:.*##/ {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

all: vet test build ## Run vet, tests, then build

build: ## Build the binary for the host platform
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BINARY) .

install: ## Install the binary into GOBIN
	$(GO) install -ldflags "$(LDFLAGS)" .

test: ## Run unit tests
	$(GO) test ./...

race: ## Run unit tests with the race detector
	$(GO) test -race ./...

cover: ## Run tests with coverage and print a summary
	$(GO) test -race -coverprofile=$(COVERFILE) -covermode=atomic ./...
	$(GO) tool cover -func=$(COVERFILE) | tail -1

lint: vet fmt-check ## Run vet, gofmt check, and golangci-lint if installed
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run ./... || echo "golangci-lint not installed; ran vet + gofmt only"

fmt: ## Rewrite sources with gofmt
	gofmt -w .

fmt-check: ## Fail if any file is not gofmt-clean
	@diff=$$(gofmt -l .); if [ -n "$$diff" ]; then echo "gofmt needed:"; echo "$$diff"; exit 1; fi

vet: ## Run go vet
	$(GO) vet ./...

tidy: ## Verify go.mod/go.sum are tidy
	$(GO) mod tidy -diff

vulncheck: ## Scan for known vulnerabilities (govulncheck)
	$(GO) run golang.org/x/vuln/cmd/govulncheck@latest ./...

crossbuild: ## Build linux/amd64 and linux/arm64 binaries into dist/
	@mkdir -p $(DISTDIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -ldflags "$(LDFLAGS)" -o $(DISTDIR)/$(BINARY)-linux-amd64 .
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -ldflags "$(LDFLAGS)" -o $(DISTDIR)/$(BINARY)-linux-arm64 .

clean: ## Remove build and coverage artifacts
	rm -rf $(BINARY) $(COVERFILE) $(DISTDIR)
