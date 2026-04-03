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


# --- Stage 2 ---
FROM alpine:latest AS base
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/ktsu /usr/local/bin/ktsu

# --- Stage 3 ---
FROM base AS demo

# Copy the entire hello example into /app. This correctly places
# /app/gateway.yaml, /app/workflows/, etc.
COPY examples/hello/ .

RUN chmod -R 777 /app

# Ensure ktsu is the entrypoint so arguments are passed to it correctly
CMD ["ktsu", "start", "--all", "--gateway-config", "/app/gateway.yaml"]