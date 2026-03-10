BINARY  := tunr
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -s -w -X main.Version=$(VERSION)

.PHONY: build install clean test lint vet security check all

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/tunr

install: build
	mv $(BINARY) /usr/local/bin/

clean:
	rm -f $(BINARY)
	rm -rf dist/

test:
	go test -race -timeout 60s ./...

lint:
	golangci-lint run --timeout=5m

vet:
	go vet ./...

security:
	go install golang.org/x/vuln/cmd/govulncheck@latest
	govulncheck ./...

check: vet lint test security

all: check build
