BIN     := bin/kaku-tab
PKG     := ./cmd/kaku-tab
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all build install test lint fmt clean

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

lint: fmt
	go vet ./...
	@test -z "$$(gofmt -l cmd internal)" || (echo "unformatted files"; exit 1)

clean:
	rm -rf bin
