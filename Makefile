BINARY   := adb-exporter
PKG      := github.com/david-igou/adb-exporter
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
REVISION ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS  := -X main.version=$(VERSION) -X main.revision=$(REVISION)

GO ?= go

.PHONY: all build test vet lint fmt clean

all: vet test build

build:
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BINARY) .

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

# lint runs vet plus gofmt-diff; if golangci-lint is installed it runs too.
lint: vet
	@diff=$$(gofmt -l .); if [ -n "$$diff" ]; then echo "gofmt needed:"; echo "$$diff"; exit 1; fi
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run || echo "golangci-lint not installed; ran vet + gofmt only"

fmt:
	gofmt -w .

clean:
	rm -f $(BINARY)
