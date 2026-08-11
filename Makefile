BIN     := bin/kaku-tab
PKG     := ./cmd/kaku-tab
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

# Pinned to match .github/workflows/ci.yml. A local lint that disagrees with CI
# is worse than no local lint.
GOLANGCI := github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2
COVCHECK := github.com/vladopajic/go-test-coverage/v2@v2.19.0
GORELEASER := github.com/goreleaser/goreleaser/v2@latest

.PHONY: all build install test lint fmt cover release-snapshot clean

all: build

build:
	go build -ldflags '$(LDFLAGS)' -o $(BIN) $(PKG)

## install: put kaku-tab on your PATH (GOBIN, else GOPATH/bin)
install:
	go install -ldflags '$(LDFLAGS)' $(PKG)

test:
	go vet ./...
	go test ./...

fmt:
	gofmt -w cmd internal

## lint: the full linter set from .golangci.yml, same as CI. Uses an installed
## golangci-lint when there is one, since `go run` rebuilds it from scratch.
lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		go run $(GOLANGCI) run; \
	fi

## cover: run tests and check the thresholds in .testcoverage.yml
cover:
	go test -covermode=atomic -coverprofile=cover.out ./...
	@go run $(COVCHECK) --config=.testcoverage.yml

## release-snapshot: build the release artifacts locally, publishing nothing
release-snapshot:
	go run $(GORELEASER) release --snapshot --clean --skip=publish

clean:
	rm -rf bin dist cover.out
