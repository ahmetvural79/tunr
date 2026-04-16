# Build the tunr CLI as a minimal Docker image.
# Usage:
#   docker build -t tunr .
#   docker run --rm tunr share --port 3000

FROM golang:1.22-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /tunr ./cmd/tunr

# ─── Runtime ───────────────────────────────────────────────────
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /tunr /usr/local/bin/tunr

# Non-root user for security
RUN adduser -D -H tunr
USER tunr

ENTRYPOINT ["tunr"]
CMD ["--help"]
