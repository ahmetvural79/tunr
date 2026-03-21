BINARY  := tunr
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -s -w -X main.Version=$(VERSION)

.PHONY: build build-dist install clean test lint vet security check all pre-push

# Root binary (same flags as typical release builds)
build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/tunr

# Release-like binary under dist/ (verify with: ./dist/tunr --version)
build-dist: build-dist-only
	@./dist/$(BINARY) --version

build-dist-only:
	mkdir -p dist
	rm -f dist/$(BINARY)
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/$(BINARY) ./cmd/tunr

pre-push: check build
	@echo "✓ Pre-push checks passed. Safe to push."
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/tunr

install: build
	mv $(BINARY) /usr/local/bin/

clean:
	rm -f $(BINARY)
	rm -rf dist/

test:
	go test -race -timeout 60s ./...

lint:
	go run github.com/golangci/golangci-lint/cmd/golangci-lint@v1.63.0 run --timeout=5m --out-format=colored-line-number ./...

vet:
	go vet ./...

security:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

check: vet lint test security

all: check build
