# Contributing to tunr

Thank you for your interest in contributing! tunr is open source under the PolyForm Shield 1.0.0 license and welcomes contributions from the community.

## What's Open Source

This repository contains the **tunr CLI** — the client binary you install and run locally. The relay server infrastructure is proprietary and not included here.

Open to contributions:
- `cmd/tunr/` — CLI commands
- `internal/` — Core packages (tunnel, proxy, inspector, mcp, auth, config, etc.)
- `sdk/` — Go and JS SDKs
- `landing/` — Static landing page (not the dashboard app)
- `docs/` — Documentation

## Getting Started

```bash
# Clone the repo
git clone https://github.com/tunr-dev/tunr.git
cd tunr

# Install dependencies
go mod download

# Build
go build -o tunr ./cmd/tunr

# Run tests
go test ./...

# Lint (requires golangci-lint)
golangci-lint run
```

## Before You Open a PR

1. **Check existing issues** — your feature or bug may already be tracked
2. **Open an issue first** for significant changes — let's discuss before you invest time
3. **Security issues** — see [SECURITY.md](SECURITY.md), do NOT open public issues for vulnerabilities

## Code Standards

- **Go idioms** — follow standard Go conventions
- **Tests** — new features need tests, bug fixes should add a regression test
- **Lint** — CI runs `golangci-lint`; fixes must pass
- **Security** — CI runs `govulncheck`; no known CVEs
- **Comments** — exported functions must have doc comments

## Pull Request Process

1. Fork → branch (`feat/`, `fix/`, `docs/` prefix)
2. Make changes + tests
3. `go test ./...` must pass
4. `go vet ./...` must be clean
5. Open PR against `main`
6. Fill out the PR template
7. CI must pass before merge

## Commit Style

We loosely follow [Conventional Commits](https://www.conventionalcommits.org/):

```
feat: add --ttl flag to share command
fix: handle graceful shutdown on SIGHUP
docs: update MCP configuration example
chore: bump gorilla/websocket to v1.5.3
```

## Questions?

- Open a [GitHub Discussion](https://github.com/tunr-dev/tunr/discussions)
- Join [Discord](https://discord.gg/tunr)

## License

By contributing, you agree that your contributions will be licensed under the [PolyForm Shield License 1.0.0](LICENSE). This means your code is open source — anyone can use, modify, and share it — but it cannot be used to build a competing tunnel service.
