# tunr-runner — the Docker-driving sidecar.
# Needs the docker CLI (talks to the host daemon via the mounted socket) and
# nixpacks (buildpacks). Build context = the relay module (./relay).

# ── build the runner binary ──
FROM golang:1.22-alpine AS build
RUN apk add --no-cache git
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-w -s" -o /runner ./cmd/runner

# ── runtime: docker CLI + nixpacks (glibc base for nixpacks) ──
FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends curl ca-certificates \
    && rm -rf /var/lib/apt/lists/*
# static docker CLI (client only; talks to the host daemon via the mounted socket)
RUN curl -fsSL https://download.docker.com/linux/static/stable/x86_64/docker-27.3.1.tgz \
    | tar -xz -C /usr/local/bin --strip-components=1 docker/docker
# buildx plugin (nixpacks builds with BuildKit; the host daemon runs it)
RUN mkdir -p /usr/local/lib/docker/cli-plugins \
    && curl -fsSL https://github.com/docker/buildx/releases/download/v0.19.3/buildx-v0.19.3.linux-amd64 \
       -o /usr/local/lib/docker/cli-plugins/docker-buildx \
    && chmod +x /usr/local/lib/docker/cli-plugins/docker-buildx
# nixpacks (auto-detect Node/Python/Go/... buildpacks)
RUN curl -sSL https://nixpacks.com/install.sh | bash
COPY --from=build /runner /usr/local/bin/tunr-runner
EXPOSE 9091
ENTRYPOINT ["tunr-runner"]
