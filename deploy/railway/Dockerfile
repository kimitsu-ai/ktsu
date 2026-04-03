# --- Stage 1: Build binary ---
# This stage mirrors the build logic from the primary deploy/Dockerfile
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build
COPY ./ .
RUN CGO_ENABLED=0 GOOS=linux go build -o ktsu ./cmd/ktsu


# --- Stage 2: Base production-like image ---
FROM alpine:latest AS base

RUN apk add --no-cache ca-certificates
WORKDIR /app

# Copy binary
COPY --from=builder /app/ktsu /usr/local/bin/ktsu

ENTRYPOINT ["ktsu"]


# --- Stage 3: Demo image (Railway Default) ---
# This stage adds the hello-world example for Railway's one-click demo.
FROM base AS demo

# Copy default configuration for the demo
COPY examples/hello/gateway.yaml /app/gateway.yaml
COPY examples/hello/workflows /app/workflows
COPY examples/hello/agents /app/agents
COPY examples/hello/servers /app/servers

# Ensure workspace is writeable for state/logs
RUN chmod -R 777 /app

# Start all Kimitsu services in a single process
CMD ["start", "--all", "--gateway-config", "/app/gateway.yaml", "--workflow-dir", "/app/workflows", "--project-dir", "/app"]
