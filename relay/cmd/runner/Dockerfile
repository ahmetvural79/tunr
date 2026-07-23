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

# ── runtime: docker CLI + nixpacks ──
FROM docker:27-cli
RUN apk add --no-cache curl bash ca-certificates
# nixpacks (auto-detect Node/Python/Go/... buildpacks)
RUN curl -sSL https://nixpacks.com/install.sh | bash
COPY --from=build /runner /usr/local/bin/tunr-runner
EXPOSE 9091
ENTRYPOINT ["tunr-runner"]
