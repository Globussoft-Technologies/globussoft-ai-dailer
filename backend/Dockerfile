# ─── Stage 1: Build ──────────────────────────────────────────────────────────
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Cache dependencies separately from source (faster rebuilds)
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build a statically-linked binary
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o /audiod ./cmd/audiod

# ─── Stage 2: Runtime ────────────────────────────────────────────────────────
FROM alpine:3.20

# ca-certificates: needed for TLS to Deepgram, ElevenLabs, etc.
# tzdata: needed for time.LoadLocation (recordings timestamps)
RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S audiod && adduser -S audiod -G audiod

COPY --from=builder /audiod /usr/local/bin/audiod

# Recordings are written to this path (mount a volume in production)
RUN mkdir -p /recordings && chown audiod:audiod /recordings

USER audiod

EXPOSE 8001

HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -qO- http://localhost:8001/health || exit 1

ENTRYPOINT ["/usr/local/bin/audiod"]
